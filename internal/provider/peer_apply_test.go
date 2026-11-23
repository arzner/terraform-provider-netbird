// Copyright (c) HashiCorp, Inc.

package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_peerRequest_keepsLiveValuesOfUnconfiguredAttributes(t *testing.T) {
	live := &api.Peer{Id: "p", Name: "live-name", SshEnabled: false, LoginExpirationEnabled: true}
	// The plan carries the prior state (ssh on); the configuration sets only the name.
	plan := PeerModel{Name: types.StringValue("new-name"), SshEnabled: types.BoolValue(true), LoginExpirationEnabled: types.BoolValue(false)}
	config := PeerModel{Name: types.StringValue("new-name"), SshEnabled: types.BoolNull(), LoginExpirationEnabled: types.BoolNull()}
	req, changed := peerRequest(live, plan, config)
	if !changed || req.Name != "new-name" {
		t.Errorf("configured name: expected new-name and a change, found %q %v", req.Name, changed)
	}
	if req.SshEnabled || !req.LoginExpirationEnabled {
		t.Errorf("left-out attributes must keep the live values, found ssh %v login expiration %v", req.SshEnabled, req.LoginExpirationEnabled)
	}
	if _, changed := peerRequest(live, plan, PeerModel{}); changed {
		t.Error("nothing configured: expected no change")
	}
}

func Test_applyPeer_oneRequest(t *testing.T) {
	for name, tc := range map[string]struct {
		failPut      bool
		failGet      bool
		setupKeyPeer bool
		loginExpiry  bool
		noChange     bool
		wantPuts     int
		wantErr      string
		wantObserved bool
	}{
		"rename and move":              {wantPuts: 1},
		"nothing to change":            {noChange: true},
		"refused, read back":           {failPut: true, wantPuts: 1, wantErr: "refused", wantObserved: true},
		"refused, read-back fails":     {failPut: true, failGet: true, wantPuts: 1, wantErr: "unavailable"},
		"expiry on a setup-key peer":   {setupKeyPeer: true, loginExpiry: true, wantErr: "added with SSO login"},
		"expiry on an SSO peer":        {loginExpiry: true, wantPuts: 1},
		"setup-key peer, other change": {setupKeyPeer: true, wantPuts: 1},
	} {
		t.Run(name, func(t *testing.T) {
			var puts []map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					if tc.failGet {
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = io.WriteString(w, `{"message":"unavailable","code":500}`)
						return
					}
					_, _ = io.WriteString(w, `{"id":"p","name":"old","ipv6":"fd00::7","dns_label":"read-back"}`)
					return
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				puts = append(puts, body)
				if tc.failPut {
					w.WriteHeader(http.StatusUnprocessableEntity)
					_, _ = io.WriteString(w, `{"message":"refused","code":422}`)
					return
				}
				out, _ := json.Marshal(map[string]any{"id": "p", "name": body["name"], "ipv6": body["ipv6"], "login_expiration_enabled": body["login_expiration_enabled"]})
				_, _ = w.Write(out)
			}))
			defer srv.Close()
			r := &Peer{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
			live := &api.Peer{Id: "p", Name: "old", Ipv6: ptr("fd00::7"), UserId: "u"}
			if tc.setupKeyPeer {
				live.UserId = ""
			}
			plan := PeerModel{Name: types.StringValue("new"), Ipv6: types.StringValue("fd00::9"), LoginExpirationEnabled: types.BoolValue(tc.loginExpiry)}
			config := plan
			if !tc.loginExpiry {
				config.LoginExpirationEnabled = types.BoolNull()
			}
			if tc.noChange {
				plan.Name, plan.Ipv6 = types.StringValue("old"), types.StringValue("fd00::7")
				config = plan
			}
			peer, observed, err := r.applyPeer(context.Background(), live, plan, config)
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("expected error containing %q, found %v", tc.wantErr, err)
			}
			if observed != tc.wantObserved {
				t.Errorf("expected observed %v, found %v", tc.wantObserved, observed)
			}
			if tc.failPut && tc.failGet && !strings.Contains(err.Error(), "refused") {
				t.Errorf("both errors must be reported, found %v", err)
			}
			if tc.wantObserved && (peer == nil || peer.DnsLabel != "read-back") {
				t.Errorf("a failed request must return the peer as read back, found %+v", peer)
			}
			if len(puts) != tc.wantPuts {
				t.Fatalf("expected %d requests, found %d: %v", tc.wantPuts, len(puts), puts)
			}
			if tc.wantPuts == 1 && !tc.noChange && (puts[0]["ipv6"] != "fd00::9" || puts[0]["name"] != "new") {
				t.Errorf("the request must carry the address and the name together, found %v", puts[0])
			}
		})
	}
}

func Test_peerChangedSincePlan(t *testing.T) {
	prior := PeerModel{Name: types.StringValue("a"), Ipv6: types.StringValue("fd00::1"), SshEnabled: types.BoolValue(false),
		LoginExpirationEnabled: types.BoolValue(true), InactivityExpirationEnabled: types.BoolValue(false), ApprovalRequired: types.BoolValue(false)}
	for name, tc := range map[string]struct {
		prior func(*PeerModel)
		plan  func(*PeerModel)
		live  func(*api.Peer)
		want  []string
	}{
		"nothing changed":  {},
		"planned change":   {plan: func(p *PeerModel) { p.Name = types.StringValue("b") }},
		"done already":     {plan: func(p *PeerModel) { p.Name = types.StringValue("b") }, live: func(l *api.Peer) { l.Name = "b" }},
		"overwritten name": {plan: func(p *PeerModel) { p.Name = types.StringValue("b") }, live: func(l *api.Peer) { l.Name = "c" }, want: []string{"name"}},
		"unplanned rename": {live: func(l *api.Peer) { l.Name = "c" }, want: []string{"name"}},
		"moved address":    {live: func(l *api.Peer) { l.Ipv6 = ptr("fd00::2") }, want: []string{"ipv6"}},
		"removed address":  {live: func(l *api.Peer) { l.Ipv6 = nil }, want: []string{"ipv6"}},
		"ssh switched on":  {live: func(l *api.Peer) { l.SshEnabled = true }, want: []string{"ssh_enabled"}},
		"address assigned, one configured": {
			prior: func(p *PeerModel) { p.Ipv6 = types.StringNull() },
			plan:  func(p *PeerModel) { p.Ipv6 = types.StringValue("fd00::5") }},
		"address assigned, none configured": {
			prior: func(p *PeerModel) { p.Ipv6 = types.StringNull() },
			plan:  func(p *PeerModel) { p.Ipv6 = types.StringNull() }, want: []string{"ipv6"}},
		"unknown is no promise": {plan: func(p *PeerModel) { p.Name = types.StringUnknown() }, live: func(l *api.Peer) { l.Name = "c" }},
		"several": {live: func(l *api.Peer) { l.LoginExpirationEnabled = false; l.ApprovalRequired = true },
			want: []string{"login_expiration_enabled", "approval_required"}},
	} {
		t.Run(name, func(t *testing.T) {
			prior := prior
			if tc.prior != nil {
				tc.prior(&prior)
			}
			plan := prior
			if tc.plan != nil {
				tc.plan(&plan)
			}
			live := &api.Peer{Name: "a", Ipv6: ptr("fd00::1"), LoginExpirationEnabled: true}
			if tc.live != nil {
				tc.live(live)
			}
			got := peerChangedSincePlan(prior, plan, live)
			if len(got) != len(tc.want) {
				t.Fatalf("expected %v, found %v", tc.want, got)
			}
			for i, w := range tc.want {
				if !strings.HasPrefix(got[i], "- "+w+":") {
					t.Errorf("expected %s at %d, found %q", w, i, got[i])
				}
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
