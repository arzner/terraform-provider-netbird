package provider

import (
	"context"
	"encoding/json"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_scimAPIToTerraform(t *testing.T) {
	syncTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	cases := []struct {
		name     string
		resource *api.ScimIntegration
		expected ScimModel
	}{
		{
			name: "basic integration",
			resource: &api.ScimIntegration{
				Id:                1,
				Provider:          "okta",
				Enabled:           true,
				AuthToken:         "masked-token",
				GroupPrefixes:     []string{"engineering-"},
				UserGroupPrefixes: []string{"users-"},
				LastSyncedAt:      syncTime,
			},
			expected: ScimModel{
				Id:                types.StringValue("1"),
				ProviderName:      types.StringValue("okta"),
				Enabled:           types.BoolValue(true),
				GroupPrefixes:     types.ListValueMust(types.StringType, []attr.Value{types.StringValue("engineering-")}),
				UserGroupPrefixes: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("users-")}),
				LastSyncedAt:      types.StringValue("2025-01-15T10:30:00Z"),
			},
		},
		{
			name: "empty prefixes",
			resource: &api.ScimIntegration{
				Id:                2,
				Provider:          "azure",
				Enabled:           false,
				AuthToken:         "masked",
				GroupPrefixes:     []string{},
				UserGroupPrefixes: []string{},
				LastSyncedAt:      syncTime,
			},
			expected: ScimModel{
				Id:                types.StringValue("2"),
				ProviderName:      types.StringValue("azure"),
				Enabled:           types.BoolValue(false),
				GroupPrefixes:     types.ListValueMust(types.StringType, []attr.Value{}),
				UserGroupPrefixes: types.ListValueMust(types.StringType, []attr.Value{}),
				LastSyncedAt:      types.StringValue("2025-01-15T10:30:00Z"),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out ScimModel
			// Preserve write-only fields
			out.AuthToken = types.StringValue("preserved-token")
			out.Prefix = types.StringValue("preserved-prefix")

			outDiag := scimAPIToTerraform(context.Background(), c.resource, &out)
			if outDiag.HasError() {
				t.Fatalf("Expected no error diagnostics, found %d errors", outDiag.ErrorsCount())
			}

			// auth_token and prefix should be preserved from existing state
			c.expected.AuthToken = types.StringValue("preserved-token")
			c.expected.Prefix = types.StringValue("preserved-prefix")

			if !reflect.DeepEqual(out, c.expected) {
				t.Fatalf("Expected:\n%#v\nFound:\n%#v", c.expected, out)
			}
		})
	}
}

func Test_scim_connectorID(t *testing.T) {
	ctx := context.Background()
	id := "dex-okta"
	var data ScimModel
	if d := scimAPIToTerraform(ctx, &api.ScimIntegration{Id: 3, ConnectorId: &id}, &data); d.HasError() {
		t.Fatal(d)
	}
	if data.ConnectorId.ValueString() != id {
		t.Errorf("read: expected %s, found %v", id, data.ConnectorId)
	}
	var none ScimModel
	if d := scimAPIToTerraform(ctx, &api.ScimIntegration{Id: 4}, &none); d.HasError() {
		t.Fatal(d)
	}
	if !none.ConnectorId.IsNull() {
		t.Errorf("read: expected null without a connector, found %v", none.ConnectorId)
	}
}

// Test_scim_connectorIDRequests drives Create and Update against a stub server
// and checks the request bodies: a configured connector is sent, a left-out one
// is not, even when the plan carries a value from state.
func Test_scim_connectorIDRequests(t *testing.T) {
	ctx := context.Background()
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":7,"provider":"okta","prefix":"p","enabled":true,"group_prefixes":[],"user_group_prefixes":[],"auth_token":"t","last_synced_at":"2026-01-01T00:00:00Z","connector_id":"dex-okta"}`)
	}))
	defer srv.Close()
	r := &Scim{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	value := func(connector tftypes.Value, id any) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		values["provider_name"] = tftypes.NewValue(tftypes.String, "okta")
		values["prefix"] = tftypes.NewValue(tftypes.String, "p")
		values["connector_id"] = connector
		if id != nil {
			values["id"] = tftypes.NewValue(tftypes.String, id)
		}
		return tftypes.NewValue(typ, values)
	}
	set := tftypes.NewValue(tftypes.String, "dex-okta")
	unset := tftypes.NewValue(tftypes.String, nil)

	create := resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch.Schema, Raw: value(set, nil)}, Config: tfsdk.Config{Schema: sch.Schema, Raw: value(set, nil)}}, &create)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}

	// Update with the connector left out of the configuration while the plan
	// carries "dex-okta" from state.
	state := tfsdk.State{Schema: sch.Schema, Raw: value(set, "7")}
	update := resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: sch.Schema, Raw: value(set, "7")}, Config: tfsdk.Config{Schema: sch.Schema, Raw: value(unset, "7")}, State: state}, &update)
	if update.Diagnostics.HasError() {
		t.Fatal(update.Diagnostics)
	}

	if len(bodies) != 2 {
		t.Fatalf("expected two requests, found %d", len(bodies))
	}
	if bodies[0]["connector_id"] != "dex-okta" {
		t.Errorf("create: expected connector_id dex-okta, found %v", bodies[0]["connector_id"])
	}
	if _, sent := bodies[1]["connector_id"]; sent {
		t.Errorf("update: a left-out connector must not be sent, found %v", bodies[1]["connector_id"])
	}
}
