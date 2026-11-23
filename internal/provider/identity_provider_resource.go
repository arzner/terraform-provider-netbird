// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &IdentityProvider{}
var _ resource.ResourceWithImportState = &IdentityProvider{}
var _ resource.ResourceWithValidateConfig = &IdentityProvider{}
var _ resource.ResourceWithModifyPlan = &IdentityProvider{}

func NewIdentityProvider() resource.Resource {
	return &IdentityProvider{}
}

// IdentityProvider defines the resource implementation.
type IdentityProvider struct {
	client *netbird.Client
}

// IdentityProviderModel describes the resource data model.
type IdentityProviderModel struct {
	Id           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	ClientId     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	Issuer       types.String `tfsdk:"issuer"`
}

func (r *IdentityProvider) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_identity_provider"
}

func (r *IdentityProvider) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "Create and manage Identity Providers",
		MarkdownDescription: "Create and manage Identity Providers, see [NetBird Docs](https://docs.netbird.io/selfhosted/identity-providers) for more information.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Identity provider ID",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name for the identity provider",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Type of identity provider (adfs, authentik, entra, google, keycloak, microsoft, oidc, okta, pocketid, zitadel). google and microsoft have a built-in issuer, used with `issuer = \"\"`; the others need an issuer URL. The server does not change the type of an existing provider, so a new type replaces it. The new provider has a new ID, which is part of each user's NetBird identity, so users who sign in through it are new users, without their old roles and groups. The old provider is deleted first, unless `lifecycle { create_before_destroy = true }` is set, which keeps the login available if the new provider fails to create.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf(identityProviderTypes...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "OAuth2 client ID",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "OAuth2 client secret",
				Required:            true,
				Sensitive:           true,
			},
			"issuer": schema.StringAttribute{
				MarkdownDescription: "OIDC issuer URL: https, with a host and without user info, query or fragment; checked at plan time. `\"\"` uses the built-in issuer of google and microsoft; the other types need a URL. The server also checks at apply time, on create and update, that the issuer answers.",
				Required:            true,
			},
		},
	}
}

func (r *IdentityProvider) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func identityProviderAPIToTerraform(idp *api.IdentityProvider, data *IdentityProviderModel) {
	data.Id = types.StringPointerValue(idp.Id)
	data.Name = types.StringValue(idp.Name)
	data.Type = types.StringValue(string(idp.Type))
	data.ClientId = types.StringValue(idp.ClientId)
	data.Issuer = types.StringValue(idp.Issuer)
}

func identityProviderTerraformToAPI(_ context.Context, data IdentityProviderModel) api.IdentityProviderRequest {
	return api.IdentityProviderRequest{
		Name:         data.Name.ValueString(),
		Type:         api.IdentityProviderType(data.Type.ValueString()),
		ClientId:     data.ClientId.ValueString(),
		ClientSecret: data.ClientSecret.ValueString(),
		Issuer:       data.Issuer.ValueString(),
	}
}

func (r *IdentityProvider) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data IdentityProviderModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	idpReq := identityProviderTerraformToAPI(ctx, data)

	idp, err := r.client.IdentityProviders.Create(ctx, idpReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating Identity Provider", err.Error())
		return
	}

	identityProviderAPIToTerraform(idp, &data)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IdentityProvider) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data IdentityProviderModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	idp, err := r.client.IdentityProviders.Get(ctx, data.Id.ValueString())
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error getting Identity Provider", err.Error())
		return
	}

	prior := data.Type
	identityProviderAPIToTerraform(idp, &data)
	resp.Diagnostics.Append(keepKnownType(prior, &data)...)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// keepKnownType keeps a provider's known type when the server reports
// another one for the same ID. The type of a provider cannot change under its
// ID, but the server infers it from any substring of the ID (v0.83, v0.82:
// inferOIDCProviderType, #86), so "keycloak-...okta..." reads as okta, and
// with type requiring replacement that misread would replace a working
// provider.
func keepKnownType(prior types.String, data *IdentityProviderModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if prior.IsNull() || prior.IsUnknown() || prior.Equal(data.Type) {
		return diags
	}
	// Only OIDC-based types are inferred; google and microsoft are read as
	// stored, so a change to or from them is real (a connector re-created
	// outside Terraform) and is kept.
	if !inferredType[prior.ValueString()] || !inferredType[data.Type.ValueString()] {
		return diags
	}
	diags.AddAttributeWarning(path.Root("type"), "Identity provider type misreported",
		fmt.Sprintf("The server reports type %q for this provider, which has type %q. NetBird infers the type from the provider's ID and can misread it (#86); the type is kept.", data.Type.ValueString(), prior.ValueString()))
	data.Type = prior
	return diags
}

func (r *IdentityProvider) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data IdentityProviderModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Id.IsNull() || data.Id.IsUnknown() || data.Id.ValueString() == "" {
		r.Create(ctx, resource.CreateRequest{Config: req.Config, Plan: req.Plan, ProviderMeta: req.ProviderMeta}, (*resource.CreateResponse)(resp))
		return
	}

	idpReq := identityProviderTerraformToAPI(ctx, data)

	idp, err := r.client.IdentityProviders.Update(ctx, data.Id.ValueString(), idpReq)
	if err != nil {
		resp.Diagnostics.AddError("Error updating Identity Provider", err.Error())
		return
	}

	planned := data.Type
	identityProviderAPIToTerraform(idp, &data)
	resp.Diagnostics.Append(keepKnownType(planned, &data)...)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IdentityProvider) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data IdentityProviderModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.IdentityProviders.Delete(ctx, data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting Identity Provider", err.Error())
	}
}

func (r *IdentityProvider) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// identityProviderTypes are the types the management server accepts (v0.83,
// v0.82: IdentityProviderType.IsValid). Test_identityProviderTypes checks
// them against the server source at the pinned version.
var identityProviderTypes = []string{"adfs", "authentik", "entra", "google", "keycloak", "microsoft", "oidc", "okta", "pocketid", "zitadel"}

// inferredType lists the types the server infers from the connector ID
// (v0.83, v0.82: inferOIDCProviderType) rather than reading them as stored.
var inferredType = map[string]bool{
	"oidc": true, "pocketid": true, "zitadel": true, "entra": true, "okta": true,
	"authentik": true, "keycloak": true, "adfs": true,
}

// builtInIssuer lists the types whose issuer the server knows (v0.83, v0.82:
// IdentityProviderType.HasBuiltInIssuer); every other type needs one.
var builtInIssuer = map[string]bool{"google": true, "microsoft": true}

// ValidateConfig refuses an empty issuer for a type without a built-in one,
// which the server rejects. A type change replaces the provider, so without
// this the plan would delete a working provider before a create that fails.
func (r *IdentityProvider) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var idpType, issuer types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("type"), &idpType)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("issuer"), &issuer)...)
	if resp.Diagnostics.HasError() || issuer.IsNull() || issuer.IsUnknown() {
		return
	}
	// The syntax rules hold for every type.
	if problem := issuerProblem(issuer.ValueString()); problem != "" {
		resp.Diagnostics.AddAttributeError(path.Root("issuer"), "Invalid issuer", problem)
	}
	if idpType.IsNull() || idpType.IsUnknown() {
		return
	}
	if issuer.ValueString() == "" && !builtInIssuer[idpType.ValueString()] {
		resp.Diagnostics.AddAttributeError(path.Root("issuer"), "Issuer required",
			fmt.Sprintf("type %q has no built-in issuer; set issuer to the provider's issuer URL. Only google and microsoft work with issuer = \"\".", idpType.ValueString()))
	}
}

// issuerProblem applies the server's syntax rules for a non-empty issuer
// (v0.83, v0.82: IdentityProvider.Validate): an https URL with a host, no
// user info, query or fragment. Whether the issuer answers is checked by the
// server only.
func issuerProblem(issuer string) string {
	if issuer == "" {
		return ""
	}
	u, err := url.Parse(issuer)
	switch {
	case err != nil || u.Host == "" || u.Hostname() == "":
		return "The issuer must be an absolute URL with a host."
	case !bracketsWellPlaced(issuer):
		return "The issuer's host has misplaced brackets; only an IPv6 address goes in brackets."
	case !validPort(u.Port()):
		return "The issuer's port must be between 1 and 65535."
	case u.Scheme != "https":
		return "The issuer must use https."
	case u.User != nil:
		return "The issuer must not contain user info."
	case strings.ContainsAny(issuer, "?#"):
		return "The issuer must not contain a query or a fragment."
	}
	return ""
}

// validPort reports whether a URL port is absent or a TCP port number.
func validPort(p string) bool {
	if p == "" {
		return true
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}

// bracketsWellPlaced reports whether the URL's authority has brackets only
// as one pair around the host (an IPv6 address); url.Parse reads the host
// from the last opening bracket and would hide extra ones.
func bracketsWellPlaced(raw string) bool {
	rest := raw
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	open, closing := strings.Count(rest, "["), strings.Count(rest, "]")
	if open == 0 && closing == 0 {
		return true
	}
	return open == 1 && closing == 1 && strings.HasPrefix(rest, "[") && strings.Index(rest, "]") > 0
}

// ModifyPlan checks the type and issuer the plan will send, which can differ
// from the configuration (ignore_changes on type keeps the stored type).
func (r *IdentityProvider) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var idpType, issuer types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("type"), &idpType)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("issuer"), &issuer)...)
	if resp.Diagnostics.HasError() || idpType.IsNull() || idpType.IsUnknown() || issuer.IsNull() || issuer.IsUnknown() {
		return
	}
	if issuer.ValueString() == "" && !builtInIssuer[idpType.ValueString()] {
		resp.Diagnostics.AddAttributeError(path.Root("issuer"), "Issuer required",
			fmt.Sprintf("The plan keeps type %q, which has no built-in issuer; set issuer to the provider's issuer URL.", idpType.ValueString()))
	}
}
