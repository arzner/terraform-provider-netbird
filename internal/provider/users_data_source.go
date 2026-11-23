// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &UsersDataSource{}

// NewUsersDataSource returns the netbird_users data source.
func NewUsersDataSource() datasource.DataSource {
	return &UsersDataSource{}
}

// UsersModel describes the data source data model.
type UsersModel struct {
	Name          types.String `tfsdk:"name"`
	Email         types.String `tfsdk:"email"`
	Role          types.String `tfsdk:"role"`
	Status        types.String `tfsdk:"status"`
	Issued        types.String `tfsdk:"issued"`
	IsServiceUser types.Bool   `tfsdk:"is_service_user"`
	IsBlocked     types.Bool   `tfsdk:"is_blocked"`
	AutoGroups    types.List   `tfsdk:"auto_groups"`
	Ids           types.List   `tfsdk:"ids"`
	Users         []UserModel  `tfsdk:"users"`
}

// UsersDataSource defines the data source implementation.
type UsersDataSource struct {
	client *netbird.Client
}

// Metadata sets the data source type name.
func (d *UsersDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_users"
}

// Schema defines the netbird_users filters and computed attributes.
func (d *UsersDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "List Users matching all the given filters",
		MarkdownDescription: "List Users matching all the given filters (at least one). Unlike `netbird_user`, matching nothing is not an error.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Only return users with this name",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "Only return the user with this email",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"role": schema.StringAttribute{
				MarkdownDescription: "Only return users with this role (`owner`, `admin`, `user`, `billing_admin`, `auditor`, `network_admin`, `agent_network_admin` or `usage_viewer`)",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.OneOf(userRoles...)},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Only return users with this status (`active`, `invited` or `blocked`). With an external identity provider, NetBird reports a user the provider does not return as `active` and not blocked, so `status`, `is_blocked` and `issued` reflect what NetBird reports.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.OneOf("active", "invited", "blocked")},
			},
			"issued": schema.StringAttribute{
				MarkdownDescription: "Only return users issued by this source (`api` or `integration`)",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.OneOf("api", "integration")},
			},
			"is_service_user": schema.BoolAttribute{
				MarkdownDescription: "Only return service users (`true`) or regular users (`false`)",
				Optional:            true,
			},
			"is_blocked": schema.BoolAttribute{
				MarkdownDescription: "Only return blocked (`true`) or unblocked (`false`) users",
				Optional:            true,
			},
			"auto_groups": schema.ListAttribute{
				MarkdownDescription: "Only return users having all these auto group IDs, even if they have more",
				ElementType:         types.StringType,
				Optional:            true,
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.NoNullValues(),
					listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"ids": schema.ListAttribute{
				MarkdownDescription: "IDs of the matching users",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"users": schema.ListNestedAttribute{
				MarkdownDescription: "Matching users",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "User ID",
							Computed:            true,
						},
						"email": schema.StringAttribute{
							MarkdownDescription: "User email",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "User name",
							Computed:            true,
						},
						"last_login": schema.StringAttribute{
							MarkdownDescription: "User last login",
							Computed:            true,
						},
						"role": schema.StringAttribute{
							MarkdownDescription: "User's NetBird account role",
							Computed:            true,
						},
						"status": schema.StringAttribute{
							MarkdownDescription: "User's status",
							Computed:            true,
						},
						"issued": schema.StringAttribute{
							MarkdownDescription: "User issue method",
							Computed:            true,
						},
						"auto_groups": schema.ListAttribute{
							MarkdownDescription: "Group IDs to auto-assign to peers registered by this user",
							ElementType:         types.StringType,
							Computed:            true,
						},
						"is_current": schema.BoolAttribute{
							MarkdownDescription: "Set to true if the caller user is the same as the one returned",
							Computed:            true,
						},
						"is_service_user": schema.BoolAttribute{
							MarkdownDescription: "If set to true then user is a service user",
							Computed:            true,
						},
						"is_blocked": schema.BoolAttribute{
							MarkdownDescription: "If set to true then user is blocked and can't use the system",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

// Configure stores the NetBird API client provided by the provider.
func (d *UsersDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*netbird.Client)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *netbird.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = client
}

// filterUsers keeps the users matching every configured filter.
func filterUsers(ctx context.Context, users []api.User, data UsersModel) ([]api.User, diag.Diagnostics) {
	var d diag.Diagnostics
	filtered := make([]api.User, 0, len(users))
	// Decoded once, not per user.
	var wantGroups []string
	if !data.AutoGroups.IsNull() && !data.AutoGroups.IsUnknown() {
		d.Append(data.AutoGroups.ElementsAs(ctx, &wantGroups, false)...)
		if d.HasError() {
			return filtered, d
		}
	}
	for _, u := range users {
		issued := ""
		if u.Issued != nil {
			issued = *u.Issued
		}
		// A user without the flag matches neither explicit selector.
		serviceUser := -1000
		if u.IsServiceUser != nil {
			serviceUser = matchBool(*u.IsServiceUser, data.IsServiceUser)
		} else if data.IsServiceUser.IsNull() || data.IsServiceUser.IsUnknown() {
			serviceUser = 0
		}
		match := matchString(u.Name, data.Name) +
			matchString(u.Email, data.Email) +
			matchString(u.Role, data.Role) +
			matchString(string(u.Status), data.Status) +
			matchString(issued, data.Issued) +
			serviceUser +
			matchBool(u.IsBlocked, data.IsBlocked)
		if match < 0 {
			continue
		}
		if wantGroups != nil && !containsAll(u.AutoGroups, wantGroups) {
			continue
		}
		filtered = append(filtered, u)
	}
	return filtered, d
}

// Read lists all users, applies the configured filters, and sets the state.
func (d *UsersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data UsersModel

	// Read Terraform configuration data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// ConfigValidators refuses a block without selectors at plan time; a
	// selector known only at apply time can still turn out null.
	if knownCount(
		data.Name, data.Email, data.Role, data.Status, data.Issued,
		data.IsServiceUser, data.IsBlocked, data.AutoGroups,
	) == 0 {
		resp.Diagnostics.AddError(
			"No selector",
			"Must add at least one of (name, email, role, status, issued, is_service_user, is_blocked, auto_groups)",
		)
		return
	}

	users, err := listUsers(ctx, d.client, data.IsServiceUser)
	if err != nil {
		resp.Diagnostics.AddError("Error listing users", err.Error())
		return
	}

	filtered, di := filterUsers(ctx, users, data)
	resp.Diagnostics.Append(di...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(usersAPIToTerraform(ctx, filtered, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// usersAPIToTerraform converts API users into the Terraform users list.
//
// The API returns users in no defined order (the server iterates a map), so
// they are sorted by ID: an unchanged account gives the same list, and the
// same index, on every read.
func usersAPIToTerraform(ctx context.Context, users []api.User, data *UsersModel) diag.Diagnostics {
	var ret diag.Diagnostics
	users = slices.Clone(users)
	slices.SortFunc(users, func(a, b api.User) int { return strings.Compare(a.Id, b.Id) })
	ids := make([]string, len(users))
	data.Users = make([]UserModel, len(users))
	for i := range users {
		ids[i] = users[i].Id
		ret.Append(userAPIToTerraform(ctx, &users[i], &data.Users[i])...)
	}
	l, diag := types.ListValueFrom(ctx, types.StringType, ids)
	ret.Append(diag...)
	data.Ids = l
	return ret
}

// userRoles are the roles NetBird knows (v0.83, v0.82: StrRoleToUserRole);
// the server refuses any other.
var userRoles = []string{"owner", "admin", "user", "billing_admin", "auditor", "network_admin", "agent_network_admin", "usage_viewer"}

// maxUsersResponse bounds what is read from the users endpoint.
const maxUsersResponse = 64 << 20

// listUsers lists the account's users. With is_service_user set, the server
// filters on it (GET /api/users?service_user=...), so an account with many
// users of the other kind does not have to be downloaded.
func listUsers(ctx context.Context, client *netbird.Client, serviceUser types.Bool) ([]api.User, error) {
	if serviceUser.IsNull() || serviceUser.IsUnknown() {
		return client.Users.List(ctx)
	}
	resp, err := client.NewRequest(ctx, http.MethodGet, "/api/users", nil,
		map[string]string{"service_user": strconv.FormatBool(serviceUser.ValueBool())})
	if err != nil {
		return nil, err
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("empty response, HTTP status %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUsersResponse+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxUsersResponse {
		return nil, fmt.Errorf("users response larger than %d bytes", maxUsersResponse)
	}
	var users []api.User
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, fmt.Errorf("decoding the users list: %w", err)
	}
	return users, nil
}

// containsAll reports whether have holds every element of want.
func containsAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

// ConfigValidators refuses a block without any selector at plan time, also
// when the read is deferred to apply; a selector whose value is not known
// yet counts as set.
func (d *UsersDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{datasourcevalidator.AtLeastOneOf(
		path.MatchRoot("name"), path.MatchRoot("email"), path.MatchRoot("role"), path.MatchRoot("status"),
		path.MatchRoot("issued"), path.MatchRoot("is_service_user"), path.MatchRoot("is_blocked"), path.MatchRoot("auto_groups"),
	)}
}

var _ datasource.DataSourceWithConfigValidators = &UsersDataSource{}
