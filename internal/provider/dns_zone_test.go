package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_dnsZoneAPIToTerraform(t *testing.T) {
	cases := []struct {
		resource *api.Zone
		expected DNSZoneModel
	}{
		{
			resource: &api.Zone{
				Id:                 "zone123",
				Name:               "Test Zone",
				Domain:             "example.com",
				Enabled:            true,
				EnableSearchDomain: false,
				DistributionGroups: []string{},
			},
			expected: DNSZoneModel{
				Id:                 types.StringValue("zone123"),
				Name:               types.StringValue("Test Zone"),
				Domain:             types.StringValue("example.com"),
				Enabled:            types.BoolValue(true),
				EnableSearchDomain: types.BoolValue(false),
				DistributionGroups: types.SetValueMust(types.StringType, []attr.Value{}),
			},
		},
		{
			resource: &api.Zone{
				Id:                 "zone456",
				Name:               "Internal Zone",
				Domain:             "internal.local",
				Enabled:            false,
				EnableSearchDomain: true,
				DistributionGroups: []string{"grp1", "grp2"},
			},
			expected: DNSZoneModel{
				Id:                 types.StringValue("zone456"),
				Name:               types.StringValue("Internal Zone"),
				Domain:             types.StringValue("internal.local"),
				Enabled:            types.BoolValue(false),
				EnableSearchDomain: types.BoolValue(true),
				DistributionGroups: types.SetValueMust(types.StringType, []attr.Value{
					types.StringValue("grp1"),
					types.StringValue("grp2"),
				}),
			},
		},
	}

	for _, c := range cases {
		var out DNSZoneModel
		outDiag := dnsZoneAPIToTerraform(context.Background(), c.resource, &out)
		if outDiag.HasError() {
			t.Fatalf("Expected no error diagnostics, found %d errors", outDiag.ErrorsCount())
		}

		if !reflect.DeepEqual(out, c.expected) {
			t.Fatalf("Expected:\n%#v\nFound:\n%#v", c.expected, out)
		}
	}
}

func Test_zoneDomain(t *testing.T) {
	for d, ok := range map[string]bool{
		"example.com":   true,
		"lan":           true,
		"test.local":    true,
		"example.com.":  false,
		"*.example.com": false,
		"-bad.example":  false,
		"a..b":          false,
	} {
		var resp validator.StringResponse
		zoneDomain{}.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("domain"), ConfigValue: types.StringValue(d)}, &resp)
		if resp.Diagnostics.HasError() == ok {
			t.Errorf("%q: expected valid %v, found %v", d, ok, resp.Diagnostics)
		}
	}
}

// Test_dnsZone_groupLookupStaysOnEndpoint: a group ID with path segments is
// refused without a request.
func Test_dnsZone_groupLookupStaysOnEndpoint(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"not found","code":404}`)
	}))
	defer srv.Close()
	ctx := context.Background()
	r := &DNSZone{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	value := func(domain string, groups ...string) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		values["id"] = tftypes.NewValue(tftypes.String, "z1")
		values["name"] = tftypes.NewValue(tftypes.String, "z")
		values["domain"] = tftypes.NewValue(tftypes.String, domain)
		var g []tftypes.Value
		for _, id := range groups {
			g = append(g, tftypes.NewValue(tftypes.String, id))
		}
		values["distribution_groups"] = tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, g)
		return tftypes.NewValue(typ, values)
	}
	config := value("after.local", "../../api/users")
	req := resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: sch.Schema, Raw: config},
		Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: config},
		State:  tfsdk.State{Schema: sch.Schema, Raw: value("before.local", "g1")},
	}
	resp := resource.ModifyPlanResponse{Plan: req.Plan}
	r.ModifyPlan(ctx, req, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected the missing group to be refused")
	}
	if len(paths) != 0 {
		t.Errorf("expected no request for a path-like group ID, found %v", paths)
	}
}

// Test_dnsZone_groupLookupStopsOnServerError: after a lookup the server
// cannot answer, the plan fails without looking up the other groups.
func Test_dnsZone_groupLookupStopsOnServerError(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"message":"unavailable","code":503}`)
	}))
	defer srv.Close()
	ctx := context.Background()
	r := &DNSZone{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	value := func(domain string, groups ...string) tftypes.Value {
		values := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			values[n] = tftypes.NewValue(at, nil)
		}
		values["id"] = tftypes.NewValue(tftypes.String, "z1")
		values["name"] = tftypes.NewValue(tftypes.String, "z")
		values["domain"] = tftypes.NewValue(tftypes.String, domain)
		var g []tftypes.Value
		for _, id := range groups {
			g = append(g, tftypes.NewValue(tftypes.String, id))
		}
		values["distribution_groups"] = tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, g)
		return tftypes.NewValue(typ, values)
	}
	config := value("after.local", "g1", "g2")
	req := resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: sch.Schema, Raw: config},
		Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: config},
		State:  tfsdk.State{Schema: sch.Schema, Raw: value("before.local", "g1")},
	}
	resp := resource.ModifyPlanResponse{Plan: req.Plan}
	r.ModifyPlan(ctx, req, &resp)
	if !resp.Diagnostics.HasError() || requests != 1 {
		t.Errorf("expected the plan refused after one request, found %v after %d requests", resp.Diagnostics, requests)
	}
}

// Test_dnsZone_firstCreateSendsNoRequest: a first create looks up no group,
// as the endpoint may not be known yet and nothing is deleted.
func Test_dnsZone_firstCreateSendsNoRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	ctx := context.Background()
	r := &DNSZone{client: netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"))}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	values := map[string]tftypes.Value{}
	for n, at := range typ.AttributeTypes {
		values[n] = tftypes.NewValue(at, nil)
	}
	values["name"] = tftypes.NewValue(tftypes.String, "z")
	values["domain"] = tftypes.NewValue(tftypes.String, "new.local")
	values["distribution_groups"] = tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "g1")})
	config := tftypes.NewValue(typ, values)
	req := resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: sch.Schema, Raw: config},
		Plan:   tfsdk.Plan{Schema: sch.Schema, Raw: config},
		State:  tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(typ, nil)},
	}
	resp := resource.ModifyPlanResponse{Plan: req.Plan}
	r.ModifyPlan(ctx, req, &resp)
	if resp.Diagnostics.HasError() || requests != 0 {
		t.Errorf("expected a first create planned without requests, found %v after %d requests", resp.Diagnostics, requests)
	}
}
