package provider

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
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
	object := func(name, email any, service any) tftypes.Value {
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
	current := func(v tftypes.Value) tftypes.Value {
		var m map[string]tftypes.Value
		if err := v.As(&m); err != nil {
			t.Fatal(err)
		}
		m["is_current"] = tftypes.NewValue(tftypes.Bool, true)
		return tftypes.NewValue(typ, m)
	}
	for name, tc := range map[string]struct {
		state, config tftypes.Value
		wantError     string
	}{
		"service rename, kind left out":     {object("ci", "", true), object("ci2", nil, nil), "Service user kind required"},
		"service rename, name unknown":      {object("ci", "", true), object(tftypes.UnknownValue, nil, true), "Name not known"},
		"kind not known":                    {object("ci", "", true), object("ci", nil, tftypes.UnknownValue), "User kind not known"},
		"email not known":                   {object("alice", "a@example.com", false), object("alice", tftypes.UnknownValue, false), "Email not known"},
		"regular to service with an email":  {object("Alice", "a@example.com", false), object("ci", "a@example.com", true), "Service users have no email"},
		"current user changes kind":         {current(object("ci", "", true)), object("Alice", "a@example.com", false), "Cannot replace the current user"},
		"new service user with an email":    {tftypes.NewValue(typ, nil), object("ci", "ci@example.com", true), "Service users have no email"},
		"current user renamed":              {current(object("ci", "", true)), object("ci2", nil, true), "Cannot replace the current user"},
		"new regular user without email":    {tftypes.NewValue(typ, nil), object("Alice", nil, nil), "Name and email required"},
		"new service user without email":    {tftypes.NewValue(typ, nil), object("ci", nil, true), ""},
		"new regular user with both":        {tftypes.NewValue(typ, nil), object("Alice", "a@example.com", false), ""},
		"service to regular with an email":  {object("ci", "", true), object("Alice", "a@example.com", false), ""},
		"service to regular without email":  {object("ci", "", true), object("ci", nil, false), "Name and email required"},
		"service to regular, email unknown": {object("ci", "", true), object("Alice", tftypes.UnknownValue, false), "Name and email not known"},
		"regular user renamed":              {object("alice", "a@example.com", false), object("alicia", nil, false), "Name cannot be changed"},
		"email changed":                     {object("ci", "a@example.com", true), object(nil, "b@example.com", true), "Email cannot be changed"},
		"service user renamed":              {object("ci", "", true), object("ci2", nil, true), ""}, // replaced instead
		"unchanged name":                    {object("alice", "a@example.com", false), object("alice", "a@example.com", false), ""},
		"name and email left out":           {object("alice", "a@example.com", false), object(nil, nil, false), ""},
		"name from an expression":           {object("alice", "a@example.com", false), object(tftypes.UnknownValue, nil, false), "Name not known"},
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
			Config: tfsdk.Config{Schema: sch.Schema, Raw: object("ci2", nil, true)},
		}, &resp)
		if resp.RequiresReplace != tc.want {
			t.Errorf("service user %v: expected replacement %v, found %v", tc.service, tc.want, resp.RequiresReplace)
		}
	}
}

// The resource reads a response without the service-user flag as a regular
// user (compatibility; current servers always send it), whatever the prior
// value.
func Test_userResourceAPIToTerraform_absentFlag(t *testing.T) {
	data := UserModel{IsServiceUser: types.BoolValue(true), AutoGroups: types.ListNull(types.StringType)}
	if d := userResourceAPIToTerraform(context.Background(), &api.User{Id: "u1", Role: "user", Status: api.UserStatusActive}, &data); d.HasError() {
		t.Fatal(d)
	}
	if !data.IsServiceUser.Equal(types.BoolValue(false)) {
		t.Errorf("expected false for an absent flag, found %v", data.IsServiceUser)
	}
}

// A new user with is_service_user left out is planned as a regular user.
func Test_user_planRegularByDefault(t *testing.T) {
	ctx := context.Background()
	r := &User{}
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
	values["name"] = tftypes.NewValue(tftypes.String, "Alice")
	values["email"] = tftypes.NewValue(tftypes.String, "a@example.com")
	config := tftypes.NewValue(typ, values)
	planned := map[string]tftypes.Value{}
	for k, v := range values {
		planned[k] = v
	}
	planned["is_service_user"] = tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue)
	plan := tftypes.NewValue(typ, planned)
	req := resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: sch.Schema, Raw: config},
		Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: plan},
		State:  tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(typ, nil)},
	}
	resp := resource.ModifyPlanResponse{Plan: req.Plan}
	r.ModifyPlan(ctx, req, &resp)
	var got types.Bool
	if d := resp.Plan.GetAttribute(ctx, path.Root("is_service_user"), &got); d.HasError() || resp.Diagnostics.HasError() {
		t.Fatal(d, resp.Diagnostics)
	}
	if !got.Equal(types.BoolValue(false)) {
		t.Errorf("expected is_service_user planned false, found %v", got)
	}
}
