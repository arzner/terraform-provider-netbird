// Copyright (c) HashiCorp, Inc.

package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
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
	if plain.Private.ValueBool() || !plain.AccessGroups.IsNull() {
		t.Errorf("read: a service without private fields must be public with null groups, found %v %v", plain.Private, plain.AccessGroups)
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
	if !fresh.AccessGroups.IsNull() {
		t.Errorf("an unset list must read as null, found %v", fresh.AccessGroups)
	}
}
