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

func Test_applyPeer_sendsIPv6FirstAndAlone(t *testing.T) {
	for name, tc := range map[string]struct {
		failFirst    bool
		failSecond   bool
		addressOnly  bool
		failGet      bool
		wantPuts     int
		wantErr      bool
		wantObserved bool
		wantPeer     string // name of the returned record: "read-back", "first" or ""
	}{
		"both succeed":                {wantPuts: 2},
		"address only":                {addressOnly: true, wantPuts: 1},
		"address refused":             {failFirst: true, wantPuts: 1, wantErr: true, wantObserved: true, wantPeer: "read-back"},
		"settings refused":            {failSecond: true, wantPuts: 2, wantErr: true, wantObserved: true, wantPeer: "read-back"},
		"address and read-back fail":  {failFirst: true, failGet: true, wantPuts: 1, wantErr: true},
		"settings and read-back fail": {failSecond: true, failGet: true, wantPuts: 2, wantErr: true, wantObserved: true, wantPeer: "first"},
	} {
		t.Run(name, func(t *testing.T) {
			var puts []map[string]any
			// The server's peer: a PUT applies the request, a GET returns it
			// marked, so the test sees whether the read-back was used.
			state := map[string]any{"id": "p", "name": "old", "ipv6": "fd00::7"}
			if tc.addressOnly {
				state["name"] = "new"
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					if tc.failGet {
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = io.WriteString(w, `{"message":"unavailable","code":500}`)
						return
					}
					out, _ := json.Marshal(map[string]any{"id": "p", "name": state["name"], "ipv6": state["ipv6"], "dns_label": "read-back"})
					_, _ = w.Write(out)
					return
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				puts = append(puts, body)
				if (tc.failFirst && len(puts) == 1) || (tc.failSecond && len(puts) == 2) {
					w.WriteHeader(http.StatusUnprocessableEntity)
					_, _ = io.WriteString(w, `{"message":"refused","code":422}`)
					return
				}
				state["name"] = body["name"]
				if v, ok := body["ipv6"]; ok {
					state["ipv6"] = v
				}
				out, _ := json.Marshal(map[string]any{"id": "p", "name": state["name"], "ipv6": state["ipv6"], "dns_label": "first"})
				_, _ = w.Write(out)
			}))
			defer srv.Close()
			r := &Peer{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
			live := &api.Peer{Id: "p", Name: state["name"].(string), Ipv6: ptr("fd00::7")}
			plan := PeerModel{Name: types.StringValue("new"), Ipv6: types.StringValue("fd00::9")}
			config := PeerModel{Name: types.StringValue("new"), Ipv6: types.StringValue("fd00::9")}
			peer, observed, err := r.applyPeer(context.Background(), live, plan, config)
			if (err != nil) != tc.wantErr || observed != tc.wantObserved {
				t.Fatalf("expected error %v observed %v, found %v %v", tc.wantErr, tc.wantObserved, err, observed)
			}
			if tc.failGet && !strings.Contains(err.Error(), "unconfirmed") {
				t.Errorf("a failed read-back must say what is unconfirmed, found %v", err)
			}
			switch tc.wantPeer {
			case "":
				if tc.wantErr && peer != nil {
					t.Errorf("nothing confirmed: expected no record, found %+v", peer)
				}
			default:
				if peer == nil || peer.DnsLabel != tc.wantPeer {
					t.Errorf("expected the %s response as the record, found %+v", tc.wantPeer, peer)
				}
			}
			if len(puts) != tc.wantPuts {
				t.Fatalf("expected %d requests, found %d: %v", tc.wantPuts, len(puts), puts)
			}
			if puts[0]["ipv6"] != "fd00::9" || puts[0]["name"] != live.Name {
				t.Errorf("the first request must carry only the address, found %v", puts[0])
			}
			if tc.wantPuts == 2 {
				if _, sent := puts[1]["ipv6"]; sent || puts[1]["name"] != "new" {
					t.Errorf("the second request must carry the settings without the address, found %v", puts[1])
				}
			}
			if !tc.wantErr && (peer.Name != "new" || peer.Ipv6 == nil || *peer.Ipv6 != "fd00::9") {
				t.Errorf("expected the final peer renamed and moved, found %+v", peer)
			}
		})
	}
}

func Test_peerChangedSincePlan(t *testing.T) {
	prior := PeerModel{Name: types.StringValue("a"), Ipv6: types.StringValue("fd00::1"), SshEnabled: types.BoolValue(false),
		LoginExpirationEnabled: types.BoolValue(true), InactivityExpirationEnabled: types.BoolValue(false), ApprovalRequired: types.BoolValue(false)}
	for name, tc := range map[string]struct {
		plan func(*PeerModel)
		live func(*api.Peer)
		want []string
	}{
		"nothing changed":       {},
		"planned change":        {plan: func(p *PeerModel) { p.Name = types.StringValue("b") }},
		"done already":          {plan: func(p *PeerModel) { p.Name = types.StringValue("b") }, live: func(l *api.Peer) { l.Name = "b" }},
		"overwritten name":      {plan: func(p *PeerModel) { p.Name = types.StringValue("b") }, live: func(l *api.Peer) { l.Name = "c" }, want: []string{"name"}},
		"unplanned rename":      {live: func(l *api.Peer) { l.Name = "c" }, want: []string{"name"}},
		"moved address":         {live: func(l *api.Peer) { l.Ipv6 = ptr("fd00::2") }, want: []string{"ipv6"}},
		"removed address":       {live: func(l *api.Peer) { l.Ipv6 = nil }, want: []string{"ipv6"}},
		"ssh switched on":       {live: func(l *api.Peer) { l.SshEnabled = true }, want: []string{"ssh_enabled"}},
		"unknown is no promise": {plan: func(p *PeerModel) { p.Name = types.StringUnknown() }, live: func(l *api.Peer) { l.Name = "c" }},
		"several": {live: func(l *api.Peer) { l.LoginExpirationEnabled = false; l.ApprovalRequired = true },
			want: []string{"login_expiration_enabled", "approval_required"}},
	} {
		t.Run(name, func(t *testing.T) {
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
