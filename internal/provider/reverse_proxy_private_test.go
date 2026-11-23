// Copyright (c) HashiCorp, Inc.

package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_reverseProxyService_privateMapping(t *testing.T) {
	ctx := context.Background()
	on := true
	var data ReverseProxyServiceModel
	svc := &api.Service{Id: "s1", Name: "s", Domain: "s.example", Private: &on, AccessGroups: &[]string{"g2", "g1"}}
	if d := reverseProxyServiceAPIToTerraform(ctx, svc, &data); d.HasError() {
		t.Fatal(d)
	}
	var groups []string
	data.AccessGroups.ElementsAs(ctx, &groups, false)
	if !data.Private.ValueBool() || !reflect.DeepEqual(groups, []string{"g1", "g2"}) {
		t.Errorf("read: expected private with g1, g2; found %v %v", data.Private, groups)
	}

	var plain ReverseProxyServiceModel
	if d := reverseProxyServiceAPIToTerraform(ctx, &api.Service{Id: "s2"}, &plain); d.HasError() {
		t.Fatal(d)
	}
	if plain.Private.ValueBool() || plain.AccessGroups.IsNull() || len(plain.AccessGroups.Elements()) != 0 {
		t.Errorf("read: a service without private fields must be public with no groups, found %v %v", plain.Private, plain.AccessGroups)
	}

	data.Targets = types.ListNull(ReverseProxyServiceTargetModel{}.TFType())
	data.Auth = types.ObjectNull(ReverseProxyServiceAuthModel{}.TFType().AttrTypes)
	data.AccessRestrictions = types.ObjectNull(ReverseProxyAccessRestrictionsModel{}.TFType().AttrTypes)
	req, d := reverseProxyServiceTerraformToAPI(ctx, &data)
	if d.HasError() {
		t.Fatal(d)
	}
	if req.Private == nil || !*req.Private || req.AccessGroups == nil || !reflect.DeepEqual(*req.AccessGroups, []string{"g1", "g2"}) {
		t.Errorf("request: expected private with g1, g2; found %v %v", req.Private, req.AccessGroups)
	}

	data.Private = types.BoolNull()
	data.AccessGroups = types.ListNull(types.StringType)
	req, d = reverseProxyServiceTerraformToAPI(ctx, &data)
	if d.HasError() {
		t.Fatal(d)
	}
	if req.Private != nil || req.AccessGroups != nil {
		t.Errorf("request: null attributes must be omitted, found %v %v", req.Private, req.AccessGroups)
	}
}

func Test_reverseProxyService_validatePrivate(t *testing.T) {
	ctx := context.Background()
	r := &ReverseProxyService{}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	groups := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "g1")})
	noGroups := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{})
	for name, tc := range map[string]struct {
		private   *bool
		mode      string
		groups    *tftypes.Value
		bearer    bool
		wantError bool
	}{
		"public":                 {private: valPtr(false)},
		"private with group":     {private: valPtr(true), groups: &groups},
		"private http":           {private: valPtr(true), mode: "http", groups: &groups},
		"private tcp":            {private: valPtr(true), mode: "tcp", groups: &groups, wantError: true},
		"private without groups": {private: valPtr(true)},
		"private empty groups":   {private: valPtr(true), groups: &noGroups, wantError: true},
		"private with bearer":    {private: valPtr(true), groups: &groups, bearer: true, wantError: true},
		"public with bearer":     {private: valPtr(false), bearer: true},
	} {
		t.Run(name, func(t *testing.T) {
			values := map[string]tftypes.Value{}
			for n, at := range typ.AttributeTypes {
				values[n] = tftypes.NewValue(at, nil)
			}
			if tc.private != nil {
				values["private"] = tftypes.NewValue(tftypes.Bool, *tc.private)
			}
			if tc.mode != "" {
				values["mode"] = tftypes.NewValue(tftypes.String, tc.mode)
			}
			if tc.groups != nil {
				values["access_groups"] = *tc.groups
			}
			if tc.bearer {
				values["auth"] = authWithBearer(t, typ.AttributeTypes["auth"])
			}
			var resp resource.ValidateConfigResponse
			r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sch.Schema, Raw: tftypes.NewValue(typ, values)}}, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("expected error %v, found %v", tc.wantError, resp.Diagnostics)
			}
		})
	}
}

// authWithBearer builds an auth value with only bearer_auth set and enabled.
func authWithBearer(t *testing.T, authType tftypes.Type) tftypes.Value {
	t.Helper()
	obj, ok := authType.(tftypes.Object)
	if !ok {
		t.Fatal("auth is not an object")
	}
	values := map[string]tftypes.Value{}
	for n, at := range obj.AttributeTypes {
		values[n] = tftypes.NewValue(at, nil)
	}
	bearerType, ok := obj.AttributeTypes["bearer_auth"].(tftypes.Object)
	if !ok {
		t.Fatal("bearer_auth is not an object")
	}
	bearer := map[string]tftypes.Value{}
	for n, at := range bearerType.AttributeTypes {
		bearer[n] = tftypes.NewValue(at, nil)
	}
	bearer["enabled"] = tftypes.NewValue(tftypes.Bool, true)
	values["bearer_auth"] = tftypes.NewValue(bearerType, bearer)
	return tftypes.NewValue(obj, values)
}

func Test_reverseProxyService_emptyAccessGroupsKept(t *testing.T) {
	ctx := context.Background()
	data := ReverseProxyServiceModel{AccessGroups: types.ListValueMust(types.StringType, []attr.Value{})}
	if d := reverseProxyServiceAPIToTerraform(ctx, &api.Service{Id: "s"}, &data); d.HasError() {
		t.Fatal(d)
	}
	if data.AccessGroups.IsNull() || len(data.AccessGroups.Elements()) != 0 {
		t.Errorf("a configured empty list must stay empty when the server omits it, found %v", data.AccessGroups)
	}
	var fresh ReverseProxyServiceModel
	if d := reverseProxyServiceAPIToTerraform(ctx, &api.Service{Id: "s"}, &fresh); d.HasError() {
		t.Fatal(d)
	}
	if fresh.AccessGroups.IsNull() || len(fresh.AccessGroups.Elements()) != 0 {
		t.Errorf("an absent list must read as empty, as an applied empty list does, found %v", fresh.AccessGroups)
	}
}

func Test_serviceAccessChangedSincePlan(t *testing.T) {
	on, off := true, false
	list := func(v ...string) types.List {
		elems := []attr.Value{}
		for _, e := range v {
			elems = append(elems, types.StringValue(e))
		}
		return types.ListValueMust(types.StringType, elems)
	}
	model := func(private bool, groups ...string) ReverseProxyServiceModel {
		return ReverseProxyServiceModel{Private: types.BoolValue(private), AccessGroups: list(groups...)}
	}
	for name, tc := range map[string]struct {
		prior, plan ReverseProxyServiceModel
		live        *api.Service
		want        []string
	}{
		"nothing changed":       {model(true, "g1", "g2"), model(true, "g1", "g2"), &api.Service{Private: &on, AccessGroups: &[]string{"g2", "g1"}}, nil},
		"closed outside":        {model(false), model(false), &api.Service{Private: &on, AccessGroups: &[]string{"g1"}}, []string{"private", "access_groups"}},
		"opened outside":        {model(true, "g1"), model(true, "g1"), &api.Service{Private: &off, AccessGroups: &[]string{"g1"}}, []string{"private"}},
		"group revoked":         {model(true, "g1", "g2"), model(true, "g2", "g1"), &api.Service{Private: &on, AccessGroups: &[]string{"g1"}}, []string{"access_groups"}},
		"all groups removed":    {model(false, "g1"), model(false, "g1"), &api.Service{}, []string{"access_groups"}},
		"no groups, nil live":   {model(false), model(false), &api.Service{}, nil},
		"planned change wins":   {model(true, "g1"), model(false, "g3"), &api.Service{Private: &on, AccessGroups: &[]string{"g2"}}, nil},
		"unknown is no promise": {model(true, "g1"), ReverseProxyServiceModel{Private: types.BoolUnknown(), AccessGroups: types.ListUnknown(types.StringType)}, &api.Service{Private: &off}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := serviceAccessChangedSincePlan(tc.prior, tc.plan, tc.live)
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

func Test_reverseProxyService_modifyPlanPrivate(t *testing.T) {
	ctx := context.Background()
	r := &ReverseProxyService{}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	groups := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "g1")})
	noGroups := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{})
	blankGroup := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, " ")})
	type vals map[string]tftypes.Value
	object := func(v vals) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		for n, x := range v {
			values[n] = x
		}
		return tftypes.NewValue(typ, values)
	}
	on := tftypes.NewValue(tftypes.Bool, true)
	http := tftypes.NewValue(tftypes.String, "http")
	tcp := tftypes.NewValue(tftypes.String, "tcp")
	bearer := authWithBearer(t, typ.AttributeTypes["auth"])
	for name, tc := range map[string]struct {
		create              bool
		state, config, plan vals
		wantError           bool
	}{
		"new without groups":     {create: true, config: vals{"private": on}, plan: vals{"private": on, "access_groups": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, tftypes.UnknownValue)}, wantError: true},
		"new with groups":        {create: true, config: vals{"private": on, "access_groups": groups}, plan: vals{"private": on, "access_groups": groups}},
		"kept private, tcp":      {config: vals{"mode": tcp}, plan: vals{"private": on, "mode": tcp, "access_groups": groups}, wantError: true},
		"kept private, bearer":   {config: vals{"auth": bearer}, plan: vals{"private": on, "mode": http, "access_groups": groups, "auth": bearer}, wantError: true},
		"kept private, no group": {config: vals{"access_groups": noGroups}, plan: vals{"private": on, "access_groups": noGroups}, wantError: true},
		"kept private, fine":     {config: vals{}, plan: vals{"private": on, "mode": http, "access_groups": groups}},
		"kept groups":            {config: vals{"private": on}, plan: vals{"private": on, "access_groups": groups}},
		"public":                 {config: vals{"auth": bearer}, plan: vals{"private": tftypes.NewValue(tftypes.Bool, false), "auth": bearer}},
		"made private, adopted empty list": {
			state:  vals{"private": tftypes.NewValue(tftypes.Bool, false), "access_groups": noGroups},
			config: vals{"private": on}, plan: vals{"private": on, "mode": http, "access_groups": noGroups}, wantError: true,
		},
		"new without access settings": {create: true, config: vals{}, plan: vals{"mode": http}},
		"made private, mode tcp from state": {
			state:  vals{"private": tftypes.NewValue(tftypes.Bool, false), "mode": tcp, "access_groups": groups},
			config: vals{"private": on, "access_groups": groups},
			plan:   vals{"private": on, "mode": tftypes.NewValue(tftypes.String, tftypes.UnknownValue), "access_groups": groups}, wantError: true,
		},
		"made private, mode http from state": {
			state:  vals{"private": tftypes.NewValue(tftypes.Bool, false), "mode": http, "access_groups": groups},
			config: vals{"private": on, "access_groups": groups},
			plan:   vals{"private": on, "mode": tftypes.NewValue(tftypes.String, tftypes.UnknownValue), "access_groups": groups},
		},
		"made private, explicit http on a tcp service (replaced)": {
			state:  vals{"private": tftypes.NewValue(tftypes.Bool, false), "mode": tcp, "access_groups": groups},
			config: vals{"private": on, "mode": http, "access_groups": groups},
			plan:   vals{"private": on, "mode": http, "access_groups": groups},
		},
		"no-op with an adopted blank group": {
			state:  vals{"private": on, "mode": http, "access_groups": blankGroup},
			config: vals{}, plan: vals{"private": on, "mode": http, "access_groups": blankGroup},
		},
		"made private, adopted blank group": {
			state:  vals{"private": tftypes.NewValue(tftypes.Bool, false), "access_groups": blankGroup},
			config: vals{"private": on}, plan: vals{"private": on, "mode": http, "access_groups": blankGroup}, wantError: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			state := tftypes.NewValue(typ, nil)
			if !tc.create {
				if tc.state == nil {
					tc.state = vals{"private": on, "access_groups": groups}
				}
				state = object(tc.state)
			}
			req := resource.ModifyPlanRequest{
				Config: tfsdk.Config{Schema: sch.Schema, Raw: object(tc.config)},
				Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: object(tc.plan)},
				State:  tfsdk.State{Schema: sch.Schema, Raw: state},
			}
			resp := resource.ModifyPlanResponse{Plan: req.Plan}
			r.ModifyPlan(ctx, req, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("expected error %v, found %v", tc.wantError, resp.Diagnostics)
			}
		})
	}
}

func Test_keepLiveUnknownAccess(t *testing.T) {
	on, off := true, false
	live := &api.Service{Private: &on, AccessGroups: &[]string{"g-live"}}
	planned := api.ServiceRequest{Private: &off, AccessGroups: &[]string{"g-plan"}}

	req := planned
	keepLiveUnknownAccess(&req, ReverseProxyServiceModel{Private: types.BoolUnknown(), AccessGroups: types.ListUnknown(types.StringType)}, live)
	if req.Private == nil || !*req.Private || req.AccessGroups == nil || !reflect.DeepEqual(*req.AccessGroups, []string{"g-live"}) {
		t.Errorf("unknown: expected the live values, found %v %v", req.Private, req.AccessGroups)
	}

	req = planned
	keepLiveUnknownAccess(&req, ReverseProxyServiceModel{Private: types.BoolValue(false), AccessGroups: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g-plan")})}, live)
	if *req.Private || !reflect.DeepEqual(*req.AccessGroups, []string{"g-plan"}) {
		t.Errorf("known: expected the planned values, found %v %v", *req.Private, *req.AccessGroups)
	}
}

func Test_notBlank(t *testing.T) {
	for value, wantError := range map[string]bool{"g1": false, "": true, " ": true, "\u00a0\t": true, " g1 ": false} {
		var resp validator.StringResponse
		notBlank{}.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(value)}, &resp)
		if resp.Diagnostics.HasError() != wantError {
			t.Errorf("%q: expected error %v, found %v", value, wantError, resp.Diagnostics)
		}
	}
}

func Test_privateRequestProblem(t *testing.T) {
	on, off := true, false
	for name, tc := range map[string]struct {
		req       api.ServiceRequest
		wantError bool
	}{
		"private with a group":                  {api.ServiceRequest{Private: &on, AccessGroups: &[]string{"g1"}}, false},
		"private, blank live ID":                {api.ServiceRequest{Private: &on, AccessGroups: &[]string{"g1", " "}}, true},
		"private, no live groups":               {api.ServiceRequest{Private: &on, AccessGroups: &[]string{}}, true},
		"private, groups omitted by the server": {api.ServiceRequest{Private: &on}, true},
		"public, blank ID":                      {api.ServiceRequest{Private: &off, AccessGroups: &[]string{" "}}, false},
		"private left out":                      {api.ServiceRequest{AccessGroups: &[]string{" "}}, false},
	} {
		if err := privateRequestProblem(tc.req); (err != nil) != tc.wantError {
			t.Errorf("%s: expected error %v, found %v", name, tc.wantError, err)
		}
	}
}

// Test_reverseProxyService_accessPlanModifiers runs the schema's own plan
// modifiers: state from before the attributes existed (null) leaves an
// unknown plan unknown, so the update sends the live values, and a known
// state value is kept.
func Test_reverseProxyService_accessPlanModifiers(t *testing.T) {
	ctx := context.Background()
	var sch resource.SchemaResponse
	(&ReverseProxyService{}).Schema(ctx, resource.SchemaRequest{}, &sch)
	private, ok := sch.Schema.Attributes["private"].(schema.BoolAttribute)
	if !ok {
		t.Fatal("private is not a bool attribute")
	}
	groups, ok := sch.Schema.Attributes["access_groups"].(schema.ListAttribute)
	if !ok {
		t.Fatal("access_groups is not a list attribute")
	}
	planBool := func(state types.Bool) types.Bool {
		req := planmodifier.BoolRequest{ConfigValue: types.BoolNull(), PlanValue: types.BoolUnknown(), StateValue: state}
		resp := planmodifier.BoolResponse{PlanValue: req.PlanValue}
		for _, m := range private.PlanModifiers {
			m.PlanModifyBool(ctx, req, &resp)
		}
		return resp.PlanValue
	}
	planList := func(state types.List) types.List {
		req := planmodifier.ListRequest{ConfigValue: types.ListNull(types.StringType), PlanValue: types.ListUnknown(types.StringType), StateValue: state}
		resp := planmodifier.ListResponse{PlanValue: req.PlanValue}
		for _, m := range groups.PlanModifiers {
			m.PlanModifyList(ctx, req, &resp)
		}
		return resp.PlanValue
	}
	if got := planBool(types.BoolNull()); !got.IsUnknown() {
		t.Errorf("private from null state: expected unknown, found %v", got)
	}
	if got := planBool(types.BoolValue(true)); !got.Equal(types.BoolValue(true)) {
		t.Errorf("private from state: expected true, found %v", got)
	}
	if got := planList(types.ListNull(types.StringType)); !got.IsUnknown() {
		t.Errorf("access_groups from null state: expected unknown, found %v", got)
	}
	g1 := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g1")})
	if got := planList(g1); !got.Equal(g1) {
		t.Errorf("access_groups from state: expected [g1], found %v", got)
	}
}

func Test_modeChangeNeedsReplace(t *testing.T) {
	for name, tc := range map[string]struct {
		state, plan types.String
		want        bool
	}{
		"tcp to http":   {types.StringValue("tcp"), types.StringValue("http"), true},
		"http to udp":   {types.StringValue("http"), types.StringValue("udp"), true},
		"tcp to tls":    {types.StringValue("tcp"), types.StringValue("tls"), true},
		"unset to http": {types.StringValue(""), types.StringValue("http"), false},
		"same":          {types.StringValue("tcp"), types.StringValue("tcp"), false},
		"left out":      {types.StringValue("tcp"), types.StringUnknown(), false},
		"new service":   {types.StringNull(), types.StringValue("tcp"), false},
	} {
		var resp stringplanmodifier.RequiresReplaceIfFuncResponse
		modeChangeNeedsReplace(context.Background(), planmodifier.StringRequest{StateValue: tc.state, PlanValue: tc.plan}, &resp)
		if resp.RequiresReplace != tc.want {
			t.Errorf("%s: expected replace %v, found %v", name, tc.want, resp.RequiresReplace)
		}
	}
}
