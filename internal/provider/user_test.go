package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
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
	with := func(v tftypes.Value, attr string, value tftypes.Value) tftypes.Value {
		var m map[string]tftypes.Value
		if err := v.As(&m); err != nil {
			t.Fatal(err)
		}
		m[attr] = value
		return tftypes.NewValue(typ, m)
	}
	blocked := func(v tftypes.Value) tftypes.Value {
		return with(v, "is_blocked", tftypes.NewValue(tftypes.Bool, true))
	}
	owner := func(v tftypes.Value) tftypes.Value { return with(v, "role", tftypes.NewValue(tftypes.String, "owner")) }
	for name, tc := range map[string]struct {
		state, config tftypes.Value
		wantError     string
	}{
		"new regular user without email":     {tftypes.NewValue(typ, nil), object("Alice", nil, nil), "Name and email required"},
		"new service user without email":     {tftypes.NewValue(typ, nil), object("ci", nil, true), ""},
		"new regular user with both":         {tftypes.NewValue(typ, nil), object("Alice", "a@example.com", false), ""},
		"new service user with an email":     {tftypes.NewValue(typ, nil), object("ci", "ci@example.com", true), "Service users have no email"},
		"new blocked user":                   {tftypes.NewValue(typ, nil), blocked(object("ci", nil, true)), "Cannot create a blocked user"},
		"new service user as owner":          {tftypes.NewValue(typ, nil), owner(object("ci", nil, true)), "Cannot create an owner"},
		"invitation as owner":                {tftypes.NewValue(typ, nil), owner(object("Alice", "a@example.com", false)), "Cannot create an owner"},
		"kind changed":                       {object("ci", "", true), object("Alice", "a@example.com", false), "User kind cannot be changed"},
		"kind left out":                      {object("ci", "", true), object("ci", nil, nil), ""},
		"kind not known":                     {object("ci", "", true), object("ci", nil, tftypes.UnknownValue), ""},
		"new user, kind unknown, neither":    {tftypes.NewValue(typ, nil), object(nil, "a@example.com", tftypes.UnknownValue), "Name and email required"},
		"new user, kind unknown, service ok": {tftypes.NewValue(typ, nil), object("ci", nil, tftypes.UnknownValue), ""},
		"new user, kind unknown, both ok":    {tftypes.NewValue(typ, nil), object("Alice", tftypes.UnknownValue, tftypes.UnknownValue), ""},
		"service user renamed":               {object("ci", "", true), object("ci2", nil, true), "Name cannot be changed"},
		"service rename, kind left out":      {object("ci", "", true), object("ci2", nil, nil), "Name cannot be changed"},
		"regular user renamed":               {object("alice", "a@example.com", false), object("alicia", nil, false), "Name cannot be changed"},
		"email changed":                      {object("ci", "a@example.com", true), object(nil, "b@example.com", true), "Email cannot be changed"},
		"email not known":                    {object("alice", "a@example.com", false), object("alice", tftypes.UnknownValue, false), ""},
		"name from an expression":            {object("alice", "a@example.com", false), object(tftypes.UnknownValue, nil, false), ""},
		"unchanged name":                     {object("alice", "a@example.com", false), object("alice", "a@example.com", false), ""},
		"name and email left out":            {object("alice", "a@example.com", false), object(nil, nil, false), ""},
		"existing user blocked":              {object("ci", "", true), blocked(object("ci", nil, true)), ""},
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
}

// The resource reads a response without the service-user flag (compatibility;
// current servers always send it) as the kind it knows, and leaves it
// unreported without one, as on import.
func Test_userResourceAPIToTerraform_absentFlag(t *testing.T) {
	for name, tc := range map[string]struct{ prior, want types.Bool }{
		"known service user": {types.BoolValue(true), types.BoolValue(true)},
		"import":             {types.BoolNull(), types.BoolNull()},
		"unknown prior":      {types.BoolUnknown(), types.BoolNull()},
	} {
		data := UserModel{IsServiceUser: tc.prior, AutoGroups: types.ListNull(types.StringType)}
		if d := userResourceAPIToTerraform(context.Background(), &api.User{Id: "u1", Role: "user", Status: api.UserStatusActive}, &data); d.HasError() {
			t.Fatal(d)
		}
		if !data.IsServiceUser.Equal(tc.want) {
			t.Errorf("%s: expected %v, found %v", name, tc.want, data.IsServiceUser)
		}
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

// A name known only at apply that resolves to another name is refused by the
// update before it sends anything.
func Test_user_updateRefusesResolvedRename(t *testing.T) {
	ctx := context.Background()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	r := &User{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	value := func(name string) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		values["id"] = tftypes.NewValue(tftypes.String, "u1")
		values["name"] = tftypes.NewValue(tftypes.String, name)
		values["email"] = tftypes.NewValue(tftypes.String, "")
		values["role"] = tftypes.NewValue(tftypes.String, "user")
		values["is_service_user"] = tftypes.NewValue(tftypes.Bool, true)
		values["auto_groups"] = tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{})
		return tftypes.NewValue(typ, values)
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: sch.Schema, Raw: value("ci")}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: value("ci2")},
		Config: tfsdk.Config{Schema: sch.Schema, Raw: value("ci2")},
		State:  tfsdk.State{Schema: sch.Schema, Raw: value("ci")},
	}, &resp)
	found := false
	for _, d := range resp.Diagnostics.Errors() {
		found = found || d.Summary() == "Name cannot be changed"
	}
	if !found || requests != 0 {
		t.Errorf("expected the rename refused without a request, found %v after %d requests", resp.Diagnostics, requests)
	}
}

// The create checks the resolved configuration again and sends nothing for
// a user it cannot make as configured.
func Test_user_createRefusesResolvedBlock(t *testing.T) {
	ctx := context.Background()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	r := &User{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
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
	values["name"] = tftypes.NewValue(tftypes.String, "ci")
	values["role"] = tftypes.NewValue(tftypes.String, "user")
	values["is_service_user"] = tftypes.NewValue(tftypes.Bool, true)
	values["is_blocked"] = tftypes.NewValue(tftypes.Bool, true)
	values["auto_groups"] = tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{})
	v := tftypes.NewValue(typ, values)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: sch.Schema, Raw: v}, Config: tfsdk.Config{Schema: sch.Schema, Raw: v}}, &resp)
	if !resp.Diagnostics.HasError() || requests != 0 {
		t.Errorf("expected the create refused without a request, found %v after %d requests", resp.Diagnostics, requests)
	}
}
