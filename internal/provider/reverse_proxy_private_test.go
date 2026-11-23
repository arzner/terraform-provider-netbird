// Copyright (c) HashiCorp, Inc.

package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
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
	mixedGroups := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "g1"), tftypes.NewValue(tftypes.String, " ")})
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
		wantPublic          bool
	}{
		"new without groups":     {create: true, config: vals{"private": on}, plan: vals{"private": on, "access_groups": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, tftypes.UnknownValue)}, wantError: true},
		"new with groups":        {create: true, config: vals{"private": on, "access_groups": groups}, plan: vals{"private": on, "access_groups": groups}},
		"kept private, bearer":   {config: vals{"auth": bearer}, plan: vals{"private": on, "mode": http, "access_groups": groups, "auth": bearer}, wantError: true},
		"kept private, no group": {config: vals{"access_groups": noGroups}, plan: vals{"private": on, "access_groups": noGroups}, wantError: true},
		"kept private, fine":     {config: vals{}, plan: vals{"private": on, "mode": http, "access_groups": groups}},
		"kept groups":            {config: vals{"private": on}, plan: vals{"private": on, "access_groups": groups}},
		"public":                 {config: vals{"auth": bearer}, plan: vals{"private": tftypes.NewValue(tftypes.Bool, false), "auth": bearer}},
		"made private, adopted empty list": {
			state:  vals{"private": tftypes.NewValue(tftypes.Bool, false), "access_groups": noGroups},
			config: vals{"private": on}, plan: vals{"private": on, "mode": http, "access_groups": noGroups}, wantError: true,
		},
		"new without access settings": {create: true, config: vals{}, plan: vals{"mode": http, "private": tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue)}, wantPublic: true},
		"new with groups only":        {create: true, config: vals{"access_groups": groups}, plan: vals{"mode": http, "private": tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue), "access_groups": groups}, wantPublic: true},
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
		"made private, explicit http on a tcp service (fails in place, fine replaced)": {
			state:  vals{"private": tftypes.NewValue(tftypes.Bool, false), "mode": tcp, "access_groups": groups},
			config: vals{"private": on, "mode": http, "access_groups": groups},
			plan:   vals{"private": on, "mode": http, "access_groups": groups},
		},
		"made private, mode from an expression": {
			state:  vals{"private": tftypes.NewValue(tftypes.Bool, false), "mode": tcp, "access_groups": groups},
			config: vals{"private": on, "mode": tftypes.NewValue(tftypes.String, tftypes.UnknownValue), "access_groups": groups},
			plan:   vals{"private": on, "mode": tftypes.NewValue(tftypes.String, tftypes.UnknownValue), "access_groups": groups},
		},
		"private kept, mode changed to tcp": {
			state:  vals{"private": on, "mode": http, "access_groups": groups},
			config: vals{"mode": tcp}, plan: vals{"private": on, "mode": tcp, "access_groups": groups},
		},
		"no-op with an adopted blank group": {
			state:  vals{"private": on, "mode": http, "access_groups": blankGroup},
			config: vals{}, plan: vals{"private": on, "mode": http, "access_groups": blankGroup},
		},
		"already private, rename keeps a blank group": {
			state:  vals{"private": on, "mode": http, "access_groups": mixedGroups},
			config: vals{"name": tftypes.NewValue(tftypes.String, "new")},
			plan:   vals{"private": on, "mode": http, "access_groups": mixedGroups, "name": tftypes.NewValue(tftypes.String, "new")},
		},
		"already private, configured blank group": {
			state:  vals{"private": on, "mode": http, "access_groups": groups},
			config: vals{"access_groups": mixedGroups},
			plan:   vals{"private": on, "mode": http, "access_groups": mixedGroups}, wantError: true,
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
			if tc.wantPublic {
				var planned ReverseProxyServiceModel
				if d := resp.Plan.Get(ctx, &planned); d.HasError() {
					t.Fatal(d)
				}
				if !planned.Private.Equal(types.BoolValue(false)) {
					t.Errorf("a new service with private left out must plan private = false, found %v", planned.Private)
				}
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
		req        api.ServiceRequest
		wasPrivate bool
		wantError  bool
	}{
		"private with a group":                  {api.ServiceRequest{Private: &on, AccessGroups: &[]string{"g1"}}, false, false},
		"private, blank live ID":                {api.ServiceRequest{Private: &on, AccessGroups: &[]string{"g1", " "}}, false, true},
		"private, no live groups":               {api.ServiceRequest{Private: &on, AccessGroups: &[]string{}}, false, true},
		"private, groups omitted by the server": {api.ServiceRequest{Private: &on}, false, true},
		"public, blank ID":                      {api.ServiceRequest{Private: &off, AccessGroups: &[]string{" "}}, false, false},
		"already private, blank kept":           {api.ServiceRequest{Private: &on, AccessGroups: &[]string{"g1", " "}}, true, false},
		"private left out":                      {api.ServiceRequest{AccessGroups: &[]string{" "}}, false, false},
	} {
		if err := privateRequestProblem(tc.req, tc.wasPrivate); (err != nil) != tc.wantError {
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

func Test_reverseProxyService_targetAccessActionPlan(t *testing.T) {
	ctx := context.Background()
	r := &ReverseProxyService{}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	listType, ok := typ.AttributeTypes["targets"].(tftypes.List)
	if !ok {
		t.Fatal("targets is not a list")
	}
	targetType, ok := listType.ElementType.(tftypes.Object)
	if !ok {
		t.Fatal("a target is not an object")
	}
	unknown := tftypes.UnknownValue
	// target builds a target at path ("" leaves it out) with an access action
	// (nil leaves it out, unknown is not known yet).
	target := func(path, action any) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range targetType.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		values["target_id"] = tftypes.NewValue(tftypes.String, "p1")
		switch p := path.(type) {
		case tftypes.Value:
			values["path"] = p
		case string:
			if p != "" {
				values["path"] = tftypes.NewValue(tftypes.String, p)
			}
		}
		values["access_action"] = tftypes.NewValue(tftypes.String, action)
		return tftypes.NewValue(targetType, values)
	}
	targets := func(ts ...tftypes.Value) tftypes.Value { return tftypes.NewValue(listType, ts) }
	disabled := func(tv tftypes.Value) tftypes.Value {
		var m map[string]tftypes.Value
		if err := tv.As(&m); err != nil {
			t.Fatal(err)
		}
		m["enabled"] = tftypes.NewValue(tftypes.Bool, false)
		return tftypes.NewValue(targetType, m)
	}
	unknownPath := tftypes.NewValue(tftypes.String, unknown)
	groups := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "g1")})
	on := tftypes.NewValue(tftypes.Bool, true)
	off := tftypes.NewValue(tftypes.Bool, false)
	http := tftypes.NewValue(tftypes.String, "http")
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
	bypassAtAdmin := targets(target("", "inherit"), target("/admin", "bypass"))
	for name, tc := range map[string]struct {
		state, config, plan vals
		wantError           string
		noHint              bool // no "Service is private from its state" error
		inPlace             bool // wantError is a warning: only an in-place update meets it
	}{
		"new, configured bypass": {
			config:    vals{"private": on, "access_groups": groups, "targets": targets(target("/a", "bypass"))},
			plan:      vals{"private": on, "access_groups": groups, "targets": targets(target("/a", "bypass"))},
			wantError: "targets[0].access_action",
		},
		"new, left out": {
			config: vals{"private": on, "access_groups": groups, "targets": targets(target("/a", nil))},
			plan:   vals{"private": on, "access_groups": groups, "targets": targets(target("/a", unknown))},
		},
		"made private, bypass kept from state": {
			state:   vals{"private": off, "mode": http, "access_groups": groups, "targets": bypassAtAdmin},
			config:  vals{"private": on, "targets": targets(target("", nil), target("/admin", nil))},
			plan:    vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("", unknown), target("/admin", unknown))},
			inPlace: true, wantError: "targets[1].access_action",
		},
		"made private, bypass at / kept for a target without a path": {
			state:   vals{"private": off, "mode": http, "access_groups": groups, "targets": targets(target("/", "bypass"))},
			config:  vals{"private": on, "targets": targets(target("", nil))},
			plan:    vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("", unknown))},
			inPlace: true, wantError: "targets[0].access_action",
		},
		"made private, bypass replaced": {
			state:  vals{"private": off, "mode": http, "access_groups": groups, "targets": bypassAtAdmin},
			config: vals{"private": on, "targets": targets(target("", nil), target("/admin", "block"))},
			plan:   vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("", unknown), target("/admin", "block"))},
		},
		"made private, target moved off the bypass path, no action set": {
			state:   vals{"private": off, "mode": http, "access_groups": groups, "targets": bypassAtAdmin},
			config:  vals{"private": on, "targets": targets(target("", nil), target("/other", nil))},
			plan:    vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("", unknown), target("/other", unknown))},
			inPlace: true, wantError: "Access action needed when a controlled path goes",
		},
		"made private, target moved off the bypass path, an action set": {
			state:  vals{"private": off, "mode": http, "access_groups": groups, "targets": bypassAtAdmin},
			config: vals{"private": on, "targets": targets(target("", "inherit"), target("/other", nil))},
			plan:   vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("", "inherit"), target("/other", unknown))},
		},
		"public, block target removed, no action set": {
			state:   vals{"private": off, "mode": http, "targets": targets(target("", "inherit"), target("/admin", "block"))},
			config:  vals{"targets": targets(target("", nil))},
			plan:    vals{"private": off, "mode": http, "targets": targets(target("", unknown))},
			inPlace: true, wantError: "Access action needed when a controlled path goes",
		},
		"public, block target removed, action set": {
			state:  vals{"private": off, "mode": http, "targets": targets(target("", "inherit"), target("/admin", "block"))},
			config: vals{"targets": targets(target("", "inherit"))},
			plan:   vals{"private": off, "mode": http, "targets": targets(target("", "inherit"))},
		},
		"public, several targets at one path with left-out actions": {
			state:   vals{"private": off, "mode": http, "targets": targets(target("", "inherit"), target("", "inherit"), target("/admin", "block"))},
			config:  vals{"name": tftypes.NewValue(tftypes.String, "new"), "targets": targets(target("", nil), target("", nil), target("/admin", nil))},
			plan:    vals{"private": off, "mode": http, "name": tftypes.NewValue(tftypes.String, "new"), "targets": targets(target("", unknown), target("", unknown), target("/admin", unknown))},
			inPlace: true, wantError: "Access action needed on every target",
		},
		"made private, action from an expression on the bypass path": {
			state:  vals{"private": off, "mode": http, "access_groups": groups, "targets": bypassAtAdmin},
			config: vals{"private": on, "targets": targets(target("", nil), target("/admin", unknown))},
			plan:   vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("", unknown), target("/admin", unknown))},
		},
		"made private, each kept path taken once": {
			state:  vals{"private": off, "mode": http, "access_groups": groups, "targets": bypassAtAdmin},
			config: vals{"private": on, "targets": targets(target("", nil), target("/admin", "inherit"), target("/admin", nil))},
			plan:   vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("", unknown), target("/admin", "inherit"), target("/admin", unknown))},
		},
		"new private, a target not known yet next to a bypass": {
			config:    vals{"private": on, "access_groups": groups, "targets": targets(tftypes.NewValue(targetType, unknown), target("/a", "bypass"))},
			plan:      vals{"private": on, "access_groups": groups, "targets": targets(tftypes.NewValue(targetType, unknown), target("/a", "bypass"))},
			wantError: "targets[1].access_action",
		},
		"new private, a target not known yet": {
			config: vals{"private": on, "access_groups": groups, "targets": targets(tftypes.NewValue(targetType, unknown), target("/a", "block"))},
			plan:   vals{"private": on, "access_groups": groups, "targets": targets(tftypes.NewValue(targetType, unknown), target("/a", "block"))},
		},
		"made private, bypass kept before a path not known yet": {
			state:   vals{"private": off, "mode": http, "access_groups": groups, "targets": targets(target("/admin", "bypass"), target("", "inherit"))},
			config:  vals{"private": on, "targets": targets(target("/admin", nil), target(unknownPath, nil))},
			plan:    vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("/admin", unknown), target(unknownPath, unknown))},
			inPlace: true, wantError: "targets[0].access_action",
		},
		"made private, bypass path after a path not known yet": {
			state:  vals{"private": off, "mode": http, "access_groups": groups, "targets": targets(target("", "inherit"), target("/admin", "bypass"))},
			config: vals{"private": on, "targets": targets(target(unknownPath, nil), target("/admin", nil))},
			plan:   vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target(unknownPath, unknown), target("/admin", unknown))},
		},
		"several current targets at one path, a path not known yet": {
			state:   vals{"private": off, "mode": http, "targets": targets(target("", "inherit"), target("", "inherit"), target("/admin", "block"))},
			config:  vals{"name": tftypes.NewValue(tftypes.String, "new"), "targets": targets(target(unknownPath, nil), target("", nil), target("/admin", nil))},
			plan:    vals{"private": off, "mode": http, "name": tftypes.NewValue(tftypes.String, "new"), "targets": targets(target(unknownPath, unknown), target("", unknown), target("/admin", unknown))},
			inPlace: true, wantError: "Access action needed on every target",
		},
		"two enabled targets at one controlled path": {
			config:    vals{"targets": targets(target("/admin", "block"), target("/admin", "inherit"))},
			plan:      vals{"private": off, "targets": targets(target("/admin", "block"), target("/admin", "inherit"))},
			wantError: "targets[1].path",
		},
		"a disabled target at a controlled path": {
			config: vals{"targets": targets(target("/admin", "block"), disabled(target("/admin", "inherit")))},
			plan:   vals{"private": off, "targets": targets(target("/admin", "block"), disabled(target("/admin", "inherit")))},
		},
		"two enabled targets at one path, all inherit": {
			config: vals{"targets": targets(target("/admin", nil), target("/admin", "inherit"))},
			plan:   vals{"private": off, "targets": targets(target("/admin", unknown), target("/admin", "inherit"))},
		},
		"a kept block makes paths strict": {
			state:   vals{"private": off, "mode": http, "targets": targets(target("", "inherit"), target("/admin", "block"))},
			config:  vals{"targets": targets(target("", nil), target("/admin", nil), target("/admin", "inherit"))},
			plan:    vals{"private": off, "mode": http, "targets": targets(target("", unknown), target("/admin", unknown), target("/admin", "inherit"))},
			inPlace: true, wantError: "targets[2].path",
		},
		"no-op with a path left out next to a controlled one": {
			state:  vals{"private": off, "mode": http, "targets": targets(target("/", "inherit"), target("/admin", "block"))},
			config: vals{"targets": targets(target("", "inherit"), target("", "block"))},
			plan:   vals{"private": off, "mode": http, "targets": targets(target("/", "inherit"), target("/admin", "block"))},
		},
		"update with a path left out next to a controlled one": {
			state:     vals{"private": off, "mode": http, "targets": targets(target("/", "inherit"), target("/admin", "block"))},
			config:    vals{"name": tftypes.NewValue(tftypes.String, "new"), "targets": targets(target("", "inherit"), target("", "block"))},
			plan:      vals{"private": off, "mode": http, "name": tftypes.NewValue(tftypes.String, "new"), "targets": targets(target(unknownPath, "inherit"), target(unknownPath, "block"))},
			wantError: "targets[1].path",
		},
		"a path left out while only a name not known yet may change": {
			state:  vals{"private": off, "mode": http, "name": tftypes.NewValue(tftypes.String, "a"), "targets": targets(target("/", "inherit"), target("/admin", "block"))},
			config: vals{"name": tftypes.NewValue(tftypes.String, unknown), "targets": targets(target("", "inherit"), target("", "block"))},
			plan:   vals{"private": off, "mode": http, "name": tftypes.NewValue(tftypes.String, unknown), "targets": targets(target(unknownPath, "inherit"), target(unknownPath, "block"))},
		},
		"kept private, a duplicate path: no hint to make it public": {
			state:   vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("", "inherit"), target("/admin", "block"))},
			config:  vals{"targets": targets(target("", nil), target("/admin", nil), target("/admin", nil))},
			plan:    vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("", unknown), target("/admin", unknown), target("/admin", unknown))},
			inPlace: true, wantError: "targets[2].path",
			noHint: true,
		},
		"block on a relative path": {
			config:    vals{"targets": targets(target("admin", "block"))},
			plan:      vals{"private": off, "targets": targets(target("admin", "block"))},
			wantError: "targets[0].path",
		},
		"block on a dot segment": {
			config:    vals{"targets": targets(target("/a/../b", "block"))},
			plan:      vals{"private": off, "targets": targets(target("/a/../b", "block"))},
			wantError: "targets[0].path",
		},
		"inherit on a relative path": {
			config: vals{"targets": targets(target("admin", "inherit"))},
			plan:   vals{"private": off, "targets": targets(target("admin", "inherit"))},
		},
		"block on the root path": {
			config: vals{"targets": targets(target("/", "block"))},
			plan:   vals{"private": off, "targets": targets(target("/", "block"))},
		},
		"new tcp with block": {
			config:    vals{"mode": tftypes.NewValue(tftypes.String, "tcp"), "targets": targets(target("", "block"))},
			plan:      vals{"mode": tftypes.NewValue(tftypes.String, "tcp"), "targets": targets(target("", "block"))},
			wantError: "targets[0].access_action",
		},
		"tcp from state with block": {
			state:     vals{"private": off, "mode": tftypes.NewValue(tftypes.String, "tcp"), "targets": targets(target("", "inherit"))},
			config:    vals{"targets": targets(target("", "block"))},
			plan:      vals{"private": off, "mode": tftypes.NewValue(tftypes.String, "tcp"), "targets": targets(target("", "block"))},
			wantError: "targets[0].access_action",
		},
		"tcp with inherit": {
			config: vals{"mode": tftypes.NewValue(tftypes.String, "tcp"), "targets": targets(target("", "inherit"))},
			plan:   vals{"mode": tftypes.NewValue(tftypes.String, "tcp"), "targets": targets(target("", "inherit"))},
		},
		"made private, path not known yet": {
			state:  vals{"private": off, "mode": http, "access_groups": groups, "targets": bypassAtAdmin},
			config: vals{"private": on, "targets": targets(target("", nil), target(unknownPath, nil))},
			plan:   vals{"private": on, "mode": http, "access_groups": groups, "targets": targets(target("", unknown), target(unknownPath, unknown))},
		},
		"no-op with bypass, actions left out": {
			state:  vals{"private": off, "mode": http, "targets": bypassAtAdmin},
			config: vals{"targets": targets(target("", nil), target("/other", nil))},
			plan:   vals{"private": off, "mode": http, "targets": bypassAtAdmin},
		},
		"stays public with bypass": {
			state:  vals{"private": off, "mode": http, "targets": bypassAtAdmin},
			config: vals{"name": tftypes.NewValue(tftypes.String, "new")},
			plan:   vals{"private": off, "mode": http, "name": tftypes.NewValue(tftypes.String, "new"), "targets": targets(target("", unknown), target("/admin", unknown))},
		},
	} {
		t.Run(name, func(t *testing.T) {
			state := tftypes.NewValue(typ, nil)
			if tc.state != nil {
				state = object(tc.state)
			}
			var diags diag.Diagnostics
			var vresp resource.ValidateConfigResponse
			r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sch.Schema, Raw: object(tc.config)}}, &vresp)
			diags.Append(vresp.Diagnostics...)
			if !diags.HasError() {
				req := resource.ModifyPlanRequest{
					Config: tfsdk.Config{Schema: sch.Schema, Raw: object(tc.config)},
					Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: object(tc.plan)},
					State:  tfsdk.State{Schema: sch.Schema, Raw: state},
				}
				resp := resource.ModifyPlanResponse{Plan: req.Plan}
				r.ModifyPlan(ctx, req, &resp)
				diags.Append(resp.Diagnostics...)
			}
			if tc.wantError == "" {
				if diags.HasError() {
					t.Fatalf("expected no error, found %v", diags)
				}
				return
			}
			found := false
			matched := diags.Errors()
			if tc.inPlace {
				if diags.HasError() {
					t.Fatalf("an in-place-only problem must be a warning, found %v", diags)
				}
				matched = diags.Warnings()
			}
			for _, d := range matched {
				p := ""
				if wp, ok := d.(diag.DiagnosticWithPath); ok {
					p = wp.Path().String()
				}
				found = found || p == tc.wantError || d.Summary() == tc.wantError
			}
			if !found {
				t.Fatalf("expected an error at %s, found %v", tc.wantError, diags)
			}
			for _, d := range diags.Errors() {
				if tc.noHint && d.Summary() == "Service is private from its state" {
					t.Errorf("making the service public does not fix %v", diags)
				}
			}
		})
	}
}

func Test_directUpstreamWarnings(t *testing.T) {
	optsType := ReverseProxyTargetOptionsModel{}.TFType()
	target := func(direct attr.Value) attr.Value {
		opts := map[string]attr.Value{}
		for n, at := range optsType.AttrTypes {
			opts[n] = nullOf(at.ValueType(context.Background()))
		}
		opts["custom_headers"] = types.MapNull(types.StringType)
		opts["direct_upstream"] = direct
		attrs := map[string]attr.Value{}
		targetType := ReverseProxyServiceTargetModel{}.TFType()
		for n, at := range targetType.AttrTypes {
			attrs[n] = nullOf(at.ValueType(context.Background()))
		}
		attrs["options"] = types.ObjectValueMust(optsType.AttrTypes, opts)
		return types.ObjectValueMust(targetType.AttrTypes, attrs)
	}
	list := types.ListValueMust(ReverseProxyServiceTargetModel{}.TFType(), []attr.Value{
		target(types.BoolValue(true)), target(types.BoolValue(false)), target(types.BoolUnknown()),
	})
	for _, mode := range []string{"tcp", "udp", "tls"} {
		d := directUpstreamWarnings(list, mode)
		if d.WarningsCount() != 1 || d.HasError() {
			t.Errorf("%s: expected one warning for the first target, found %v", mode, d)
		}
	}
}

// directTarget builds a target with a host and direct_upstream.
func directTarget(host string, direct attr.Value) attr.Value {
	ctx := context.Background()
	optsType := ReverseProxyTargetOptionsModel{}.TFType()
	opts := map[string]attr.Value{}
	for n, at := range optsType.AttrTypes {
		opts[n] = nullOf(at.ValueType(ctx))
	}
	opts["custom_headers"] = types.MapNull(types.StringType)
	opts["direct_upstream"] = direct
	targetType := ReverseProxyServiceTargetModel{}.TFType()
	attrs := map[string]attr.Value{}
	for n, at := range targetType.AttrTypes {
		attrs[n] = nullOf(at.ValueType(ctx))
	}
	attrs["host"] = types.StringValue(host)
	attrs["target_type"] = types.StringValue("peer")
	attrs["options"] = types.ObjectValueMust(optsType.AttrTypes, opts)
	return types.ObjectValueMust(targetType.AttrTypes, attrs)
}

func Test_hostProblem(t *testing.T) {
	const on, off, unknown = 1, 0, -1
	for _, tc := range []struct {
		mode, targetType, host string
		direct                 int
		want                   bool
	}{
		{"http", "peer", "127.0.0.1", on, true},
		{"http", "peer", "[::1]", on, true},
		{"http", "host", "224.0.0.1", on, true},
		{"http", "domain", "169.254.1.1", on, true},
		{"http", "peer", "fe80::1%eth0", on, true},
		{"http", "peer", "10.0.0.5:80", on, true},
		{"http", "peer", "a b", on, true},
		{"http", "peer", "a/b", on, true},
		{"http", "peer", "10.0.0.5", on, false},
		{"http", "peer", "backend.lan", on, false},
		{"http", "peer", "bücher.example", on, false},
		{"http", "peer", "2001:db8::1", on, false},
		{"http", "peer", "", on, false},  // the looked-up address
		{"http", "peer", " ", on, false}, // trimmed empty, as the server reads it
		{"http", "peer", "127.0.0.1", off, false},
		{"http", "peer", "127.0.0.1", unknown, false},
		{"tcp", "peer", "127.0.0.1", on, true},
		{"http", "subnet", "", off, true},
		{"http", "subnet", "", unknown, true},
		{"http", "subnet", " ", off, true},
		{"http", "subnet", "fe80::1%eth0", off, true},
		{"http", "subnet", "10.0.0.5:80", unknown, true},
		{"http", "subnet", "169.254.1.1", off, false},
		{"http", "subnet", "169.254.1.1", unknown, false},
		{"http", "subnet", "169.254.1.1", on, true},
		{"http", "subnet", "bücher.example", on, false},
		{"tcp", "subnet", "169.254.1.1", on, false}, // L4 subnet targets need only a host
		{"tcp", "subnet", "", on, true},
		{"udp", "subnet", " ", off, false},
		{"", "subnet", "", off, true}, // mode not known: empty fails in every mode
		{"", "subnet", "169.254.1.1", on, false},
		{"", "subnet", "a b", off, false}, // refused in HTTP only
		{"", "peer", "127.0.0.1", on, true},
	} {
		if got := hostProblem(tc.mode, tc.targetType, tc.host, tc.direct) != ""; got != tc.want {
			t.Errorf("%s %s %q direct=%d: expected a problem %v, found %v", tc.mode, tc.targetType, tc.host, tc.direct, tc.want, got)
		}
	}
	// The diagnostic names the reason, not the value.
	list := types.ListValueMust(ReverseProxyServiceTargetModel{}.TFType(), []attr.Value{directTarget("https://user:secret@db.lan:5432", types.BoolValue(true))})
	for _, d := range targetHostProblems(list, "http") {
		if strings.Contains(d.Detail(), "secret") || strings.Contains(d.Summary(), "secret") {
			t.Errorf("the rejected host must not be repeated: %s", d.Detail())
		}
	}
	if !targetHostProblems(list, "http").HasError() {
		t.Error("a URL as host must be refused")
	}
	// A target type not known yet: an HTTP direct upstream host is checked
	// the same way for every type, an empty one is left to the type.
	unknownType := func(host string) types.List {
		obj, ok := directTarget(host, types.BoolValue(true)).(types.Object)
		if !ok {
			t.Fatal("target is not an object")
		}
		attrs := obj.Attributes()
		attrs["target_type"] = types.StringUnknown()
		return types.ListValueMust(ReverseProxyServiceTargetModel{}.TFType(), []attr.Value{types.ObjectValueMust(obj.AttributeTypes(context.Background()), attrs)})
	}
	if !targetHostProblems(unknownType("127.0.0.1"), "http").HasError() {
		t.Error("loopback with direct_upstream fails for every target type of an HTTP service")
	}
	if targetHostProblems(unknownType(""), "http").HasError() {
		t.Error("an empty host depends on the target type")
	}
	if targetHostProblems(unknownType("127.0.0.1"), "tcp").HasError() {
		t.Error("an L4 subnet target accepts any host, so an unknown type waits")
	}
}

func Test_reverseProxyService_directUpstreamWarningWithoutChanges(t *testing.T) {
	ctx := context.Background()
	r := &ReverseProxyService{}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	list := types.ListValueMust(ReverseProxyServiceTargetModel{}.TFType(), []attr.Value{directTarget("10.0.0.5", types.BoolValue(true))})
	targets, err := list.ToTerraformValue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		values[n] = tftypes.NewValue(at, nil)
	}
	values["mode"] = tftypes.NewValue(tftypes.String, "tcp")
	values["private"] = tftypes.NewValue(tftypes.Bool, false)
	values["targets"] = targets
	state := tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(typ, values)}
	req := resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: sch.Schema, Raw: state.Raw},
		Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: state.Raw},
		State:  state,
	}
	resp := resource.ModifyPlanResponse{Plan: req.Plan}
	r.ModifyPlan(ctx, req, &resp)
	if resp.Diagnostics.WarningsCount() != 1 || resp.Diagnostics.HasError() {
		t.Fatalf("expected one warning on a plan without changes, found %v", resp.Diagnostics)
	}
}

func Test_reverseProxyService_emptyRestrictionsKept(t *testing.T) {
	ctx := context.Background()
	arType := ReverseProxyAccessRestrictionsModel{}.TFType()
	empty, d := types.ObjectValueFrom(ctx, arType.AttrTypes, ReverseProxyAccessRestrictionsModel{
		AllowedCidrs: types.ListNull(types.StringType), BlockedCidrs: types.ListNull(types.StringType),
		AllowedCountries: types.ListNull(types.StringType), BlockedCountries: types.ListNull(types.StringType),
		CrowdsecMode: types.StringNull(),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	set, d := types.ObjectValueFrom(ctx, arType.AttrTypes, ReverseProxyAccessRestrictionsModel{
		AllowedCidrs: types.ListNull(types.StringType), BlockedCidrs: types.ListNull(types.StringType),
		AllowedCountries: types.ListNull(types.StringType), BlockedCountries: types.ListNull(types.StringType),
		CrowdsecMode: types.StringValue("enforce"),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	for name, tc := range map[string]struct {
		prior    types.Object
		wantNull bool
	}{
		"planned object that sets nothing":  {prior: empty},
		"stored mode removed on the server": {prior: set, wantNull: true},
		"no object":                         {prior: types.ObjectNull(arType.AttrTypes), wantNull: true},
	} {
		t.Run(name, func(t *testing.T) {
			data := ReverseProxyServiceModel{AccessRestrictions: tc.prior}
			if d := reverseProxyServiceAPIToTerraform(ctx, &api.Service{Id: "s"}, &data); d.HasError() {
				t.Fatal(d)
			}
			if data.AccessRestrictions.IsNull() != tc.wantNull {
				t.Fatalf("expected null %v, found %v", tc.wantNull, data.AccessRestrictions)
			}
		})
	}
}

func Test_reverseProxyService_emptyListBesideCrowdsec(t *testing.T) {
	ctx := context.Background()
	arType := ReverseProxyAccessRestrictionsModel{}.TFType()
	prior, d := types.ObjectValueFrom(ctx, arType.AttrTypes, ReverseProxyAccessRestrictionsModel{
		AllowedCidrs: types.ListValueMust(types.StringType, []attr.Value{}), BlockedCidrs: types.ListNull(types.StringType),
		AllowedCountries: types.ListNull(types.StringType), BlockedCountries: types.ListNull(types.StringType),
		CrowdsecMode: types.StringValue("enforce"),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	mode := api.AccessRestrictionsCrowdsecMode("enforce")
	data := ReverseProxyServiceModel{AccessRestrictions: prior}
	if d := reverseProxyServiceAPIToTerraform(ctx, &api.Service{Id: "s", AccessRestrictions: &api.AccessRestrictions{CrowdsecMode: &mode}}, &data); d.HasError() {
		t.Fatal(d)
	}
	attrs := data.AccessRestrictions.Attributes()
	if l, ok := attrs["allowed_cidrs"].(types.List); !ok || l.IsNull() || len(l.Elements()) != 0 {
		t.Errorf("a planned empty list stays empty where the server omits it, found %v", attrs["allowed_cidrs"])
	}
	if !attrs["blocked_cidrs"].IsNull() {
		t.Errorf("a list left out stays null, found %v", attrs["blocked_cidrs"])
	}
}

func Test_reverseProxyService_planOmittedTargetValues(t *testing.T) {
	ctx := context.Background()
	r := &ReverseProxyService{}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	listType, ok := typ.AttributeTypes["targets"].(tftypes.List)
	if !ok {
		t.Fatal("targets is not a list")
	}
	targetType, ok := listType.ElementType.(tftypes.Object)
	if !ok {
		t.Fatal("a target is not an object")
	}
	str := func(v any) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	target := func(kind string, host, path, action any) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range targetType.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		values["target_id"], values["target_type"] = str("r1"), str(kind)
		values["host"], values["path"], values["access_action"] = str(host), str(path), str(action)
		return tftypes.NewValue(targetType, values)
	}
	object := func(name string, targets ...tftypes.Value) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		values["name"], values["mode"] = str(name), str("http")
		values["private"] = tftypes.NewValue(tftypes.Bool, false)
		values["targets"] = tftypes.NewValue(listType, targets)
		return tftypes.NewValue(typ, values)
	}
	unknown := tftypes.UnknownValue
	t.Run("a left-out path is planned as the server's none", func(t *testing.T) {
		state := object("a", target("peer", "10.0.0.1", "/public", "bypass"))
		config := object("b", target("peer", nil, nil, "bypass"))
		plan := object("b", target("peer", unknown, unknown, "bypass"))
		req := resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: sch.Schema, Raw: config},
			Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: plan},
			State:  tfsdk.State{Schema: sch.Schema, Raw: state},
		}
		resp := resource.ModifyPlanResponse{Plan: req.Plan}
		r.ModifyPlan(ctx, req, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		var p types.String
		if d := resp.Plan.GetAttribute(ctx, path.Root("targets").AtListIndex(0).AtName("path"), &p); d.HasError() {
			t.Fatal(d)
		}
		if !p.IsNull() {
			t.Errorf("expected path null (the server's \"/\") in the plan, found %v", p)
		}
	})
	t.Run("a path stays unknown while only unknown values differ", func(t *testing.T) {
		state := object("a", target("peer", "10.0.0.1", "/public", "bypass"))
		config := object("a", target("peer", nil, nil, "bypass"))
		plan := object("a", target("peer", unknown, unknown, "bypass"))
		config, plan = withName(t, config, unknown), withName(t, plan, unknown)
		req := resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: sch.Schema, Raw: config},
			Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: plan},
			State:  tfsdk.State{Schema: sch.Schema, Raw: state},
		}
		resp := resource.ModifyPlanResponse{Plan: req.Plan}
		r.ModifyPlan(ctx, req, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		var p types.String
		if d := resp.Plan.GetAttribute(ctx, path.Root("targets").AtListIndex(0).AtName("path"), &p); d.HasError() {
			t.Fatal(d)
		}
		if !p.IsUnknown() {
			t.Errorf("the final plan may keep /public, so the path must stay unknown, found %v", p)
		}
	})
	t.Run("a subnet target without a host", func(t *testing.T) {
		config := object("a", target("subnet", nil, "/", nil))
		plan := object("a", target("subnet", unknown, "/", unknown))
		req := resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: sch.Schema, Raw: config},
			Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: plan},
			State:  tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(typ, nil)},
		}
		resp := resource.ModifyPlanResponse{Plan: req.Plan}
		r.ModifyPlan(ctx, req, &resp)
		if !resp.Diagnostics.HasError() {
			t.Error("a subnet target needs a host")
		}
	})
}

// withName returns obj with its name set to v.
func withName(t *testing.T, obj tftypes.Value, v any) tftypes.Value {
	t.Helper()
	var m map[string]tftypes.Value
	if err := obj.As(&m); err != nil {
		t.Fatal(err)
	}
	m["name"] = tftypes.NewValue(tftypes.String, v)
	return tftypes.NewValue(obj.Type(), m)
}

func Test_targetActionDiagnosticsOmitPaths(t *testing.T) {
	targetType := ReverseProxyServiceTargetModel{}.TFType()
	target := func(p, action string) attr.Value {
		attrs := map[string]attr.Value{}
		for n, at := range targetType.AttrTypes {
			attrs[n] = nullOf(at.ValueType(context.Background()))
		}
		attrs["options"] = types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes)
		attrs["path"], attrs["access_action"] = types.StringValue(p), types.StringValue(action)
		return types.ObjectValueMust(targetType.AttrTypes, attrs)
	}
	list := types.ListValueMust(targetType, []attr.Value{
		target("/cb?token=secret", "block"), target("/x-secret", "bypass"), target("/x-secret", "inherit"),
	})
	privacy, diags, updateOnly := targetActions(list, nil, true).problems(true, "http")
	diags.Append(privacy...)
	diags.Append(updateOnly...)
	if diags.ErrorsCount() < 3 {
		t.Fatalf("expected the query path, the bypass and the duplicate to be refused, found %v", diags)
	}
	for _, d := range diags {
		if strings.Contains(d.Detail(), "secret") || strings.Contains(d.Summary(), "secret") {
			t.Errorf("a path must not be repeated in a diagnostic: %s", d.Detail())
		}
	}
}

func Test_keptActionProblems(t *testing.T) {
	ctx := context.Background()
	r := &ReverseProxyService{}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	listType, ok := typ.AttributeTypes["targets"].(tftypes.List)
	if !ok {
		t.Fatal("targets is not a list")
	}
	targetType, ok := listType.ElementType.(tftypes.Object)
	if !ok {
		t.Fatal("a target is not an object")
	}
	target := map[string]tftypes.Value{}
	for n, at := range targetType.AttributeTypes {
		target[n] = tftypes.NewValue(at, nil)
	}
	target["target_id"] = tftypes.NewValue(tftypes.String, "p1")
	values := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		values[n] = tftypes.NewValue(at, nil)
	}
	values["targets"] = tftypes.NewValue(listType, []tftypes.Value{tftypes.NewValue(targetType, target)})
	config := tfsdk.Config{Schema: sch.Schema, Raw: tftypes.NewValue(typ, values)}
	block, inherit, admin := api.ServiceTargetAccessAction("block"), api.ServiceTargetAccessAction("inherit"), "/admin"
	live := &api.Service{Id: "s", Targets: []api.ServiceTarget{
		{TargetId: "p1", AccessAction: &inherit},
		{TargetId: "p1", Path: &admin, AccessAction: &block},
	}}
	if d := keptActionProblems(ctx, config, api.ServiceRequest{}, live); !d.HasError() {
		t.Error("dropping /admin (block) with no action set must stop the update")
	}
	live.Targets = live.Targets[:1]
	if d := keptActionProblems(ctx, config, api.ServiceRequest{}, live); d.HasError() {
		t.Errorf("nothing controlled is dropped, found %v", d)
	}
}
