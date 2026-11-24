// Copyright (c) HashiCorp, Inc.

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Test_ReverseProxyService_legacyStatePlan plans through the provider server,
// as Terraform does with -refresh=false, against state written before private
// and access_groups existed (both null). An unchanged configuration stays a
// no-op, so the service is not rewritten; a real change leaves both unknown,
// so the update sends the live values instead of none.
func Test_ReverseProxyService_legacyStatePlan(t *testing.T) {
	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	var sch resource.SchemaResponse
	(&ReverseProxyService{}).Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	object := func(v map[string]tftypes.Value) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		for n, x := range v {
			values[n] = x
		}
		return tftypes.NewValue(typ, values)
	}
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	dynamic := func(v tftypes.Value) *tfprotov6.DynamicValue {
		d, err := tfprotov6.NewDynamicValue(typ, v)
		if err != nil {
			t.Fatal(err)
		}
		return &d
	}
	targetsType, ok := typ.AttributeTypes["targets"].(tftypes.List)
	if !ok {
		t.Fatal("targets is not a list")
	}
	targetType, ok := targetsType.ElementType.(tftypes.Object)
	if !ok {
		t.Fatal("a target is not an object")
	}
	target := func(v map[string]tftypes.Value) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range targetType.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		for n, x := range v {
			values[n] = x
		}
		return tftypes.NewValue(targetsType, []tftypes.Value{tftypes.NewValue(targetType, values)})
	}
	authType, ok := typ.AttributeTypes["auth"].(tftypes.Object)
	if !ok {
		t.Fatal("auth is not an object")
	}
	emptyAuth := map[string]tftypes.Value{}
	for n, at := range authType.AttributeTypes {
		emptyAuth[n] = tftypes.NewValue(at, nil)
	}
	configuredTarget := map[string]tftypes.Value{
		"target_id": str("peer1"), "target_type": str("peer"),
		"port": tftypes.NewValue(tftypes.Number, 8080), "protocol": str("http"),
	}
	priorTarget := map[string]tftypes.Value{"host": str("100.64.0.5"), "path": str("/"), "enabled": tftypes.NewValue(tftypes.Bool, true)}
	for k, v := range configuredTarget {
		priorTarget[k] = v
	}
	configured := map[string]tftypes.Value{
		"name": str("svc"), "domain": str("svc.example"), "mode": str("http"),
		"targets": target(configuredTarget), "auth": tftypes.NewValue(authType, emptyAuth),
	}
	boolean := func(b bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, b) }
	// State as 0.1.0 wrote it: every attribute it knew, the new ones null.
	prior := map[string]tftypes.Value{
		"id": str("s1"), "proxy_cluster": str("example"), "enabled": boolean(true),
		"pass_host_header": boolean(false), "rewrite_redirects": boolean(false),
		"listen_port": tftypes.NewValue(tftypes.Number, 0), "port_auto_assigned": boolean(false),
	}
	for k, v := range configured {
		prior[k] = v
	}
	prior["targets"] = target(priorTarget)

	for name, tc := range map[string]struct {
		rename      bool
		wantUnknown bool
		noMode      bool // state from before mode, configuration without it
		emptyMode   bool // mode stored as "" by an older read, left out
		unknownName bool // the new name is known only at apply
		private     bool // made private with a group
	}{
		"name unknown, empty mode": {rename: true, emptyMode: true, unknownName: true},
		"made private, empty mode": {emptyMode: true, private: true},
		"renamed, empty mode":      {rename: true, wantUnknown: true, emptyMode: true},
		"unchanged":                {},
		"renamed":                  {rename: true, wantUnknown: true},
		"renamed, no mode":         {rename: true, wantUnknown: true, noMode: true},
	} {
		t.Run(name, func(t *testing.T) {
			config := map[string]tftypes.Value{}
			proposed := map[string]tftypes.Value{}
			for k, v := range configured {
				config[k] = v
			}
			for k, v := range prior {
				proposed[k] = v
			}
			prior := prior
			if tc.emptyMode {
				empty := map[string]tftypes.Value{}
				for k, v := range prior {
					empty[k] = v
				}
				empty["mode"] = str("")
				delete(config, "mode")
				proposed["mode"] = str("")
				prior = empty
			}
			if tc.noMode {
				noMode := map[string]tftypes.Value{}
				for k, v := range prior {
					noMode[k] = v
				}
				delete(noMode, "mode")
				delete(config, "mode")
				delete(proposed, "mode")
				prior = noMode
			}
			if tc.rename {
				config["name"], proposed["name"] = str("svc2"), str("svc2")
			}
			if tc.unknownName {
				unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
				config["name"], proposed["name"] = unknown, unknown
			}
			if tc.private {
				groups := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{str("g1")})
				config["private"], proposed["private"] = boolean(true), boolean(true)
				config["access_groups"], proposed["access_groups"] = groups, groups
			}
			if _, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{}); err != nil {
				t.Fatal(err)
			}
			valid, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{
				TypeName: "netbird_reverse_proxy_service", Config: dynamic(object(config)),
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range valid.Diagnostics {
				if d.Severity == tfprotov6.DiagnosticSeverityError {
					t.Fatalf("configuration: %s: %s", d.Summary, d.Detail)
				}
			}
			resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
				TypeName:         "netbird_reverse_proxy_service",
				PriorState:       dynamic(object(prior)),
				ProposedNewState: dynamic(object(proposed)),
				Config:           dynamic(object(config)),
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range resp.Diagnostics {
				if d.Severity == tfprotov6.DiagnosticSeverityError {
					t.Fatalf("plan: %s: %s", d.Summary, d.Detail)
				}
			}
			planned, err := resp.PlannedState.Unmarshal(typ)
			if err != nil {
				t.Fatal(err)
			}
			var attrs map[string]tftypes.Value
			if err := planned.As(&attrs); err != nil {
				t.Fatal(err)
			}
			if !tc.rename && !tc.private && !planned.Equal(object(prior)) {
				t.Errorf("an unchanged plan must equal the prior state, found %v", planned)
			}
			switch {
			case tc.emptyMode && tc.unknownName:
				// The plan may write nothing: the mode stays unknown.
				if attrs["mode"].IsKnown() {
					t.Errorf("mode: expected unknown while the plan may not write, found %v", attrs["mode"])
				}
				return
			case tc.emptyMode && !attrs["mode"].Equal(str("http")):
				t.Errorf("mode: a stored empty mode must plan as http, found %v", attrs["mode"])
			}
			if tc.private {
				return
			}
			if tc.noMode && attrs["mode"].IsKnown() {
				// Null would be planned, and the update's "http" would not
				// match it.
				t.Errorf("mode: a null stored mode must stay unknown, found %v", attrs["mode"])
			}
			for _, a := range []string{"private", "access_groups"} {
				if got := !attrs[a].IsKnown(); got != tc.wantUnknown {
					t.Errorf("%s: expected unknown %v, found %v", a, tc.wantUnknown, attrs[a])
				}
			}
		})
	}
}
