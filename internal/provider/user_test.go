package provider

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_userAPIToTerraform(t *testing.T) {
	timeNow := time.Now()

	cases := []struct {
		resource *api.User
		expected UserModel
	}{
		{
			resource: &api.User{
				Id:            "r1",
				AutoGroups:    []string{"g1"},
				Name:          "sk",
				LastLogin:     &timeNow,
				Email:         "me@me.com",
				IsBlocked:     true,
				IsCurrent:     valPtr(true),
				IsServiceUser: valPtr(true),
				Issued:        valPtr("api"),
				Role:          "admin",
				Status:        api.UserStatusActive,
			},
			expected: UserModel{
				Id:            types.StringValue("r1"),
				Name:          types.StringValue("sk"),
				AutoGroups:    types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g1")}),
				LastLogin:     types.StringValue(timeNow.Format(time.RFC3339)),
				IsBlocked:     types.BoolValue(true),
				IsCurrent:     types.BoolValue(true),
				IsServiceUser: types.BoolValue(true),
				Issued:        types.StringValue("api"),
				Email:         types.StringValue("me@me.com"),
				Role:          types.StringValue("admin"),
				Status:        types.StringValue(string(api.UserStatusActive)),
			},
		},
	}

	for _, c := range cases {
		var out UserModel
		outDiag := userAPIToTerraform(context.Background(), c.resource, &out)
		if outDiag.HasError() {
			t.Fatalf("Expected no error diagnostics, found %d errors", outDiag.ErrorsCount())
		}

		if !reflect.DeepEqual(out, c.expected) {
			t.Fatalf("Expected:\n%#v\nFound:\n%#v", c.expected, out)
		}
	}
}

func Test_userAPIToTerraform_keepsConfiguredServiceUser(t *testing.T) {
	// is_service_user is Required on netbird_user: an API response without it
	// must leave the configured value in place, not null it.
	out := UserModel{IsServiceUser: types.BoolValue(true)}
	if d := userAPIToTerraform(context.Background(), &api.User{Id: "r1", Status: api.UserStatusActive}, &out); d.HasError() {
		t.Fatalf("Expected no error diagnostics, found %d errors", d.ErrorsCount())
	}
	if !out.IsServiceUser.Equal(types.BoolValue(true)) {
		t.Fatalf("Expected the configured is_service_user to be kept, found %s", out.IsServiceUser)
	}
}

func Test_user_modifyPlanNameEmail(t *testing.T) {
	ctx := context.Background()
	r := &User{}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	object := func(name, email any, service bool) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		values["id"] = tftypes.NewValue(tftypes.String, "u1")
		values["name"] = tftypes.NewValue(tftypes.String, name)
		values["email"] = tftypes.NewValue(tftypes.String, email)
		values["is_service_user"] = tftypes.NewValue(tftypes.Bool, service)
		return tftypes.NewValue(typ, values)
	}
	for name, tc := range map[string]struct {
		state, config tftypes.Value
		wantError     string
	}{
		"regular user renamed":    {object("alice", "a@example.com", false), object("alicia", nil, false), "Name cannot be changed"},
		"email changed":           {object("ci", "a@example.com", true), object(nil, "b@example.com", true), "Email cannot be changed"},
		"service user renamed":    {object("ci", "", true), object("ci2", nil, true), ""}, // replaced instead
		"unchanged name":          {object("alice", "a@example.com", false), object("alice", "a@example.com", false), ""},
		"name and email left out": {object("alice", "a@example.com", false), object(nil, nil, false), ""},
		"name from an expression": {object("alice", "a@example.com", false), object(tftypes.UnknownValue, nil, false), ""},
	} {
		t.Run(name, func(t *testing.T) {
			req := resource.ModifyPlanRequest{
				Config: tfsdk.Config{Schema: sch.Schema, Raw: tc.config},
				Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: tc.config},
				State:  tfsdk.State{Schema: sch.Schema, Raw: tc.state},
			}
			resp := resource.ModifyPlanResponse{Plan: req.Plan}
			r.ModifyPlan(ctx, req, &resp)
			if tc.wantError == "" {
				if resp.Diagnostics.HasError() {
					t.Fatalf("expected no error, found %v", resp.Diagnostics)
				}
				return
			}
			found := false
			for _, d := range resp.Diagnostics.Errors() {
				found = found || d.Summary() == tc.wantError
			}
			if !found {
				t.Fatalf("expected %q, found %v", tc.wantError, resp.Diagnostics)
			}
		})
	}
	// A service user's new name requires replacement, a regular user's not.
	for _, tc := range []struct {
		service bool
		want    bool
	}{{true, true}, {false, false}} {
		state := tfsdk.State{Schema: sch.Schema, Raw: object("ci", "", tc.service)}
		var resp stringplanmodifier.RequiresReplaceIfFuncResponse
		serviceUserRenamed(ctx, planmodifier.StringRequest{
			State: state, StateValue: types.StringValue("ci"), ConfigValue: types.StringValue("ci2"),
		}, &resp)
		if resp.RequiresReplace != tc.want {
			t.Errorf("service user %v: expected replacement %v, found %v", tc.service, tc.want, resp.RequiresReplace)
		}
	}
}
