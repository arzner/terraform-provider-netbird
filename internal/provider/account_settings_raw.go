// Copyright (c) HashiCorp, Inc.

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// The management server builds the stored settings from the update request
// alone, so a setting missing from the request is reset. accountTerraformToAPI
// starts from the current settings, but only from the fields the compiled API
// types know: a server newer than the client library has settings this
// provider cannot even decode, and those would still be reset. The update
// therefore goes through raw JSON: the account's settings as the server sent
// them, with the typed request laid over them key by key.

// readOnlySettings are computed by the server and never read from a request.
var readOnlySettings = []string{"embedded_idp_enabled", "local_auth_disabled"}

// maxAccountsResponse bounds what is read from the accounts endpoint.
const maxAccountsResponse = 16 << 20

type rawAccount struct {
	account  api.Account
	settings map[string]json.RawMessage
}

// listAccountsRaw lists the accounts, keeping each one's settings object as
// the server sent it next to the decoded account.
func listAccountsRaw(ctx context.Context, client *netbird.Client) ([]rawAccount, error) {
	resp, err := client.NewRequest(ctx, http.MethodGet, "/api/accounts", nil, nil)
	if err != nil {
		return nil, err
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("decoding the accounts list: %w", err)
	}
	ret := make([]rawAccount, 0, len(items))
	for _, item := range items {
		var a rawAccount
		var raw struct {
			Settings map[string]json.RawMessage `json:"settings"`
		}
		if err := json.Unmarshal(item, &a.account); err != nil {
			return nil, fmt.Errorf("decoding an account: %w", err)
		}
		if err := json.Unmarshal(item, &raw); err != nil {
			return nil, fmt.Errorf("decoding an account's settings: %w", err)
		}
		a.settings = raw.Settings
		ret = append(ret, a)
	}
	return ret, nil
}

// selectAccount returns the account with the given ID, or the only account
// the token sees when id is empty (the resource is being created).
func selectAccount(accounts []rawAccount, id string) (*rawAccount, error) {
	if id == "" {
		if len(accounts) == 0 {
			return nil, fmt.Errorf("the management server lists no account for this token")
		}
		return &accounts[0], nil
	}
	for i := range accounts {
		if accounts[i].account.Id == id {
			return &accounts[i], nil
		}
	}
	return nil, fmt.Errorf("account %s is not among the %d accounts the management server lists", id, len(accounts))
}

// mergeSettings lays the typed request over the current raw settings. Keys the
// typed request carries replace the current ones; every other key, including
// ones the compiled API types do not know, is sent back unchanged. Where both
// sides hold an object (extra, dashboard_features) the merge recurses, so the
// object's unknown members are kept as well.
func mergeSettings(current map[string]json.RawMessage, request api.AccountRequest) (map[string]json.RawMessage, error) {
	typed, err := json.Marshal(request.Settings)
	if err != nil {
		return nil, err
	}
	var overlay map[string]json.RawMessage
	if err := json.Unmarshal(typed, &overlay); err != nil {
		return nil, err
	}
	out := maps.Clone(current)
	if out == nil {
		out = map[string]json.RawMessage{}
	}
	for _, k := range readOnlySettings {
		delete(out, k)
	}
	if err := mergeInto(out, overlay); err != nil {
		return nil, err
	}
	return out, nil
}

// mergeInto copies overlay into base key by key, recursing where both values
// are JSON objects.
func mergeInto(base, overlay map[string]json.RawMessage) error {
	for k, v := range overlay {
		cur, ok := asObject(base[k])
		top, isObj := asObject(v)
		if ok && isObj {
			if err := mergeInto(cur, top); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			merged, err := json.Marshal(cur)
			if err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			v = merged
		}
		base[k] = v
	}
	return nil
}

// asObject decodes raw as a JSON object; ok is false for anything else.
func asObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

// stateAccount returns the account a resource in state refers to: the one with
// its ID, or the first one listed when the state has no ID yet. Nil means the
// account is gone.
func stateAccount(accounts []api.Account, id string) *api.Account {
	if id == "" && len(accounts) > 0 {
		return &accounts[0]
	}
	for i := range accounts {
		if accounts[i].Id == id {
			return &accounts[i]
		}
	}
	return nil
}

// updateAccountSettings applies the plan to the account's settings and returns
// the account as the server stored it.
func updateAccountSettings(ctx context.Context, client *netbird.Client, id string, data AccountSettingsModel) (*api.Account, error) {
	accounts, err := listAccountsRaw(ctx, client)
	if err != nil {
		return nil, err
	}
	current, err := selectAccount(accounts, id)
	if err != nil {
		return nil, err
	}
	settings, err := mergeSettings(current.settings, accountTerraformToAPI(ctx, &current.account, data))
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"settings": settings})
	if err != nil {
		return nil, err
	}
	resp, err := client.NewRequest(ctx, http.MethodPut, "/api/accounts/"+url.PathEscape(current.account.Id), bytes.NewReader(body), nil)
	if err != nil {
		return nil, err
	}
	respBody, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	var updated api.Account
	if err := json.Unmarshal(respBody, &updated); err != nil {
		return nil, fmt.Errorf("decoding the updated account: %w", err)
	}
	return &updated, nil
}

func readBody(resp *http.Response) ([]byte, error) {
	if resp.Body == nil {
		return nil, fmt.Errorf("empty response, HTTP status %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAccountsResponse+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxAccountsResponse {
		return nil, fmt.Errorf("response larger than %d bytes", maxAccountsResponse)
	}
	return body, nil
}
