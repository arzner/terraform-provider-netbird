package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
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

			// auth_token is preserved; the prefix is read from the response.
			c.expected.AuthToken = types.StringValue("preserved-token")
			c.expected.Prefix = types.StringValue(c.resource.Prefix)

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

	// Update with a configured connector: the planned value is sent, not the
	// raw configuration (they differ here to tell them apart).
	planned := tftypes.NewValue(tftypes.String, "dex-planned")
	configured := tftypes.NewValue(tftypes.String, "dex-config")
	update2 := resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: sch.Schema, Raw: value(planned, "7")}, Config: tfsdk.Config{Schema: sch.Schema, Raw: value(configured, "7")}, State: state}, &update2)
	if update2.Diagnostics.HasError() {
		t.Fatal(update2.Diagnostics)
	}

	if len(bodies) != 3 {
		t.Fatalf("expected three requests, found %d", len(bodies))
	}
	if bodies[2]["connector_id"] != "dex-planned" {
		t.Errorf("update: expected the planned connector dex-planned, found %v", bodies[2]["connector_id"])
	}
	if bodies[0]["connector_id"] != "dex-okta" {
		t.Errorf("create: expected connector_id dex-okta, found %v", bodies[0]["connector_id"])
	}
	if _, sent := bodies[1]["connector_id"]; sent {
		t.Errorf("update: a left-out connector must not be sent, found %v", bodies[1]["connector_id"])
	}
}

// Test_scim_disabledOnCreate: the create request has no enabled field, so a
// planned enabled = false is sent by an update right after the create; the
// prefix is read from the response.
func Test_scim_disabledOnCreate(t *testing.T) {
	ctx := context.Background()
	var methods, paths []string
	var bodies []map[string]any
	enabled := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		methods, bodies, paths = append(methods, r.Method), append(bodies, body), append(paths, r.URL.Path)
		if v, ok := body["enabled"].(bool); ok {
			enabled = v
		}
		w.Header().Set("Content-Type", "application/json")
		token := "full-token" // only the create returns the full token
		if r.Method != http.MethodPost {
			token = "nbs_****"
		}
		_, _ = fmt.Fprintf(w, `{"id":7,"provider":"okta","prefix":"server-prefix","enabled":%t,"group_prefixes":[],"user_group_prefixes":[],"auth_token":%q,"last_synced_at":"2026-01-01T00:00:00Z"}`, enabled, token)
	}))
	defer srv.Close()
	r := &Scim{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	values := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		values[n] = tftypes.NewValue(at, nil)
	}
	values["provider_name"] = tftypes.NewValue(tftypes.String, "okta")
	values["prefix"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	values["enabled"] = tftypes.NewValue(tftypes.Bool, false)
	plan := tftypes.NewValue(typ, values)
	create := resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch.Schema, Raw: plan}, Config: tfsdk.Config{Schema: sch.Schema, Raw: plan}}, &create)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	if !slices.Equal(methods, []string{"POST", "PUT"}) || bodies[1]["enabled"] != false || len(bodies[1]) != 1 {
		t.Fatalf("expected a create then an update with only enabled false, found %v %v", methods, bodies)
	}
	if paths[1] != "/api/integrations/scim-idp/7" {
		t.Errorf("expected the disable to target the created integration, found %s", paths[1])
	}
	var got ScimModel
	if d := create.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if !got.Enabled.Equal(types.BoolValue(false)) || got.Prefix.ValueString() != "server-prefix" || got.AuthToken.ValueString() != "full-token" {
		t.Errorf("expected enabled false, the server's prefix and the token, found %v %v %v", got.Enabled, got.Prefix, got.AuthToken)
	}
}

func Test_scimAPIToTerraform_prefix(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		prior    types.String
		response string
		want     types.String
	}{
		"changed on the server": {types.StringValue("old"), "new", types.StringValue("new")},
		"import":                {types.StringNull(), "p", types.StringValue("p")},
		"cleared":               {types.StringValue("old"), "", types.StringValue("")},
	} {
		data := ScimModel{Prefix: tc.prior}
		if d := scimAPIToTerraform(ctx, &api.ScimIntegration{Id: 1, Provider: "okta", Prefix: tc.response}, &data); d.HasError() {
			t.Fatal(d)
		}
		if !data.Prefix.Equal(tc.want) {
			t.Errorf("%s: expected %v, found %v", name, tc.want, data.Prefix)
		}
	}
}

// scimStub serves one integration; failPut makes every PUT fail.
func scimStub(t *testing.T, prefix string, failPut bool) (*Scim, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && failPut {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"refused","code":400}`)
			return
		}
		token := "full-token"
		if r.Method != http.MethodPost {
			token = "nbs_****"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":7,"provider":"okta","prefix":%q,"enabled":true,"group_prefixes":[],"user_group_prefixes":[],"auth_token":%q,"last_synced_at":"2026-01-01T00:00:00Z"}`, prefix, token)
	}))
	return &Scim{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}, srv.Close
}

// scimValue builds a resource value with the given attributes set.
func scimValue(t *testing.T, sch resource.SchemaResponse, set map[string]tftypes.Value) tftypes.Value {
	t.Helper()
	typ, ok := sch.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	values := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		values[n] = tftypes.NewValue(at, nil)
	}
	for n, v := range set {
		values[n] = v
	}
	return tftypes.NewValue(typ, values)
}

func Test_scim_disableFailureKeepsState(t *testing.T) {
	ctx := context.Background()
	r, stop := scimStub(t, "p", true)
	defer stop()
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	plan := scimValue(t, sch, map[string]tftypes.Value{
		"provider_name": tftypes.NewValue(tftypes.String, "okta"),
		"prefix":        tftypes.NewValue(tftypes.String, "p"),
		"enabled":       tftypes.NewValue(tftypes.Bool, false),
	})
	create := resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(plan.Type(), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch.Schema, Raw: plan}, Config: tfsdk.Config{Schema: sch.Schema, Raw: plan}}, &create)
	if !create.Diagnostics.HasError() {
		t.Fatal("expected the failed disable to be reported")
	}
	var got ScimModel
	if d := create.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if got.Id.ValueString() != "7" || got.AuthToken.ValueString() != "full-token" || !got.Enabled.ValueBool() {
		t.Errorf("expected the created integration in state (id 7, the full token, enabled true), found %v %v %v", got.Id, got.AuthToken, got.Enabled)
	}
}

func Test_scim_readPrefix(t *testing.T) {
	ctx := context.Background()
	r, stop := scimStub(t, "server-prefix", false)
	defer stop()
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	for name, state := range map[string]tftypes.Value{
		"drift":  scimValue(t, sch, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "7"), "prefix": tftypes.NewValue(tftypes.String, "old"), "auth_token": tftypes.NewValue(tftypes.String, "full-token")}),
		"import": scimValue(t, sch, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "7")}),
	} {
		resp := resource.ReadResponse{State: tfsdk.State{Schema: sch.Schema, Raw: state}}
		r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: sch.Schema, Raw: state}}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		var got ScimModel
		if d := resp.State.Get(ctx, &got); d.HasError() {
			t.Fatal(d)
		}
		if got.Prefix.ValueString() != "server-prefix" {
			t.Errorf("%s: expected the server's prefix, found %v", name, got.Prefix)
		}
	}
}

func Test_scim_updateKeepsPlannedPrefix(t *testing.T) {
	ctx := context.Background()
	r, stop := scimStub(t, "changed-meanwhile", false)
	defer stop()
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	v := scimValue(t, sch, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, "7"),
		"provider_name": tftypes.NewValue(tftypes.String, "okta"),
		"prefix":        tftypes.NewValue(tftypes.String, "old"),
		"enabled":       tftypes.NewValue(tftypes.Bool, true),
		"auth_token":    tftypes.NewValue(tftypes.String, "full-token"),
	})
	state := tfsdk.State{Schema: sch.Schema, Raw: v}
	resp := resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: sch.Schema, Raw: v}, Config: tfsdk.Config{Schema: sch.Schema, Raw: v}, State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got ScimModel
	if d := resp.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if got.Prefix.ValueString() != "old" || got.AuthToken.ValueString() != "full-token" {
		t.Errorf("expected the planned prefix and the stored token, found %v %v", got.Prefix, got.AuthToken)
	}
}

// Test_scim_updateKeepsNullPrefix: state from before the prefix was read
// holds null, and an update must return the planned null.
func Test_scim_updateKeepsNullPrefix(t *testing.T) {
	ctx := context.Background()
	r, stop := scimStub(t, "server-prefix", false)
	defer stop()
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	v := scimValue(t, sch, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, "7"),
		"provider_name": tftypes.NewValue(tftypes.String, "okta"),
		"enabled":       tftypes.NewValue(tftypes.Bool, true),
		"auth_token":    tftypes.NewValue(tftypes.String, "full-token"),
	})
	state := tfsdk.State{Schema: sch.Schema, Raw: v}
	resp := resource.UpdateResponse{State: state}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: sch.Schema, Raw: v}, Config: tfsdk.Config{Schema: sch.Schema, Raw: v}, State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got ScimModel
	if d := resp.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if !got.Prefix.IsNull() {
		t.Errorf("expected the planned null prefix, found %v", got.Prefix)
	}
}

// Test_scim_disableKeepsCreatedPrefix: the prefix changes between the create
// and the disable; a known planned prefix stays as the create returned it.
func Test_scim_disableKeepsCreatedPrefix(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix, token, enabled := "p", "full-token", true
		if r.Method == http.MethodPut {
			prefix, token, enabled = "changed", "nbs_****", false
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":7,"provider":"okta","prefix":%q,"enabled":%t,"group_prefixes":[],"user_group_prefixes":[],"auth_token":%q,"last_synced_at":"2026-01-01T00:00:00Z"}`, prefix, enabled, token)
	}))
	defer srv.Close()
	r := &Scim{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	plan := scimValue(t, sch, map[string]tftypes.Value{
		"provider_name": tftypes.NewValue(tftypes.String, "okta"),
		"prefix":        tftypes.NewValue(tftypes.String, "p"),
		"enabled":       tftypes.NewValue(tftypes.Bool, false),
	})
	create := resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(plan.Type(), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch.Schema, Raw: plan}, Config: tfsdk.Config{Schema: sch.Schema, Raw: plan}}, &create)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	var got ScimModel
	if d := create.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if got.Prefix.ValueString() != "p" || !got.Enabled.Equal(types.BoolValue(false)) {
		t.Errorf("expected the created prefix p and enabled false, found %v %v", got.Prefix, got.Enabled)
	}
}

func Test_scimDataSource_prefix(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `{"id":7,"provider":"okta","prefix":"looked-up","enabled":true,"group_prefixes":[],"user_group_prefixes":[],"auth_token":"nbs_****","last_synced_at":"2026-01-01T00:00:00Z"}`
		if !strings.HasSuffix(r.URL.Path, "/7") {
			body = "[" + body + "]"
		}
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	d := &ScimDataSource{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
	var sch datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	values := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		values[n] = tftypes.NewValue(at, nil)
	}
	values["provider_name"] = tftypes.NewValue(tftypes.String, "okta")
	cfg := tftypes.NewValue(typ, values)
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: sch.Schema, Raw: cfg}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: sch.Schema, Raw: cfg}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got ScimDataSourceModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got.Prefix.ValueString() != "looked-up" {
		t.Errorf("expected the integration's prefix, found %v", got.Prefix)
	}
}
