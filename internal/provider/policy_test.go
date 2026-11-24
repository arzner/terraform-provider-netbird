package provider

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_policyAPIToTerraform(t *testing.T) {
	cases := []struct {
		resource *api.Policy
		expected PolicyModel
	}{
		{
			resource: &api.Policy{
				Id:                  valPtr("p1"),
				Name:                "testPol",
				Description:         valPtr("Test"),
				Enabled:             true,
				SourcePostureChecks: []string{"pc1"},
				Rules: []api.PolicyRule{
					{
						Action:        api.PolicyRuleActionAccept,
						Bidirectional: true,
						Description:   valPtr("Test"),
						Sources: &[]api.GroupMinimum{
							{
								Id: "g1",
							},
						},
						Destinations: &[]api.GroupMinimum{
							{
								Id: "g2",
							},
						},
						Enabled:  true,
						Id:       valPtr("p1"),
						Name:     "testPol",
						Ports:    &[]string{"22"},
						Protocol: api.PolicyRuleProtocolAll,
					},
				},
			},
			expected: PolicyModel{
				Id:                  types.StringValue("p1"),
				Name:                types.StringValue("testPol"),
				Description:         types.StringValue("Test"),
				Enabled:             types.BoolValue(true),
				SourcePostureChecks: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("pc1")}),
				Rules: types.ListValueMust(PolicyRuleModel{}.TFType(), []attr.Value{types.ObjectValueMust(PolicyRuleModel{}.TFType().AttrTypes, map[string]attr.Value{
					"id":                   types.StringValue("p1"),
					"action":               types.StringValue("accept"),
					"bidirectional":        types.BoolValue(true),
					"description":          types.StringValue("Test"),
					"sources":              types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g1")}),
					"destinations":         types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g2")}),
					"enabled":              types.BoolValue(true),
					"name":                 types.StringValue("testPol"),
					"ports":                types.ListValueMust(types.StringType, []attr.Value{types.StringValue("22")}),
					"protocol":             types.StringValue("all"),
					"port_ranges":          types.ListNull(PolicyRulePortRangeModel{}.TFType()),
					"source_resource":      types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
					"destination_resource": types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
					"authorized_groups":    types.MapNull(types.ListType{ElemType: types.StringType}),
				})}),
			},
		},
		{
			resource: &api.Policy{
				Id:                  valPtr("p1"),
				Name:                "testPol",
				Description:         valPtr("Test"),
				Enabled:             true,
				SourcePostureChecks: []string{"pc1"},
				Rules: []api.PolicyRule{
					{
						Action:        api.PolicyRuleActionAccept,
						Bidirectional: true,
						Description:   valPtr("Test"),
						SourceResource: &api.Resource{
							Id:   "r1",
							Type: api.ResourceTypeDomain,
						},
						DestinationResource: &api.Resource{
							Id:   "r2",
							Type: api.ResourceTypeDomain,
						},
						Enabled:  true,
						Id:       valPtr("p1"),
						Name:     "testPol",
						Ports:    &[]string{"22"},
						Protocol: api.PolicyRuleProtocolAll,
					},
				},
			},
			expected: PolicyModel{
				Id:                  types.StringValue("p1"),
				Name:                types.StringValue("testPol"),
				Description:         types.StringValue("Test"),
				Enabled:             types.BoolValue(true),
				SourcePostureChecks: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("pc1")}),
				Rules: types.ListValueMust(PolicyRuleModel{}.TFType(), []attr.Value{types.ObjectValueMust(PolicyRuleModel{}.TFType().AttrTypes, map[string]attr.Value{
					"id":            types.StringValue("p1"),
					"action":        types.StringValue("accept"),
					"bidirectional": types.BoolValue(true),
					"description":   types.StringValue("Test"),
					"sources":       types.ListNull(types.StringType),
					"destinations":  types.ListNull(types.StringType),
					"enabled":       types.BoolValue(true),
					"name":          types.StringValue("testPol"),
					"ports":         types.ListValueMust(types.StringType, []attr.Value{types.StringValue("22")}),
					"protocol":      types.StringValue("all"),
					"port_ranges":   types.ListNull(PolicyRulePortRangeModel{}.TFType()),
					"source_resource": types.ObjectValueMust(PolicyRuleResourceModel{}.TFType().AttrTypes, map[string]attr.Value{
						"id":   types.StringValue("r1"),
						"type": types.StringValue("domain"),
					}),
					"destination_resource": types.ObjectValueMust(PolicyRuleResourceModel{}.TFType().AttrTypes, map[string]attr.Value{
						"id":   types.StringValue("r2"),
						"type": types.StringValue("domain"),
					}),
					"authorized_groups": types.MapNull(types.ListType{ElemType: types.StringType}),
				})}),
			},
		},
		{
			resource: &api.Policy{
				Id:                  valPtr("p3"),
				Name:                "sshPolicy",
				Description:         valPtr("SSH with authorized groups"),
				Enabled:             true,
				SourcePostureChecks: []string{},
				Rules: []api.PolicyRule{
					{
						Action:        api.PolicyRuleActionAccept,
						Bidirectional: true,
						Sources: &[]api.GroupMinimum{
							{Id: "g1"},
						},
						Destinations: &[]api.GroupMinimum{
							{Id: "g2"},
						},
						Enabled:  true,
						Id:       valPtr("r3"),
						Name:     "ssh-rule",
						Protocol: api.PolicyRuleProtocolNetbirdSsh,
						AuthorizedGroups: &map[string][]string{
							"g1": {"root", "admin"},
						},
					},
				},
			},
			expected: PolicyModel{
				Id:                  types.StringValue("p3"),
				Name:                types.StringValue("sshPolicy"),
				Description:         types.StringValue("SSH with authorized groups"),
				Enabled:             types.BoolValue(true),
				SourcePostureChecks: types.ListValueMust(types.StringType, []attr.Value{}),
				Rules: types.ListValueMust(PolicyRuleModel{}.TFType(), []attr.Value{types.ObjectValueMust(PolicyRuleModel{}.TFType().AttrTypes, map[string]attr.Value{
					"id":                   types.StringValue("r3"),
					"action":               types.StringValue("accept"),
					"bidirectional":        types.BoolValue(true),
					"description":          types.StringNull(),
					"sources":              types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g1")}),
					"destinations":         types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g2")}),
					"enabled":              types.BoolValue(true),
					"name":                 types.StringValue("ssh-rule"),
					"ports":                types.ListNull(types.StringType),
					"protocol":             types.StringValue("netbird-ssh"),
					"port_ranges":          types.ListNull(PolicyRulePortRangeModel{}.TFType()),
					"source_resource":      types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
					"destination_resource": types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
					"authorized_groups": types.MapValueMust(types.ListType{ElemType: types.StringType}, map[string]attr.Value{
						"g1": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("admin"), types.StringValue("root")}),
					}),
				})}),
			},
		},
	}

	for _, c := range cases {
		var out PolicyModel
		outDiag := policyAPIToTerraform(context.Background(), c.resource, &out)
		if outDiag.HasError() {
			t.Fatalf("Expected no error diagnostics, found %d errors", outDiag.ErrorsCount())
		}

		if !reflect.DeepEqual(out, c.expected) {
			t.Fatalf("Expected:\n%#v\nFound:\n%#v", c.expected, out)
		}
	}
}

func Test_policyRulesTerraformToAPI(t *testing.T) {
	cases := []struct {
		resource *PolicyModel
		expected []api.PolicyRuleUpdate
	}{
		{
			resource: &PolicyModel{
				Rules: types.ListValueMust(PolicyRuleModel{}.TFType(), []attr.Value{
					types.ObjectValueMust(PolicyRuleModel{}.TFType().AttrTypes, map[string]attr.Value{
						"id":                   types.StringValue("r1"),
						"action":               types.StringValue("accept"),
						"bidirectional":        types.BoolValue(false),
						"description":          types.StringValue("Test"),
						"sources":              types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g1")}),
						"destinations":         types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g2")}),
						"enabled":              types.BoolValue(false),
						"name":                 types.StringValue("test"),
						"ports":                types.ListValueMust(types.StringType, []attr.Value{types.StringValue("22")}),
						"protocol":             types.StringValue("all"),
						"port_ranges":          types.ListNull(PolicyRulePortRangeModel{}.TFType()),
						"source_resource":      types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
						"destination_resource": types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
						"authorized_groups":    types.MapNull(types.ListType{ElemType: types.StringType}),
					}),
				}),
			},
			expected: []api.PolicyRuleUpdate{
				{
					Action:        api.PolicyRuleUpdateActionAccept,
					Bidirectional: false,
					Description:   valPtr("Test"),
					Sources:       &[]string{"g1"},
					Destinations:  &[]string{"g2"},
					Enabled:       false,
					Id:            valPtr("r1"),
					Name:          "test",
					Ports:         &[]string{"22"},
					Protocol:      api.PolicyRuleUpdateProtocolAll,
				},
			},
		},
		{
			resource: &PolicyModel{
				Rules: types.ListValueMust(PolicyRuleModel{}.TFType(), []attr.Value{
					types.ObjectValueMust(PolicyRuleModel{}.TFType().AttrTypes, map[string]attr.Value{
						"id":                   types.StringValue("r2"),
						"action":               types.StringValue("accept"),
						"bidirectional":        types.BoolValue(true),
						"description":          types.StringNull(),
						"sources":              types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g1")}),
						"destinations":         types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g2")}),
						"enabled":              types.BoolValue(true),
						"name":                 types.StringValue("ssh-rule"),
						"ports":                types.ListNull(types.StringType),
						"protocol":             types.StringValue("netbird-ssh"),
						"port_ranges":          types.ListNull(PolicyRulePortRangeModel{}.TFType()),
						"source_resource":      types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
						"destination_resource": types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
						"authorized_groups": types.MapValueMust(types.ListType{ElemType: types.StringType}, map[string]attr.Value{
							"g1": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("root")}),
						}),
					}),
				}),
			},
			expected: []api.PolicyRuleUpdate{
				{
					Action:        api.PolicyRuleUpdateActionAccept,
					Bidirectional: true,
					Sources:       &[]string{"g1"},
					Destinations:  &[]string{"g2"},
					Enabled:       true,
					Id:            valPtr("r2"),
					Name:          "ssh-rule",
					Protocol:      api.PolicyRuleUpdateProtocolNetbirdSsh,
					AuthorizedGroups: &map[string][]string{
						"g1": {"root"},
					},
				},
			},
		},
	}

	for _, c := range cases {
		out, outDiag := policyRulesTerraformToAPI(context.Background(), c.resource)
		if outDiag.HasError() {
			t.Fatalf("Expected no error diagnostics, found %d errors", outDiag.ErrorsCount())
		}

		if !reflect.DeepEqual(out, c.expected) {
			t.Fatalf("Expected:\n%#v\nFound:\n%#v", c.expected, out)
		}
	}

}

func Test_policyRulesAuthorizedGroupsValidation(t *testing.T) {
	// authorized_groups should be rejected for non netbird-ssh protocols
	protocols := []string{"all", "tcp", "udp", "icmp"}
	for _, proto := range protocols {
		model := &PolicyModel{
			Rules: types.ListValueMust(PolicyRuleModel{}.TFType(), []attr.Value{
				types.ObjectValueMust(PolicyRuleModel{}.TFType().AttrTypes, map[string]attr.Value{
					"id":                   types.StringValue("r1"),
					"action":               types.StringValue("accept"),
					"bidirectional":        types.BoolValue(true),
					"description":          types.StringNull(),
					"sources":              types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g1")}),
					"destinations":         types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g2")}),
					"enabled":              types.BoolValue(true),
					"name":                 types.StringValue("test"),
					"ports":                types.ListNull(types.StringType),
					"protocol":             types.StringValue(proto),
					"port_ranges":          types.ListNull(PolicyRulePortRangeModel{}.TFType()),
					"source_resource":      types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
					"destination_resource": types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
					"authorized_groups": types.MapValueMust(types.ListType{ElemType: types.StringType}, map[string]attr.Value{
						"g1": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("root")}),
					}),
				}),
			}),
		}
		_, diag := policyRulesTerraformToAPI(context.Background(), model)
		if !diag.HasError() {
			t.Fatalf("Expected validation error for protocol %q with authorized_groups set, but got none", proto)
		}
	}

	// a non-empty authorized_groups needs an entry for every source
	model := &PolicyModel{
		Rules: types.ListValueMust(PolicyRuleModel{}.TFType(), []attr.Value{
			types.ObjectValueMust(PolicyRuleModel{}.TFType().AttrTypes, map[string]attr.Value{
				"id":                   types.StringValue("r1"),
				"action":               types.StringValue("accept"),
				"bidirectional":        types.BoolValue(true),
				"description":          types.StringNull(),
				"sources":              types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g1"), types.StringValue("g2")}),
				"destinations":         types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g2")}),
				"enabled":              types.BoolValue(true),
				"name":                 types.StringValue("test"),
				"ports":                types.ListNull(types.StringType),
				"protocol":             types.StringValue("netbird-ssh"),
				"port_ranges":          types.ListNull(PolicyRulePortRangeModel{}.TFType()),
				"source_resource":      types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
				"destination_resource": types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes),
				"authorized_groups": types.MapValueMust(types.ListType{ElemType: types.StringType}, map[string]attr.Value{
					"g1": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("root")}),
				}),
			}),
		}),
	}
	_, diag := policyRulesTerraformToAPI(context.Background(), model)
	if !diag.HasError() || !strings.Contains(fmt.Sprint(diag), "g2") {
		t.Fatalf("Expected a validation error naming g2, found %v", diag)
	}

	// a user list that does not convert is not sent (an empty list would
	// mean every local user)
	first, ok := model.Rules.Elements()[0].(types.Object)
	if !ok {
		t.Fatal("a rule is not an object")
	}
	rule := first.Attributes()
	attrs := map[string]attr.Value{}
	for k, v := range rule {
		attrs[k] = v
	}
	attrs["authorized_groups"] = types.MapValueMust(types.ListType{ElemType: types.StringType}, map[string]attr.Value{
		"g1": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("root")}),
		"g2": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("admin"), types.StringNull()}),
	})
	model.Rules = types.ListValueMust(PolicyRuleModel{}.TFType(), []attr.Value{types.ObjectValueMust(PolicyRuleModel{}.TFType().AttrTypes, attrs)})
	if req, diag := policyRulesTerraformToAPI(context.Background(), model); !diag.HasError() || req != nil {
		t.Fatalf("Expected an error and no request for a null user, found %v, %v", req, diag)
	}
	// nor a null list, which the server would store as every local user
	attrs["authorized_groups"] = types.MapValueMust(types.ListType{ElemType: types.StringType}, map[string]attr.Value{
		"g1": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("root")}),
		"g2": types.ListNull(types.StringType),
	})
	model.Rules = types.ListValueMust(PolicyRuleModel{}.TFType(), []attr.Value{types.ObjectValueMust(PolicyRuleModel{}.TFType().AttrTypes, attrs)})
	if req, diag := policyRulesTerraformToAPI(context.Background(), model); !diag.HasError() || req != nil {
		t.Fatalf("Expected an error and no request for a null list, found %v, %v", req, diag)
	}
}

// The server needs an entry for every source and accepts other keys.
func Test_sourcesWithoutAuthorizedUsers(t *testing.T) {
	users := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("root")})
	groups := func(keys ...string) types.Map {
		m := map[string]attr.Value{}
		for _, k := range keys {
			m[k] = users
		}
		return types.MapValueMust(types.ListType{ElemType: types.StringType}, m)
	}
	for name, tc := range map[string]struct {
		sources []string
		groups  types.Map
		want    []string
	}{
		"every source":      {[]string{"g1", "g2"}, groups("g1", "g2"), nil},
		"an extra key":      {[]string{"g1"}, groups("g1", "other"), nil},
		"a source left out": {[]string{"g1", "g2"}, groups("g1"), []string{"g2"}},
		"empty map":         {[]string{"g1"}, groups(), nil},
		"no sources":        {nil, groups("g1"), nil},
		"unknown map":       {[]string{"g1"}, types.MapUnknown(types.ListType{ElemType: types.StringType}), nil},
	} {
		var sources *[]string
		if tc.sources != nil {
			sources = &tc.sources
		}
		if got := sourcesWithoutAuthorizedUsers(sources, tc.groups); !slices.Equal(got, tc.want) {
			t.Errorf("%s: expected %v, found %v", name, tc.want, got)
		}
	}
}

// ValidateConfig refuses at plan time what the server would refuse or drop.
func Test_policyValidateConfigAuthorizedGroups(t *testing.T) {
	ctx := context.Background()
	r := &Policy{}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	rulesType, ok := typ.AttributeTypes["rule"].(tftypes.List)
	if !ok {
		t.Fatal("rule is not a list")
	}
	ruleType, ok := rulesType.ElementType.(tftypes.Object)
	if !ok {
		t.Fatal("a rule is not an object")
	}
	strs := func(v ...string) tftypes.Value {
		var l []tftypes.Value
		for _, s := range v {
			l = append(l, tftypes.NewValue(tftypes.String, s))
		}
		return tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, l)
	}
	config := func(protocol string, keys ...string) tftypes.Value {
		rule := map[string]tftypes.Value{}
		for n, at := range ruleType.AttributeTypes {
			rule[n] = tftypes.NewValue(at, nil)
		}
		if protocol != "" {
			rule["protocol"] = tftypes.NewValue(tftypes.String, protocol)
		}
		rule["sources"] = strs("g1", "g2")
		m := map[string]tftypes.Value{}
		for _, k := range keys {
			m[k] = strs("root")
		}
		rule["authorized_groups"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.List{ElementType: tftypes.String}}, m)
		values := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		values["rule"] = tftypes.NewValue(rulesType, []tftypes.Value{tftypes.NewValue(ruleType, rule)})
		return tftypes.NewValue(typ, values)
	}
	// withRule changes the rule of a configuration built by config.
	withRule := func(v tftypes.Value, change func(map[string]tftypes.Value)) tftypes.Value {
		var top map[string]tftypes.Value
		if err := v.As(&top); err != nil {
			t.Fatal(err)
		}
		var rules []tftypes.Value
		if err := top["rule"].As(&rules); err != nil {
			t.Fatal(err)
		}
		var rule map[string]tftypes.Value
		if err := rules[0].As(&rule); err != nil {
			t.Fatal(err)
		}
		change(rule)
		top["rule"] = tftypes.NewValue(rulesType, []tftypes.Value{tftypes.NewValue(ruleType, rule)})
		return tftypes.NewValue(typ, top)
	}
	withProtocol := func(v, protocol tftypes.Value) tftypes.Value {
		return withRule(v, func(r map[string]tftypes.Value) { r["protocol"] = protocol })
	}
	nullUsers := func(v tftypes.Value) tftypes.Value {
		return withRule(v, func(r map[string]tftypes.Value) {
			r["authorized_groups"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.List{ElementType: tftypes.String}}, map[string]tftypes.Value{
				"g1": strs("root"), "g2": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, nil),
			})
		})
	}
	for name, tc := range map[string]struct {
		config    tftypes.Value
		wantError bool
	}{
		"every source":                     {config("netbird-ssh", "g1", "g2"), false},
		"an extra key":                     {config("netbird-ssh", "g1", "g2", "g3"), false},
		"a source left out":                {config("netbird-ssh", "g1"), true},
		"not netbird-ssh":                  {config("tcp", "g1", "g2"), true},
		"protocol left out":                {config("", "g1", "g2"), true}, // the default all
		"protocol unknown, complete":       {withProtocol(config("netbird-ssh", "g1", "g2"), tftypes.NewValue(tftypes.String, tftypes.UnknownValue)), false},
		"protocol unknown, source missing": {withProtocol(config("netbird-ssh", "g1"), tftypes.NewValue(tftypes.String, tftypes.UnknownValue)), true},
		"null users":                       {nullUsers(config("netbird-ssh", "g1", "g2")), true},
	} {
		resp := resource.ValidateConfigResponse{}
		r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sch.Schema, Raw: tc.config}}, &resp)
		if resp.Diagnostics.HasError() != tc.wantError {
			t.Errorf("%s: expected error %v, found %v", name, tc.wantError, resp.Diagnostics)
		}
	}
}

func Test_portRegex(t *testing.T) {
	r := regexp.MustCompile(portStringRegex)
	for i := range 65536 {
		str := strconv.FormatInt(int64(i), 10)
		if !r.MatchString(str) {
			t.Fatalf("Expected %d to match regex", i)
		}
	}

	noMatchCases := []string{
		"-1",
		"65536",
		"100000",
		"65635",
	}
	for _, c := range noMatchCases {
		if r.MatchString(c) {
			t.Fatalf("Exepected %s not to match regex", c)
		}
	}
}

// The server leaves an empty authorized_groups out of the response: a planned
// or stored empty map stays empty, and without one the read is null.
func Test_policyAPIToTerraform_emptyAuthorizedGroups(t *testing.T) {
	ctx := context.Background()
	mapType := types.ListType{ElemType: types.StringType}
	prior := func(groups types.Map) types.List {
		attrs := map[string]attr.Value{}
		for n, at := range (PolicyRuleModel{}).TFType().AttrTypes {
			v, err := at.ValueFromTerraform(ctx, tftypes.NewValue(at.TerraformType(ctx), nil))
			if err != nil {
				t.Fatal(err)
			}
			attrs[n] = v
		}
		attrs["authorized_groups"] = groups
		return types.ListValueMust(PolicyRuleModel{}.TFType(), []attr.Value{types.ObjectValueMust(PolicyRuleModel{}.TFType().AttrTypes, attrs)})
	}
	empty := types.MapValueMust(mapType, map[string]attr.Value{})
	for name, tc := range map[string]struct {
		prior types.List
		want  types.Map
	}{
		"planned empty": {prior(empty), empty},
		"import":        {types.ListNull(PolicyRuleModel{}.TFType()), types.MapNull(mapType)},
	} {
		data := PolicyModel{Rules: tc.prior, SourcePostureChecks: types.ListNull(types.StringType)}
		policy := &api.Policy{Id: valPtr("p1"), Name: "p", Enabled: true, Rules: []api.PolicyRule{{
			Id: valPtr("r1"), Name: "r", Action: api.PolicyRuleActionAccept, Protocol: api.PolicyRuleProtocolNetbirdSsh, Enabled: true,
		}}}
		if d := policyAPIToTerraform(ctx, policy, &data); d.HasError() {
			t.Fatalf("%s: %v", name, d)
		}
		var rules []PolicyRuleModel
		data.Rules.ElementsAs(ctx, &rules, false)
		if len(rules) != 1 || !rules[0].AuthorizedGroups.Equal(tc.want) {
			t.Errorf("%s: expected %v, found %v", name, tc.want, rules)
		}
	}
}
