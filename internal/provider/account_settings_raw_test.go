// Copyright (c) HashiCorp, Inc.

package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// The account as a newer management server returns it: local MFA and the
// other unmodelled settings on, plus settings the compiled API types do not
// know at all.
const stubAccounts = `[{
  "id": "acc1",
  "domain": "example.test",
  "domain_category": "private",
  "created_at": "2026-01-01T00:00:00Z",
  "created_by": "owner",
  "settings": {
    "peer_login_expiration_enabled": true,
    "peer_login_expiration": 86400,
    "peer_inactivity_expiration_enabled": false,
    "peer_inactivity_expiration": 600,
    "regular_users_view_blocked": true,
    "peer_expose_enabled": false,
    "peer_expose_groups": [],
    "local_mfa_enabled": true,
    "metrics_push_enabled": true,
    "auto_update_always": true,
    "ipv6_enabled_groups": ["g-all"],
    "peer_hostname_collision_rejected": true,
    "future_setting": {"nested": [1, 2]},
    "embedded_idp_enabled": true,
    "local_auth_disabled": false,
    "extra": {
      "peer_approval_enabled": false,
      "user_approval_required": true,
      "network_traffic_logs_enabled": false,
      "network_traffic_logs_groups": [],
      "network_traffic_packet_counter_enabled": false,
      "future_extra": "kept"
    }
  }
}]`

func Test_updateAccountSettings_keepsSettingsUnknownToTheClient(t *testing.T) {
	var put map[string]map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/accounts":
			_, _ = io.WriteString(w, stubAccounts)
		case r.Method == http.MethodPut && r.URL.Path == "/api/accounts/acc1":
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &put); err != nil {
				t.Errorf("request body is not a JSON object: %v", err)
			}
			var accounts []json.RawMessage
			_ = json.Unmarshal([]byte(stubAccounts), &accounts)
			_, _ = w.Write(accounts[0])
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	data := AccountSettingsModel{
		Id:                         types.StringValue("acc1"),
		PeerLoginExpiration:        types.Int32Value(3600),
		PeerApprovalEnabled:        types.BoolValue(true),
		NetworkTrafficLogsGroups:   types.ListNull(types.StringType),
		PeerExposeGroups:           types.ListNull(types.StringType),
		JwtAllowGroups:             types.ListNull(types.StringType),
		PeerLoginExpirationEnabled: types.BoolNull(),
	}
	if _, err := updateAccountSettings(context.Background(), netbird.New(srv.URL, "token"), "acc1", data); err != nil {
		t.Fatalf("update: %v", err)
	}
	settings := put["settings"]
	if settings == nil {
		t.Fatalf("no settings in the request: %v", put)
	}

	want := map[string]string{
		// Managed and changed by the plan.
		"peer_login_expiration": `3600`,
		// Not in the plan: the account's values, whether the client knows them or not.
		"peer_login_expiration_enabled":    `true`,
		"local_mfa_enabled":                `true`,
		"metrics_push_enabled":             `true`,
		"auto_update_always":               `true`,
		"ipv6_enabled_groups":              `["g-all"]`,
		"peer_hostname_collision_rejected": `true`,
		"future_setting":                   `{"nested":[1,2]}`,
	}
	for k, v := range want {
		if got := compact(t, settings[k]); got != v {
			t.Errorf("%s: expected %s, found %s", k, v, got)
		}
	}
	for _, k := range readOnlySettings {
		if _, ok := settings[k]; ok {
			t.Errorf("%s is read-only and must not be sent", k)
		}
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(settings["extra"], &extra); err != nil {
		t.Fatalf("extra: %v", err)
	}
	for k, v := range map[string]string{
		"peer_approval_enabled":  `true`,
		"user_approval_required": `true`,
		"future_extra":           `"kept"`,
	} {
		if got := compact(t, extra[k]); got != v {
			t.Errorf("extra.%s: expected %s, found %s", k, v, got)
		}
	}
}

func Test_updateAccountSettings_refusesAMissingAccount(t *testing.T) {
	for name, tc := range map[string]struct{ list, id string }{
		"empty list on create":    {`[]`, ""},
		"other account on update": {stubAccounts, "acc2"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("no update may be sent, got %s %s", r.Method, r.URL.Path)
				}
				_, _ = io.WriteString(w, tc.list)
			}))
			defer srv.Close()
			if _, err := updateAccountSettings(context.Background(), netbird.New(srv.URL, "token"), tc.id, AccountSettingsModel{}); err == nil {
				t.Fatalf("expected an error")
			}
		})
	}
}

func Test_mergeSettings_withoutCurrentSettings(t *testing.T) {
	out, err := mergeSettings(nil, api.AccountRequest{Settings: api.AccountSettings{PeerLoginExpiration: 60}})
	if err != nil {
		t.Fatal(err)
	}
	if got := compact(t, out["peer_login_expiration"]); got != `60` {
		t.Fatalf("peer_login_expiration: expected 60, found %s", got)
	}
}

func compact(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	if raw == nil {
		return "<absent>"
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("invalid JSON %s: %v", raw, err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
