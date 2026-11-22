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
// ones the compiled API types do not know, is sent back unchanged. The nested
// extra object is merged the same way.
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
	for k, v := range overlay {
		if k == "extra" {
			merged, err := mergeObject(out[k], v)
			if err != nil {
				return nil, fmt.Errorf("merging extra settings: %w", err)
			}
			v = merged
		}
		out[k] = v
	}
	return out, nil
}

func mergeObject(current, overlay json.RawMessage) (json.RawMessage, error) {
	base := map[string]json.RawMessage{}
	if len(current) > 0 && string(current) != "null" {
		if err := json.Unmarshal(current, &base); err != nil {
			return nil, err
		}
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(overlay, &top); err != nil {
		return nil, err
	}
	maps.Copy(base, top)
	return json.Marshal(base)
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
