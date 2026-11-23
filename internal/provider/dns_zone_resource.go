// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	nbdomain "github.com/netbirdio/netbird/shared/management/domain"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &DNSZone{}
var _ resource.ResourceWithImportState = &DNSZone{}
var _ resource.ResourceWithModifyPlan = &DNSZone{}

func NewDNSZone() resource.Resource {
	return &DNSZone{}
}

// DNSZone defines the resource implementation.
type DNSZone struct {
	client *netbird.Client
}

// DNSZoneModel describes the resource data model.
type DNSZoneModel struct {
	Id                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Domain             types.String `tfsdk:"domain"`
	Enabled            types.Bool   `tfsdk:"enabled"`
	EnableSearchDomain types.Bool   `tfsdk:"enable_search_domain"`
	DistributionGroups types.Set    `tfsdk:"distribution_groups"`
}

func (r *DNSZone) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

func (r *DNSZone) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "Create and manage custom DNS zones",
		MarkdownDescription: "Create and manage custom DNS zones. See [NetBird Docs](https://docs.netbird.io/manage/dns/custom-zones) for more information.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "DNS Zone ID",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "DNS Zone name identifier",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "Zone domain (FQDN, no wildcard, no trailing dot; checked at plan time the way the server does). The server does not change the domain of a zone, so a new domain replaces the zone. The server deletes the old zone with all its records. A `netbird_dns_record` in the same configuration whose `zone_id` refers to this zone is created again in the new zone (its name has to move to the new domain too); any other record, added outside Terraform, with a literal zone ID or in another state, is lost. The plan does not check that the new domain is free: one used by another zone or by the peer DNS domain fails after the old zone is deleted, unless `lifecycle { create_before_destroy = true }` is set.",
				Required:            true,
				Validators:          []validator.String{zoneDomain{}},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "DNS Zone status",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"enable_search_domain": schema.BoolAttribute{
				MarkdownDescription: "Enable this zone as a search domain",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"distribution_groups": schema.SetAttribute{
				MarkdownDescription: "Group IDs that define groups of peers that will resolve this zone. Needed to create the zone, also when a new domain replaces it; left out on an existing zone, its groups are kept.",
				Validators:          []validator.Set{setvalidator.SizeAtLeast(1), setvalidator.NoNullValues(), setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))},
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *DNSZone) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func dnsZoneAPIToTerraform(ctx context.Context, zone *api.Zone, data *DNSZoneModel) diag.Diagnostics {
	var ret diag.Diagnostics
	data.Id = types.StringValue(zone.Id)
	data.Name = types.StringValue(zone.Name)
	data.Domain = types.StringValue(zone.Domain)
	data.Enabled = types.BoolValue(zone.Enabled)
	data.EnableSearchDomain = types.BoolValue(zone.EnableSearchDomain)

	distributionGroups, diag := types.SetValueFrom(ctx, types.StringType, zone.DistributionGroups)
	ret.Append(diag...)
	data.DistributionGroups = distributionGroups

	return ret
}

func (r *DNSZone) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data DNSZoneModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	enabled := data.Enabled.ValueBool()
	zoneReq := api.PostApiDnsZonesJSONRequestBody{
		Name:               data.Name.ValueString(),
		Domain:             data.Domain.ValueString(),
		Enabled:            &enabled,
		EnableSearchDomain: data.EnableSearchDomain.ValueBool(),
		DistributionGroups: stringSetDefault(ctx, data.DistributionGroups, []string{}),
	}

	zone, err := r.client.DNSZones.CreateZone(ctx, zoneReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating DNS zone", err.Error())
		return
	}

	resp.Diagnostics.Append(dnsZoneAPIToTerraform(ctx, zone, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DNSZone) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data DNSZoneModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	zone, err := r.client.DNSZones.GetZone(ctx, data.Id.ValueString())
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			resp.State.RemoveResource(ctx)
			return
		} else {
			resp.Diagnostics.AddError("Error getting DNS Zone", err.Error())
		}
		return
	}

	resp.Diagnostics.Append(dnsZoneAPIToTerraform(ctx, zone, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DNSZone) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data DNSZoneModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.Id.ValueString() == "" {
		r.Create(ctx, resource.CreateRequest{Config: req.Config, Plan: req.Plan, ProviderMeta: req.ProviderMeta}, (*resource.CreateResponse)(resp))
		return
	}

	enabled := data.Enabled.ValueBool()
	zoneReq := api.PutApiDnsZonesZoneIdJSONRequestBody{
		Name:               data.Name.ValueString(),
		Domain:             data.Domain.ValueString(),
		Enabled:            &enabled,
		EnableSearchDomain: data.EnableSearchDomain.ValueBool(),
		DistributionGroups: stringSetDefault(ctx, data.DistributionGroups, []string{}),
	}

	zone, err := r.client.DNSZones.UpdateZone(ctx, data.Id.ValueString(), zoneReq)
	if err != nil {
		resp.Diagnostics.AddError("Error updating DNS Zone", err.Error())
		return
	}

	resp.Diagnostics.Append(dnsZoneAPIToTerraform(ctx, zone, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DNSZone) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data DNSZoneModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DNSZones.DeleteZone(ctx, data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting DNS Zone", err.Error())
	}
}

func (r *DNSZone) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// zoneDomain checks a zone domain the way the server does (v0.83, v0.82:
// Zone.Validate, domain.IsValidDomainNoWildcard): a domain replacement
// deletes the old zone first, so a domain the server refuses must fail the
// plan instead.
type zoneDomain struct{}

func (zoneDomain) Description(context.Context) string {
	return "must be a domain name without a wildcard or a trailing dot"
}

func (v zoneDomain) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (zoneDomain) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if !nbdomain.IsValidDomainNoWildcard(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid zone domain",
			"The server refuses this zone domain: it must be a domain name without a wildcard or a trailing dot.")
	}
}

// ModifyPlan refuses a create, or a replacement for a new domain, without
// distribution_groups: the server needs at least one group to create a zone,
// and a replacement would delete the old zone before that create fails.
func (r *DNSZone) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var configured types.Set
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("distribution_groups"), &configured)...)
	if resp.Diagnostics.HasError() || !configured.IsNull() {
		return
	}
	creates := req.State.Raw.IsNull()
	if !creates {
		var planned, current types.String
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("domain"), &planned)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("domain"), &current)...)
		creates = !planned.IsUnknown() && !planned.Equal(current)
	}
	if creates {
		resp.Diagnostics.AddAttributeError(path.Root("distribution_groups"), "Distribution groups required",
			"Creating the zone, also to replace it for a new domain, needs at least one group in distribution_groups.")
	}
}
