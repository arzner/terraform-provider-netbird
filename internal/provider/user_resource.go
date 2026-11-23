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
				MarkdownDescription: "User Name. NetBird does not change a user's name: for a service user a new name replaces the user (its tokens are deleted with it), which needs `is_service_user = true` in the configuration, a name known at plan time, and a user other than the one the provider authenticates as; for a regular user, whose name comes from the identity provider, a change is refused at plan time. A new regular user is an invitation and needs `name` and `email`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIf(serviceUserRenamed,
						"A service user's name cannot be changed, so a new name replaces the user.",
						"A service user's name cannot be changed, so a new name replaces the user."),
				},
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
				MarkdownDescription: "If set to true, creates a Service Account User. Left out, a new user is a regular user and an existing one keeps what it is, as for an imported identity provider user. A configured change replaces the user.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
					boolplanmodifier.RequiresReplaceIfConfigured(),
				},
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

// serviceUserRenamed requires replacement when a service user's configured
// name changes: UserRequest (v0.83, v0.82) carries no name, and a service
// user's name is its own, so a new name means a new user.
func serviceUserRenamed(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	if req.State.Raw.IsNull() || req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || req.ConfigValue.Equal(req.StateValue) {
		return
	}
	var stateService, configService types.Bool
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("is_service_user"), &stateService)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("is_service_user"), &configService)...)
	// ModifyPlan refuses the other combinations; the replacement creates a
	// service user only when the configuration says so.
	resp.RequiresReplace = stateService.ValueBool() && configService.Equal(types.BoolValue(true))
}

// ModifyPlan refuses what the API cannot do or what would fail after the
// old user is deleted. UserRequest (v0.83, v0.82) carries only role,
// auto_groups and is_blocked, so a name or email change is either a
// replacement or impossible; a new regular user is an invitation, which needs
// a name and an email.
func (r *User) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var config, state UserModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	known := func(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }
	configuredService := config.IsServiceUser.Equal(types.BoolValue(true))
	// Left out, a new user is a regular user.
	configuredRegular := config.IsServiceUser.IsNull() || config.IsServiceUser.Equal(types.BoolValue(false))

	// A new user, or one replaced because its kind changes, is created as
	// the configuration says.
	creates := req.State.Raw.IsNull()
	kindChanges := !creates && !config.IsServiceUser.IsNull() && !config.IsServiceUser.IsUnknown() && !config.IsServiceUser.Equal(state.IsServiceUser)
	if creates || kindChanges {
		if configuredRegular {
			missing := func(v types.String) bool { return v.IsNull() || (known(v) && v.ValueString() == "") }
			if missing(config.Email) || missing(config.Name) {
				resp.Diagnostics.AddError("Name and email required",
					"A regular user is created as an invitation, which needs a name and an email; set both, or is_service_user = true for a service user.")
			}
		}
		return
	}

	if known(config.Email) && !config.Email.Equal(state.Email) {
		resp.Diagnostics.AddAttributeError(path.Root("email"), "Email cannot be changed",
			fmt.Sprintf("NetBird does not change a user's email (%q). Keep the current email in the configuration, or leave it out.", state.Email.ValueString()))
	}
	if config.Name.IsNull() || config.Name.Equal(state.Name) {
		return
	}
	switch {
	case !state.IsServiceUser.ValueBool() && config.Name.IsUnknown():
		// Whether it changes is known only at apply.
	case !state.IsServiceUser.ValueBool():
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Name cannot be changed",
			fmt.Sprintf("A regular user's name (%q) comes from the identity provider and NetBird does not change it. Keep the current name in the configuration, or leave it out.", state.Name.ValueString()))
	case config.Name.IsUnknown():
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Service user name not known",
			"A service user's new name replaces the user, so the plan needs the name to decide; it is not known until apply.")
	case !configuredService:
		resp.Diagnostics.AddAttributeError(path.Root("is_service_user"), "Service user kind required",
			"Renaming a service user replaces it, and the new user is created as the configuration says: set is_service_user = true, or keep the current name.")
	case state.IsCurrent.ValueBool():
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Cannot replace the current user",
			"This service user is the one the provider authenticates as, and NetBird refuses to delete it; rename it with another identity's token.")
	}
}

// userResourceAPIToTerraform maps a user for the resource: an
// is_service_user the API omits (identity provider users) and no prior value
// for, as on import, is a regular user.
func userResourceAPIToTerraform(ctx context.Context, user *api.User, data *UserModel) diag.Diagnostics {
	d := userAPIToTerraform(ctx, user, data)
	// Compatibility: a response without the flag is a regular user.
	if user.IsServiceUser == nil {
		data.IsServiceUser = types.BoolValue(false)
	}
	return d
}
