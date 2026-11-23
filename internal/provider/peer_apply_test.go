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

func Test_applyPeer_sendsIPv6Separately(t *testing.T) {
	for name, tc := range map[string]struct {
		failFirst    bool
		failSecond   bool
		addressOnly  bool
		failGet      bool
		wantPuts     int
		wantErr      bool
		wantObserved bool
	}{
		"both succeed":               {wantPuts: 2},
		"first update refused":       {failFirst: true, wantPuts: 1, wantErr: true, wantObserved: true},
		"address refused":            {failSecond: true, wantPuts: 2, wantErr: true, wantObserved: true},
		"address-only failure":       {addressOnly: true, failFirst: true, wantPuts: 1, wantErr: true, wantObserved: true},
		"address and read-back fail": {failSecond: true, failGet: true, wantPuts: 2, wantErr: true, wantObserved: true},
	} {
		t.Run(name, func(t *testing.T) {
			var puts []map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && tc.failGet {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = io.WriteString(w, `{"message":"unavailable","code":500}`)
					return
				}
				if r.Method == http.MethodGet {
					// The read-back after a failed address request: distinct from
					// every PUT response, so the test sees whether it was used.
					_, _ = io.WriteString(w, `{"id":"p","name":"read-back","ipv6":"fd00::9"}`)
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
				ipv6 := "fd00::7"
				if v, ok := body["ipv6"].(string); ok {
					ipv6 = v
				}
				out, _ := json.Marshal(map[string]any{"id": "p", "name": body["name"], "ipv6": ipv6, "ssh_enabled": body["ssh_enabled"]})
				_, _ = w.Write(out)
			}))
			defer srv.Close()
			r := &Peer{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
			live := &api.Peer{Id: "p", Name: "old", Ipv6: ptr("fd00::7")}
			plan := PeerModel{Name: types.StringValue("new"), Ipv6: types.StringValue("fd00::9")}
			config := PeerModel{Name: types.StringValue("new"), Ipv6: types.StringValue("fd00::9")}
			if tc.addressOnly {
				live.Name = "new"
			}
			peer, observed, err := r.applyPeer(context.Background(), live, plan, config)
			if (err != nil) != tc.wantErr || observed != tc.wantObserved {
				t.Fatalf("expected error %v observed %v, found %v %v", tc.wantErr, tc.wantObserved, err, observed)
			}
			switch {
			case tc.failGet:
				// The first response is the record; the address is unconfirmed.
				if peer == nil || peer.Name != "new" || !strings.Contains(err.Error(), "unconfirmed") {
					t.Errorf("expected the first response and an unconfirmed address, found %+v, %v", peer, err)
				}
			case tc.wantObserved:
				if peer == nil || peer.Name != "read-back" || peer.Ipv6 == nil || *peer.Ipv6 != "fd00::9" {
					t.Errorf("a failed request must return the peer as read back, found %+v", peer)
				}
			}
			if len(puts) != tc.wantPuts {
				t.Fatalf("expected %d requests, found %d: %v", tc.wantPuts, len(puts), puts)
			}
			if _, sent := puts[0]["ipv6"]; sent && !tc.addressOnly {
				t.Errorf("the first request must not carry the address, found %v", puts[0]["ipv6"])
			}
			if tc.wantPuts == 2 && (puts[1]["ipv6"] != "fd00::9" || puts[1]["name"] != "new") {
				t.Errorf("the second request must carry the address and the applied name, found %v", puts[1])
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
