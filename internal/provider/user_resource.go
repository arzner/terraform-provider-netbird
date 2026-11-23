// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &User{}
var _ resource.ResourceWithImportState = &User{}
var _ resource.ResourceWithModifyPlan = &User{}

func NewUser() resource.Resource {
	return &User{}
}

// User defines the resource implementation.
type User struct {
	client *netbird.Client
}

// UserModel describes the resource data model.
type UserModel struct {
	Id            types.String `tfsdk:"id"`
	Email         types.String `tfsdk:"email"`
	Name          types.String `tfsdk:"name"`
	LastLogin     types.String `tfsdk:"last_login"`
	Role          types.String `tfsdk:"role"`
	Status        types.String `tfsdk:"status"`
	Issued        types.String `tfsdk:"issued"`
	AutoGroups    types.List   `tfsdk:"auto_groups"`
	IsCurrent     types.Bool   `tfsdk:"is_current"`
	IsServiceUser types.Bool   `tfsdk:"is_service_user"`
	IsBlocked     types.Bool   `tfsdk:"is_blocked"`
}

func (r *User) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *User) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "Invite and Manage Users",
		MarkdownDescription: "Invite and Manage Users, see [NetBird Docs](https://docs.netbird.io/how-to/add-users-to-your-network) for more information, existing users should be imported.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of a user",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "User Email. NetBird does not change a user's email: a change is refused at plan time.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "User Name. NetBird does not change a user's name, so a change is refused at plan time; for a service user under a new name, declare a new `netbird_user` (with new tokens) and remove the old one. A new regular user is an invitation and needs `name` and `email`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"last_login": schema.StringAttribute{
				MarkdownDescription: "User Last Login timedate",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"role": schema.StringAttribute{
				MarkdownDescription: "User's NetBird account role (owner|admin|user|billing_admin|auditor|network_admin).",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("user"),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.OneOf("owner", "admin", "user", "billing_admin", "auditor", "network_admin")},
			},
			"status": schema.StringAttribute{
				// No UseStateForUnknown: status tracks is_blocked, so keeping the
				// prior value through the plan made Terraform expect "active" while
				// the apply returned "blocked", which it reports as the provider
				// producing an inconsistent result. It has to be planned as unknown
				// because it is a value only the server can supply.
				MarkdownDescription: "User status (active, invited or blocked)",
				Computed:            true,
			},
			"issued": schema.StringAttribute{
				MarkdownDescription: "User issue method",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"auto_groups": schema.ListAttribute{
				MarkdownDescription: "Group IDs to auto-assign to peers registered by this user",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Default:             listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"is_current": schema.BoolAttribute{
				MarkdownDescription: "Set to true if the caller user is the same as the resource user",
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"is_service_user": schema.BoolAttribute{
				MarkdownDescription: "If set to true, creates a Service Account User. Left out, a new user is a regular user and an existing one keeps what it is, as for an imported identity provider user. NetBird does not change a user's kind, so a change is refused; declare a new `netbird_user` of the other kind and remove the old one.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"is_blocked": schema.BoolAttribute{
				MarkdownDescription: "If set to true then user is blocked and can't use the system",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *User) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// userAPIToTerraform copies an API user into the Terraform model, mapping absent optional fields to null.
func userAPIToTerraform(ctx context.Context, user *api.User, data *UserModel) diag.Diagnostics {
	var ret diag.Diagnostics
	data.Id = types.StringValue(user.Id)
	data.Name = types.StringValue(user.Name)
	data.LastLogin = types.StringNull()
	if user.LastLogin != nil {
		data.LastLogin = types.StringValue(user.LastLogin.Format(time.RFC3339))
	}
	data.Email = types.StringValue(user.Email)
	data.IsBlocked = types.BoolValue(user.IsBlocked)
	data.IsCurrent = types.BoolPointerValue(user.IsCurrent)
	// is_service_user: keep the prior value when the API omits it. Fresh
	// data source models are already null; the resource reads an absent
	// value as a regular user (userResourceAPIToTerraform).
	if user.IsServiceUser != nil {
		data.IsServiceUser = types.BoolValue(*user.IsServiceUser)
	}
	data.Issued = types.StringPointerValue(user.Issued)
	data.Role = types.StringValue(user.Role)
	data.Status = types.StringValue(string(user.Status))
	l, diag := sortedStringList(ctx, data.AutoGroups, user.AutoGroups)
	ret.Append(diag...)
	data.AutoGroups = l
	return ret
}

func (r *User) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data UserModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	userReq := api.UserCreateRequest{
		AutoGroups:    stringListDefault(ctx, data.AutoGroups, []string{}),
		IsServiceUser: data.IsServiceUser.ValueBool(),
		Role:          data.Role.ValueString(),
		Email:         stringDefaultPointer(data.Email, nil),
		Name:          stringDefaultPointer(data.Name, nil),
	}

	user, err := r.client.Users.Create(ctx, userReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating user", err.Error())
		return
	}

	resp.Diagnostics.Append(userResourceAPIToTerraform(ctx, user, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *User) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data UserModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	users, err := r.client.Users.List(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing users", err.Error())
		return
	}
	for _, u := range users {
		if u.Id == data.Id.ValueString() {
			resp.Diagnostics.Append(userResourceAPIToTerraform(ctx, &u, &data)...)

			if resp.Diagnostics.HasError() {
				return
			}

			// Save updated data into Terraform state
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			return
		}
	}

	resp.State.RemoveResource(ctx)
}

func (r *User) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data UserModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.Id.ValueString() == "" {
		r.Create(ctx, resource.CreateRequest{Config: req.Config, Plan: req.Plan, ProviderMeta: req.ProviderMeta}, (*resource.CreateResponse)(resp))
		return
	}

	// Values not known at plan time are checked now, before any write.
	var state UserModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d := immutableUserChanges(data, state); d.HasError() {
		resp.Diagnostics.Append(d...)
		return
	}

	user, err := r.client.Users.Update(ctx, data.Id.ValueString(), api.UserRequest{
		AutoGroups: stringListDefault(ctx, data.AutoGroups, []string{}),
		IsBlocked:  data.IsBlocked.ValueBool(),
		Role:       data.Role.ValueString(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Error updating User", err.Error())
		return
	}

	resp.Diagnostics.Append(userResourceAPIToTerraform(ctx, user, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If applicable, this is a great opportunity to initialize any necessary
	// provider client data and make a call using it.
	// httpResp, err := r.client.Do(httpReq)
	// if err != nil {
	//     resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update user, got error: %s", err))
	//     return
	// }

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *User) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data UserModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Users.Delete(ctx, data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting User", err.Error())
	}
}

func (r *User) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ModifyPlan refuses what the API cannot do. UserRequest (v0.83, v0.82)
// carries only role, auto_groups and is_blocked, so a change of name, email
// or kind is impossible; the create request has no block flag, NetBird
// refuses an owner as a service user or an invitation, and a new regular user
// is an invitation, which needs a name and an email.
func (r *User) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var config, plan, state UserModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if !req.State.Raw.IsNull() {
		// Unknown values are checked by the update, before it writes.
		resp.Diagnostics.Append(immutableUserChanges(plan, state)...)
		return
	}

	known := func(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }
	missing := func(v types.String) bool { return v.IsNull() || (known(v) && v.ValueString() == "") }
	service := config.IsServiceUser.Equal(types.BoolValue(true))
	// Left out, a new user is a regular user: plan it as such.
	if config.IsServiceUser.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("is_service_user"), types.BoolValue(false))...)
	}
	regular := config.IsServiceUser.IsNull() || config.IsServiceUser.Equal(types.BoolValue(false))
	if config.IsBlocked.Equal(types.BoolValue(true)) {
		resp.Diagnostics.AddAttributeError(path.Root("is_blocked"), "Cannot create a blocked user",
			"NetBird creates users unblocked (the create request has no block flag); create the user first, then set is_blocked = true.")
	}
	if config.Role.Equal(types.StringValue("owner")) {
		resp.Diagnostics.AddAttributeError(path.Root("role"), "Cannot create an owner",
			"NetBird refuses the owner role for a new service user or an invitation; create the user with another role and transfer ownership in NetBird.")
	}
	switch {
	case regular && (missing(config.Email) || missing(config.Name)):
		resp.Diagnostics.AddError("Name and email required",
			"A regular user is created as an invitation, which needs a name and an email; set both, or is_service_user = true for a service user.")
	case service && known(config.Email) && config.Email.ValueString() != "":
		resp.Diagnostics.AddAttributeError(path.Root("email"), "Service users have no email",
			"NetBird creates service users without an email; leave email out for a service user.")
	}
}

// immutableUserChanges reports a known planned name, email or kind that
// differs from the user's: NetBird changes none of them. Unknown values are
// left to the update, which sees them resolved.
func immutableUserChanges(plan, state UserModel) diag.Diagnostics {
	var d diag.Diagnostics
	changed := func(p, s attr.Value) bool { return !p.IsNull() && !p.IsUnknown() && !p.Equal(s) }
	if changed(plan.Email, state.Email) {
		d.AddAttributeError(path.Root("email"), "Email cannot be changed",
			"NetBird does not change a user's email. Keep the current email in the configuration, or leave it out.")
	}
	if changed(plan.Name, state.Name) {
		if state.IsServiceUser.ValueBool() {
			d.AddAttributeError(path.Root("name"), "Name cannot be changed",
				"NetBird does not change a service user's name. Keep the current name, or declare a new netbird_user under the new name (with new tokens) and remove this one.")
		} else {
			d.AddAttributeError(path.Root("name"), "Name cannot be changed",
				"A regular user's name comes from the identity provider and NetBird does not change it. Keep the current name in the configuration, or leave it out.")
		}
	}
	if changed(plan.IsServiceUser, state.IsServiceUser) {
		d.AddAttributeError(path.Root("is_service_user"), "User kind cannot be changed",
			"NetBird does not turn a regular user into a service user or back. Keep the current kind, or declare a new netbird_user of the other kind and remove this one.")
	}
	return d
}

// userResourceAPIToTerraform maps a user for the resource: an
// is_service_user the API omits (identity provider users) and no prior value
// for, as on import, is a regular user.
func userResourceAPIToTerraform(ctx context.Context, user *api.User, data *UserModel) diag.Diagnostics {
	prior := data.IsServiceUser
	d := userAPIToTerraform(ctx, user, data)
	// Compatibility: a response without the flag keeps a known kind and is
	// otherwise, as on import, a regular user.
	if user.IsServiceUser == nil {
		data.IsServiceUser = prior
		if prior.IsNull() || prior.IsUnknown() {
			data.IsServiceUser = types.BoolValue(false)
		}
	}
	return d
}
