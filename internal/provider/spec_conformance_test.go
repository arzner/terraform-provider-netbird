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
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"go.yaml.in/yaml/v3"
)

// Test_SpecConformance compares every resource's request schemas in the NetBird
// OpenAPI spec, at the netbird version go.mod pins, with the resource's
// Terraform schema. Each request property must be modelled by an attribute or
// block, or be listed in specUnmodelled with the reason it is not. A property a
// NetBird release adds therefore fails this test until someone decides what the
// provider does with it, instead of being reset silently on a user's account.
//
// Top-level request properties are checked, the nested objects whose members
// the provider flattens into top-level attributes or blocks (account extra
// settings, posture checks, guardrail checks, policy limits), and, recursively,
// every nested object or array of objects that a nested attribute or block
// models (policy rules, reverse-proxy targets and auth, and so on). Read-only
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
				checkNested(t, spec, rm.typeName, prop, a, seenAllowlist, checked, 1)
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
	for key := range specNestedFlattened {
		if !checked[key] {
			t.Errorf("specNestedFlattened lists %s, which no checked schema has any more; remove it", key)
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
	// children are the attributes and blocks of a nested attribute or block;
	// nil for a plain attribute.
	children map[string]attrInfo
}

// resourceAttributes returns the resource's attributes and blocks, with the
// members of nested ones.
func resourceAttributes(t *testing.T, r resource.Resource) map[string]attrInfo {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return schemaMembers(resp.Schema.Attributes, resp.Schema.Blocks)
}

func schemaMembers(attributes map[string]rschema.Attribute, blocks map[string]rschema.Block) map[string]attrInfo {
	out := map[string]attrInfo{}
	for name, a := range attributes {
		info := attrInfo{name: name, configurable: a.IsRequired() || a.IsOptional()}
		switch n := a.(type) {
		case rschema.SingleNestedAttribute:
			info.children = schemaMembers(n.Attributes, nil)
		case rschema.ListNestedAttribute:
			info.children = schemaMembers(n.NestedObject.Attributes, nil)
		case rschema.SetNestedAttribute:
			info.children = schemaMembers(n.NestedObject.Attributes, nil)
		case rschema.MapNestedAttribute:
			info.children = schemaMembers(n.NestedObject.Attributes, nil)
		}
		out[name] = info
	}
	for name, b := range blocks {
		info := attrInfo{name: name, configurable: true}
		switch n := b.(type) {
		case rschema.SingleNestedBlock:
			info.children = schemaMembers(n.Attributes, n.Blocks)
		case rschema.ListNestedBlock:
			info.children = schemaMembers(n.NestedObject.Attributes, n.NestedObject.Blocks)
		case rschema.SetNestedBlock:
			info.children = schemaMembers(n.NestedObject.Attributes, n.NestedObject.Blocks)
		}
		out[name] = info
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
	// items is the component an array property's items refer to.
	items string
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
	Items      *schemaDoc           `yaml:"items"`
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
	if d.Items != nil && d.Items.Ref != "" {
		out.items = refName(d.Items.Ref)
	}
	if d.Ref != "" {
		inner, ok := s.schemas[refName(d.Ref)]
		if !ok {
			return specProperty{}, false
		}
		out.ref = refName(d.Ref)
		// The referenced schema may itself be a reference or a composition.
		resolved, ok := s.property(inner, depth+1)
		if !ok {
			return specProperty{}, false
		}
		out.readOnly = out.readOnly || resolved.readOnly
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
				if prop.items == "" {
					prop.items = prev.items
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
  Alias:
    allOf:
      - $ref: '#/components/schemas/ReadOnlyThing'
  AliasOfAlias:
    $ref: '#/components/schemas/Alias'
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
          chained: {$ref: '#/components/schemas/Alias'}
          chainedTwice:
            allOf:
              - $ref: '#/components/schemas/AliasOfAlias'
          children:
            type: array
            items:
              $ref: '#/components/schemas/Child'
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
		"shared":       {readOnly: true},
		"child":        {ref: "Child"},
		"wrapped":      {ref: "Child"},
		"computed":     {readOnly: true, ref: "ReadOnlyThing"},
		"composed":     {readOnly: true},
		"plain":        {},
		"chained":      {readOnly: true, ref: "Alias"},
		"chainedTwice": {readOnly: true, ref: "AliasOfAlias"},
		"children":     {items: "Child"},
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

// checkNested compares the members of the component a nested attribute or
// block models (prop.ref for an object, prop.items for an array of objects)
// with the attribute's own members, with the same rules as the top level.
func checkNested(t *testing.T, spec openAPISpec, typeName string, prop specProperty, a attrInfo, seen, checked map[string]bool, depth int) {
	t.Helper()
	component := prop.ref
	if component == "" {
		component = prop.items
	}
	if a.children == nil || component == "" || depth > 6 {
		return
	}
	props, ok := spec.properties(component)
	if !ok {
		t.Errorf("%s: nested schema %s of %q is not in the spec", typeName, component, a.name)
		return
	}
	for _, name := range sortedKeys(props) {
		p := props[name]
		key := component + "." + name
		checked[key] = true
		if specNestedFlattened[key] {
			// The provider spreads this array's item members over the parent.
			if p.items == "" {
				t.Errorf("%s: %s is listed in specNestedFlattened but is not an array of objects", typeName, key)
				continue
			}
			checkNested(t, spec, typeName, specProperty{items: p.items}, a, seen, checked, depth+1)
			continue
		}
		if _, listed := specUnmodelled[key]; listed {
			seen[key] = true
			if c, modelled := a.children[attrName(key, name)]; modelled {
				t.Errorf("%s: %s is listed in specUnmodelled but modelled by %q; remove it from the list", typeName, key, c.name)
			}
			continue
		}
		c, modelled := a.children[attrName(key, name)]
		if !modelled && p.readOnly {
			continue
		}
		if !modelled {
			t.Errorf("%s: %s (NetBird %s, inside %q) is neither modelled nor listed in specUnmodelled", typeName, key, spec.version, a.name)
			continue
		}
		if p.readOnly && c.configurable {
			t.Errorf("%s: %s is read-only in the spec but %q is configurable", typeName, key, c.name)
		}
		checkNested(t, spec, typeName, p, c, seen, checked, depth+1)
	}
}
