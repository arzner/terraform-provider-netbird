// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int32validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &Policy{}
var _ resource.ResourceWithImportState = &Policy{}
var _ resource.ResourceWithValidateConfig = &Policy{}

// sourcesWithoutAuthorizedUsers lists the source groups a non-empty
// authorized_groups has no entry for: the server requires one for each
// source of a netbird-ssh rule and accepts extra keys
// (policies_handler.go, v0.83.2 and v0.82.1). An empty map is not checked.
func sourcesWithoutAuthorizedUsers(sources *[]string, groups types.Map) []string {
	if sources == nil || groups.IsNull() || groups.IsUnknown() {
		return nil
	}
	entries := groups.Elements() // a copy on every call
	if len(entries) == 0 {
		return nil
	}
	var missing []string
	for _, s := range *sources {
		if _, ok := entries[s]; !ok {
			missing = append(missing, s)
		}
	}
	return missing
}

// ValidateConfig checks authorized_groups at plan time for the values
// already known: only for netbird-ssh, and an entry for every source.
func (r *Policy) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var rules types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("rule"), &rules)...)
	if resp.Diagnostics.HasError() || rules.IsNull() || rules.IsUnknown() {
		return
	}
	for i, e := range rules.Elements() {
		obj, ok := e.(types.Object)
		if !ok || obj.IsNull() || obj.IsUnknown() {
			continue
		}
		attrs := obj.Attributes()
		groups, _ := attrs["authorized_groups"].(types.Map)
		protocol, _ := attrs["protocol"].(types.String)
		if protocol.IsNull() {
			protocol = types.StringValue("all") // the schema default
		}
		if groups.IsNull() || groups.IsUnknown() {
			continue
		}
		at := path.Root("rule").AtListIndex(i).AtName("authorized_groups")
		for k, v := range groups.Elements() {
			if v.IsNull() {
				resp.Diagnostics.AddAttributeError(at.AtMapKey(k), "Local users required",
					"A null list would be stored as an empty one, which allows every local user; set the users, or [] to allow every local user.")
				continue
			}
			list, ok := v.(types.List)
			if !ok || list.IsUnknown() {
				continue
			}
			for j, u := range list.Elements() {
				if u.IsNull() {
					resp.Diagnostics.AddAttributeError(at.AtMapKey(k).AtListIndex(j), "Local user required",
						"A local user name cannot be null.")
				}
			}
		}
		if !protocol.IsUnknown() && protocol.ValueString() != "netbird-ssh" {
			resp.Diagnostics.AddAttributeError(at, "authorized_groups needs netbird-ssh",
				fmt.Sprintf("authorized_groups applies only to protocol netbird-ssh, and the server ignores it for %q.", protocol.ValueString()))
			continue
		}
		// With the protocol not known yet, only netbird-ssh can take the
		// map, so the source entries are checked already.
		sources, _ := attrs["sources"].(types.List)
		if sources.IsNull() || sources.IsUnknown() {
			continue
		}
		var known []string
		for _, s := range sources.Elements() {
			if str, ok := s.(types.String); ok && !str.IsNull() && !str.IsUnknown() {
				known = append(known, str.ValueString())
			}
		}
		if missing := sourcesWithoutAuthorizedUsers(&known, groups); len(missing) > 0 {
			resp.Diagnostics.AddAttributeError(at, "authorized_groups incomplete",
				fmt.Sprintf("The server needs an entry for every group in sources when authorized_groups is set; missing: %s.", strings.Join(missing, ", ")))
		}
	}
}

const portStringRegex = "^([0-9]{1,4}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5])$"

func NewPolicy() resource.Resource {
	return &Policy{}
}

// Policy defines the resource implementation.
type Policy struct {
	client *netbird.Client
}

// PolicyModel describes the resource data model.
type PolicyModel struct {
	Id                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	Description         types.String `tfsdk:"description"`
	Enabled             types.Bool   `tfsdk:"enabled"`
	SourcePostureChecks types.List   `tfsdk:"source_posture_checks"`
	Rules               types.List   `tfsdk:"rule"`
}

type PolicyRuleModel struct {
	Id                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	Description         types.String `tfsdk:"description"`
	Action              types.String `tfsdk:"action"`
	Protocol            types.String `tfsdk:"protocol"`
	Ports               types.List   `tfsdk:"ports"`
	PortRanges          types.List   `tfsdk:"port_ranges"`
	Enabled             types.Bool   `tfsdk:"enabled"`
	Bidirectional       types.Bool   `tfsdk:"bidirectional"`
	Sources             types.List   `tfsdk:"sources"`
	SourceResource      types.Object `tfsdk:"source_resource"`
	Destinations        types.List   `tfsdk:"destinations"`
	DestinationResource types.Object `tfsdk:"destination_resource"`
	AuthorizedGroups    types.Map    `tfsdk:"authorized_groups"`
}

func (p PolicyRuleModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"id":          types.StringType,
			"name":        types.StringType,
			"description": types.StringType,
			"action":      types.StringType,
			"protocol":    types.StringType,
			"ports": types.ListType{
				ElemType: types.StringType,
			},
			"port_ranges": types.ListType{
				ElemType: PolicyRulePortRangeModel{}.TFType(),
			},
			"enabled":       types.BoolType,
			"bidirectional": types.BoolType,
			"sources": types.ListType{
				ElemType: types.StringType,
			},
			"source_resource": PolicyRuleResourceModel{}.TFType(),
			"destinations": types.ListType{
				ElemType: types.StringType,
			},
			"destination_resource": PolicyRuleResourceModel{}.TFType(),
			"authorized_groups": types.MapType{
				ElemType: types.ListType{ElemType: types.StringType},
			},
		},
	}
}

type PolicyRuleResourceModel struct {
	Id   types.String `tfsdk:"id"`
	Type types.String `tfsdk:"type"`
}

func (p PolicyRuleResourceModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"id":   types.StringType,
			"type": types.StringType,
		},
	}
}

type PolicyRulePortRangeModel struct {
	Start types.Int32 `tfsdk:"start"`
	End   types.Int32 `tfsdk:"end"`
}

func (p PolicyRulePortRangeModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"start": types.Int32Type,
			"end":   types.Int32Type,
		},
	}
}

func (r *Policy) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy"
}

func (r *Policy) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "Create and Manage Policies",
		MarkdownDescription: "Create and Manage Policies, See [NetBird Docs](https://docs.netbird.io/how-to/manage-network-access#policies) for more information.",

		Blocks: map[string]schema.Block{
			"rule": schema.ListNestedBlock{
				Validators: []validator.List{listvalidator.SizeBetween(1, 1)},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Policy ID",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Policy Name",
							Required:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Policy description",
							Optional:            true,
						},
						"action": schema.StringAttribute{
							MarkdownDescription: "Policy Rule Action (accept|drop)",
							Optional:            true,
							Computed:            true,
							Default:             stringdefault.StaticString("accept"),
							Validators:          []validator.String{stringvalidator.OneOf("accept", "drop")},
						},
						"protocol": schema.StringAttribute{
							MarkdownDescription: "Policy Rule Protocol (tcp|udp|icmp|all|netbird-ssh)",
							Optional:            true,
							Computed:            true,
							Default:             stringdefault.StaticString("all"),
							Validators:          []validator.String{stringvalidator.OneOf("tcp", "udp", "icmp", "all", "netbird-ssh")},
						},
						"ports": schema.ListAttribute{
							MarkdownDescription: "Policy Rule Ports (mutually exclusive with port_ranges)",
							ElementType:         types.StringType,
							Optional:            true,
							Computed:            true,
							Validators:          []validator.List{listvalidator.ConflictsWith(path.MatchRelative().AtParent().AtName("port_ranges")), listvalidator.ValueStringsAre(stringvalidator.RegexMatches(regexp.MustCompile(portStringRegex), "Port outside range 0 to 65535"))},
						},
						"port_ranges": schema.ListNestedAttribute{
							MarkdownDescription: "Policy Rule Port Ranges (mutually exclusive with ports)",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"start": schema.Int32Attribute{
										Required:   true,
										Validators: []validator.Int32{int32validator.Between(0, 65535)},
									},
									"end": schema.Int32Attribute{
										Required:   true,
										Validators: []validator.Int32{int32validator.Between(0, 65535)},
									},
								},
							},
							Optional:   true,
							Computed:   true,
							Validators: []validator.List{listvalidator.ConflictsWith(path.MatchRelative().AtParent().AtName("ports"))},
						},
						"enabled": schema.BoolAttribute{
							MarkdownDescription: "Policy Rule Enabled",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(true),
						},
						"bidirectional": schema.BoolAttribute{
							MarkdownDescription: "Policy Rule Bidirectional",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(true),
						},
						"sources": schema.ListAttribute{
							MarkdownDescription: "Policy Rule Source Groups (mutually exclusive with source_resource)",
							ElementType:         types.StringType,
							Optional:            true,
							Computed:            true,
							Validators:          []validator.List{listvalidator.ConflictsWith(path.MatchRelative().AtParent().AtName("source_resource")), listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))},
						},
						"source_resource": schema.ObjectAttribute{
							MarkdownDescription: "Policy Rule Source Resource (mutually exclusive with sources)",
							AttributeTypes: map[string]attr.Type{
								"id":   types.StringType,
								"type": types.StringType,
							},
							Optional:   true,
							Computed:   true,
							Validators: []validator.Object{objectvalidator.ConflictsWith(path.MatchRelative().AtParent().AtName("sources"))},
						},
						"destinations": schema.ListAttribute{
							MarkdownDescription: "Policy Rule Destination Groups (mutually exclusive with destination_resource)",
							ElementType:         types.StringType,
							Optional:            true,
							Computed:            true,
							Validators:          []validator.List{listvalidator.ConflictsWith(path.MatchRelative().AtParent().AtName("destination_resource")), listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))},
						},
						"destination_resource": schema.ObjectAttribute{
							MarkdownDescription: "Policy Rule Destination Resource (mutually exclusive with destinations)",
							AttributeTypes: map[string]attr.Type{
								"id":   types.StringType,
								"type": types.StringType,
							},
							Optional:   true,
							Computed:   true,
							Validators: []validator.Object{objectvalidator.ConflictsWith(path.MatchRelative().AtParent().AtName("destinations"))},
						},
						"authorized_groups": schema.MapAttribute{
							MarkdownDescription: "Map of group IDs to the local users their members may log in as over SSH; an empty list allows every local user. When set and not empty, it needs an entry for every group in `sources`, and any other key grants its group's users access too. Not set or empty, every active NetBird user (not only those in `sources`) may log in as any local user, while the rule's sources still decide which peers can reach the destination; a temporary SSH access policy created by the server names its user itself, but an update through this resource drops that user (the policy API does not carry it), so do not manage such a policy here. Only applicable when protocol is `netbird-ssh`.",
							ElementType:         types.ListType{ElemType: types.StringType},
							Optional:            true,
							Computed:            true,
						},
					},
				},
			},
		},

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Policy ID",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Policy Name",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Policy Description",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Policy enabled",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"source_posture_checks": schema.ListAttribute{
				MarkdownDescription: "Posture checks associated with policy",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (r *Policy) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func policyAPIToTerraform(ctx context.Context, policy *api.Policy, data *PolicyModel) diag.Diagnostics {
	var ret diag.Diagnostics
	var diag diag.Diagnostics
	data.Id = types.StringValue(*policy.Id)
	data.Name = types.StringValue(policy.Name)
	if policy.Description == nil || *policy.Description == "" {
		data.Description = types.StringNull()
	} else {
		data.Description = types.StringValue(*policy.Description)
	}
	data.Enabled = types.BoolValue(policy.Enabled)
	data.SourcePostureChecks, diag = sortedStringList(ctx, data.SourcePostureChecks, policy.SourcePostureChecks)
	ret.Append(diag...)
	var rulesList []PolicyRuleModel
	for i, r := range policy.Rules {
		prior := listElementObject(data.Rules, i)
		ruleModel := PolicyRuleModel{
			Id:            types.StringValue(*r.Id),
			Name:          types.StringValue(r.Name),
			Action:        types.StringValue(string(r.Action)),
			Protocol:      types.StringValue(string(r.Protocol)),
			Enabled:       types.BoolValue(r.Enabled),
			Bidirectional: types.BoolValue(r.Bidirectional),
			Description:   types.StringPointerValue(r.Description),
		}
		if r.Description == nil || *r.Description == "" {
			ruleModel.Description = types.StringNull()
		}
		if r.Sources != nil {
			var sources []string
			for _, v := range *r.Sources {
				sources = append(sources, v.Id)
			}
			ruleModel.Sources, diag = sortedStringList(ctx, objectListAttr(prior, "sources"), sources)
			ret.Append(diag...)
		} else {
			ruleModel.Sources = types.ListNull(types.StringType)
		}
		if r.Destinations != nil {
			var destinations []string
			for _, v := range *r.Destinations {
				destinations = append(destinations, v.Id)
			}
			ruleModel.Destinations, diag = sortedStringList(ctx, objectListAttr(prior, "destinations"), destinations)
			ret.Append(diag...)
		} else {
			ruleModel.Destinations = types.ListNull(types.StringType)
		}
		if r.Ports != nil {
			ruleModel.Ports, diag = sortedStringList(ctx, objectListAttr(prior, "ports"), *r.Ports)
			ret.Append(diag...)
		} else {
			ruleModel.Ports = types.ListNull(types.StringType)
		}
		if r.PortRanges != nil {
			var portRanges []PolicyRulePortRangeModel
			for _, v := range *r.PortRanges {
				portRanges = append(portRanges, PolicyRulePortRangeModel{
					Start: types.Int32Value(int32(v.Start)),
					End:   types.Int32Value(int32(v.End)),
				})
			}
			ruleModel.PortRanges, diag = types.ListValueFrom(ctx, PolicyRulePortRangeModel{}.TFType(), portRanges)
			ret.Append(diag...)
			ruleModel.PortRanges, diag = sortedList(ctx, objectListAttr(prior, "port_ranges"), ruleModel.PortRanges)
			ret.Append(diag...)
		} else {
			ruleModel.PortRanges = types.ListNull(PolicyRulePortRangeModel{}.TFType())
		}
		if r.SourceResource != nil {
			ruleModel.SourceResource, diag = types.ObjectValueFrom(ctx, PolicyRuleResourceModel{}.TFType().AttrTypes, PolicyRuleResourceModel{
				Id:   types.StringValue(r.SourceResource.Id),
				Type: types.StringValue(string(r.SourceResource.Type)),
			})
			ret.Append(diag...)
		} else {
			ruleModel.SourceResource = types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes)
		}
		if r.DestinationResource != nil {
			ruleModel.DestinationResource, diag = types.ObjectValueFrom(ctx, PolicyRuleResourceModel{}.TFType().AttrTypes, PolicyRuleResourceModel{
				Id:   types.StringValue(r.DestinationResource.Id),
				Type: types.StringValue(string(r.DestinationResource.Type)),
			})
			ret.Append(diag...)
		} else {
			ruleModel.DestinationResource = types.ObjectNull(PolicyRuleResourceModel{}.TFType().AttrTypes)
		}
		mapElemType := types.ListType{ElemType: types.StringType}
		if r.AuthorizedGroups != nil && len(*r.AuthorizedGroups) > 0 {
			mapValues := map[string]attr.Value{}
			priorGroups, _ := prior.Attributes()["authorized_groups"].(types.Map)
			priorEntries := priorGroups.Elements()
			for k, v := range *r.AuthorizedGroups {
				priorList, _ := priorEntries[k].(types.List)
				listVal, d := sortedStringList(ctx, priorList, v)
				ret.Append(d...)
				mapValues[k] = listVal
			}
			ruleModel.AuthorizedGroups, diag = types.MapValue(mapElemType, mapValues)
			ret.Append(diag...)
		} else {
			// The server leaves an empty map out of the response: a planned
			// or stored empty map stays empty.
			ruleModel.AuthorizedGroups = types.MapNull(mapElemType)
			if priorGroups, ok := prior.Attributes()["authorized_groups"].(types.Map); ok && r.Protocol == api.PolicyRuleProtocolNetbirdSsh &&
				!priorGroups.IsNull() && !priorGroups.IsUnknown() && len(priorGroups.Elements()) == 0 {
				ruleModel.AuthorizedGroups = types.MapValueMust(mapElemType, map[string]attr.Value{})
			}
		}
		rulesList = append(rulesList, ruleModel)
	}

	data.Rules, diag = types.ListValueFrom(ctx, PolicyRuleModel{}.TFType(), rulesList)
	ret.Append(diag...)
	return ret
}

func policyRulesTerraformToAPI(ctx context.Context, data *PolicyModel) ([]api.PolicyRuleUpdate, diag.Diagnostics) {
	var rules []api.PolicyRuleUpdate
	var ret diag.Diagnostics
	for i, r := range data.Rules.Elements() {
		ruleObject, ok := r.(types.Object)
		if !ok {
			ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d] expected to be types.Object, found %T", i, r))
			return nil, ret
		}
		ruleAction, ok := ruleObject.Attributes()["action"].(types.String)
		if !ok {
			ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].action expected to be types.String, found %T", i, ruleObject.Attributes()["action"]))
			return nil, ret
		}
		ruleBidirectional, ok := ruleObject.Attributes()["bidirectional"].(types.Bool)
		if !ok {
			ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].bidirectional expected to be types.Bool, found %T", i, ruleObject.Attributes()["bidirectional"]))
			return nil, ret
		}
		ruleEnabled, ok := ruleObject.Attributes()["enabled"].(types.Bool)
		if !ok {
			ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].enabled expected to be types.Bool, found %T", i, ruleObject.Attributes()["enabled"]))
			return nil, ret
		}
		ruleName, ok := ruleObject.Attributes()["name"].(types.String)
		if !ok {
			ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].name expected to be types.String, found %T", i, ruleObject.Attributes()["name"]))
			return nil, ret
		}
		ruleProtocol, ok := ruleObject.Attributes()["protocol"].(types.String)
		if !ok {
			ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].protocol expected to be types.String, found %T", i, ruleObject.Attributes()["protocol"]))
			return nil, ret
		}
		ruleID, ok := ruleObject.Attributes()["id"].(types.String)
		if !ok {
			ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].id expected to be types.String, found %T", i, ruleObject.Attributes()["id"]))
			return nil, ret
		}
		rule := api.PolicyRuleUpdate{
			Id:            ruleID.ValueStringPointer(),
			Action:        api.PolicyRuleUpdateAction(ruleAction.ValueString()),
			Bidirectional: ruleBidirectional.ValueBool(),
			Enabled:       ruleEnabled.ValueBool(),
			Name:          ruleName.ValueString(),
			Protocol:      api.PolicyRuleUpdateProtocol(ruleProtocol.ValueString()),
		}
		if v, ok := ruleObject.Attributes()["description"].(types.String); ok && !v.IsNull() && !v.IsUnknown() {
			rule.Description = v.ValueStringPointer()
		}
		if v, ok := ruleObject.Attributes()["destination_resource"].(types.Object); ok && !v.IsNull() && !v.IsUnknown() {
			if _, ok := v.Attributes()["id"]; ok {
				rID, ok := v.Attributes()["id"].(types.String)
				if !ok {
					ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].destination_resource.id expected to be types.String, found %T", i, v.Attributes()["id"]))
					return nil, ret
				}
				rType, ok := v.Attributes()["type"].(types.String)
				if !ok {
					ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].destination_resource.type expected to be types.String, found %T", i, v.Attributes()["id"]))
					return nil, ret
				}
				rule.DestinationResource = &api.Resource{
					Id:   rID.ValueString(),
					Type: api.ResourceType(rType.ValueString()),
				}
			}
		}
		if v, ok := ruleObject.Attributes()["source_resource"].(types.Object); ok && !v.IsNull() && !v.IsUnknown() {
			if _, ok := v.Attributes()["id"]; ok {
				rID, ok := v.Attributes()["id"].(types.String)
				if !ok {
					ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].source_resource.id expected to be types.String, found %T", i, v.Attributes()["id"]))
					return nil, ret
				}
				rType, ok := v.Attributes()["type"].(types.String)
				if !ok {
					ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].source_resource.type expected to be types.String, found %T", i, v.Attributes()["id"]))
					return nil, ret
				}
				rule.SourceResource = &api.Resource{
					Id:   rID.ValueString(),
					Type: api.ResourceType(rType.ValueString()),
				}
			}
		}
		if v, ok := ruleObject.Attributes()["sources"].(types.List); ok && !v.IsNull() && !v.IsUnknown() {
			rule.Sources = stringListDefaultPointer(ctx, v, nil)
		}
		if v, ok := ruleObject.Attributes()["destinations"].(types.List); ok && !v.IsNull() && !v.IsUnknown() {
			rule.Destinations = stringListDefaultPointer(ctx, v, nil)
		}
		if v, ok := ruleObject.Attributes()["ports"].(types.List); ok && !v.IsNull() && !v.IsUnknown() {
			rule.Ports = stringListDefaultPointer(ctx, v, nil)
		}
		if v, ok := ruleObject.Attributes()["port_ranges"].(types.List); ok && !v.IsNull() && !v.IsUnknown() {
			portRanges := []api.RulePortRange{}
			for j, k := range v.Elements() {
				portRange, ok := k.(types.Object)
				if !ok {
					ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].port_ranges[%d] expected to be types.Object, found %T", i, j, k))
					return nil, ret
				}
				prStart, ok := portRange.Attributes()["start"].(types.Int32)
				if !ok {
					ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].port_ranges[%d].start expected to be types.Int32, found %T", i, j, portRange.Attributes()["start"]))
					return nil, ret
				}
				prEnd, ok := portRange.Attributes()["end"].(types.Int32)
				if !ok {
					ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].port_ranges[%d].end expected to be types.Int32, found %T", i, j, portRange.Attributes()["end"]))
					return nil, ret
				}
				portRanges = append(portRanges, api.RulePortRange{
					Start: int(prStart.ValueInt32()),
					End:   int(prEnd.ValueInt32()),
				})
			}
			rule.PortRanges = &portRanges
		}
		if v, ok := ruleObject.Attributes()["authorized_groups"].(types.Map); ok && !v.IsNull() && !v.IsUnknown() {
			if ruleProtocol.ValueString() != "netbird-ssh" {
				ret.AddError("Invalid Configuration", fmt.Sprintf(`rule[%d]: "authorized_groups" can only be set when protocol is "netbird-ssh"`, i))
				return nil, ret
			}
			if missing := sourcesWithoutAuthorizedUsers(rule.Sources, v); len(missing) > 0 {
				ret.AddError("Invalid Configuration", fmt.Sprintf(`rule[%d]: authorized_groups needs an entry for every group in "sources"; missing: %s`, i, strings.Join(missing, ", ")))
				return nil, ret
			}
			ag := make(map[string][]string)
			for k, val := range v.Elements() {
				listVal, ok := val.(types.List)
				if ok && listVal.IsNull() {
					ret.AddError("Invalid Configuration", fmt.Sprintf(`rule[%d]: authorized_groups[%q] is null, which the server would store as every local user`, i, k))
					return nil, ret
				}
				if !ok {
					ret.AddError("Unexpected Value", fmt.Sprintf("data.Rules[%d].authorized_groups[%s] expected to be types.List, found %T", i, k, val))
					return nil, ret
				}
				// A list that does not convert (a null user) must not be sent:
				// the server reads an empty list as every local user.
				var users []string
				if d := listVal.ElementsAs(ctx, &users, false); d.HasError() {
					ret.Append(d...)
					return nil, ret
				}
				ag[k] = users
			}
			rule.AuthorizedGroups = &ag
		}

		rules = append(rules, rule)
	}

	return rules, ret
}

func (r *Policy) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data PolicyModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	rules, d := policyRulesTerraformToAPI(ctx, &data)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	policyReq := api.PostApiPoliciesJSONRequestBody{
		Name:                data.Name.ValueString(),
		Description:         data.Description.ValueStringPointer(),
		Enabled:             data.Enabled.ValueBool(),
		SourcePostureChecks: stringListDefaultPointer(ctx, data.SourcePostureChecks, nil),
		Rules:               rules,
	}

	policy, err := r.client.Policies.Create(ctx, policyReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy", err.Error())
		return
	}

	resp.Diagnostics.Append(policyAPIToTerraform(ctx, policy, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *Policy) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data PolicyModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if data.Id.IsNull() {
		return
	}

	policy, err := r.client.Policies.Get(ctx, data.Id.ValueString())
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			resp.State.RemoveResource(ctx)
			return
		} else {
			resp.Diagnostics.AddError("Error getting Policy", err.Error())
		}
		return
	}

	resp.Diagnostics.Append(policyAPIToTerraform(ctx, policy, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *Policy) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data PolicyModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	rules, d := policyRulesTerraformToAPI(ctx, &data)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	policyReq := api.PolicyUpdate{
		Name:                data.Name.ValueString(),
		Description:         data.Description.ValueStringPointer(),
		Enabled:             data.Enabled.ValueBool(),
		SourcePostureChecks: stringListDefaultPointer(ctx, data.SourcePostureChecks, nil),
		Rules:               rules,
	}

	var policy *api.Policy
	var err error
	if data.Id.IsNull() || data.Id.IsUnknown() || data.Id.ValueString() == "" {
		policy, err = r.client.Policies.Create(ctx, policyReq)
	} else {
		policy, err = r.client.Policies.Update(ctx, data.Id.ValueString(), api.PutApiPoliciesPolicyIdJSONRequestBody(policyReq))
	}
	if err != nil {
		resp.Diagnostics.AddError("Error updating Policy", err.Error())
		return
	}

	resp.Diagnostics.Append(policyAPIToTerraform(ctx, policy, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *Policy) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data PolicyModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Policies.Delete(ctx, data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error deleting Policy", err.Error())
	}
}

func (r *Policy) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
