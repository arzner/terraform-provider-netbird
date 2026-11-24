// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
)

// Every resource's Delete treats an object that is already gone (404) as
// deleted: it can disappear between the refresh and the delete.
func Test_deleteNotFoundIsSuccess(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"not found","code":404}`)
	}))
	defer srv.Close()
	client := netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))
	for _, newResource := range (&NetBirdProvider{}).Resources(ctx) {
		r := newResource()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "netbird"}, &meta)
		t.Run(meta.TypeName, func(t *testing.T) {
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
			if resp.Diagnostics.HasError() {
				t.Errorf("expected a 404 to count as deleted, found %v", resp.Diagnostics)
			}
		})
	}
}
