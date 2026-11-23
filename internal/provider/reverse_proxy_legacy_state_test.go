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
	configured := map[string]tftypes.Value{"name": str("svc"), "domain": str("svc.example"), "mode": str("http")}
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

	for name, tc := range map[string]struct {
		rename      bool
		wantUnknown bool
	}{
		"unchanged": {},
		"renamed":   {rename: true, wantUnknown: true},
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
			if tc.rename {
				config["name"], proposed["name"] = str("svc2"), str("svc2")
			}
			if _, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{}); err != nil {
				t.Fatal(err)
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
			if !tc.rename && !planned.Equal(object(prior)) {
				t.Errorf("an unchanged plan must equal the prior state, found %v", planned)
			}
			for _, a := range []string{"private", "access_groups"} {
				if got := !attrs[a].IsKnown(); got != tc.wantUnknown {
					t.Errorf("%s: expected unknown %v, found %v", a, tc.wantUnknown, attrs[a])
				}
			}
		})
	}
}
