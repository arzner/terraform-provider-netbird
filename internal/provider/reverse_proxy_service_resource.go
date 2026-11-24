package provider

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// NewReverseProxyService creates a new reverse proxy service resource.
func NewReverseProxyService() resource.Resource {
	return &ReverseProxyService{}
}

// ReverseProxyService defines the resource implementation.
type ReverseProxyService struct {
	client *netbird.Client
}

// ReverseProxyServiceModel describes the resource data model.
type ReverseProxyServiceModel struct {
	Id                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Domain             types.String `tfsdk:"domain"`
	Mode               types.String `tfsdk:"mode"`
	ListenPort         types.Int64  `tfsdk:"listen_port"`
	PortAutoAssigned   types.Bool   `tfsdk:"port_auto_assigned"`
	Enabled            types.Bool   `tfsdk:"enabled"`
	PassHostHeader     types.Bool   `tfsdk:"pass_host_header"`
	RewriteRedirects   types.Bool   `tfsdk:"rewrite_redirects"`
	ProxyCluster       types.String `tfsdk:"proxy_cluster"`
	Private            types.Bool   `tfsdk:"private"`
	AccessGroups       types.List   `tfsdk:"access_groups"`
	Targets            types.List   `tfsdk:"targets"`
	Auth               types.Object `tfsdk:"auth"`
	AccessRestrictions types.Object `tfsdk:"access_restrictions"`
}

// ReverseProxyServiceTargetModel describes a service target.
type ReverseProxyServiceTargetModel struct {
	TargetId     types.String `tfsdk:"target_id"`
	TargetType   types.String `tfsdk:"target_type"`
	Host         types.String `tfsdk:"host"`
	Port         types.Int64  `tfsdk:"port"`
	Protocol     types.String `tfsdk:"protocol"`
	Path         types.String `tfsdk:"path"`
	Enabled      types.Bool   `tfsdk:"enabled"`
	AccessAction types.String `tfsdk:"access_action"`
	Options      types.Object `tfsdk:"options"`
}

// TFType returns the Terraform object type for service targets.
func (m ReverseProxyServiceTargetModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"target_id":     types.StringType,
			"target_type":   types.StringType,
			"host":          types.StringType,
			"port":          types.Int64Type,
			"protocol":      types.StringType,
			"path":          types.StringType,
			"enabled":       types.BoolType,
			"access_action": types.StringType,
			"options":       ReverseProxyTargetOptionsModel{}.TFType(),
		},
	}
}

// ReverseProxyTargetOptionsModel describes per-target options.
type ReverseProxyTargetOptionsModel struct {
	SkipTLSVerify      types.Bool   `tfsdk:"skip_tls_verify"`
	RequestTimeout     types.String `tfsdk:"request_timeout"`
	PathRewrite        types.String `tfsdk:"path_rewrite"`
	CustomHeaders      types.Map    `tfsdk:"custom_headers"`
	ProxyProtocol      types.Bool   `tfsdk:"proxy_protocol"`
	SessionIdleTimeout types.String `tfsdk:"session_idle_timeout"`
	DirectUpstream     types.Bool   `tfsdk:"direct_upstream"`
}

// TFType returns the Terraform object type for target options.
func (m ReverseProxyTargetOptionsModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"skip_tls_verify": types.BoolType,
			"request_timeout": types.StringType,
			"path_rewrite":    types.StringType,
			"custom_headers": types.MapType{
				ElemType: types.StringType,
			},
			"proxy_protocol":       types.BoolType,
			"session_idle_timeout": types.StringType,
			"direct_upstream":      types.BoolType,
		},
	}
}

// ReverseProxyServiceAuthModel describes the auth config.
type ReverseProxyServiceAuthModel struct {
	PasswordAuth types.Object `tfsdk:"password_auth"`
	PinAuth      types.Object `tfsdk:"pin_auth"`
	BearerAuth   types.Object `tfsdk:"bearer_auth"`
	LinkAuth     types.Object `tfsdk:"link_auth"`
	HeaderAuths  types.List   `tfsdk:"header_auths"`
}

// TFType returns the Terraform object type for service auth.
func (m ReverseProxyServiceAuthModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"password_auth": ReverseProxyPasswordAuthModel{}.TFType(),
			"pin_auth":      ReverseProxyPinAuthModel{}.TFType(),
			"bearer_auth":   ReverseProxyBearerAuthModel{}.TFType(),
			"link_auth":     ReverseProxyLinkAuthModel{}.TFType(),
			"header_auths":  types.ListType{ElemType: ReverseProxyHeaderAuthModel{}.TFType()},
		},
	}
}

// ReverseProxyPasswordAuthModel describes password auth config.
type ReverseProxyPasswordAuthModel struct {
	Enabled  types.Bool   `tfsdk:"enabled"`
	Password types.String `tfsdk:"password"`
}

// TFType returns the Terraform object type for password auth.
func (m ReverseProxyPasswordAuthModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"enabled":  types.BoolType,
			"password": types.StringType,
		},
	}
}

// ReverseProxyPinAuthModel describes PIN auth config.
type ReverseProxyPinAuthModel struct {
	Enabled types.Bool   `tfsdk:"enabled"`
	Pin     types.String `tfsdk:"pin"`
}

// TFType returns the Terraform object type for PIN auth.
func (m ReverseProxyPinAuthModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"enabled": types.BoolType,
			"pin":     types.StringType,
		},
	}
}

// ReverseProxyBearerAuthModel describes bearer auth config.
type ReverseProxyBearerAuthModel struct {
	Enabled            types.Bool `tfsdk:"enabled"`
	DistributionGroups types.List `tfsdk:"distribution_groups"`
}

// TFType returns the Terraform object type for bearer auth.
func (m ReverseProxyBearerAuthModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"enabled": types.BoolType,
			"distribution_groups": types.ListType{
				ElemType: types.StringType,
			},
		},
	}
}

// ReverseProxyLinkAuthModel describes link auth config.
type ReverseProxyLinkAuthModel struct {
	Enabled types.Bool `tfsdk:"enabled"`
}

// TFType returns the Terraform object type for link auth.
func (m ReverseProxyLinkAuthModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"enabled": types.BoolType,
		},
	}
}

// ReverseProxyHeaderAuthModel describes header auth config.
type ReverseProxyHeaderAuthModel struct {
	Enabled types.Bool   `tfsdk:"enabled"`
	Header  types.String `tfsdk:"header"`
	Value   types.String `tfsdk:"value"`
}

// TFType returns the Terraform object type for header auth.
func (m ReverseProxyHeaderAuthModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"enabled": types.BoolType,
			"header":  types.StringType,
			"value":   types.StringType,
		},
	}
}

// ReverseProxyAccessRestrictionsModel describes access restrictions.
type ReverseProxyAccessRestrictionsModel struct {
	AllowedCidrs     types.List   `tfsdk:"allowed_cidrs"`
	BlockedCidrs     types.List   `tfsdk:"blocked_cidrs"`
	AllowedCountries types.List   `tfsdk:"allowed_countries"`
	BlockedCountries types.List   `tfsdk:"blocked_countries"`
	CrowdsecMode     types.String `tfsdk:"crowdsec_mode"`
}

// TFType returns the Terraform object type for access restrictions.
func (m ReverseProxyAccessRestrictionsModel) TFType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"allowed_cidrs":     types.ListType{ElemType: types.StringType},
			"blocked_cidrs":     types.ListType{ElemType: types.StringType},
			"allowed_countries": types.ListType{ElemType: types.StringType},
			"blocked_countries": types.ListType{ElemType: types.StringType},
			"crowdsec_mode":     types.StringType,
		},
	}
}

func (r *ReverseProxyService) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_reverse_proxy_service"
}

func (r *ReverseProxyService) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "Create and manage Reverse Proxy Services",
		MarkdownDescription: "Create and manage Reverse Proxy Services for the NetBird reverse proxy.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Service ID",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Service name",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "Domain for the service",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"mode": schema.StringAttribute{
				MarkdownDescription: "Service mode: \"http\" for L7 reverse proxy, \"tcp\"/\"udp\"/\"tls\" for L4 passthrough. Left out, a new service is HTTP and an existing one keeps its mode. The server does not change the mode of a service: a change is a plan warning and the update stops before writing; replace the service instead (`-replace`).",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.OneOf("http", "tcp", "udp", "tls")},
				// Sent on every update: a request without a mode is validated
				// as HTTP, which an L4 service refuses. A null stored mode
				// (state from before the attribute) stays unknown, and the
				// update takes the live one.
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseNonNullStateForUnknown()},
			},
			"listen_port": schema.Int64Attribute{
				MarkdownDescription: "Port the proxy listens on (L4/TLS only). Set to 0 for auto-assignment.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.Int64{int64validator.Between(0, 65535)},
			},
			"port_auto_assigned": schema.BoolAttribute{
				MarkdownDescription: "Whether the listen port was auto-assigned by the server",
				Computed:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the service is enabled",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"pass_host_header": schema.BoolAttribute{
				MarkdownDescription: "When true, the original client Host header is passed through to the backend",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"rewrite_redirects": schema.BoolAttribute{
				MarkdownDescription: "When true, Location headers in backend responses are rewritten to replace the backend address with the public-facing domain",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"proxy_cluster": schema.StringAttribute{
				MarkdownDescription: "The proxy cluster handling this service (derived from domain)",
				Computed:            true,
			},
			"private": schema.BoolAttribute{
				MarkdownDescription: "When true, the service is reachable only over the NetBird tunnel: peers authenticate by their tunnel identity and the server generates an access policy from `access_groups` to the cluster's proxy peers. The proxy then skips the service's other authentication (SSO, password, PIN, header auth), even where `auth` still enables it. CIDR access restrictions still apply, matched against the peer's NetBird address; country restrictions do not. Peers reach it only through a proxy of the cluster that has private access enabled (`NB_PROXY_PRIVATE=true` or `--private`, off by default); the server accepts the setting without one. Requires `mode = \"http\"`, at least one access group, no bearer auth and no target with the `bypass` access action, including one a target keeps from the server while its `access_action` is left out. Left out, a new service is public and an existing one keeps its setting.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseNonNullStateForUnknown()},
			},
			"access_groups": schema.ListAttribute{
				MarkdownDescription: "Group IDs whose peers may reach the private service over the tunnel. Required when `private` is true; ignored otherwise. A proxy reuses a peer's granted access for up to five minutes by default (`NB_PROXY_TUNNEL_CACHE_TTL`), so removing a group, or turning a public service private, can take that long to stop new requests from a peer the proxy already admitted and that another policy still lets reach the proxy; responses already in progress are not cut off. IDs are not checked for existence: when a service turns private with the groups left out, it keeps the IDs it has, and an ID of a deleted group grants nothing.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseNonNullStateForUnknown()},
				Validators: []validator.List{
					listvalidator.NoNullValues(),
					listvalidator.ValueStringsAre(notBlank{}),
				},
			},
			"targets": schema.ListNestedAttribute{
				MarkdownDescription: "List of target backends for this service",
				Required:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"target_id": schema.StringAttribute{
							MarkdownDescription: "Target ID (resource or peer ID)",
							Required:            true,
						},
						"target_type": schema.StringAttribute{
							MarkdownDescription: "Target type (peer, host, domain, subnet)",
							Required:            true,
							Validators:          []validator.String{stringvalidator.OneOf("peer", "host", "domain", "subnet")},
						},
						"host": schema.StringAttribute{
							MarkdownDescription: "Backend IP or domain for this target. If omitted, the API resolves it from the target peer.",
							Optional:            true,
							Computed:            true,
						},
						"port": schema.Int64Attribute{
							MarkdownDescription: "Backend port for this target (0 for scheme default)",
							Required:            true,
							Validators:          []validator.Int64{int64validator.Between(0, 65535)},
						},
						"protocol": schema.StringAttribute{
							MarkdownDescription: "Protocol to use when connecting to the backend (http, https for HTTP mode; tcp, udp for L4 mode)",
							Required:            true,
							Validators:          []validator.String{stringvalidator.OneOf("http", "https", "tcp", "udp")},
						},
						"path": schema.StringAttribute{
							MarkdownDescription: "URL path prefix for this target. Left out, no path is sent and the server uses \"/\"; the plan shows null, also for a target that has another path now.",
							Optional:            true,
							Computed:            true,
						},
						"enabled": schema.BoolAttribute{
							MarkdownDescription: "Whether this target is enabled",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(true),
						},
						"access_action": schema.StringAttribute{
							MarkdownDescription: "Access action once this target's path prefix is selected (HTTP services only): `inherit` applies the service's authentication, `bypass` skips it (not allowed for private services), `block` denies access. Left out, the server keeps the current action of the target at this path (`inherit` for a new one). While a current target has `bypass` or `block`, an update that sets no action on any target must keep that target's path, and if two current targets share a path, every target of the update must set its action. The plan warns about both, since it cannot tell an update from a replacement, and an in-place update stops before writing anything. `bypass` and `block` are refused for TCP, UDP and TLS services, need a canonical path prefix, and need each enabled target on its own path. Once any enabled target has `bypass` or `block`, the proxy checks request paths strictly for the whole service, also for targets that inherit: a request whose path has `//`, `\\`, `;`, dot segments, control characters or an escaped `/`, `\\`, `.` or `%` gets HTTP 400.",
							Optional:            true,
							Computed:            true,
							Validators:          []validator.String{stringvalidator.OneOf("inherit", "bypass", "block")},
						},
						"options": schema.SingleNestedAttribute{
							MarkdownDescription: "Per-target options",
							Optional:            true,
							Attributes: map[string]schema.Attribute{
								"skip_tls_verify": schema.BoolAttribute{
									MarkdownDescription: "Skip TLS certificate verification for this backend (HTTPS targets only)",
									Optional:            true,
								},
								"request_timeout": schema.StringAttribute{
									MarkdownDescription: "Per-target response timeout as a Go duration string (e.g. \"30s\", \"2m\")",
									Optional:            true,
								},
								"path_rewrite": schema.StringAttribute{
									MarkdownDescription: "Controls how the request path is rewritten before forwarding. Default strips the matched prefix. \"preserve\" keeps the full original path. (HTTP only)",
									Optional:            true,
									Validators:          []validator.String{stringvalidator.OneOf("preserve")},
								},
								"custom_headers": schema.MapAttribute{
									MarkdownDescription: "Extra headers sent to the backend (HTTP only). Marked sensitive since values commonly carry credentials, e.g. an `Authorization` header.",
									Optional:            true,
									ElementType:         types.StringType,
									Sensitive:           true,
								},
								"proxy_protocol": schema.BoolAttribute{
									MarkdownDescription: "Send PROXY Protocol v2 header to this backend (TCP/TLS only)",
									Optional:            true,
								},
								"session_idle_timeout": schema.StringAttribute{
									MarkdownDescription: "Idle timeout before a UDP session is reaped, as a Go duration string (e.g. \"30s\", \"2m\"). Maximum 10m. (UDP only)",
									Optional:            true,
								},
								"direct_upstream": schema.BoolAttribute{
									MarkdownDescription: "Dial this target through the proxy host's own network stack instead of its embedded NetBird client, for upstreams reachable without WireGuard (public APIs, LAN services). The server refuses a loopback, multicast or link-local IP address as the host (except for a subnet target of a TCP, UDP or TLS service, where it only requires a host), and a proxy started with `NB_PROXY_DIRECT_UPSTREAM_BLOCK_PRIVATE` refuses, when it connects, addresses in a fixed list of special-use ranges (private, CGNAT, loopback, link-local, unique local, documentation, multicast and similar); any other address is dialled, publicly reachable or not. Needs a proxy cluster whose every active proxy runs in private mode (`private` on the service does not satisfy it). Applies to HTTP services only: for TCP, UDP and TLS services NetBird stores the flag but does not pass it to the proxy, and the plan warns. Defaults to false.",
									Optional:            true,
									// The server omits false; reading it as false wherever
									// options exist keeps an import or a refresh in line with
									// a configuration that writes false or leaves it out.
									Computed: true,
									Default:  booldefault.StaticBool(false),
								},
							},
						},
					},
				},
			},
			"auth": schema.SingleNestedAttribute{
				MarkdownDescription: "Authentication configuration",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"password_auth": schema.SingleNestedAttribute{
						MarkdownDescription: "Password authentication",
						Optional:            true,
						Attributes: map[string]schema.Attribute{
							"enabled": schema.BoolAttribute{
								Required: true,
							},
							"password": schema.StringAttribute{
								Optional:  true,
								Sensitive: true,
							},
						},
					},
					"pin_auth": schema.SingleNestedAttribute{
						MarkdownDescription: "PIN authentication",
						Optional:            true,
						Attributes: map[string]schema.Attribute{
							"enabled": schema.BoolAttribute{
								Required: true,
							},
							"pin": schema.StringAttribute{
								Optional:  true,
								Sensitive: true,
							},
						},
					},
					"bearer_auth": schema.SingleNestedAttribute{
						MarkdownDescription: "Bearer token authentication",
						Optional:            true,
						Attributes: map[string]schema.Attribute{
							"enabled": schema.BoolAttribute{
								Required: true,
							},
							"distribution_groups": schema.ListAttribute{
								MarkdownDescription: "List of group IDs that can use bearer auth",
								Optional:            true,
								ElementType:         types.StringType,
							},
						},
					},
					"link_auth": schema.SingleNestedAttribute{
						MarkdownDescription: "Link authentication",
						Optional:            true,
						Attributes: map[string]schema.Attribute{
							"enabled": schema.BoolAttribute{
								Required: true,
							},
						},
					},
					"header_auths": schema.ListNestedAttribute{
						MarkdownDescription: "Static header-value authentication rules",
						Optional:            true,
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"enabled": schema.BoolAttribute{
									Required: true,
								},
								"header": schema.StringAttribute{
									MarkdownDescription: "HTTP header name to check",
									Required:            true,
								},
								"value": schema.StringAttribute{
									MarkdownDescription: "Expected header value",
									Required:            true,
									Sensitive:           true,
								},
							},
						},
					},
				},
			},
			"access_restrictions": schema.SingleNestedAttribute{
				MarkdownDescription: "Connection-level access restrictions based on IP or geography",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"allowed_cidrs": schema.ListAttribute{
						MarkdownDescription: "CIDR allowlist",
						Optional:            true,
						ElementType:         types.StringType,
					},
					"blocked_cidrs": schema.ListAttribute{
						MarkdownDescription: "CIDR blocklist",
						Optional:            true,
						ElementType:         types.StringType,
					},
					"allowed_countries": schema.ListAttribute{
						MarkdownDescription: "ISO 3166-1 alpha-2 country codes to allow",
						Optional:            true,
						ElementType:         types.StringType,
					},
					"blocked_countries": schema.ListAttribute{
						MarkdownDescription: "ISO 3166-1 alpha-2 country codes to block",
						Optional:            true,
						ElementType:         types.StringType,
					},
					"crowdsec_mode": schema.StringAttribute{
						MarkdownDescription: "CrowdSec IP reputation mode: `off`, `enforce` or `observe`. The server accepts each mode whether or not the cluster supports CrowdSec: with `enforce`, a proxy without a ready CrowdSec checker denies every request from outside the tunnel, while `observe` lets them through. Requests over the NetBird tunnel, including all access to a private service, are not checked against CrowdSec. Left out, the server treats it as `off`.",
						Optional:            true,
						Validators:          []validator.String{stringvalidator.OneOf("off", "enforce", "observe")},
					},
				},
			},
		},
	}
}

func (r *ReverseProxyService) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func targetOptionsAPIToTerraform(ctx context.Context, opts *api.ServiceTargetOptions) (types.Object, diag.Diagnostics) {
	if opts == nil {
		return types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes), nil
	}

	// If all fields are at zero/nil, treat as no options set.
	if opts.SkipTlsVerify == nil && opts.RequestTimeout == nil && opts.PathRewrite == nil &&
		opts.CustomHeaders == nil && opts.SessionIdleTimeout == nil &&
		(opts.ProxyProtocol == nil || !*opts.ProxyProtocol) &&
		(opts.DirectUpstream == nil || !*opts.DirectUpstream) {
		return types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes), nil
	}

	// Treat proxy_protocol=false the same as nil so it doesn't appear in
	// state when the user never configured it.
	var proxyProtocol *bool
	if opts.ProxyProtocol != nil && *opts.ProxyProtocol {
		proxyProtocol = opts.ProxyProtocol
	}

	// direct_upstream=false is the server default and not reported; the
	// attribute defaults to false, so read it as false.
	directUpstream := opts.DirectUpstream != nil && *opts.DirectUpstream

	model := ReverseProxyTargetOptionsModel{
		SkipTLSVerify:  types.BoolPointerValue(opts.SkipTlsVerify),
		RequestTimeout: types.StringPointerValue(opts.RequestTimeout),
		ProxyProtocol:  types.BoolPointerValue(proxyProtocol),
		DirectUpstream: types.BoolValue(directUpstream),
	}

	if opts.PathRewrite != nil {
		model.PathRewrite = types.StringValue(string(*opts.PathRewrite))
	} else {
		model.PathRewrite = types.StringNull()
	}

	if opts.SessionIdleTimeout != nil {
		model.SessionIdleTimeout = types.StringValue(*opts.SessionIdleTimeout)
	} else {
		model.SessionIdleTimeout = types.StringNull()
	}

	var d diag.Diagnostics
	if opts.CustomHeaders != nil {
		model.CustomHeaders, d = types.MapValueFrom(ctx, types.StringType, *opts.CustomHeaders)
	} else {
		model.CustomHeaders = types.MapNull(types.StringType)
	}

	obj, objD := types.ObjectValueFrom(ctx, ReverseProxyTargetOptionsModel{}.TFType().AttrTypes, model)
	d.Append(objD...)
	return obj, d
}

func targetOptionsTerraformToAPI(ctx context.Context, opts types.Object) (*api.ServiceTargetOptions, diag.Diagnostics) {
	if opts.IsNull() || opts.IsUnknown() {
		return nil, nil
	}

	var ret diag.Diagnostics
	attrs := opts.Attributes()
	result := &api.ServiceTargetOptions{}
	hasValue := false

	if v, ok := attrs["skip_tls_verify"].(types.Bool); ok && !v.IsNull() && !v.IsUnknown() {
		b := v.ValueBool()
		result.SkipTlsVerify = &b
		hasValue = true
	}
	if v, ok := attrs["request_timeout"].(types.String); ok && !v.IsNull() && !v.IsUnknown() {
		s := v.ValueString()
		result.RequestTimeout = &s
		hasValue = true
	}
	if v, ok := attrs["path_rewrite"].(types.String); ok && !v.IsNull() && !v.IsUnknown() {
		pr := api.ServiceTargetOptionsPathRewrite(v.ValueString())
		result.PathRewrite = &pr
		hasValue = true
	}
	if v, ok := attrs["custom_headers"].(types.Map); ok && !v.IsNull() && !v.IsUnknown() {
		var headers map[string]string
		ret.Append(v.ElementsAs(ctx, &headers, false)...)
		result.CustomHeaders = &headers
		hasValue = true
	}
	if v, ok := attrs["proxy_protocol"].(types.Bool); ok && !v.IsNull() && !v.IsUnknown() {
		b := v.ValueBool()
		result.ProxyProtocol = &b
		hasValue = true
	}
	if v, ok := attrs["session_idle_timeout"].(types.String); ok && !v.IsNull() && !v.IsUnknown() {
		s := v.ValueString()
		result.SessionIdleTimeout = &s
		hasValue = true
	}
	if v, ok := attrs["direct_upstream"].(types.Bool); ok && !v.IsNull() && !v.IsUnknown() {
		b := v.ValueBool()
		result.DirectUpstream = &b
		hasValue = true
	}

	if !hasValue {
		return nil, ret
	}
	return result, ret
}

func reverseProxyServiceAPIToTerraform(ctx context.Context, svc *api.Service, data *ReverseProxyServiceModel) diag.Diagnostics {
	var ret diag.Diagnostics
	var d diag.Diagnostics

	data.Id = types.StringValue(svc.Id)
	data.Name = types.StringValue(svc.Name)
	data.Domain = types.StringValue(svc.Domain)
	data.Enabled = types.BoolValue(svc.Enabled)

	// An unset mode is HTTP; the server reports it as nil or "".
	data.Mode = types.StringValue("http")
	if svc.Mode != nil && *svc.Mode != "" {
		data.Mode = types.StringValue(string(*svc.Mode))
	}

	if svc.ListenPort != nil {
		data.ListenPort = types.Int64Value(int64(*svc.ListenPort))
	} else {
		data.ListenPort = types.Int64Null()
	}

	if svc.PortAutoAssigned != nil {
		data.PortAutoAssigned = types.BoolValue(*svc.PortAutoAssigned)
	} else {
		data.PortAutoAssigned = types.BoolValue(false)
	}

	if svc.PassHostHeader != nil {
		data.PassHostHeader = types.BoolValue(*svc.PassHostHeader)
	} else {
		data.PassHostHeader = types.BoolValue(false)
	}

	if svc.RewriteRedirects != nil {
		data.RewriteRedirects = types.BoolValue(*svc.RewriteRedirects)
	} else {
		data.RewriteRedirects = types.BoolValue(false)
	}

	data.ProxyCluster = types.StringPointerValue(svc.ProxyCluster)
	data.Private = types.BoolValue(svc.Private != nil && *svc.Private)
	if svc.AccessGroups == nil {
		// The server omits an empty list; an empty list is the one form for
		// no groups, so an import reads what an apply of [] stored.
		data.AccessGroups = types.ListValueMust(types.StringType, []attr.Value{})
	} else {
		accessGroups, agDiags := sortedStringListPointer(ctx, data.AccessGroups, svc.AccessGroups)
		ret.Append(agDiags...)
		data.AccessGroups = accessGroups
	}

	var targets []ReverseProxyServiceTargetModel
	for _, t := range svc.Targets {
		opts, optsDiags := targetOptionsAPIToTerraform(ctx, t.Options)
		ret.Append(optsDiags...)

		target := ReverseProxyServiceTargetModel{
			TargetId:   types.StringValue(t.TargetId),
			TargetType: types.StringValue(string(t.TargetType)),
			Port:       types.Int64Value(int64(t.Port)),
			Protocol:   types.StringValue(string(t.Protocol)),
			Enabled:    types.BoolValue(t.Enabled),
			Host:       types.StringPointerValue(t.Host),
			Path:       types.StringPointerValue(t.Path),
			Options:    opts,
		}
		if t.AccessAction != nil {
			target.AccessAction = types.StringValue(string(*t.AccessAction))
		} else {
			target.AccessAction = types.StringNull()
		}
		targets = append(targets, target)
	}
	data.Targets, d = types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), targets)
	ret.Append(d...)

	authModel := ReverseProxyServiceAuthModel{}

	if svc.Auth.PasswordAuth != nil {
		authModel.PasswordAuth, d = types.ObjectValueFrom(ctx, ReverseProxyPasswordAuthModel{}.TFType().AttrTypes, ReverseProxyPasswordAuthModel{
			Enabled:  types.BoolValue(svc.Auth.PasswordAuth.Enabled),
			Password: types.StringValue(svc.Auth.PasswordAuth.Password),
		})
		ret.Append(d...)
	} else {
		authModel.PasswordAuth = types.ObjectNull(ReverseProxyPasswordAuthModel{}.TFType().AttrTypes)
	}

	if svc.Auth.PinAuth != nil {
		authModel.PinAuth, d = types.ObjectValueFrom(ctx, ReverseProxyPinAuthModel{}.TFType().AttrTypes, ReverseProxyPinAuthModel{
			Enabled: types.BoolValue(svc.Auth.PinAuth.Enabled),
			Pin:     types.StringValue(svc.Auth.PinAuth.Pin),
		})
		ret.Append(d...)
	} else {
		authModel.PinAuth = types.ObjectNull(ReverseProxyPinAuthModel{}.TFType().AttrTypes)
	}

	if svc.Auth.BearerAuth != nil {
		bearerModel := ReverseProxyBearerAuthModel{
			Enabled: types.BoolValue(svc.Auth.BearerAuth.Enabled),
		}
		if svc.Auth.BearerAuth.DistributionGroups != nil {
			bearerModel.DistributionGroups, d = sortedStringList(ctx, objectListAttr(data.Auth, "bearer_auth", "distribution_groups"), *svc.Auth.BearerAuth.DistributionGroups)
			ret.Append(d...)
		} else {
			bearerModel.DistributionGroups = types.ListNull(types.StringType)
		}
		authModel.BearerAuth, d = types.ObjectValueFrom(ctx, ReverseProxyBearerAuthModel{}.TFType().AttrTypes, bearerModel)
		ret.Append(d...)
	} else {
		authModel.BearerAuth = types.ObjectNull(ReverseProxyBearerAuthModel{}.TFType().AttrTypes)
	}

	if svc.Auth.LinkAuth != nil {
		authModel.LinkAuth, d = types.ObjectValueFrom(ctx, ReverseProxyLinkAuthModel{}.TFType().AttrTypes, ReverseProxyLinkAuthModel{
			Enabled: types.BoolValue(svc.Auth.LinkAuth.Enabled),
		})
		ret.Append(d...)
	} else {
		authModel.LinkAuth = types.ObjectNull(ReverseProxyLinkAuthModel{}.TFType().AttrTypes)
	}

	if svc.Auth.HeaderAuths != nil && len(*svc.Auth.HeaderAuths) > 0 {
		var headerAuthModels []ReverseProxyHeaderAuthModel
		for _, h := range *svc.Auth.HeaderAuths {
			headerAuthModels = append(headerAuthModels, ReverseProxyHeaderAuthModel{
				Enabled: types.BoolValue(h.Enabled),
				Header:  types.StringValue(h.Header),
				Value:   types.StringValue(h.Value),
			})
		}
		authModel.HeaderAuths, d = types.ListValueFrom(ctx, ReverseProxyHeaderAuthModel{}.TFType(), headerAuthModels)
		ret.Append(d...)
	} else {
		authModel.HeaderAuths = types.ListNull(ReverseProxyHeaderAuthModel{}.TFType())
	}

	data.Auth, d = types.ObjectValueFrom(ctx, ReverseProxyServiceAuthModel{}.TFType().AttrTypes, authModel)
	ret.Append(d...)

	if svc.AccessRestrictions != nil {
		arModel := ReverseProxyAccessRestrictionsModel{}
		if svc.AccessRestrictions.AllowedCidrs != nil {
			arModel.AllowedCidrs, d = sortedStringList(ctx, objectListAttr(data.AccessRestrictions, "allowed_cidrs"), *svc.AccessRestrictions.AllowedCidrs)
			ret.Append(d...)
		} else {
			arModel.AllowedCidrs = emptyOrNull(objectListAttr(data.AccessRestrictions, "allowed_cidrs"))
		}
		if svc.AccessRestrictions.BlockedCidrs != nil {
			arModel.BlockedCidrs, d = sortedStringList(ctx, objectListAttr(data.AccessRestrictions, "blocked_cidrs"), *svc.AccessRestrictions.BlockedCidrs)
			ret.Append(d...)
		} else {
			arModel.BlockedCidrs = emptyOrNull(objectListAttr(data.AccessRestrictions, "blocked_cidrs"))
		}
		if svc.AccessRestrictions.AllowedCountries != nil {
			arModel.AllowedCountries, d = sortedStringList(ctx, objectListAttr(data.AccessRestrictions, "allowed_countries"), *svc.AccessRestrictions.AllowedCountries)
			ret.Append(d...)
		} else {
			arModel.AllowedCountries = emptyOrNull(objectListAttr(data.AccessRestrictions, "allowed_countries"))
		}
		if svc.AccessRestrictions.BlockedCountries != nil {
			arModel.BlockedCountries, d = sortedStringList(ctx, objectListAttr(data.AccessRestrictions, "blocked_countries"), *svc.AccessRestrictions.BlockedCountries)
			ret.Append(d...)
		} else {
			arModel.BlockedCountries = emptyOrNull(objectListAttr(data.AccessRestrictions, "blocked_countries"))
		}
		if svc.AccessRestrictions.CrowdsecMode != nil {
			arModel.CrowdsecMode = types.StringValue(string(*svc.AccessRestrictions.CrowdsecMode))
		} else {
			arModel.CrowdsecMode = types.StringNull()
		}
		data.AccessRestrictions, d = types.ObjectValueFrom(ctx, ReverseProxyAccessRestrictionsModel{}.TFType().AttrTypes, arModel)
		ret.Append(d...)
	} else if !holdsNothing(data.AccessRestrictions) {
		// The server reports no restrictions object when none is set; a
		// planned or stored object that sets nothing means the same.
		data.AccessRestrictions = types.ObjectNull(ReverseProxyAccessRestrictionsModel{}.TFType().AttrTypes)
	}

	return ret
}

// preserveAuthSecrets copies sensitive auth fields (password, pin) from prior state/plan
// into the current model since the API redacts these values on read.
// It also preserves the structure of optional auth blocks (like link_auth) that the API
// may not return when disabled, ensuring state matches plan.
func preserveAuthSecrets(priorAuth, currentAuth types.Object) (types.Object, diag.Diagnostics) {
	var ret diag.Diagnostics

	if priorAuth.IsNull() || priorAuth.IsUnknown() || currentAuth.IsNull() || currentAuth.IsUnknown() {
		return currentAuth, ret
	}

	priorAttrs := priorAuth.Attributes()
	currentAttrs := currentAuth.Attributes()

	// Preserve password_auth sensitive field from plan/prior state.
	if priorPw, ok := priorAttrs["password_auth"].(types.Object); ok && !priorPw.IsNull() {
		if curPw, ok := currentAttrs["password_auth"].(types.Object); ok && !curPw.IsNull() {
			priorPwAttrs := priorPw.Attributes()
			curPwAttrs := curPw.Attributes()
			// Always use prior password value — API redacts it on read.
			if pw, ok := priorPwAttrs["password"]; ok {
				curPwAttrs["password"] = pw
				obj, d := types.ObjectValue(ReverseProxyPasswordAuthModel{}.TFType().AttrTypes, curPwAttrs)
				ret.Append(d...)
				currentAttrs["password_auth"] = obj
			}
		}
	}

	// Preserve pin_auth sensitive field from plan/prior state.
	if priorPin, ok := priorAttrs["pin_auth"].(types.Object); ok && !priorPin.IsNull() {
		if curPin, ok := currentAttrs["pin_auth"].(types.Object); ok && !curPin.IsNull() {
			priorPinAttrs := priorPin.Attributes()
			curPinAttrs := curPin.Attributes()
			// Always use prior pin value — API redacts it on read.
			if pin, ok := priorPinAttrs["pin"]; ok {
				curPinAttrs["pin"] = pin
				obj, d := types.ObjectValue(ReverseProxyPinAuthModel{}.TFType().AttrTypes, curPinAttrs)
				ret.Append(d...)
				currentAttrs["pin_auth"] = obj
			}
		}
	}

	// Preserve link_auth from plan/prior when API returns null.
	if priorLink, ok := priorAttrs["link_auth"].(types.Object); ok && !priorLink.IsNull() {
		if curLink, ok := currentAttrs["link_auth"].(types.Object); ok && curLink.IsNull() {
			currentAttrs["link_auth"] = priorLink
		}
	}

	// Preserve header_auths sensitive values from plan/prior state.
	if priorHeaders, ok := priorAttrs["header_auths"].(types.List); ok && !priorHeaders.IsNull() {
		if curHeaders, ok := currentAttrs["header_auths"].(types.List); ok && !curHeaders.IsNull() {
			var priorModels, curModels []ReverseProxyHeaderAuthModel
			ret.Append(priorHeaders.ElementsAs(context.Background(), &priorModels, false)...)
			ret.Append(curHeaders.ElementsAs(context.Background(), &curModels, false)...)
			// Match by index (order is stable) and restore redacted values.
			for i := range curModels {
				if i < len(priorModels) {
					curModels[i].Value = priorModels[i].Value
				}
			}
			newList, d := types.ListValueFrom(context.Background(), ReverseProxyHeaderAuthModel{}.TFType(), curModels)
			ret.Append(d...)
			currentAttrs["header_auths"] = newList
		}
	}

	result, d := types.ObjectValue(ReverseProxyServiceAuthModel{}.TFType().AttrTypes, currentAttrs)
	ret.Append(d...)
	return result, ret
}

// preserveTargetPlanValues keeps the user's planned host/path/options values in targets
// since the API may override them (e.g. resolving host from the peer's IP).
// Targets are matched by target_id to handle potential reordering by the API.
func preserveTargetPlanValues(ctx context.Context, planTargets, apiTargets types.List) (types.List, diag.Diagnostics) {
	var ret diag.Diagnostics

	if planTargets.IsNull() || planTargets.IsUnknown() || apiTargets.IsNull() || apiTargets.IsUnknown() {
		return apiTargets, ret
	}

	var planModels, apiModels []ReverseProxyServiceTargetModel
	ret.Append(planTargets.ElementsAs(ctx, &planModels, false)...)
	ret.Append(apiTargets.ElementsAs(ctx, &apiModels, false)...)
	if ret.HasError() {
		return apiTargets, ret
	}

	matches := matchPlanTargets(planModels, apiModels)
	for i, apiModel := range apiModels {
		planModel, ok := matches[i]
		if !ok {
			continue
		}
		// For peer, host and domain targets the server reports the address it
		// looked up, not the configured one; subnet and cluster targets keep
		// theirs, so the server's value stands and a change shows.
		if !planModel.Host.IsNull() && !planModel.Host.IsUnknown() && serverResolvesHost(apiModel.TargetType.ValueString()) {
			apiModels[i].Host = planModel.Host
		}
		if !planModel.Path.IsNull() && !planModel.Path.IsUnknown() {
			apiModels[i].Path = planModel.Path
		}
		if !planModel.Options.IsNull() && !planModel.Options.IsUnknown() {
			options, d := reconcileOptions(planModel.Options, apiModel.Options)
			ret.Append(d...)
			apiModels[i].Options = options
		}
	}

	result, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), apiModels)
	ret.Append(d...)
	return result, ret
}

// serverResolvesHost reports whether NetBird replaces a target's host with
// the address it looks up for the target (v0.83: peer, host and domain).
func serverResolvesHost(targetType string) bool {
	switch targetType {
	case "peer", "host", "domain":
		return true
	}
	return false
}

// matchPlanTargets pairs each API target with at most one planned target, and
// each planned target with at most one API target. One backend can serve
// several paths, and several ports on one path, so a pair needs the same ID
// and effective path (a left-out path is the server's "/"), and port and
// protocol decide between targets that share both. A planned target whose path
// is not known yet pairs on ID, port and protocol, or with the one remaining
// API target of its ID. A known planned host decides between targets of one
// subnet resource that share all of that. An API target left unpaired keeps
// its server values,
// so a change made outside Terraform (a moved path, a removed target) shows
// in the next plan.
func matchPlanTargets(planned, live []ReverseProxyServiceTargetModel) map[int]ReverseProxyServiceTargetModel {
	samePath := func(p, l ReverseProxyServiceTargetModel) bool {
		return !p.Path.IsUnknown() && targetLocation(p.Path) == targetLocation(l.Path)
	}
	unknownPath := func(p, _ ReverseProxyServiceTargetModel) bool { return p.Path.IsUnknown() }
	sameEndpoint := func(p, l ReverseProxyServiceTargetModel) bool {
		return p.Port.Equal(l.Port) && p.Protocol.Equal(l.Protocol)
	}
	// A known planned host tells apart targets of one subnet resource; for a
	// peer target the server reports the peer's address instead, so this only
	// narrows, never decides alone.
	sameHost := func(p, l ReverseProxyServiceTargetModel) bool {
		return !p.Host.IsNull() && !p.Host.IsUnknown() && p.Host.Equal(l.Host)
	}
	out := map[int]ReverseProxyServiceTargetModel{}
	used := make([]bool, len(planned))
	// Only targets of one ID are compared, in the API's order.
	byID := map[string][]int{}
	for i, l := range live {
		if !l.TargetId.IsNull() && !l.TargetId.IsUnknown() {
			byID[l.TargetId.ValueString()] = append(byID[l.TargetId.ValueString()], i)
		}
	}
	// pair matches unpaired targets of the same ID; with onlyOne it pairs a
	// planned target only when exactly one API target qualifies.
	// For a known planned path, only live targets of that ID and path qualify.
	byIDPath := map[[2]string][]int{}
	for i, l := range live {
		if !l.TargetId.IsNull() && !l.TargetId.IsUnknown() && !l.Path.IsUnknown() {
			k := [2]string{l.TargetId.ValueString(), targetLocation(l.Path)}
			byIDPath[k] = append(byIDPath[k], i)
		}
	}
	pair := func(match func(p, l ReverseProxyServiceTargetModel) bool, onlyOne, byPath bool) {
		for j, p := range planned {
			if used[j] {
				continue
			}
			var found []int
			if p.TargetId.IsNull() || p.TargetId.IsUnknown() {
				continue
			}
			if !byPath && !p.Path.IsUnknown() {
				continue // the unknown-path round pairs only targets whose path is unknown
			}
			candidates := byID[p.TargetId.ValueString()]
			if byPath {
				if p.Path.IsUnknown() {
					continue
				}
				candidates = byIDPath[[2]string{p.TargetId.ValueString(), targetLocation(p.Path)}]
			}
			for _, i := range candidates {
				if _, taken := out[i]; !taken && match(p, live[i]) {
					found = append(found, i)
				}
			}
			if len(found) == 1 || (!onlyOne && len(found) > 0) {
				// Among equals: with as many targets on both sides (a create or
				// update response, an unchanged refresh) the one at the same
				// position wins; otherwise a target was added or removed, and
				// equals pair in their relative order.
				pick := found[0]
				for _, i := range found {
					if i == j && len(planned) == len(live) {
						pick = i
					}
				}
				out[pick], used[j] = p, true
			}
		}
	}
	// From the most specific match to the least. Targets equal in everything
	// compared pair in list order; a looser match pairs only when it is the
	// one candidate left.
	for round, known := range []func(p, l ReverseProxyServiceTargetModel) bool{samePath, unknownPath} {
		byPath := round == 0
		pair(func(p, l ReverseProxyServiceTargetModel) bool {
			return known(p, l) && sameEndpoint(p, l) && sameHost(p, l)
		}, false, byPath)
		pair(func(p, l ReverseProxyServiceTargetModel) bool { return known(p, l) && sameEndpoint(p, l) }, false, byPath)
		pair(func(p, l ReverseProxyServiceTargetModel) bool { return known(p, l) && sameHost(p, l) }, true, byPath)
		pair(known, true, byPath)
	}
	return out
}

// reconcileOptions keeps the planned spelling of each target option where it
// means what the server has (the server drops defaults and writes durations
// its own way, "2m" as "2m0s") and takes the server's value where it does not,
// so a change made outside Terraform, or a target paired with the wrong
// planned one, shows in the next plan.
func reconcileOptions(planned, live types.Object) (types.Object, diag.Diagnostics) {
	var liveAttrs map[string]attr.Value
	if !live.IsNull() && !live.IsUnknown() {
		liveAttrs = live.Attributes()
	}
	attrs := planned.Attributes()
	if liveAttrs == nil {
		// The server reports no options: keep the planned object only if it
		// sets nothing but defaults, otherwise the change shows as null.
		for name, plannedValue := range attrs {
			if !sameOption(name, plannedValue, nullOf(plannedValue)) {
				return types.ObjectNull(planned.AttributeTypes(context.Background())), nil
			}
		}
		return planned, nil
	}
	for name, plannedValue := range attrs {
		liveValue, ok := liveAttrs[name]
		if !ok {
			liveValue = nullOf(plannedValue)
		}
		if !sameOption(name, plannedValue, liveValue) {
			attrs[name] = liveValue
		} else if name == "direct_upstream" && plannedValue.IsNull() && !liveValue.IsNull() {
			// State written before the attribute existed holds null; the
			// attribute now defaults to false, so keep the server's false
			// rather than plan null -> false after a refresh.
			attrs[name] = liveValue
		}
	}
	return types.ObjectValue(planned.AttributeTypes(context.Background()), attrs)
}

// durationOptions are the target options the server writes as Go durations.
var durationOptions = map[string]bool{"request_timeout": true, "session_idle_timeout": true}

// sameOption reports whether a planned option value means the server's one.
func sameOption(name string, planned, live attr.Value) bool {
	if planned.IsUnknown() {
		return false
	}
	switch p := planned.(type) {
	case types.Bool:
		l, _ := live.(types.Bool)
		return p.ValueBool() == l.ValueBool() // null reads as false
	case types.String:
		l, _ := live.(types.String)
		if pd, err := time.ParseDuration(p.ValueString()); err == nil && durationOptions[name] {
			ld, lerr := time.ParseDuration(l.ValueString())
			return (lerr == nil && pd == ld) || (pd == 0 && l.IsNull())
		}
		return p.ValueString() == l.ValueString() // null reads as ""
	case types.Map:
		l, _ := live.(types.Map)
		return (len(p.Elements()) == 0 && len(l.Elements()) == 0) || p.Equal(l)
	}
	return planned.Equal(live)
}

// nullOf returns a null value of the same type.
func nullOf(v attr.Value) attr.Value {
	switch v.(type) {
	case types.Bool:
		return types.BoolNull()
	case types.String:
		return types.StringNull()
	case types.Map:
		return types.MapNull(types.StringType)
	}
	return v
}

func reverseProxyServiceTerraformToAPI(ctx context.Context, data *ReverseProxyServiceModel) (api.ServiceRequest, diag.Diagnostics) {
	var ret diag.Diagnostics

	req := api.ServiceRequest{
		Name:    data.Name.ValueString(),
		Domain:  data.Domain.ValueString(),
		Enabled: data.Enabled.ValueBool(),
	}

	if !data.Mode.IsNull() && !data.Mode.IsUnknown() {
		v := api.ServiceRequestMode(data.Mode.ValueString())
		req.Mode = &v
	}

	if !data.ListenPort.IsNull() && !data.ListenPort.IsUnknown() {
		v := int(data.ListenPort.ValueInt64())
		req.ListenPort = &v
	}

	if !data.PassHostHeader.IsNull() && !data.PassHostHeader.IsUnknown() {
		v := data.PassHostHeader.ValueBool()
		req.PassHostHeader = &v
	}
	if !data.Private.IsNull() && !data.Private.IsUnknown() {
		v := data.Private.ValueBool()
		req.Private = &v
	}
	if !data.AccessGroups.IsNull() && !data.AccessGroups.IsUnknown() {
		groups := []string{}
		ret.Append(data.AccessGroups.ElementsAs(ctx, &groups, false)...)
		req.AccessGroups = &groups
	}
	if !data.RewriteRedirects.IsNull() && !data.RewriteRedirects.IsUnknown() {
		v := data.RewriteRedirects.ValueBool()
		req.RewriteRedirects = &v
	}

	var targetModels []ReverseProxyServiceTargetModel
	ret.Append(data.Targets.ElementsAs(ctx, &targetModels, false)...)
	if ret.HasError() {
		return req, ret
	}

	targets := make([]api.ServiceTarget, 0, len(targetModels))
	for _, t := range targetModels {
		target := api.ServiceTarget{
			TargetId:   t.TargetId.ValueString(),
			TargetType: api.ServiceTargetTargetType(t.TargetType.ValueString()),
			Port:       int(t.Port.ValueInt64()),
			Protocol:   api.ServiceTargetProtocol(t.Protocol.ValueString()),
			Enabled:    t.Enabled.ValueBool(),
		}
		if !t.Host.IsNull() && !t.Host.IsUnknown() {
			v := t.Host.ValueString()
			target.Host = &v
		}
		if !t.Path.IsNull() && !t.Path.IsUnknown() {
			v := t.Path.ValueString()
			target.Path = &v
		}
		// Left out (unknown in the plan), the server keeps the action the target
		// at this path has now.
		if !t.AccessAction.IsNull() && !t.AccessAction.IsUnknown() {
			v := api.ServiceTargetAccessAction(t.AccessAction.ValueString())
			target.AccessAction = &v
		}

		opts, d := targetOptionsTerraformToAPI(ctx, t.Options)
		ret.Append(d...)
		target.Options = opts

		targets = append(targets, target)
	}
	req.Targets = &targets

	authAttrs := data.Auth.Attributes()
	req.Auth = &api.ServiceAuthConfig{}

	authCfg := &api.ServiceAuthConfig{}

	if v, ok := authAttrs["password_auth"].(types.Object); ok && !v.IsNull() && !v.IsUnknown() {
		pwAttrs := v.Attributes()
		enabled, _ := pwAttrs["enabled"].(types.Bool)
		password, _ := pwAttrs["password"].(types.String)
		authCfg.PasswordAuth = &api.PasswordAuthConfig{
			Enabled:  enabled.ValueBool(),
			Password: password.ValueString(),
		}
	}

	if v, ok := authAttrs["pin_auth"].(types.Object); ok && !v.IsNull() && !v.IsUnknown() {
		pinAttrs := v.Attributes()
		enabled, _ := pinAttrs["enabled"].(types.Bool)
		pin, _ := pinAttrs["pin"].(types.String)
		authCfg.PinAuth = &api.PINAuthConfig{
			Enabled: enabled.ValueBool(),
			Pin:     pin.ValueString(),
		}
	}

	if v, ok := authAttrs["bearer_auth"].(types.Object); ok && !v.IsNull() && !v.IsUnknown() {
		bearerAttrs := v.Attributes()
		enabled, _ := bearerAttrs["enabled"].(types.Bool)
		bearerAuth := &api.BearerAuthConfig{
			Enabled: enabled.ValueBool(),
		}
		if groupsList, ok := bearerAttrs["distribution_groups"].(types.List); ok && !groupsList.IsNull() && !groupsList.IsUnknown() {
			var groups []string
			ret.Append(groupsList.ElementsAs(ctx, &groups, false)...)
			bearerAuth.DistributionGroups = &groups
		}
		authCfg.BearerAuth = bearerAuth
	}

	if v, ok := authAttrs["link_auth"].(types.Object); ok && !v.IsNull() && !v.IsUnknown() {
		linkAttrs := v.Attributes()
		enabled, _ := linkAttrs["enabled"].(types.Bool)
		authCfg.LinkAuth = &api.LinkAuthConfig{
			Enabled: enabled.ValueBool(),
		}
	}

	if v, ok := authAttrs["header_auths"].(types.List); ok && !v.IsNull() && !v.IsUnknown() {
		var headerAuthModels []ReverseProxyHeaderAuthModel
		ret.Append(v.ElementsAs(ctx, &headerAuthModels, false)...)
		var headerAuths []api.HeaderAuthConfig
		for _, h := range headerAuthModels {
			headerAuths = append(headerAuths, api.HeaderAuthConfig{
				Enabled: h.Enabled.ValueBool(),
				Header:  h.Header.ValueString(),
				Value:   h.Value.ValueString(),
			})
		}
		authCfg.HeaderAuths = &headerAuths
	}

	req.Auth = authCfg

	if !data.AccessRestrictions.IsNull() && !data.AccessRestrictions.IsUnknown() {
		arAttrs := data.AccessRestrictions.Attributes()
		ar := &api.AccessRestrictions{}
		hasAR := false
		if v, ok := arAttrs["allowed_cidrs"].(types.List); ok && !v.IsNull() && !v.IsUnknown() {
			var cidrs []string
			ret.Append(v.ElementsAs(ctx, &cidrs, false)...)
			ar.AllowedCidrs = &cidrs
			hasAR = true
		}
		if v, ok := arAttrs["blocked_cidrs"].(types.List); ok && !v.IsNull() && !v.IsUnknown() {
			var cidrs []string
			ret.Append(v.ElementsAs(ctx, &cidrs, false)...)
			ar.BlockedCidrs = &cidrs
			hasAR = true
		}
		if v, ok := arAttrs["allowed_countries"].(types.List); ok && !v.IsNull() && !v.IsUnknown() {
			var countries []string
			ret.Append(v.ElementsAs(ctx, &countries, false)...)
			ar.AllowedCountries = &countries
			hasAR = true
		}
		if v, ok := arAttrs["blocked_countries"].(types.List); ok && !v.IsNull() && !v.IsUnknown() {
			var countries []string
			ret.Append(v.ElementsAs(ctx, &countries, false)...)
			ar.BlockedCountries = &countries
			hasAR = true
		}
		if v, ok := arAttrs["crowdsec_mode"].(types.String); ok && !v.IsNull() && !v.IsUnknown() {
			mode := api.AccessRestrictionsCrowdsecMode(v.ValueString())
			ar.CrowdsecMode = &mode
			hasAR = true
		}
		if hasAR {
			req.AccessRestrictions = ar
		}
	}

	return req, ret
}

// Create creates a new reverse proxy service.
func (r *ReverseProxyService) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ReverseProxyServiceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serviceReq, d := reverseProxyServiceTerraformToAPI(ctx, &data)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	svc, err := r.client.ReverseProxyServices.Create(ctx, serviceReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating reverse proxy service", err.Error())
		return
	}

	// Save plan values to preserve fields the API may override
	planAuth := data.Auth
	planTargets := data.Targets

	resp.Diagnostics.Append(reverseProxyServiceAPIToTerraform(ctx, svc, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	auth, authDiags := preserveAuthSecrets(planAuth, data.Auth)
	resp.Diagnostics.Append(authDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Auth = auth

	targets, targetDiags := preserveTargetPlanValues(ctx, planTargets, data.Targets)
	resp.Diagnostics.Append(targetDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Targets = targets

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the Terraform state with the latest data from the API.
func (r *ReverseProxyService) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ReverseProxyServiceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save prior state to preserve fields the API may override
	priorAuth := data.Auth
	priorTargets := data.Targets

	svc, err := r.client.ReverseProxyServices.Get(ctx, data.Id.ValueString())
	if err != nil {
		if netbird.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error getting reverse proxy service", err.Error())
		return
	}

	resp.Diagnostics.Append(reverseProxyServiceAPIToTerraform(ctx, svc, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	auth, authDiags := preserveAuthSecrets(priorAuth, data.Auth)
	resp.Diagnostics.Append(authDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Auth = auth

	targets, targetDiags := preserveTargetPlanValues(ctx, priorTargets, data.Targets)
	resp.Diagnostics.Append(targetDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Targets = targets

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update modifies an existing reverse proxy service.
func (r *ReverseProxyService) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ReverseProxyServiceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serviceReq, d := reverseProxyServiceTerraformToAPI(ctx, &data)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The update replaces the whole service, so an access setting the plan
	// shows unchanged but that changed on the server since the plan (a closed
	// service, a revoked group) would be overwritten without the plan saying
	// so. Stop instead and let a new plan show the change.
	var prior ReverseProxyServiceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	live, err := r.client.ReverseProxyServices.Get(ctx, data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading reverse proxy service", err.Error())
		return
	}
	// Against the live mode: a state from before the attribute holds none.
	liveMode := types.StringValue("http")
	if live.Mode != nil && *live.Mode != "" {
		liveMode = types.StringValue(string(*live.Mode))
	}
	if d := modeChangeProblem(liveMode, data.Mode); d.HasError() {
		resp.Diagnostics.Append(d...)
		return
	}
	if changed := serviceAccessChangedSincePlan(prior, data, live); len(changed) > 0 {
		resp.Diagnostics.AddError("Reverse proxy service changed after the plan",
			"The service's access settings changed after this plan was made, so applying the plan would overwrite "+
				"that change although the plan shows the setting unchanged:\n\n"+strings.Join(changed, "\n")+
				"\n\nNothing was changed. Run plan again.")
		return
	}
	if serviceReq.Mode == nil && live.Mode != nil && *live.Mode != "" {
		// A mode the plan does not know is the live one, and the checks the
		// plan could not make for it are made now.
		mode := api.ServiceRequestMode(*live.Mode)
		serviceReq.Mode = &mode
		if *live.Mode != "http" {
			if d := l4DisabledTargetProblems(data.Targets); d.HasError() {
				resp.Diagnostics.Append(d...)
				return
			}
			var config ReverseProxyServiceModel
			resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
			if resp.Diagnostics.HasError() {
				return
			}
			resp.Diagnostics.Append(directUpstreamWarnings(config.Targets, string(*live.Mode))...)
		}
	}
	keepLiveUnknownAccess(&serviceReq, data, live)
	if err := privateRequestProblem(serviceReq, live.Private != nil && *live.Private); err != nil {
		resp.Diagnostics.AddError("Private service without usable access groups", err.Error())
		return
	}
	resp.Diagnostics.Append(keptActionProblems(ctx, serviceReq, live)...)
	if resp.Diagnostics.HasError() {
		return
	}

	svc, err := r.client.ReverseProxyServices.Update(ctx, data.Id.ValueString(), serviceReq)
	if err != nil {
		resp.Diagnostics.AddError("Error updating reverse proxy service", err.Error())
		return
	}

	// Save plan values to preserve fields the API may override
	planAuth := data.Auth
	planTargets := data.Targets

	resp.Diagnostics.Append(reverseProxyServiceAPIToTerraform(ctx, svc, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	auth, authDiags := preserveAuthSecrets(planAuth, data.Auth)
	resp.Diagnostics.Append(authDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Auth = auth

	targets, targetDiags := preserveTargetPlanValues(ctx, planTargets, data.Targets)
	resp.Diagnostics.Append(targetDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Targets = targets

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete removes a reverse proxy service.
func (r *ReverseProxyService) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ReverseProxyServiceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.ReverseProxyServices.Delete(ctx, data.Id.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error deleting reverse proxy service", err.Error())
	}
}

// ImportState imports an existing reverse proxy service into Terraform state.
func (r *ReverseProxyService) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

var (
	_ resource.Resource                   = &ReverseProxyService{}
	_ resource.ResourceWithImportState    = &ReverseProxyService{}
	_ resource.ResourceWithValidateConfig = &ReverseProxyService{}
	_ resource.ResourceWithModifyPlan     = &ReverseProxyService{}
)

// ValidateConfig checks the private-service contract the server enforces
// (HTTP mode, at least one access group, no bearer auth) on the configured
// values; ModifyPlan checks the values a service keeps from its state and
// the target access actions.
func (r *ReverseProxyService) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data ReverseProxyServiceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	private := !data.Private.IsUnknown() && data.Private.ValueBool()
	// The target checks run in ModifyPlan on the planned targets, which are
	// what the request sends: with ignore_changes they can differ from the
	// configuration.
	if !private {
		return
	}
	// A left-out list may be adopted from an existing service; ModifyPlan
	// refuses it for a new one.
	resp.Diagnostics.Append(privateServiceProblems(data.Mode, data.AccessGroups, data.Auth, true)...)
}

// ModifyPlan refuses what the server would reject when the configuration
// alone does not show it: a private service that is new without access
// groups, or whose groups, mode, auth or target access actions come from
// state or the plan, and target access actions the server keeps from the
// service's current targets. Access settings absent from state (written
// before they existed, planned without a refresh) stay unknown on a real
// update through UseNonNullStateForUnknown, and the update sends their live
// values.
func (r *ReverseProxyService) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	create := req.State.Raw.IsNull()
	var plan, config, state ReverseProxyServiceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if create && config.Private.IsNull() {
		// The server creates a service public unless told otherwise; show
		// that in the plan rather than "known after apply".
		plan.Private = types.BoolValue(false)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("private"), plan.Private)...)
	}
	if !plan.Mode.IsNull() && !plan.Mode.IsUnknown() && normalMode(plan.Mode) != "http" {
		// Also on a plan without changes, which is where an imported value
		// would otherwise pass for one that works.
		resp.Diagnostics.Append(directUpstreamWarnings(config.Targets, plan.Mode.ValueString())...)
	}
	// writes is false when the plan differs from state only by values not
	// known yet: the service may not be written at all, and what it holds
	// was accepted by the server, so the target checks wait for the final
	// plan.
	writes := true
	if !create {
		if req.Plan.Raw.Equal(req.State.Raw) {
			// Nothing is written, so values the server already holds are
			// not this plan's problem.
			return
		}
		writes = !onlyUnknownDifferences(req.Plan.Raw, req.State.Raw)
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if !create && config.Mode.IsNull() && plan.Mode.Equal(types.StringValue("")) {
		// A stored "" (an older read) is HTTP, which an update sends; while
		// the plan may not write at all, it stays unknown.
		plan.Mode = types.StringUnknown()
		if writes {
			plan.Mode = types.StringValue("http")
		}
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("mode"), plan.Mode)...)
	}
	if !create {
		resp.Diagnostics.Append(inPlaceOnly(modeChangeProblem(state.Mode, config.Mode))...)
	}
	// The mode the server will hold: a left-out mode is not sent, so an
	// existing service keeps its own and a new one is HTTP.
	mode := plan.Mode
	switch {
	case config.Mode.IsNull() && create:
		mode = types.StringValue("http")
	case config.Mode.IsNull():
		mode = state.Mode
	}
	modeKnown := ""
	if !mode.IsNull() && !mode.IsUnknown() {
		modeKnown = normalMode(mode)
	}
	if modeKnown != "" && modeKnown != "http" {
		// The server turns every L4 target on (validateL4Target, v0.83.2
		// and v0.82.1), so a disabled one would be written enabled.
		resp.Diagnostics.Append(l4DisabledTargetProblems(config.Targets)...)
	}
	private := !plan.Private.IsUnknown() && plan.Private.ValueBool()
	var existing []ReverseProxyServiceTargetModel
	if !create && !state.Targets.IsNull() && !state.Targets.IsUnknown() {
		resp.Diagnostics.Append(state.Targets.ElementsAs(ctx, &existing, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	// A left-out path is "/" only on a write; otherwise it may be kept.
	actions := targetActions(plan.Targets, config.Targets, existing, writes)
	if !create && writes {
		resp.Diagnostics.Append(inPlaceOnly(actions.keptProblems())...)
	}
	var targetPrivacy, targetProblems diag.Diagnostics
	if writes {
		resp.Diagnostics.Append(planOmittedPaths(ctx, config.Targets, plan.Targets, resp)...)
		var updateOnly diag.Diagnostics
		targetPrivacy, targetProblems, updateOnly = actions.problems(private, modeKnown)
		resp.Diagnostics.Append(inPlaceOnly(updateOnly)...)
		resp.Diagnostics.Append(targetHostProblems(config.Targets, modeKnown)...)
	}
	if !private {
		resp.Diagnostics.Append(targetProblems...)
		return
	}
	resp.Diagnostics.Append(targetProblems...)
	if create && config.AccessGroups.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("access_groups"), "Access groups required",
			"A new service with private = true needs at least one group in access_groups.")
		return
	}
	// Blank IDs are refused in a configured list and when a service turns
	// private; an already-private service keeps a list that still works
	// through its other IDs.
	checkBlank := create || !config.AccessGroups.IsNull() || !state.Private.ValueBool()
	if !create && !config.Mode.IsNull() && !config.Mode.Equal(state.Mode) {
		// A configured mode change (or one not known yet) fails in place
		// (#77) and is planned anew against an empty state when the service
		// is replaced, so the values kept from this state say nothing about
		// the new service.
		resp.Diagnostics.Append(targetPrivacy...)
		return
	}
	// A configuration error stops at ValidateConfig, so this reports only
	// problems with values kept from state.
	if modeKnown != "" {
		mode = types.StringValue(modeKnown) // "" is HTTP
	}
	problems := privateServiceProblems(mode, plan.AccessGroups, plan.Auth, checkBlank)
	problems.Append(targetPrivacy...)
	// The hint covers errors that come from the service being private, not
	// the ones that public would leave in place.
	if config.Private.IsNull() && problems.HasError() {
		problems.AddAttributeError(path.Root("private"), "Service is private from its state",
			"private is left out, so the service stays private and the errors above apply. Set private = false to make it "+
				"public; that is also needed for -replace, which plans against the existing service first.")
	}
	resp.Diagnostics.Append(problems...)
}

// emptyOrNull keeps a prior empty list where the server omits the list: an
// empty list and an absent one mean the same to it.
func emptyOrNull(prior types.List) types.List {
	if !prior.IsNull() && !prior.IsUnknown() && len(prior.Elements()) == 0 {
		return prior
	}
	return types.ListNull(types.StringType)
}

// holdsNothing reports whether a known object sets no attribute: each is
// null or an empty list.
func holdsNothing(obj types.Object) bool {
	if obj.IsNull() || obj.IsUnknown() {
		return false
	}
	for _, v := range obj.Attributes() {
		if l, ok := v.(types.List); ok && !l.IsUnknown() && len(l.Elements()) == 0 {
			continue
		}
		if !v.IsNull() {
			return false
		}
	}
	return true
}

// planOmittedPaths plans path = null for a target whose path the
// configuration leaves out. On a create or an update the path is not sent
// and the server stores none, which it reads as "/"; "known after apply"
// would hide that a target kept at another path (after an import) moves to
// "/", together with its access action.
func planOmittedPaths(ctx context.Context, configured, planned types.List, resp *resource.ModifyPlanResponse) diag.Diagnostics {
	var diags diag.Diagnostics
	if configured.IsNull() || configured.IsUnknown() || planned.IsNull() || planned.IsUnknown() {
		return diags
	}
	cfg, plan := configured.Elements(), planned.Elements()
	if len(cfg) != len(plan) {
		return diags
	}
	elements := make([]attr.Value, len(plan))
	changed := false
	for i, e := range cfg {
		elements[i] = plan[i]
		c, ok := e.(types.Object)
		p, pok := plan[i].(types.Object)
		if !ok || !pok || c.IsNull() || c.IsUnknown() || p.IsNull() || p.IsUnknown() {
			continue
		}
		if cp, ok := c.Attributes()["path"].(types.String); !ok || !cp.IsNull() {
			continue
		}
		attrs := p.Attributes()
		if pp, ok := attrs["path"].(types.String); !ok || !pp.IsUnknown() {
			continue
		}
		attrs["path"] = types.StringNull()
		obj, d := types.ObjectValue(p.AttributeTypes(ctx), attrs)
		diags.Append(d...)
		elements[i], changed = obj, true
	}
	if !changed || diags.HasError() {
		return diags
	}
	// One write for the whole list: each SetAttribute rebuilds the plan.
	list, d := types.ListValue(planned.ElementType(ctx), elements)
	diags.Append(d...)
	if !diags.HasError() {
		diags.Append(resp.Plan.SetAttribute(ctx, path.Root("targets"), list)...)
	}
	return diags
}

// plannedAction is what the server will hold for a configured target.
type plannedAction struct {
	action    string // "" when not known at plan time
	kept      bool   // left out and taken from the current target at its path
	path      string // effective path ("/" when left out)
	pathKnown bool
	enabled   int // 1 enabled, 0 disabled, -1 not known
}

// targetAccessActions is what the server will do with the configured
// targets' access actions on a create (existing nil) or an update.
type targetAccessActions struct {
	targets []plannedAction
	// removed lists the paths whose current non-inherit action the update
	// drops while no target sets an action; duplicate is a path that has
	// several current targets while some planned target leaves its action
	// out. The server refuses both.
	removed      []int  // current target indexes
	duplicate    [2]int // current target indexes sharing a path, valid when hasDuplicate
	hasDuplicate bool
}

// targetActions follows the server (v0.83, v0.82:
// preserveTargetAccessActions): when the current service has a non-inherit
// action and not every target sets one, a target that leaves its action out
// takes the action of the first current target at its path, and each current
// path is taken at most once, in target order. The targets are followed in
// order up to the first one whose path is not known at plan time, since that
// one could take any current path; actions after it stay unknown.
//
// omittedPathIsRoot says whether a left-out path is "/": on a create or an
// update it is (nothing is sent and the server uses "/"), but without a plan
// it may stand for a path kept unchanged from state.
func targetActions(planned, configured types.List, existing []ReverseProxyServiceTargetModel, omittedPathIsRoot bool) targetAccessActions {
	var out targetAccessActions
	if planned.IsNull() || planned.IsUnknown() {
		return out
	}
	elements := planned.Elements()
	// The configuration tells a left-out value from one not known yet, as
	// long as it lines up with the plan (ignore_changes can keep targets the
	// configuration no longer lists).
	var cfg []attr.Value
	if !configured.IsNull() && !configured.IsUnknown() && len(configured.Elements()) == len(elements) {
		cfg = configured.Elements()
	}
	leftOut := func(i int, name string) bool {
		if cfg == nil {
			return false
		}
		c, ok := cfg[i].(types.Object)
		if !ok || c.IsNull() || c.IsUnknown() {
			return false
		}
		v, ok := c.Attributes()[name].(types.String)
		return ok && v.IsNull()
	}
	out.targets = make([]plannedAction, len(elements))
	omitted := make([]bool, len(elements))
	// With no targets the server still checks for dropped controlled paths.
	allProvided, provided := len(elements) > 0, false
	for i, e := range elements {
		t := &out.targets[i]
		t.enabled = -1
		obj, ok := e.(types.Object)
		if !ok || obj.IsUnknown() || obj.IsNull() {
			provided = true // may set one
			continue
		}
		attrs := obj.Attributes()
		p, _ := attrs["path"].(types.String)
		switch {
		case !p.IsNull() && !p.IsUnknown():
			t.path, t.pathKnown = targetLocation(p), true
		case omittedPathIsRoot && (p.IsNull() || leftOut(i, "path")):
			// Not sent: the server uses "/".
			t.path, t.pathKnown = "/", true
		}
		if en, ok := attrs["enabled"].(types.Bool); ok && !en.IsUnknown() {
			t.enabled = 1 // left out, the schema default is true
			if !en.IsNull() && !en.ValueBool() {
				t.enabled = 0
			}
		}
		a, _ := attrs["access_action"].(types.String)
		switch {
		case a.IsNull() || (a.IsUnknown() && leftOut(i, "access_action")):
			omitted[i], allProvided = true, false
		case a.IsUnknown():
			provided = true
		default:
			provided = true
			t.action = a.ValueString()
		}
	}
	controlled := false
	for _, t := range existing {
		if v := t.AccessAction.ValueString(); v != "" && v != "inherit" {
			controlled = true
		}
	}
	if !controlled || allProvided {
		for i := range out.targets {
			if omitted[i] {
				out.targets[i].action = "inherit"
			}
		}
		return out
	}
	byPath := map[string]int{}
	for j, t := range existing {
		loc := targetLocation(t.Path)
		if first, dup := byPath[loc]; dup && !out.hasDuplicate {
			out.duplicate, out.hasDuplicate = [2]int{first, j}, true
		}
		byPath[loc] = j
	}
	if out.hasDuplicate {
		// The server stops before taking any action.
		return out
	}
	for i := range out.targets {
		t := &out.targets[i]
		if !t.pathKnown {
			return out
		}
		if omitted[i] {
			t.action = "inherit"
			if j, ok := byPath[t.path]; ok && existing[j].AccessAction.ValueString() != "" {
				t.action, t.kept = existing[j].AccessAction.ValueString(), true
			}
		}
		delete(byPath, t.path)
	}
	if !provided {
		for _, j := range byPath {
			if action := existing[j].AccessAction.ValueString(); action != "" && action != "inherit" {
				out.removed = append(out.removed, j)
			}
		}
		slices.Sort(out.removed)
	}
	return out
}

// keptProblems reports the updates the server refuses while keeping
// left-out actions.
func (a targetAccessActions) keptProblems() diag.Diagnostics {
	var diags diag.Diagnostics
	// Paths are not repeated in diagnostics: a mistaken value may carry
	// something secret, and Terraform cannot redact diagnostic text.
	if a.hasDuplicate {
		diags.AddAttributeError(path.Root("targets"), "Access action needed on every target",
			fmt.Sprintf("Current targets %d and %d share a path and a target has an access_action other than "+
				"\"inherit\", so the server keeps left-out actions only when every target sets access_action. Set "+
				"access_action on every target.", a.duplicate[0], a.duplicate[1]))
	}
	for _, j := range a.removed {
		diags.AddAttributeError(path.Root("targets"), "Access action needed when a controlled path goes",
			fmt.Sprintf("Current target %d has an access_action other than \"inherit\" on the server, and this update "+
				"removes or moves its path while no target sets access_action, which the server refuses. Set "+
				"access_action on at least one target (\"inherit\" keeps the default).", j))
	}
	return diags
}

// problems reports actions the server refuses for this service (v0.83,
// v0.82: validateL4Target, validateTargetAccessAction, validateHTTPTargets):
// a non-inherit action outside HTTP mode, bypass on a private service, a
// non-inherit action on a path that is not a canonical absolute prefix, and
// two enabled targets on one path once any enabled target has a non-inherit
// action. mode is "" when not known.
//
// A configured bypass on a private service comes back as privacy, the only
// error the service being private causes; errors that rest on an action the
// server keeps from a current target come back as updateOnly.
func (a targetAccessActions) problems(private bool, mode string) (privacy, diags, updateOnly diag.Diagnostics) {
	// strict is set by an action the request carries or defaults to;
	// keptStrict by one the server only keeps on an update.
	strict, keptStrict := false, false
	for i, t := range a.targets {
		if t.action == "" || t.action == "inherit" {
			continue
		}
		if t.enabled == 1 {
			if t.kept {
				keptStrict = true
			} else {
				strict = true
			}
		}
		at := path.Root("targets").AtListIndex(i).AtName("access_action")
		switch {
		case mode != "" && mode != "http":
			d := diag.NewAttributeErrorDiagnostic(at, "Access action needs HTTP",
				fmt.Sprintf("access_action %q is only supported for HTTP services; mode is %q. Leave it out or set \"inherit\".",
					t.action, mode))
			if t.kept {
				// Kept from the current HTTP target: a new service starts
				// at inherit.
				updateOnly.Append(d)
			} else {
				diags.Append(d)
			}
			continue
		case private && t.action == "bypass" && t.kept:
			updateOnly.AddAttributeError(at, "Bypass not allowed on a private service",
				"The current target at this path has access_action = \"bypass\" on the server, and this target keeps it "+
					"while access_action is left out, but a private service cannot have it. Set access_action = \"inherit\" or \"block\".")
		case private && t.action == "bypass":
			privacy.AddAttributeError(at, "Bypass not allowed on a private service",
				"private = true cannot be combined with access_action = \"bypass\" on a target; use \"inherit\" or \"block\".")
		}
		if t.pathKnown {
			if problem := accessActionPathProblem(t.path); problem != "" {
				diags.AddAttributeError(path.Root("targets").AtListIndex(i).AtName("path"), "Path not usable with an access action",
					fmt.Sprintf("access_action %q needs a canonical path prefix: %s.", t.action, problem))
			}
		}
	}
	if (!strict && !keptStrict) || (mode != "" && mode != "http") {
		return privacy, diags, updateOnly
	}
	seen := map[string]int{}
	for i, t := range a.targets {
		if t.enabled != 1 || !t.pathKnown {
			continue
		}
		if first, dup := seen[t.path]; dup {
			d := diag.NewAttributeErrorDiagnostic(path.Root("targets").AtListIndex(i).AtName("path"), "Duplicate target path",
				fmt.Sprintf("Targets %d and %d are both enabled at one path, which the server refuses once a target has an "+
					"access_action other than \"inherit\". Give each enabled target its own path.", first, i))
			if strict {
				diags.Append(d)
			} else {
				updateOnly.Append(d)
			}
			continue
		}
		seen[t.path] = i
	}
	return privacy, diags, updateOnly
}

// keptActionProblems refuses, before the update is sent, what the server
// refuses when it keeps left-out actions from the live targets; the plan
// only warned, since it could not tell an update from a replacement.
func keptActionProblems(ctx context.Context, req api.ServiceRequest, live *api.Service) diag.Diagnostics {
	var diags diag.Diagnostics
	// The server decides from the targets the request carries.
	targetType := ReverseProxyServiceTargetModel{}.TFType()
	var sent []attr.Value
	if req.Targets != nil {
		for _, t := range *req.Targets {
			attrs := map[string]attr.Value{}
			for n, at := range targetType.AttrTypes {
				attrs[n] = nullOf(at.ValueType(ctx))
			}
			attrs["options"] = types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes)
			attrs["path"] = types.StringPointerValue(t.Path)
			attrs["enabled"] = types.BoolValue(t.Enabled)
			if t.AccessAction != nil {
				attrs["access_action"] = types.StringValue(string(*t.AccessAction))
			}
			obj, d := types.ObjectValue(targetType.AttrTypes, attrs)
			diags.Append(d...)
			sent = append(sent, obj)
		}
	}
	requested, d := types.ListValue(targetType, sent)
	diags.Append(d...)
	var current ReverseProxyServiceModel
	diags.Append(reverseProxyServiceAPIToTerraform(ctx, live, &current)...)
	if diags.HasError() {
		return diags
	}
	var existing []ReverseProxyServiceTargetModel
	if !current.Targets.IsNull() && !current.Targets.IsUnknown() {
		diags.Append(current.Targets.ElementsAs(ctx, &existing, false)...)
	}
	if diags.HasError() {
		return diags
	}
	mode := "http"
	if req.Mode != nil {
		mode = string(*req.Mode)
	} else if live.Mode != nil {
		mode = string(*live.Mode)
	}
	actions := targetActions(requested, requested, existing, true)
	diags.Append(actions.keptProblems()...)
	_, _, updateOnly := actions.problems(req.Private != nil && *req.Private, mode)
	diags.Append(updateOnly...)
	return diags
}

// modeChangeProblem refuses a known mode other than the stored one:
// validateProtocolChange (v0.83.2, v0.82.1) allows no change but between an
// unset mode and HTTP, and the read reports unset as HTTP.
func modeChangeProblem(stored, planned types.String) diag.Diagnostics {
	var d diag.Diagnostics
	if stored.IsNull() || stored.IsUnknown() || planned.IsNull() || planned.IsUnknown() || normalMode(stored) == normalMode(planned) {
		return d
	}
	d.AddAttributeError(path.Root("mode"), "Mode cannot be changed",
		fmt.Sprintf("NetBird does not change the mode of a service (%q to %q). Keep the current mode, or replace the service (-replace, or a new netbird_reverse_proxy_service).", stored.ValueString(), planned.ValueString()))
	return d
}

// l4DisabledTargetProblems refuses a target configured enabled = false on
// an L4 service.
func l4DisabledTargetProblems(targets types.List) diag.Diagnostics {
	var d diag.Diagnostics
	if targets.IsNull() || targets.IsUnknown() {
		return d
	}
	for i, e := range targets.Elements() {
		obj, ok := e.(types.Object)
		if !ok || obj.IsNull() || obj.IsUnknown() {
			continue
		}
		if enabled, ok := obj.Attributes()["enabled"].(types.Bool); ok && enabled.Equal(types.BoolValue(false)) {
			d.AddAttributeError(path.Root("targets").AtListIndex(i).AtName("enabled"), "L4 targets are always enabled",
				"NetBird keeps the target of a TCP, UDP or TLS service enabled; set enabled on the service instead.")
		}
	}
	return d
}

// normalMode is a known mode with unset read as HTTP, as the server does.
func normalMode(v types.String) string {
	if v.ValueString() == "" {
		return "http"
	}
	return v.ValueString()
}

// inPlaceOnly turns errors that only an in-place update meets into
// warnings: the plan cannot tell an update from a replacement (-replace
// plans against the current service first), and a new service keeps
// nothing. Update refuses the same cases before writing.
func inPlaceOnly(errs diag.Diagnostics) diag.Diagnostics {
	var out diag.Diagnostics
	for _, d := range errs {
		detail := d.Detail() + " An in-place update stops before changing anything; a replacement is not affected."
		if wp, ok := d.(diag.DiagnosticWithPath); ok {
			out.AddAttributeWarning(wp.Path(), d.Summary(), detail)
		} else {
			out.AddWarning(d.Summary(), detail)
		}
	}
	return out
}

// accessActionPathProblem returns why a path cannot carry a non-inherit
// action (v0.83, v0.82: validateAccessActionPath), or "".
func accessActionPathProblem(p string) string {
	if p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		return "it must start with /"
	}
	if !utf8.ValidString(p) || strings.ContainsAny(p, "%\\?#;") || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return "it must not contain %, \\, ?, #, ; or control characters"
	}
	segments := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, segment := range segments {
		if segment == "." || segment == ".." || (segment == "" && i != len(segments)-1) {
			return "it must not contain empty, . or .. segments"
		}
	}
	return ""
}

// targetHostProblems refuses a configured host the server rejects for the
// service's mode and the target's type (v0.83, v0.82:
// validateDirectUpstreamHost, validateSubnetTarget, validateL4Target). A
// left-out host is not sent, so it counts as empty. Checks that depend on a
// value not known at plan time wait for it; mode "" means not known, and
// then only the rules both modes share are checked. The
// host itself is not repeated in the diagnostic, since a mistaken value may
// carry credentials.
func targetHostProblems(configured types.List, mode string) diag.Diagnostics {
	var diags diag.Diagnostics
	if configured.IsNull() || configured.IsUnknown() {
		return diags
	}
	for i, e := range configured.Elements() {
		obj, ok := e.(types.Object)
		if !ok || obj.IsNull() || obj.IsUnknown() {
			continue
		}
		attrs := obj.Attributes()
		targetType, _ := attrs["target_type"].(types.String)
		host, _ := attrs["host"].(types.String)
		if targetType.IsNull() || host.IsUnknown() {
			continue
		}
		direct := 0
		if opts, ok := attrs["options"].(types.Object); ok && opts.IsUnknown() {
			direct = -1
		} else if ok && !opts.IsNull() {
			if v, ok := opts.Attributes()["direct_upstream"].(types.Bool); ok && v.IsUnknown() {
				direct = -1
			} else if ok && v.ValueBool() {
				direct = 1
			}
		}
		problem := ""
		switch {
		case !targetType.IsUnknown():
			problem = hostProblem(mode, targetType.ValueString(), host.ValueString(), direct)
		case mode == "http" && direct == 1:
			// Every target type of an HTTP service checks a direct
			// upstream host the same way, apart from an empty one.
			problem = directHostProblem(strings.TrimSpace(host.ValueString()), direct)
		}
		if problem != "" {
			diags.AddAttributeError(path.Root("targets").AtListIndex(i).AtName("host"), "Host not usable",
				fmt.Sprintf("The server refuses this target's host: %s.", problem))
		}
	}
	return diags
}

// hostProblem returns why the server refuses host for a target, or "".
// direct is 1 with direct_upstream, 0 without, -1 when not known.
func hostProblem(mode, targetType, host string, direct int) string {
	trimmed := strings.TrimSpace(host)
	if targetType != "subnet" {
		return directHostProblem(trimmed, direct) // the same in every mode
	}
	if mode != "http" {
		// L4 (or not known yet: an empty host fails in every mode).
		if host == "" {
			return "a subnet target needs a host"
		}
		return ""
	}
	if trimmed == "" {
		return "a subnet target needs a host"
	}
	if problem := hostSyntaxProblem(trimmed); problem != "" {
		return problem
	}
	ip, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
	switch {
	case err != nil:
		return ""
	case ip.Zone() != "":
		return "IPv6 zones are refused"
	case direct == 1 && (ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast()):
		return "with direct_upstream, loopback, multicast and link-local addresses are refused"
	}
	return ""
}

// directHostProblem checks a peer, host or domain target's host, which the
// server validates only with direct_upstream; an empty one is replaced by
// the looked-up address.
func directHostProblem(trimmed string, direct int) string {
	if direct != 1 || trimmed == "" {
		return ""
	}
	if problem := hostSyntaxProblem(trimmed); problem != "" {
		return problem
	}
	ip, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
	if err == nil && (ip.Zone() != "" || ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast()) {
		return "with direct_upstream, loopback, multicast, link-local and zoned addresses are refused"
	}
	return ""
}

// hostSyntaxProblem returns the server's syntax objection to a trimmed host.
func hostSyntaxProblem(trimmed string) string {
	if strings.ContainsAny(trimmed, " \t/") {
		return "it contains a space, tab or slash"
	}
	if _, _, err := net.SplitHostPort(trimmed); err == nil {
		return "it includes a port (set port instead)"
	}
	return ""
}

// onlyUnknownDifferences reports whether every known value of plan equals
// the one in state, so the plan changes nothing unless a value not known yet
// turns out different.
func onlyUnknownDifferences(plan, state tftypes.Value) bool {
	if !plan.IsKnown() {
		return true
	}
	if plan.IsNull() || state.IsNull() || !state.IsKnown() || !plan.Type().Equal(state.Type()) {
		return plan.Equal(state)
	}
	switch plan.Type().(type) {
	case tftypes.Object, tftypes.Map:
		var p, s map[string]tftypes.Value
		if plan.As(&p) != nil || state.As(&s) != nil || len(p) != len(s) {
			return false
		}
		for k, v := range p {
			if sv, ok := s[k]; !ok || !onlyUnknownDifferences(v, sv) {
				return false
			}
		}
		return true
	case tftypes.List, tftypes.Tuple:
		var p, s []tftypes.Value
		if plan.As(&p) != nil || state.As(&s) != nil || len(p) != len(s) {
			return false
		}
		for i := range p {
			if !onlyUnknownDifferences(p[i], s[i]) {
				return false
			}
		}
		return true
	}
	return plan.Equal(state)
}

// directUpstreamWarnings flags direct_upstream on an L4 service: NetBird
// (v0.83, v0.82) stores it but does not pass it to the proxy for TCP, UDP
// and TLS services, so the target is still dialled through the embedded
// client.
func directUpstreamWarnings(configured types.List, mode string) diag.Diagnostics {
	var diags diag.Diagnostics
	if configured.IsNull() || configured.IsUnknown() {
		return diags
	}
	for i, e := range configured.Elements() {
		obj, ok := e.(types.Object)
		if !ok || obj.IsNull() || obj.IsUnknown() {
			continue
		}
		opts, ok := obj.Attributes()["options"].(types.Object)
		if !ok || opts.IsNull() || opts.IsUnknown() {
			continue
		}
		if v, ok := opts.Attributes()["direct_upstream"].(types.Bool); ok && !v.IsUnknown() && v.ValueBool() {
			diags.AddAttributeWarning(path.Root("targets").AtListIndex(i).AtName("options").AtName("direct_upstream"),
				"direct_upstream has no effect in this mode",
				fmt.Sprintf("NetBird stores direct_upstream for a %q service but does not pass it to the proxy, which dials "+
					"the target through its embedded NetBird client as before. It takes effect for HTTP services only.", mode))
		}
	}
	return diags
}

// targetLocation is the server's reading of a target path: left out is "/".
func targetLocation(p types.String) string {
	if p.IsNull() || p.ValueString() == "" {
		return "/"
	}
	return p.ValueString()
}

// privateRequestProblem checks the access groups the update would send for a
// private service once the live values of unknown settings are merged in;
// the plan could not see those.
func privateRequestProblem(req api.ServiceRequest, wasPrivate bool) error {
	if req.Private == nil || !*req.Private {
		return nil
	}
	// The server omits an empty list, so nil from a live read means none.
	if req.AccessGroups == nil || len(*req.AccessGroups) == 0 {
		return fmt.Errorf("the service would be private with no access groups. Nothing was changed; set access_groups")
	}
	for _, id := range *req.AccessGroups {
		if !wasPrivate && strings.TrimSpace(id) == "" {
			return fmt.Errorf("the service's access groups on the server include the blank ID %q, which no peer matches. Nothing was changed; set access_groups", id)
		}
	}
	return nil
}

// notBlank refuses a string that is empty or only whitespace.
type notBlank struct{}

func (notBlank) Description(context.Context) string         { return "must not be blank" }
func (notBlank) MarkdownDescription(context.Context) string { return "must not be blank" }

func (notBlank) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if strings.TrimSpace(req.ConfigValue.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Blank group ID",
			fmt.Sprintf("%s must not be blank, got %q.", req.Path, req.ConfigValue.ValueString()))
	}
}

// keepLiveUnknownAccess sends the live value of an access setting the plan
// leaves unknown, so the whole-service update does not drop it.
func keepLiveUnknownAccess(req *api.ServiceRequest, plan ReverseProxyServiceModel, live *api.Service) {
	if plan.Private.IsUnknown() {
		v := live.Private != nil && *live.Private
		req.Private = &v
	}
	if plan.AccessGroups.IsUnknown() {
		req.AccessGroups = live.AccessGroups
	}
}

// privateServiceProblems returns the errors the server would give for a
// private service with these values; unknown values are not checked.
func privateServiceProblems(mode types.String, groups types.List, auth types.Object, checkBlank bool) diag.Diagnostics {
	var diags diag.Diagnostics
	if !mode.IsNull() && !mode.IsUnknown() && mode.ValueString() != "http" {
		diags.AddAttributeError(path.Root("mode"), "Private services are HTTP only",
			fmt.Sprintf("private = true requires mode = \"http\", got %q. An existing service keeps its mode when mode is "+
				"left out, and the server does not change the mode of an existing service.", mode.ValueString()))
	}
	if !groups.IsNull() && !groups.IsUnknown() {
		if len(groups.Elements()) == 0 {
			diags.AddAttributeError(path.Root("access_groups"), "Access groups required",
				"private = true requires at least one group in access_groups.")
		}
		for i, v := range groups.Elements() {
			if !checkBlank {
				break
			}
			if id, ok := v.(types.String); ok && !id.IsNull() && !id.IsUnknown() && strings.TrimSpace(id.ValueString()) == "" {
				diags.AddAttributeError(path.Root("access_groups").AtListIndex(i), "Blank group ID",
					fmt.Sprintf("private = true needs real group IDs in access_groups, got %q; no peer would match it.", id.ValueString()))
			}
		}
	}
	if auth.IsNull() || auth.IsUnknown() {
		return diags
	}
	bearer, ok := auth.Attributes()["bearer_auth"].(types.Object)
	if !ok || bearer.IsNull() || bearer.IsUnknown() {
		return diags
	}
	if enabled, ok := bearer.Attributes()["enabled"].(types.Bool); ok && !enabled.IsUnknown() && enabled.ValueBool() {
		diags.AddAttributeError(path.Root("auth").AtName("bearer_auth"), "Bearer auth not allowed",
			"private = true is NetBird-only access and cannot be combined with bearer auth (SSO).")
	}
	return diags
}

// serviceAccessChangedSincePlan lists the access settings that the plan shows
// unchanged from prior but that the live service no longer has; settings the
// plan changes get the planned value.
func serviceAccessChangedSincePlan(prior, plan ReverseProxyServiceModel, live *api.Service) []string {
	var changed []string
	livePrivate := live.Private != nil && *live.Private
	if plan.Private.Equal(prior.Private) && !prior.Private.IsNull() && !prior.Private.IsUnknown() && prior.Private.ValueBool() != livePrivate {
		changed = append(changed, fmt.Sprintf("- private: %t when planned, now %t", prior.Private.ValueBool(), livePrivate))
	}
	if !prior.AccessGroups.IsNull() && !prior.AccessGroups.IsUnknown() && sameElements(plan.AccessGroups, prior.AccessGroups) {
		var was []string
		for _, v := range prior.AccessGroups.Elements() {
			if s, ok := v.(types.String); ok {
				was = append(was, s.ValueString())
			}
		}
		var now []string
		if live.AccessGroups != nil {
			now = *live.AccessGroups
		}
		if !sameStrings(was, now) {
			changed = append(changed, fmt.Sprintf("- access_groups: %v when planned, now %v", sortedCopy(was), sortedCopy(now)))
		}
	}
	return changed
}

func sortedCopy(v []string) []string {
	c := slices.Clone(v)
	slices.Sort(c)
	if c == nil {
		return []string{}
	}
	return c
}

func sameStrings(a, b []string) bool {
	return slices.Equal(sortedCopy(a), sortedCopy(b))
}
