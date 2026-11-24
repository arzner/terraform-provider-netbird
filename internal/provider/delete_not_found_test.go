// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
)

// A Delete answered with 404 counts as deleted only when the object's own
// lookup answers 404 too and the token still works: NetBird answers 404 for
// a revoked token and for another object the delete touches as well.
func Test_deleteNotFound(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		lookupFound, tokenWorks bool
		wantError               bool
	}{
		"gone":          {false, true, false},
		"token revoked": {false, false, true},
		"still there":   {true, true, true},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			notFound := func() {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"message":"not found","code":404}`)
			}
			switch {
			case r.Method == http.MethodDelete:
				notFound()
			case r.URL.Path == "/api/accounts":
				if !tc.tokenWorks {
					notFound()
					return
				}
				_, _ = io.WriteString(w, `[]`)
			case r.URL.Path == "/api/users":
				if tc.lookupFound {
					_, _ = io.WriteString(w, `[{"id":"x1"}]`)
					return
				}
				_, _ = io.WriteString(w, `[]`)
			case tc.lookupFound:
				_, _ = io.WriteString(w, `{"id":"x1"}`)
			default:
				notFound()
			}
		}))
		client := netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))
		for _, newResource := range (&NetBirdProvider{}).Resources(ctx) {
			r := newResource()
			var meta resource.MetadataResponse
			r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "netbird"}, &meta)
			switch meta.TypeName {
			case "netbird_account_settings", "netbird_dns_settings":
				continue // their delete does nothing
			}
			if strings.HasPrefix(meta.TypeName, "netbird_agent_network") || meta.TypeName == "netbird_reverse_proxy_domain" {
				continue // unchanged here
			}
			t.Run(name+"/"+meta.TypeName, func(t *testing.T) {
				if c, ok := r.(resource.ResourceWithConfigure); ok {
					var cr resource.ConfigureResponse
					c.Configure(ctx, resource.ConfigureRequest{ProviderData: client}, &cr)
					if cr.Diagnostics.HasError() {
						t.Fatal(cr.Diagnostics)
					}
				}
				var sch resource.SchemaResponse
				r.Schema(ctx, resource.SchemaRequest{}, &sch)
				typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
				if !ok {
					t.Fatal("schema type is not an object")
				}
				values := map[string]tftypes.Value{}
				for n, at := range typ.AttributeTypes {
					values[n] = tftypes.NewValue(at, nil)
					if at.Is(tftypes.String) {
						values[n] = tftypes.NewValue(tftypes.String, "x1")
					}
				}
				state := tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(typ, values)}
				resp := resource.DeleteResponse{State: state}
				r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
				if resp.Diagnostics.HasError() != tc.wantError {
					t.Errorf("expected error %v, found %v", tc.wantError, resp.Diagnostics)
				}
			})
		}
		srv.Close()
	}
}
