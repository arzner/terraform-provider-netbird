// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int32validator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int32planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &AccountSettings{}
var _ resource.ResourceWithImportState = &AccountSettings{}
var _ resource.ResourceWithValidateConfig = &AccountSettings{}

// dashboardFeaturesTypes are the attributes of dashboard_features.
var dashboardFeaturesTypes = map[string]attr.Type{"agent_network": types.BoolType}

func NewAccountSettings() resource.Resource {
	return &AccountSettings{}
}

// AccountSettings defines the resource implementation.
type AccountSettings struct {
	client *netbird.Client
}

// AccountSettingsModel describes the resource data model.
type AccountSettingsModel struct {
	Id                                 types.String `tfsdk:"id"`
	JwtAllowGroups                     types.List   `tfsdk:"jwt_allow_groups"`
	JwtGroupsClaimName                 types.String `tfsdk:"jwt_groups_claim_name"`
	PeerLoginExpiration                types.Int32  `tfsdk:"peer_login_expiration"`
	PeerInactivityExpiration           types.Int32  `tfsdk:"peer_inactivity_expiration"`
	PeerLoginExpirationEnabled         types.Bool   `tfsdk:"peer_login_expiration_enabled"`
	PeerInactivityExpirationEnabled    types.Bool   `tfsdk:"peer_inactivity_expiration_enabled"`
	RegularUsersViewBlocked            types.Bool   `tfsdk:"regular_users_view_blocked"`
	GroupsPropagationEnabled           types.Bool   `tfsdk:"groups_propagation_enabled"`
	JwtGroupsEnabled                   types.Bool   `tfsdk:"jwt_groups_enabled"`
	RoutingPeerDnsResolutionEnabled    types.Bool   `tfsdk:"routing_peer_dns_resolution_enabled"`
	PeerApprovalEnabled                types.Bool   `tfsdk:"peer_approval_enabled"`
	NetworkTrafficLogsEnabled          types.Bool   `tfsdk:"network_traffic_logs_enabled"`
	NetworkTrafficPacketCounterEnabled types.Bool   `tfsdk:"network_traffic_packet_counter_enabled"`
	AutoUpdateVersion                  types.String `tfsdk:"auto_update_version"`
	DnsDomain                          types.String `tfsdk:"dns_domain"`
	NetworkRange                       types.String `tfsdk:"network_range"`
	NetworkRangeV6                     types.String `tfsdk:"network_range_v6"`
	Ipv6EnabledGroups                  types.List   `tfsdk:"ipv6_enabled_groups"`
	LazyConnectionEnabled              types.Bool   `tfsdk:"lazy_connection_enabled"`
	LocalMfaEnabled                    types.Bool   `tfsdk:"local_mfa_enabled"`
	MetricsPushEnabled                 types.Bool   `tfsdk:"metrics_push_enabled"`
	AutoUpdateAlways                   types.Bool   `tfsdk:"auto_update_always"`
	PeerHostnameCollisionRejected      types.Bool   `tfsdk:"peer_hostname_collision_rejected"`
	AgentNetworkOnly                   types.Bool   `tfsdk:"agent_network_only"`
	DashboardFeatures                  types.Object `tfsdk:"dashboard_features"`
	UserApprovalRequired               types.Bool   `tfsdk:"user_approval_required"`
	NetworkTrafficLogsGroups           types.List   `tfsdk:"network_traffic_logs_groups"`
	PeerExposeEnabled                  types.Bool   `tfsdk:"peer_expose_enabled"`
	PeerExposeGroups                   types.List   `tfsdk:"peer_expose_groups"`
}

func (r *AccountSettings) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account_settings"
}

func (r *AccountSettings) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage Account-wide Settings",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of an account",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"jwt_allow_groups": schema.ListAttribute{
				MarkdownDescription: "List of groups to which users are allowed access",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"jwt_groups_claim_name": schema.StringAttribute{
				MarkdownDescription: "Name of the claim from which we extract groups names to add it to account groups.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"peer_login_expiration": schema.Int32Attribute{
				MarkdownDescription: "Period of time after which peer login expires (seconds).",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Int32{int32planmodifier.UseStateForUnknown()},
				// https://github.com/netbirdio/netbird/blob/main/management/server/account.go#L281
				Validators: []validator.Int32{int32validator.Between(3600, 180*24*3600)},
			},
			"peer_inactivity_expiration": schema.Int32Attribute{
				MarkdownDescription: "Period of time of inactivity after which peer session expires (seconds).",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Int32{int32planmodifier.UseStateForUnknown()},
			},
			"peer_login_expiration_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enables or disables peer login expiration globally. After peer's login has expired the user has to log in (authenticate). Applies only to peers that were added by a user (interactive SSO login).",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"peer_inactivity_expiration_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enables or disables peer inactivity expiration globally. After peer's session has expired the user has to log in (authenticate). Applies only to peers that were added by a user (interactive SSO login).",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"regular_users_view_blocked": schema.BoolAttribute{
				MarkdownDescription: "Allows blocking regular users from viewing parts of the system.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"groups_propagation_enabled": schema.BoolAttribute{
				MarkdownDescription: "Allows propagate the new user auto groups to peers that belongs to the user",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"jwt_groups_enabled": schema.BoolAttribute{
				MarkdownDescription: "Allows extract groups from JWT claim and add it to account groups.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"routing_peer_dns_resolution_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enables or disables DNS resolution on the routing peers",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"peer_approval_enabled": schema.BoolAttribute{
				MarkdownDescription: "(Cloud only) Enables or disables peer approval globally. If enabled, all peers added will be in pending state until approved by an admin.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"network_traffic_logs_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enables or disables network traffic logging. If enabled, all network traffic events from peers will be stored.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"network_traffic_packet_counter_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enables or disables network traffic packet counter. If enabled, network packets and their size will be counted and reported. (This can have an slight impact on performance)",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"auto_update_version": schema.StringAttribute{
				MarkdownDescription: "Set Clients auto-update version. \"latest\", \"disabled\", or a specific version (e.g \"0.64.5\")",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dns_domain": schema.StringAttribute{
				MarkdownDescription: "Allows to define a custom DNS domain for the account",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"network_range": schema.StringAttribute{
				MarkdownDescription: "Allows to define a custom network range for the account in CIDR format",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{canonicalPrefix{ipv6: false}},
			},
			"network_range_v6": schema.StringAttribute{
				MarkdownDescription: "Allows to define a custom IPv6 overlay network range for the account in CIDR format",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{canonicalPrefix{ipv6: true}},
			},
			"ipv6_enabled_groups": schema.ListAttribute{
				MarkdownDescription: "List of group IDs whose peers receive IPv6 overlay addresses. Peers not in any of these groups are not allocated an IPv6 address.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"lazy_connection_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enables or disables experimental lazy connection",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"local_mfa_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enables or disables TOTP multi-factor authentication for local users. Only applicable when the embedded identity provider is enabled.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"metrics_push_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enables or disables client metrics push for all peers in the account.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"auto_update_always": schema.BoolAttribute{
				MarkdownDescription: "When true, client updates are installed automatically in the background; when false, they require user interaction in the client UI. Applies when `auto_update_version` is not `disabled`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"peer_hostname_collision_rejected": schema.BoolAttribute{
				MarkdownDescription: "When true, registering or renaming a peer to a hostname another peer of the account already uses fails, instead of the peer receiving an IP-based suffix. Applies to ephemeral peers as well.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"agent_network_only": schema.BoolAttribute{
				MarkdownDescription: "Limits the dashboard to the Agent Network surface. Requires the Agent Network menu (`dashboard_features.agent_network`) to be shown on the account; setting it false together with this is rejected at plan time.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"dashboard_features": schema.SingleNestedAttribute{
				MarkdownDescription: "Per-account dashboard section visibility overrides. Left out, the account's current overrides are kept.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"agent_network": schema.BoolAttribute{
						MarkdownDescription: "Shows (true) or hides (false) the Agent Network menu regardless of the deployment feature flag. Left out, the current value is kept; it cannot be reset to the server default from Terraform.",
						Optional:            true,
						Computed:            true,
						PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
					},
				},
			},
			"user_approval_required": schema.BoolAttribute{
				MarkdownDescription: "Enables manual approval for new users joining via domain matching. When enabled, users are blocked with pending approval status until explicitly approved by an admin.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"network_traffic_logs_groups": schema.ListAttribute{
				MarkdownDescription: "Limits traffic logging to these groups. If unset all peers are enabled.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"peer_expose_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enables or disables peer expose. If enabled, peers can expose local services through the reverse proxy using the CLI.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"peer_expose_groups": schema.ListAttribute{
				MarkdownDescription: "Limits which peer groups are allowed to expose services. If empty, all peers are allowed when peer expose is enabled.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *AccountSettings) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*netbird.Client)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *netbird.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

func accountAPIToTerraform(ctx context.Context, account *api.Account, data *AccountSettingsModel) diag.Diagnostics {
	var ret diag.Diagnostics
	data.Id = types.StringValue(account.Id)
	data.JwtAllowGroups, ret = sortedStringListPointer(ctx, data.JwtAllowGroups, account.Settings.JwtAllowGroups)
	data.JwtGroupsClaimName = types.StringPointerValue(account.Settings.JwtGroupsClaimName)
	data.PeerLoginExpiration = types.Int32Value(int32(account.Settings.PeerLoginExpiration))
	data.PeerInactivityExpiration = types.Int32Value(int32(account.Settings.PeerInactivityExpiration))
	data.PeerLoginExpirationEnabled = types.BoolValue(account.Settings.PeerLoginExpirationEnabled)
	data.PeerInactivityExpirationEnabled = types.BoolValue(account.Settings.PeerInactivityExpirationEnabled)
	data.RegularUsersViewBlocked = types.BoolValue(account.Settings.RegularUsersViewBlocked)
	data.GroupsPropagationEnabled = types.BoolPointerValue(account.Settings.GroupsPropagationEnabled)
	data.JwtGroupsEnabled = types.BoolPointerValue(account.Settings.JwtGroupsEnabled)
	data.RoutingPeerDnsResolutionEnabled = types.BoolPointerValue(account.Settings.RoutingPeerDnsResolutionEnabled)
	data.PeerApprovalEnabled = types.BoolValue(account.Settings.Extra.PeerApprovalEnabled)
	data.NetworkTrafficLogsEnabled = types.BoolValue(account.Settings.Extra.NetworkTrafficLogsEnabled)
	data.NetworkTrafficPacketCounterEnabled = types.BoolValue(account.Settings.Extra.NetworkTrafficPacketCounterEnabled)
	data.AutoUpdateVersion = types.StringPointerValue(account.Settings.AutoUpdateVersion)
	data.DnsDomain = types.StringPointerValue(account.Settings.DnsDomain)
	data.NetworkRange = types.StringPointerValue(account.Settings.NetworkRange)
	data.NetworkRangeV6 = types.StringPointerValue(account.Settings.NetworkRangeV6)
	ipv6Groups, d6 := sortedStringListPointer(ctx, data.Ipv6EnabledGroups, account.Settings.Ipv6EnabledGroups)
	ret.Append(d6...)
	data.Ipv6EnabledGroups = ipv6Groups
	data.LazyConnectionEnabled = types.BoolPointerValue(account.Settings.LazyConnectionEnabled)
	data.LocalMfaEnabled = types.BoolPointerValue(account.Settings.LocalMfaEnabled)
	data.MetricsPushEnabled = types.BoolPointerValue(account.Settings.MetricsPushEnabled)
	data.AutoUpdateAlways = types.BoolPointerValue(account.Settings.AutoUpdateAlways)
	data.PeerHostnameCollisionRejected = types.BoolPointerValue(account.Settings.PeerHostnameCollisionRejected)
	data.AgentNetworkOnly = types.BoolPointerValue(account.Settings.AgentNetworkOnly)
	features, df := dashboardFeaturesValue(account.Settings.DashboardFeatures)
	ret.Append(df...)
	data.DashboardFeatures = features
	data.UserApprovalRequired = types.BoolValue(account.Settings.Extra.UserApprovalRequired)
	logsGroups, d := sortedStringList(ctx, data.NetworkTrafficLogsGroups, account.Settings.Extra.NetworkTrafficLogsGroups)
	ret.Append(d...)
	data.NetworkTrafficLogsGroups = logsGroups
	data.PeerExposeEnabled = types.BoolValue(account.Settings.PeerExposeEnabled)
	data.PeerExposeGroups, d = sortedStringList(ctx, data.PeerExposeGroups, account.Settings.PeerExposeGroups)
	ret.Append(d...)
	return ret
}

// accountTerraformToAPI builds the update request for the account's settings.
// The management server builds the stored settings from the request alone and
// saves every column, so a field missing from the request is reset. The
// request therefore starts from the account's current settings, and fields
// this resource does not model keep their values.
func accountTerraformToAPI(ctx context.Context, account *api.Account, data AccountSettingsModel) api.AccountRequest {
	settings := account.Settings
	// Read-only in the API schema: the server computes them and does not read
	// them from a request.
	settings.EmbeddedIdpEnabled = nil
	settings.LocalAuthDisabled = nil

	var extra api.AccountExtraSettings
	if account.Settings.Extra != nil {
		extra = *account.Settings.Extra
	}
	extra.NetworkTrafficLogsEnabled = boolDefault(data.NetworkTrafficLogsEnabled, extra.NetworkTrafficLogsEnabled)
	extra.NetworkTrafficPacketCounterEnabled = boolDefault(data.NetworkTrafficPacketCounterEnabled, extra.NetworkTrafficPacketCounterEnabled)
	extra.PeerApprovalEnabled = boolDefault(data.PeerApprovalEnabled, extra.PeerApprovalEnabled)
	extra.UserApprovalRequired = boolDefault(data.UserApprovalRequired, extra.UserApprovalRequired)
	extra.NetworkTrafficLogsGroups = stringListDefault(ctx, data.NetworkTrafficLogsGroups, extra.NetworkTrafficLogsGroups)
	settings.Extra = &extra

	settings.GroupsPropagationEnabled = boolDefaultPointer(data.GroupsPropagationEnabled, account.Settings.GroupsPropagationEnabled)
	settings.JwtAllowGroups = stringListDefaultPointer(ctx, data.JwtAllowGroups, account.Settings.JwtAllowGroups)
	settings.JwtGroupsClaimName = stringDefaultPointer(data.JwtGroupsClaimName, account.Settings.JwtGroupsClaimName)
	settings.JwtGroupsEnabled = boolDefaultPointer(data.JwtGroupsEnabled, account.Settings.JwtGroupsEnabled)
	settings.PeerInactivityExpiration = int(int32Default(data.PeerInactivityExpiration, int32(account.Settings.PeerInactivityExpiration)))
	settings.PeerInactivityExpirationEnabled = boolDefault(data.PeerInactivityExpirationEnabled, account.Settings.PeerInactivityExpirationEnabled)
	settings.PeerLoginExpiration = int(int32Default(data.PeerLoginExpiration, int32(account.Settings.PeerLoginExpiration)))
	settings.PeerLoginExpirationEnabled = boolDefault(data.PeerLoginExpirationEnabled, account.Settings.PeerLoginExpirationEnabled)
	settings.RegularUsersViewBlocked = boolDefault(data.RegularUsersViewBlocked, account.Settings.RegularUsersViewBlocked)
	settings.RoutingPeerDnsResolutionEnabled = boolDefaultPointer(data.RoutingPeerDnsResolutionEnabled, account.Settings.RoutingPeerDnsResolutionEnabled)
	settings.AutoUpdateVersion = stringDefaultPointer(data.AutoUpdateVersion, account.Settings.AutoUpdateVersion)
	settings.DnsDomain = stringDefaultPointer(data.DnsDomain, account.Settings.DnsDomain)
	settings.NetworkRange = stringDefaultPointer(data.NetworkRange, account.Settings.NetworkRange)
	settings.NetworkRangeV6 = stringDefaultPointer(data.NetworkRangeV6, account.Settings.NetworkRangeV6)
	settings.Ipv6EnabledGroups = stringListDefaultPointer(ctx, data.Ipv6EnabledGroups, account.Settings.Ipv6EnabledGroups)
	settings.LazyConnectionEnabled = boolDefaultPointer(data.LazyConnectionEnabled, account.Settings.LazyConnectionEnabled)
	settings.LocalMfaEnabled = boolDefaultPointer(data.LocalMfaEnabled, account.Settings.LocalMfaEnabled)
	settings.MetricsPushEnabled = boolDefaultPointer(data.MetricsPushEnabled, account.Settings.MetricsPushEnabled)
	settings.AutoUpdateAlways = boolDefaultPointer(data.AutoUpdateAlways, account.Settings.AutoUpdateAlways)
	settings.PeerHostnameCollisionRejected = boolDefaultPointer(data.PeerHostnameCollisionRejected, account.Settings.PeerHostnameCollisionRejected)
	settings.AgentNetworkOnly = boolDefaultPointer(data.AgentNetworkOnly, account.Settings.AgentNetworkOnly)
	settings.DashboardFeatures = dashboardFeaturesRequest(data.DashboardFeatures, account.Settings.DashboardFeatures)
	settings.PeerExposeEnabled = boolDefault(data.PeerExposeEnabled, account.Settings.PeerExposeEnabled)
	settings.PeerExposeGroups = stringListDefault(ctx, data.PeerExposeGroups, account.Settings.PeerExposeGroups)

	return api.AccountRequest{Settings: settings}
}

func (r *AccountSettings) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AccountSettingsModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	var config AccountSettingsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	account, err := updateAccountSettings(ctx, r.client, "", nullUnconfigured(data, config))
	if err != nil {
		resp.Diagnostics.AddError("Error updating AccountSettings", err.Error())
		return
	}

	resp.Diagnostics.Append(accountAPIToTerraform(ctx, account, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AccountSettings) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AccountSettingsModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	accounts, err := r.client.Accounts.List(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error getting AccountSettings", err.Error())
		return
	}
	account := stateAccount(accounts, data.Id.ValueString())
	if account == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(accountAPIToTerraform(ctx, account, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AccountSettings) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AccountSettingsModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.Id.ValueString() == "" {
		r.Create(ctx, resource.CreateRequest{Config: req.Config, Plan: req.Plan, ProviderMeta: req.ProviderMeta}, (*resource.CreateResponse)(resp))
		return
	}

	var config AccountSettingsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	account, err := updateAccountSettings(ctx, r.client, data.Id.ValueString(), nullUnconfigured(data, config))
	if err != nil {
		resp.Diagnostics.AddError("Error updating AccountSettings", err.Error())
		return
	}

	resp.Diagnostics.Append(accountAPIToTerraform(ctx, account, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AccountSettings) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Do nothing
}

// currentAccountID is the import ID that stands for the account of the token.
const currentAccountID = "current"

// ImportState takes an account ID, or "current" for the token's account: the
// dashboard does not show account IDs. "current" needs the token to see
// exactly one account, so it never guesses between several.
func (r *AccountSettings) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != currentAccountID {
		resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
		return
	}
	accounts, err := r.client.Accounts.List(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing accounts", err.Error())
		return
	}
	if len(accounts) != 1 {
		resp.Diagnostics.AddError("Account ID required",
			fmt.Sprintf(`The token sees %d accounts, so "current" is ambiguous; import with the account ID instead.`, len(accounts)))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), accounts[0].Id)...)
}

// dashboardFeaturesValue maps the API's dashboard overrides; a missing object
// is null.
func dashboardFeaturesValue(f *api.AccountDashboardFeatures) (types.Object, diag.Diagnostics) {
	if f == nil {
		return types.ObjectNull(dashboardFeaturesTypes), nil
	}
	return types.ObjectValue(dashboardFeaturesTypes, map[string]attr.Value{"agent_network": types.BoolPointerValue(f.AgentNetwork)})
}

// dashboardFeaturesRequest keeps the current overrides unless the plan sets
// them; within the object a null or unknown member keeps its current value too.
func dashboardFeaturesRequest(planned types.Object, current *api.AccountDashboardFeatures) *api.AccountDashboardFeatures {
	if planned.IsNull() || planned.IsUnknown() {
		return current
	}
	out := api.AccountDashboardFeatures{}
	if current != nil {
		out = *current
	}
	if v, ok := planned.Attributes()["agent_network"].(types.Bool); ok {
		out.AgentNetwork = boolDefaultPointer(v, out.AgentNetwork)
	}
	return &out
}

// ValidateConfig rejects agent_network_only together with an explicitly hidden
// Agent Network menu, which the server refuses. A menu the configuration does
// not set may already be shown on the account; the server checks that case.
func (r *AccountSettings) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data AccountSettingsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.AgentNetworkOnly.IsNull() || data.AgentNetworkOnly.IsUnknown() || !data.AgentNetworkOnly.ValueBool() {
		return
	}
	if data.DashboardFeatures.IsNull() || data.DashboardFeatures.IsUnknown() {
		return
	}
	v, ok := data.DashboardFeatures.Attributes()["agent_network"].(types.Bool)
	if ok && !v.IsNull() && !v.IsUnknown() && !v.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("agent_network_only"), "Agent Network menu required",
			"agent_network_only = true cannot be combined with dashboard_features.agent_network = false.")
	}
}
