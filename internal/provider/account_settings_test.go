package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_AccountSettings_LocalMFASchema(t *testing.T) {
	ctx := context.Background()
	var resourceResp resource.SchemaResponse
	(&AccountSettings{}).Schema(ctx, resource.SchemaRequest{}, &resourceResp)
	attribute, ok := resourceResp.Schema.Attributes["local_mfa_enabled"]
	if !ok {
		t.Fatal("account settings resource must expose local_mfa_enabled")
	}
	if !attribute.IsOptional() || !attribute.IsComputed() {
		t.Fatal("local_mfa_enabled must be optional and computed to adopt server settings")
	}
	var dataSourceResp datasource.SchemaResponse
	(&AccountSettingsDataSource{}).Schema(ctx, datasource.SchemaRequest{}, &dataSourceResp)
	attributeDS, ok := dataSourceResp.Schema.Attributes["local_mfa_enabled"]
	if !ok || !attributeDS.IsComputed() {
		t.Fatal("account settings data source must expose computed local_mfa_enabled")
	}
}

func Test_AccountSettings_LocalMFAMapping(t *testing.T) {
	cases := []struct {
		name    string
		current *bool
		planned types.Bool
		want    *bool
	}{
		{"enable", valPtr(false), types.BoolValue(true), valPtr(true)},
		{"disable", valPtr(true), types.BoolValue(false), valPtr(false)},
		{"omitted enabled", valPtr(true), types.BoolNull(), valPtr(true)},
		{"omitted disabled", valPtr(false), types.BoolNull(), valPtr(false)},
		{"unknown enabled", valPtr(true), types.BoolUnknown(), valPtr(true)},
		{"unknown disabled", valPtr(false), types.BoolUnknown(), valPtr(false)},
		{"unavailable", nil, types.BoolNull(), nil},
		{"unknown unavailable", nil, types.BoolUnknown(), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			account := &api.Account{Settings: api.AccountSettings{
				LocalMfaEnabled: c.current,
				Extra:           &api.AccountExtraSettings{},
			}}
			var data AccountSettingsModel
			if diagnostics := accountAPIToTerraform(ctx, account, &data); diagnostics.HasError() {
				t.Fatalf("map account settings: %v", diagnostics)
			}
			if !data.LocalMfaEnabled.Equal(types.BoolPointerValue(c.current)) {
				t.Fatalf("read local_mfa_enabled: got %v, want %v", data.LocalMfaEnabled, c.current)
			}
			data.LocalMfaEnabled = c.planned
			request := accountTerraformToAPI(ctx, account, data)
			if !reflect.DeepEqual(request.Settings.LocalMfaEnabled, c.want) {
				t.Fatalf("write local_mfa_enabled: got %v, want %v", request.Settings.LocalMfaEnabled, c.want)
			}
		})
	}
}

func Test_accountAPIToTerraform(t *testing.T) {
	cases := []struct {
		resource *api.Account
		expected AccountSettingsModel
	}{
		{
			resource: &api.Account{
				Id: "a",
				Settings: api.AccountSettings{
					AutoUpdateVersion:               nil,
					DnsDomain:                       nil,
					NetworkRange:                    nil,
					LazyConnectionEnabled:           nil,
					GroupsPropagationEnabled:        nil,
					JwtAllowGroups:                  nil,
					JwtGroupsClaimName:              nil,
					JwtGroupsEnabled:                nil,
					PeerInactivityExpiration:        1800,
					PeerInactivityExpirationEnabled: false,
					PeerLoginExpiration:             1800,
					PeerLoginExpirationEnabled:      false,
					RegularUsersViewBlocked:         false,
					RoutingPeerDnsResolutionEnabled: nil,
					PeerExposeEnabled:               false,
					PeerExposeGroups:                nil,
					Extra: &api.AccountExtraSettings{
						NetworkTrafficLogsEnabled:          false,
						NetworkTrafficPacketCounterEnabled: false,
						PeerApprovalEnabled:                false,
						UserApprovalRequired:               false,
						NetworkTrafficLogsGroups:           nil,
					},
				},
			},
			expected: AccountSettingsModel{
				Id:                                 types.StringValue("a"),
				JwtAllowGroups:                     types.ListNull(types.StringType),
				JwtGroupsClaimName:                 types.StringNull(),
				PeerLoginExpiration:                types.Int32Value(1800),
				PeerInactivityExpiration:           types.Int32Value(1800),
				PeerLoginExpirationEnabled:         types.BoolValue(false),
				PeerInactivityExpirationEnabled:    types.BoolValue(false),
				RegularUsersViewBlocked:            types.BoolValue(false),
				GroupsPropagationEnabled:           types.BoolNull(),
				JwtGroupsEnabled:                   types.BoolNull(),
				RoutingPeerDnsResolutionEnabled:    types.BoolNull(),
				PeerApprovalEnabled:                types.BoolValue(false),
				NetworkTrafficLogsEnabled:          types.BoolValue(false),
				NetworkTrafficPacketCounterEnabled: types.BoolValue(false),
				AutoUpdateVersion:                  types.StringNull(),
				DnsDomain:                          types.StringNull(),
				NetworkRange:                       types.StringNull(),
				NetworkRangeV6:                     types.StringNull(),
				Ipv6EnabledGroups:                  types.ListNull(types.StringType),
				DashboardFeatures:                  types.ObjectNull(dashboardFeaturesTypes),
				LazyConnectionEnabled:              types.BoolNull(),
				UserApprovalRequired:               types.BoolValue(false),
				NetworkTrafficLogsGroups:           types.ListNull(types.StringType),
				PeerExposeEnabled:                  types.BoolValue(false),
				PeerExposeGroups:                   types.ListNull(types.StringType),
			},
		},
		{
			resource: &api.Account{
				Id: "b",
				Settings: api.AccountSettings{
					AutoUpdateVersion:               valPtr("latest"),
					DnsDomain:                       valPtr("custom.com"),
					NetworkRange:                    valPtr("100.64.0.0/10"),
					NetworkRangeV6:                  valPtr("fd00:1234::/64"),
					Ipv6EnabledGroups:               &[]string{"g2", "g1"},
					LazyConnectionEnabled:           valPtr(true),
					LocalMfaEnabled:                 valPtr(true),
					GroupsPropagationEnabled:        valPtr(true),
					JwtAllowGroups:                  &[]string{"test"},
					JwtGroupsClaimName:              valPtr("test"),
					JwtGroupsEnabled:                valPtr(true),
					PeerInactivityExpiration:        3600,
					PeerInactivityExpirationEnabled: true,
					PeerLoginExpiration:             3600,
					PeerLoginExpirationEnabled:      true,
					RegularUsersViewBlocked:         true,
					RoutingPeerDnsResolutionEnabled: valPtr(true),
					PeerExposeEnabled:               true,
					PeerExposeGroups:                []string{"group1"},
					Extra: &api.AccountExtraSettings{
						NetworkTrafficLogsEnabled:          true,
						NetworkTrafficPacketCounterEnabled: true,
						PeerApprovalEnabled:                true,
						UserApprovalRequired:               true,
						NetworkTrafficLogsGroups:           []string{"group1"},
					},
				},
			},
			expected: AccountSettingsModel{
				Id:                                 types.StringValue("b"),
				JwtAllowGroups:                     types.ListValueMust(types.StringType, []attr.Value{types.StringValue("test")}),
				JwtGroupsClaimName:                 types.StringValue("test"),
				PeerLoginExpiration:                types.Int32Value(3600),
				PeerInactivityExpiration:           types.Int32Value(3600),
				PeerLoginExpirationEnabled:         types.BoolValue(true),
				PeerInactivityExpirationEnabled:    types.BoolValue(true),
				RegularUsersViewBlocked:            types.BoolValue(true),
				GroupsPropagationEnabled:           types.BoolValue(true),
				JwtGroupsEnabled:                   types.BoolValue(true),
				RoutingPeerDnsResolutionEnabled:    types.BoolValue(true),
				PeerApprovalEnabled:                types.BoolValue(true),
				NetworkTrafficLogsEnabled:          types.BoolValue(true),
				NetworkTrafficPacketCounterEnabled: types.BoolValue(true),
				AutoUpdateVersion:                  types.StringValue("latest"),
				DnsDomain:                          types.StringValue("custom.com"),
				NetworkRange:                       types.StringValue("100.64.0.0/10"),
				NetworkRangeV6:                     types.StringValue("fd00:1234::/64"),
				Ipv6EnabledGroups:                  types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g1"), types.StringValue("g2")}),
				DashboardFeatures:                  types.ObjectNull(dashboardFeaturesTypes),
				LazyConnectionEnabled:              types.BoolValue(true),
				LocalMfaEnabled:                    types.BoolValue(true),
				UserApprovalRequired:               types.BoolValue(true),
				NetworkTrafficLogsGroups:           types.ListValueMust(types.StringType, []attr.Value{types.StringValue("group1")}),
				PeerExposeEnabled:                  types.BoolValue(true),
				PeerExposeGroups:                   types.ListValueMust(types.StringType, []attr.Value{types.StringValue("group1")}),
			},
		},
	}

	for _, c := range cases {
		var out AccountSettingsModel
		outDiag := accountAPIToTerraform(context.Background(), c.resource, &out)
		if outDiag.HasError() {
			t.Fatalf("Expected no error diagnostics, found %d errors", outDiag.ErrorsCount())
		}

		if !reflect.DeepEqual(out, c.expected) {
			t.Fatalf("Expected:\n%#v\nFound:\n%#v", c.expected, out)
		}
	}
}

func Test_accountTerraformToAPI(t *testing.T) {
	cases := []struct {
		currentAccount *api.Account
		data           AccountSettingsModel
		expected       api.AccountRequest
	}{
		{
			currentAccount: &api.Account{
				Id: "a1",
				Settings: api.AccountSettings{
					AutoUpdateVersion:               nil,
					DnsDomain:                       nil,
					NetworkRange:                    nil,
					LazyConnectionEnabled:           nil,
					GroupsPropagationEnabled:        nil,
					JwtAllowGroups:                  nil,
					JwtGroupsClaimName:              nil,
					JwtGroupsEnabled:                nil,
					PeerInactivityExpiration:        1800,
					PeerInactivityExpirationEnabled: false,
					PeerLoginExpiration:             1800,
					PeerLoginExpirationEnabled:      false,
					RegularUsersViewBlocked:         false,
					RoutingPeerDnsResolutionEnabled: nil,
					PeerExposeEnabled:               false,
					PeerExposeGroups:                nil,
					Extra: &api.AccountExtraSettings{
						NetworkTrafficLogsEnabled:          false,
						NetworkTrafficPacketCounterEnabled: false,
						PeerApprovalEnabled:                false,
						UserApprovalRequired:               false,
						NetworkTrafficLogsGroups:           nil,
					},
				},
			},
			data: AccountSettingsModel{
				Id:                  types.StringValue("a1"),
				PeerLoginExpiration: types.Int32Value(3600),
			},
			expected: api.AccountRequest{
				Settings: api.AccountSettings{
					AutoUpdateVersion:               nil,
					DnsDomain:                       nil,
					NetworkRange:                    nil,
					LazyConnectionEnabled:           nil,
					GroupsPropagationEnabled:        nil,
					JwtAllowGroups:                  nil,
					JwtGroupsClaimName:              nil,
					JwtGroupsEnabled:                nil,
					PeerInactivityExpiration:        1800,
					PeerInactivityExpirationEnabled: false,
					PeerLoginExpiration:             3600,
					PeerLoginExpirationEnabled:      false,
					RegularUsersViewBlocked:         false,
					RoutingPeerDnsResolutionEnabled: nil,
					PeerExposeEnabled:               false,
					PeerExposeGroups:                nil,
					Extra: &api.AccountExtraSettings{
						NetworkTrafficLogsEnabled:          false,
						NetworkTrafficPacketCounterEnabled: false,
						PeerApprovalEnabled:                false,
						UserApprovalRequired:               false,
						NetworkTrafficLogsGroups:           nil,
					},
				},
			},
		},
		{
			currentAccount: &api.Account{
				Id: "a2",
				Settings: api.AccountSettings{
					AutoUpdateVersion:               nil,
					DnsDomain:                       nil,
					NetworkRange:                    nil,
					LazyConnectionEnabled:           nil,
					GroupsPropagationEnabled:        nil,
					JwtAllowGroups:                  nil,
					JwtGroupsClaimName:              nil,
					JwtGroupsEnabled:                nil,
					PeerInactivityExpiration:        1800,
					PeerInactivityExpirationEnabled: false,
					PeerLoginExpiration:             1800,
					PeerLoginExpirationEnabled:      false,
					RegularUsersViewBlocked:         false,
					RoutingPeerDnsResolutionEnabled: nil,
					PeerExposeEnabled:               false,
					PeerExposeGroups:                nil,
					Extra: &api.AccountExtraSettings{
						NetworkTrafficLogsEnabled:          false,
						NetworkTrafficPacketCounterEnabled: false,
						PeerApprovalEnabled:                false,
						UserApprovalRequired:               false,
						NetworkTrafficLogsGroups:           nil,
					},
				},
			},
			data: AccountSettingsModel{
				Id:                                 types.StringValue("a2"),
				PeerLoginExpiration:                types.Int32Value(3600),
				PeerLoginExpirationEnabled:         types.BoolValue(true),
				JwtAllowGroups:                     types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g1")}),
				NetworkTrafficLogsEnabled:          types.BoolValue(true),
				PeerInactivityExpirationEnabled:    types.BoolValue(true),
				RegularUsersViewBlocked:            types.BoolValue(true),
				GroupsPropagationEnabled:           types.BoolValue(true),
				JwtGroupsEnabled:                   types.BoolValue(true),
				RoutingPeerDnsResolutionEnabled:    types.BoolValue(true),
				PeerApprovalEnabled:                types.BoolValue(true),
				NetworkTrafficPacketCounterEnabled: types.BoolValue(true),
				PeerInactivityExpiration:           types.Int32Value(3600),
				JwtGroupsClaimName:                 types.StringValue("name"),
				AutoUpdateVersion:                  types.StringValue("latest"),
				DnsDomain:                          types.StringValue("custom.com"),
				NetworkRange:                       types.StringValue("100.64.0.0/10"),
				LazyConnectionEnabled:              types.BoolValue(true),
				UserApprovalRequired:               types.BoolValue(true),
				NetworkTrafficLogsGroups:           types.ListValueMust(types.StringType, []attr.Value{types.StringValue("group1")}),
				PeerExposeEnabled:                  types.BoolValue(true),
				PeerExposeGroups:                   types.ListValueMust(types.StringType, []attr.Value{types.StringValue("group1")}),
			},
			expected: api.AccountRequest{
				Settings: api.AccountSettings{
					AutoUpdateVersion:               valPtr("latest"),
					DnsDomain:                       valPtr("custom.com"),
					NetworkRange:                    valPtr("100.64.0.0/10"),
					LazyConnectionEnabled:           valPtr(true),
					GroupsPropagationEnabled:        valPtr(true),
					JwtAllowGroups:                  &[]string{"g1"},
					JwtGroupsClaimName:              valPtr("name"),
					JwtGroupsEnabled:                valPtr(true),
					PeerInactivityExpiration:        3600,
					PeerInactivityExpirationEnabled: true,
					PeerLoginExpiration:             3600,
					PeerLoginExpirationEnabled:      true,
					RegularUsersViewBlocked:         true,
					RoutingPeerDnsResolutionEnabled: valPtr(true),
					PeerExposeEnabled:               true,
					PeerExposeGroups:                []string{"group1"},
					Extra: &api.AccountExtraSettings{
						NetworkTrafficLogsEnabled:          true,
						NetworkTrafficPacketCounterEnabled: true,
						PeerApprovalEnabled:                true,
						UserApprovalRequired:               true,
						NetworkTrafficLogsGroups:           []string{"group1"},
					},
				},
			},
		},
	}

	for _, c := range cases {
		out := accountTerraformToAPI(context.Background(), c.currentAccount, c.data)

		if !reflect.DeepEqual(out, c.expected) {
			t.Fatalf("Expected:\n%#v\nFound:\n%#v", c.expected, out)
		}
	}
}

func Test_accountTerraformToAPI_keepsUnmodeledSettings(t *testing.T) {
	enabled := true
	groups := []string{"group6"}
	v6Range := "fd00:1234:5678::/64"
	account := &api.Account{
		Id: "a",
		Settings: api.AccountSettings{
			AgentNetworkOnly:   &enabled,
			AutoUpdateAlways:   &enabled,
			DashboardFeatures:  &api.AccountDashboardFeatures{AgentNetwork: &enabled},
			EmbeddedIdpEnabled: &enabled,
			LocalAuthDisabled:  &enabled,
			Ipv6EnabledGroups:  &groups,
			LocalMfaEnabled:    &enabled,
			MetricsPushEnabled: &enabled,
			NetworkRangeV6:     &v6Range,
			Extra:              &api.AccountExtraSettings{},
		},
	}
	data := AccountSettingsModel{
		Id:               types.StringValue("a"),
		JwtGroupsEnabled: types.BoolValue(true),
	}

	out := accountTerraformToAPI(context.Background(), account, data).Settings

	if out.JwtGroupsEnabled == nil || !*out.JwtGroupsEnabled {
		t.Fatalf("jwt_groups_enabled: expected true, found %v", out.JwtGroupsEnabled)
	}
	if out.EmbeddedIdpEnabled != nil || out.LocalAuthDisabled != nil {
		t.Errorf("read-only fields: expected none in the request, found embedded_idp_enabled %v, local_auth_disabled %v",
			out.EmbeddedIdpEnabled, out.LocalAuthDisabled)
	}
	for name, v := range map[string]*bool{
		"agent_network_only":               out.AgentNetworkOnly,
		"auto_update_always":               out.AutoUpdateAlways,
		"local_mfa_enabled":                out.LocalMfaEnabled,
		"metrics_push_enabled":             out.MetricsPushEnabled,
		"dashboard_features.agent_network": dashboardAgentNetwork(out.DashboardFeatures),
	} {
		if v == nil || !*v {
			t.Errorf("%s: expected the account's true to be kept, found %v", name, v)
		}
	}
	if out.Ipv6EnabledGroups == nil || !reflect.DeepEqual(*out.Ipv6EnabledGroups, groups) {
		t.Errorf("ipv6_enabled_groups: expected %v, found %v", groups, out.Ipv6EnabledGroups)
	}
	if out.NetworkRangeV6 == nil || *out.NetworkRangeV6 != v6Range {
		t.Errorf("network_range_v6: expected %s, found %v", v6Range, out.NetworkRangeV6)
	}
}

func Test_accountTerraformToAPI_withoutExtra(t *testing.T) {
	account := &api.Account{Id: "a"}
	data := AccountSettingsModel{
		Id:                       types.StringValue("a"),
		PeerApprovalEnabled:      types.BoolValue(true),
		NetworkTrafficLogsGroups: types.ListNull(types.StringType),
	}

	out := accountTerraformToAPI(context.Background(), account, data).Settings

	if out.Extra == nil || !out.Extra.PeerApprovalEnabled {
		t.Fatalf("extra: expected peer_approval_enabled true, found %#v", out.Extra)
	}
}

// dashboardAgentNetwork reads dashboard_features.agent_network, nil when the
// features object is absent.
func dashboardAgentNetwork(f *api.AccountDashboardFeatures) *bool {
	if f == nil {
		return nil
	}
	return f.AgentNetwork
}

func Test_accountTerraformToAPI_ipv6(t *testing.T) {
	v6 := "fd00:1234::/64"
	current := &api.Account{Id: "a", Settings: api.AccountSettings{
		NetworkRangeV6:    &v6,
		Ipv6EnabledGroups: &[]string{"all"},
		Extra:             &api.AccountExtraSettings{},
	}}

	kept := accountTerraformToAPI(context.Background(), current, AccountSettingsModel{
		Id:                types.StringValue("a"),
		NetworkRangeV6:    types.StringNull(),
		Ipv6EnabledGroups: types.ListNull(types.StringType),
	}).Settings
	if kept.NetworkRangeV6 == nil || *kept.NetworkRangeV6 != v6 || kept.Ipv6EnabledGroups == nil || !reflect.DeepEqual(*kept.Ipv6EnabledGroups, []string{"all"}) {
		t.Errorf("unset: expected the account's values, found %v and %v", kept.NetworkRangeV6, kept.Ipv6EnabledGroups)
	}

	set := accountTerraformToAPI(context.Background(), current, AccountSettingsModel{
		Id:                types.StringValue("a"),
		NetworkRangeV6:    types.StringValue("fd00:5678::/64"),
		Ipv6EnabledGroups: types.ListValueMust(types.StringType, []attr.Value{}),
	}).Settings
	if set.NetworkRangeV6 == nil || *set.NetworkRangeV6 != "fd00:5678::/64" {
		t.Errorf("network_range_v6: expected fd00:5678::/64, found %v", set.NetworkRangeV6)
	}
	if set.Ipv6EnabledGroups == nil || len(*set.Ipv6EnabledGroups) != 0 {
		t.Errorf("ipv6_enabled_groups: expected an explicit empty list, found %v", set.Ipv6EnabledGroups)
	}
}

func Test_accountSettings_remainingSettings(t *testing.T) {
	on, off := true, false
	account := &api.Account{Id: "a", Settings: api.AccountSettings{
		MetricsPushEnabled:            &on,
		AutoUpdateAlways:              &off,
		PeerHostnameCollisionRejected: &on,
		AgentNetworkOnly:              &off,
		DashboardFeatures:             &api.AccountDashboardFeatures{AgentNetwork: &on},
		Extra:                         &api.AccountExtraSettings{},
	}}

	var read AccountSettingsModel
	if d := accountAPIToTerraform(context.Background(), account, &read); d.HasError() {
		t.Fatal(d)
	}
	if !read.MetricsPushEnabled.ValueBool() || read.AutoUpdateAlways.ValueBool() || !read.PeerHostnameCollisionRejected.ValueBool() || read.AgentNetworkOnly.ValueBool() {
		t.Errorf("read: unexpected values %v %v %v %v", read.MetricsPushEnabled, read.AutoUpdateAlways, read.PeerHostnameCollisionRejected, read.AgentNetworkOnly)
	}
	if v, ok := read.DashboardFeatures.Attributes()["agent_network"].(types.Bool); !ok || !v.ValueBool() {
		t.Errorf("read: dashboard_features.agent_network expected true, found %v", read.DashboardFeatures)
	}

	// Unset in the plan: the account's values are sent back.
	kept := accountTerraformToAPI(context.Background(), account, AccountSettingsModel{
		Id: types.StringValue("a"), DashboardFeatures: types.ObjectNull(dashboardFeaturesTypes),
	}).Settings
	if *kept.MetricsPushEnabled != true || *kept.AutoUpdateAlways != false || *kept.PeerHostnameCollisionRejected != true || *kept.AgentNetworkOnly != false || *kept.DashboardFeatures.AgentNetwork != true {
		t.Errorf("unset: expected the account's values, found %+v", kept)
	}

	// Set in the plan: applied; a null member of dashboard_features keeps its value.
	set := accountTerraformToAPI(context.Background(), account, AccountSettingsModel{
		Id:                            types.StringValue("a"),
		MetricsPushEnabled:            types.BoolValue(false),
		AutoUpdateAlways:              types.BoolValue(true),
		PeerHostnameCollisionRejected: types.BoolValue(false),
		AgentNetworkOnly:              types.BoolValue(false),
		DashboardFeatures:             types.ObjectValueMust(dashboardFeaturesTypes, map[string]attr.Value{"agent_network": types.BoolNull()}),
	}).Settings
	if *set.MetricsPushEnabled != false || *set.AutoUpdateAlways != true || *set.PeerHostnameCollisionRejected != false {
		t.Errorf("set: unexpected values %+v", set)
	}
	if set.DashboardFeatures == nil || set.DashboardFeatures.AgentNetwork == nil || *set.DashboardFeatures.AgentNetwork != true {
		t.Errorf("set: a null dashboard member must keep the current value, found %+v", set.DashboardFeatures)
	}
	if account.Settings.DashboardFeatures.AgentNetwork != &on {
		t.Error("the current account must not be modified")
	}

	// No dashboard overrides on the server: null in state.
	var none AccountSettingsModel
	if d := accountAPIToTerraform(context.Background(), &api.Account{Id: "a", Settings: api.AccountSettings{Extra: &api.AccountExtraSettings{}}}, &none); d.HasError() {
		t.Fatal(d)
	}
	if !none.DashboardFeatures.IsNull() {
		t.Errorf("expected null dashboard_features, found %v", none.DashboardFeatures)
	}
}

func Test_accountSettings_validateAgentNetworkOnly(t *testing.T) {
	ctx := context.Background()
	r, schema := accountSettingsResource(t, nil)
	for name, tc := range map[string]struct {
		only      *bool
		dashboard tftypes.Value
		wantError bool
	}{
		"off":                  {only: valPtr(false), dashboard: tftypes.NewValue(dashboardObjectType(), nil)},
		"on without dashboard": {only: valPtr(true), dashboard: tftypes.NewValue(dashboardObjectType(), nil)},
		"on with menu hidden":  {only: valPtr(true), dashboard: dashboardObject(false), wantError: true},
		"on with menu shown":   {only: valPtr(true), dashboard: dashboardObject(true)},
		"on with unknown menu": {only: valPtr(true), dashboard: tftypes.NewValue(dashboardObjectType(), tftypes.UnknownValue)},
		"unset":                {dashboard: tftypes.NewValue(dashboardObjectType(), nil)},
	} {
		t.Run(name, func(t *testing.T) {
			typ, ok := schema.Schema.Type().TerraformType(ctx).(tftypes.Object)
			if !ok {
				t.Fatal("schema type is not an object")
			}
			values := map[string]tftypes.Value{}
			for n, at := range typ.AttributeTypes {
				values[n] = tftypes.NewValue(at, nil)
			}
			if tc.only != nil {
				values["agent_network_only"] = tftypes.NewValue(tftypes.Bool, *tc.only)
			}
			values["dashboard_features"] = tc.dashboard
			var resp resource.ValidateConfigResponse
			r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: tftypes.NewValue(typ, values)}}, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("expected error %v, found %v", tc.wantError, resp.Diagnostics)
			}
		})
	}
}

func dashboardObjectType() tftypes.Object {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"agent_network": tftypes.Bool}}
}

func dashboardObject(agentNetwork bool) tftypes.Value {
	return tftypes.NewValue(dashboardObjectType(), map[string]tftypes.Value{"agent_network": tftypes.NewValue(tftypes.Bool, agentNetwork)})
}

func Test_nullUnconfigured(t *testing.T) {
	plan := AccountSettingsModel{
		Id:                 types.StringValue("a"),
		MetricsPushEnabled: types.BoolValue(true),
		AutoUpdateAlways:   types.BoolValue(false),
		JwtAllowGroups:     types.ListValueMust(types.StringType, []attr.Value{types.StringValue("g")}),
	}
	config := AccountSettingsModel{
		Id:                 types.StringNull(),
		MetricsPushEnabled: types.BoolNull(),
		AutoUpdateAlways:   types.BoolValue(false),
		JwtAllowGroups:     types.ListNull(types.StringType),
	}
	got := nullUnconfigured(plan, config)
	if got.Id.ValueString() != "a" {
		t.Errorf("id must stay from the plan, found %v", got.Id)
	}
	if !got.MetricsPushEnabled.IsNull() || !got.JwtAllowGroups.IsNull() {
		t.Errorf("unconfigured attributes must be null, found %v %v", got.MetricsPushEnabled, got.JwtAllowGroups)
	}
	if got.AutoUpdateAlways.IsNull() || got.AutoUpdateAlways.ValueBool() {
		t.Errorf("a configured attribute must keep the plan value, found %v", got.AutoUpdateAlways)
	}
	if plan.MetricsPushEnabled.IsNull() {
		t.Error("the plan must not be modified")
	}
}

func Test_nullUnconfigured_objectMembers(t *testing.T) {
	plan := AccountSettingsModel{DashboardFeatures: types.ObjectValueMust(dashboardFeaturesTypes, map[string]attr.Value{"agent_network": types.BoolValue(true)})}
	config := AccountSettingsModel{DashboardFeatures: types.ObjectValueMust(dashboardFeaturesTypes, map[string]attr.Value{"agent_network": types.BoolNull()})}
	got := nullUnconfigured(plan, config)
	if v, ok := got.DashboardFeatures.Attributes()["agent_network"].(types.Bool); !ok || !v.IsNull() {
		t.Errorf("a member left out of a configured object must be null, found %v", got.DashboardFeatures)
	}
	current := true
	req := dashboardFeaturesRequest(got.DashboardFeatures, &api.AccountDashboardFeatures{AgentNetwork: &current})
	if req == nil || req.AgentNetwork == nil || !*req.AgentNetwork {
		t.Errorf("the request must carry the live value for a left-out member, found %+v", req)
	}
	if v := plan.DashboardFeatures.Attributes()["agent_network"].(types.Bool); !v.ValueBool() { //nolint:forcetypeassert
		t.Error("the plan must not be modified")
	}
}
