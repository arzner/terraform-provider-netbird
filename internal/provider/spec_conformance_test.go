// Copyright (c) HashiCorp, Inc.

package provider

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"go.yaml.in/yaml/v3"
)

// Test_SpecConformance compares every resource's request schemas in the NetBird
// OpenAPI spec, at the netbird version go.mod pins, with the resource's
// Terraform schema. Each request property must be modelled by an attribute or
// block, or be listed in specUnmodelled with the reason it is not. A property a
// NetBird release adds therefore fails this test until someone decides what the
// provider does with it, instead of being reset silently on a user's account.
//
// Top-level request properties are checked, plus the nested objects whose
// members the provider flattens into top-level attributes or blocks (account
// extra settings, posture checks, guardrail checks, policy limits). Other
// nested objects, such as policy rules, are not compared yet. Read-only
// properties need no attribute; if one has a configurable attribute, that is
// an error.
func Test_SpecConformance(t *testing.T) {
	spec := loadSpec(t)

	seenAllowlist := map[string]bool{}
	checked := map[string]bool{}
	for _, rm := range specResources {
		attrs := resourceAttributes(t, rm.resource())
		for _, sm := range rm.schemas {
			props, ok := spec.properties(sm.schema)
			if !ok {
				t.Errorf("%s: schema %s is not in the spec", rm.typeName, sm.schema)
				continue
			}
			for _, name := range sortedKeys(sm.flatten) {
				if _, ok := props[name]; !ok {
					t.Errorf("%s: flattened property %s.%s is not in the spec any more; update specResources", rm.typeName, sm.schema, name)
				}
			}
			for _, name := range sortedKeys(props) {
				prop := props[name]
				key := sm.schema + "." + name
				checked[key] = true
				if _, listed := specUnmodelled[key]; listed {
					seenAllowlist[key] = true
					if a, modelled := attrs[attrName(key, name)]; modelled {
						t.Errorf("%s: %s is listed in specUnmodelled but modelled by %q; remove it from the list", rm.typeName, key, a.name)
					}
					continue
				}
				if child, flattened := sm.flatten[name]; flattened {
					// The provider spreads this object's members over the
					// resource; they are checked as the child schema, which must
					// be what the property refers to and must itself be checked.
					if prop.ref != child {
						t.Errorf("%s: %s refers to schema %q, not the flattened %q; update specResources", rm.typeName, key, prop.ref, child)
					}
					if !rm.checks(child) {
						t.Errorf("%s: flattened schema %s is not checked; add it to specResources", rm.typeName, child)
					}
					continue
				}
				a, modelled := attrs[attrName(key, name)]
				if !modelled && prop.readOnly {
					// Computed by the server and never sent; nothing to model.
					continue
				}
				if !modelled {
					t.Errorf("%s: %s (NetBird %s) is neither modelled nor listed in specUnmodelled", rm.typeName, key, spec.version)
					continue
				}
				if prop.readOnly && a.configurable {
					t.Errorf("%s: %s is read-only in the spec but %q is configurable", rm.typeName, key, a.name)
				}
			}
		}
	}

	for key, reason := range specUnmodelled {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("specUnmodelled lists %s without a reason", key)
		}
		if !seenAllowlist[key] {
			t.Errorf("specUnmodelled lists %s, which no checked schema has any more; remove it", key)
		}
	}
	for key := range specRenamed {
		if !checked[key] {
			t.Errorf("specRenamed lists %s, which no checked schema has any more; remove it", key)
		}
	}
}

// specResource maps a Terraform resource to the request schemas it sends.
type specResource struct {
	typeName string
	resource func() resource.Resource
	schemas  []specSchema
}

type specSchema struct {
	schema string
	// flatten maps a property whose members the provider spreads over the
	// resource to the schema it refers to; that schema is checked as its own
	// specSchema entry.
	flatten map[string]string
}

// checks reports whether the resource's schema list includes name.
func (r specResource) checks(name string) bool {
	for _, s := range r.schemas {
		if s.schema == name {
			return true
		}
	}
	return false
}

var specResources = []specResource{
	{"netbird_account_settings", NewAccountSettings, []specSchema{{schema: "AccountSettings", flatten: map[string]string{"extra": "AccountExtraSettings"}}, {schema: "AccountExtraSettings"}}},
	{"netbird_agent_network_guardrail", NewAgentNetworkGuardrail, []specSchema{{schema: "AgentNetworkGuardrailRequest", flatten: map[string]string{"checks": "AgentNetworkGuardrailChecks"}}, {schema: "AgentNetworkGuardrailChecks"}}},
	{"netbird_agent_network_policy", NewAgentNetworkPolicy, []specSchema{{schema: "AgentNetworkPolicyRequest", flatten: map[string]string{"limits": "AgentNetworkPolicyLimits"}}, {schema: "AgentNetworkPolicyLimits"}}},
	{"netbird_agent_network_provider", NewAgentNetworkProvider, []specSchema{{schema: "AgentNetworkProviderRequest"}}},
	{"netbird_agent_network_settings", NewAgentNetworkSettings, []specSchema{{schema: "AgentNetworkSettingsRequest"}, {schema: "AgentNetworkSettingsCreateRequest"}}},
	{"netbird_dns_record", NewDNSRecord, []specSchema{{schema: "DNSRecordRequest"}}},
	{"netbird_dns_settings", NewDNSSettings, []specSchema{{schema: "DNSSettings"}}},
	{"netbird_dns_zone", NewDNSZone, []specSchema{{schema: "ZoneRequest"}}},
	{"netbird_group", NewGroup, []specSchema{{schema: "GroupRequest"}}},
	{"netbird_identity_provider", NewIdentityProvider, []specSchema{{schema: "IdentityProviderRequest"}}},
	{"netbird_nameserver_group", NewNameserverGroup, []specSchema{{schema: "NameserverGroupRequest"}}},
	{"netbird_network", NewNetwork, []specSchema{{schema: "NetworkRequest"}}},
	{"netbird_network_resource", NewNetworkResource, []specSchema{{schema: "NetworkResourceRequest"}}},
	{"netbird_network_router", NewNetworkRouter, []specSchema{{schema: "NetworkRouterRequest"}}},
	{"netbird_peer", NewPeer, []specSchema{{schema: "PeerRequest"}}},
	{"netbird_policy", NewPolicy, []specSchema{{schema: "PolicyUpdate"}}},
	{"netbird_posture_check", NewPostureCheck, []specSchema{{schema: "PostureCheckUpdate", flatten: map[string]string{"checks": "Checks"}}, {schema: "Checks"}}},
	{"netbird_reverse_proxy_domain", NewReverseProxyDomain, []specSchema{{schema: "ReverseProxyDomainRequest"}}},
	{"netbird_reverse_proxy_service", NewReverseProxyService, []specSchema{{schema: "ServiceRequest"}}},
	{"netbird_route", NewRoute, []specSchema{{schema: "RouteRequest"}}},
	{"netbird_scim", NewScim, []specSchema{{schema: "CreateScimIntegrationRequest"}, {schema: "UpdateScimIntegrationRequest"}}},
	{"netbird_setup_key", NewSetupKey, []specSchema{{schema: "CreateSetupKeyRequest"}, {schema: "SetupKeyRequest"}}},
	{"netbird_token", NewToken, []specSchema{{schema: "PersonalAccessTokenRequest"}}},
	{"netbird_user", NewUser, []specSchema{{schema: "UserCreateRequest"}, {schema: "UserRequest"}}},
}

type attrInfo struct {
	name         string
	configurable bool
}

// resourceAttributes returns the resource's top-level attributes and blocks.
func resourceAttributes(t *testing.T, r resource.Resource) map[string]attrInfo {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	out := map[string]attrInfo{}
	for name, a := range resp.Schema.Attributes {
		out[name] = attrInfo{name: name, configurable: a.IsRequired() || a.IsOptional()}
	}
	for name := range resp.Schema.Blocks {
		out[name] = attrInfo{name: name, configurable: true}
	}
	return out
}

// attrName is the attribute a spec property maps to: its own name unless
// specRenamed says otherwise.
func attrName(key, property string) string {
	if a, ok := specRenamed[key]; ok {
		return a
	}
	return property
}

// specProperty is a resolved request property: whether it is read-only and,
// when it refers to a component schema, which one.
type specProperty struct {
	readOnly bool
	ref      string
}

type openAPISpec struct {
	version string
	schemas map[string]schemaDoc
}

// loadSpec reads openapi.yml from the netbird module go.mod pins.
func loadSpec(t *testing.T) openAPISpec {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}} {{.Version}}", "github.com/netbirdio/netbird").Output()
	if err != nil {
		t.Fatalf("locate the netbird module (run `go mod download`): %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 || fields[0] == "" {
		t.Fatalf("locate the netbird module: unexpected output %q (run `go mod download`)", out)
	}
	raw, err := os.ReadFile(filepath.Join(fields[0], "shared", "management", "http", "api", "openapi.yml"))
	if err != nil {
		t.Fatalf("read the OpenAPI spec: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]schemaDoc `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse the OpenAPI spec: %v", err)
	}
	if len(doc.Components.Schemas) == 0 {
		t.Fatal("the OpenAPI spec has no component schemas")
	}
	return openAPISpec{version: fields[1], schemas: doc.Components.Schemas}
}

type schemaDoc struct {
	Ref        string               `yaml:"$ref"`
	AllOf      []schemaDoc          `yaml:"allOf"`
	ReadOnly   bool                 `yaml:"readOnly"`
	Properties map[string]schemaDoc `yaml:"properties"`
}

func refName(ref string) string {
	return strings.TrimPrefix(ref, "#/components/schemas/")
}

// property resolves a property's read-only flag through its own $ref and
// allOf parts: it is read-only when any part says so. ref is the component it
// refers to, directly or as its only allOf part.
func (s openAPISpec) property(d schemaDoc, depth int) (specProperty, bool) {
	if depth > 16 {
		return specProperty{}, false
	}
	out := specProperty{readOnly: d.ReadOnly}
	if d.Ref != "" {
		inner, ok := s.schemas[refName(d.Ref)]
		if !ok {
			return specProperty{}, false
		}
		out.ref = refName(d.Ref)
		out.readOnly = out.readOnly || inner.ReadOnly
	}
	for _, part := range d.AllOf {
		p, ok := s.property(part, depth+1)
		if !ok {
			return specProperty{}, false
		}
		out.readOnly = out.readOnly || p.readOnly
		if len(d.AllOf) == 1 && out.ref == "" {
			out.ref = p.ref
		}
	}
	return out, true
}

// properties resolves a component schema's properties through $ref and allOf.
func (s openAPISpec) properties(name string) (map[string]specProperty, bool) {
	doc, ok := s.schemas[name]
	if !ok {
		return nil, false
	}
	out := map[string]specProperty{}
	var walk func(d schemaDoc, depth int) bool
	walk = func(d schemaDoc, depth int) bool {
		if depth > 16 {
			return false
		}
		if d.Ref != "" {
			inner, ok := s.schemas[refName(d.Ref)]
			if !ok {
				return false
			}
			return walk(inner, depth+1)
		}
		for _, part := range d.AllOf {
			if !walk(part, depth+1) {
				return false
			}
		}
		for p, v := range d.Properties {
			prop, ok := s.property(v, depth+1)
			if !ok {
				return false
			}
			// A property repeated across allOf parts keeps every restriction.
			if prev, seen := out[p]; seen {
				prop.readOnly = prop.readOnly || prev.readOnly
				if prop.ref == "" {
					prop.ref = prev.ref
				}
			}
			out[p] = prop
		}
		return true
	}
	if !walk(doc, 0) {
		return nil, false
	}
	return out, true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Test_specResolver pins the resolver's handling of references and
// composition on small fixtures, independent of the real spec.
func Test_specResolver(t *testing.T) {
	var doc struct {
		Schemas map[string]schemaDoc `yaml:"schemas"`
	}
	const fixture = `
schemas:
  Child:
    type: object
    properties:
      a: {type: string}
  ReadOnlyThing:
    readOnly: true
    type: string
  Base:
    type: object
    properties:
      shared: {type: string}
  Request:
    allOf:
      - $ref: '#/components/schemas/Base'
      - type: object
        properties:
          shared: {type: string, readOnly: true}
          child: {$ref: '#/components/schemas/Child'}
          wrapped:
            allOf:
              - $ref: '#/components/schemas/Child'
          computed: {$ref: '#/components/schemas/ReadOnlyThing'}
          composed:
            allOf:
              - {type: string}
              - {readOnly: true}
          plain: {type: string}
`
	if err := yaml.Unmarshal([]byte(fixture), &doc); err != nil {
		t.Fatal(err)
	}
	s := openAPISpec{schemas: doc.Schemas}
	props, ok := s.properties("Request")
	if !ok {
		t.Fatal("Request did not resolve")
	}
	want := map[string]specProperty{
		"shared":   {readOnly: true},
		"child":    {ref: "Child"},
		"wrapped":  {ref: "Child"},
		"computed": {readOnly: true, ref: "ReadOnlyThing"},
		"composed": {readOnly: true},
		"plain":    {},
	}
	for name, w := range want {
		if got := props[name]; got != w {
			t.Errorf("%s: expected %+v, found %+v", name, w, got)
		}
	}
	if len(props) != len(want) {
		t.Errorf("expected %d properties, found %d: %v", len(want), len(props), props)
	}
	if _, ok := s.properties("Missing"); ok {
		t.Error("an unknown schema must not resolve")
	}
}
