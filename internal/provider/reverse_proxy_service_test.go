package provider

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_reverseProxyServiceAPIToTerraform(t *testing.T) {
	cases := []struct {
		name    string
		service *api.Service
	}{
		{
			name: "basic service with password auth",
			service: &api.Service{
				Id:      "svc1",
				Name:    "test-service",
				Domain:  "test.example.com",
				Enabled: true,
				Targets: []api.ServiceTarget{
					{
						TargetId:   "peer1",
						TargetType: api.ServiceTargetTargetTypePeer,
						Port:       8080,
						Protocol:   api.ServiceTargetProtocolHttp,
						Enabled:    true,
					},
				},
				Auth: api.ServiceAuthConfig{
					PasswordAuth: &api.PasswordAuthConfig{
						Enabled:  true,
						Password: "secret",
					},
				},
			},
		},
		{
			name: "service with optional fields and bearer auth",
			service: &api.Service{
				Id:               "svc2",
				Name:             "advanced-service",
				Domain:           "app.example.com",
				Enabled:          false,
				PassHostHeader:   valPtr(true),
				RewriteRedirects: valPtr(true),
				Targets: []api.ServiceTarget{
					{
						TargetId:   "res1",
						TargetType: api.ServiceTargetTargetType("subnet"),
						Port:       443,
						Protocol:   api.ServiceTargetProtocolHttps,
						Enabled:    true,
						Host:       valPtr("backend.internal"),
						Path:       valPtr("/api"),
					},
				},
				Auth: api.ServiceAuthConfig{
					BearerAuth: &api.BearerAuthConfig{
						Enabled:            true,
						DistributionGroups: &[]string{"group1", "group2"},
					},
				},
			},
		},
		{
			name: "service with pin auth and nil optional pointers",
			service: &api.Service{
				Id:      "svc3",
				Name:    "pin-service",
				Domain:  "pin.example.com",
				Enabled: true,
				Targets: []api.ServiceTarget{
					{
						TargetId:   "peer2",
						TargetType: api.ServiceTargetTargetTypePeer,
						Port:       0,
						Protocol:   api.ServiceTargetProtocolHttp,
						Enabled:    false,
					},
				},
				Auth: api.ServiceAuthConfig{
					PinAuth: &api.PINAuthConfig{
						Enabled: true,
						Pin:     "1234",
					},
				},
			},
		},
		{
			name: "service with link auth",
			service: &api.Service{
				Id:      "svc4",
				Name:    "link-service",
				Domain:  "link.example.com",
				Enabled: true,
				Targets: []api.ServiceTarget{
					{
						TargetId:   "peer1",
						TargetType: api.ServiceTargetTargetTypePeer,
						Port:       80,
						Protocol:   api.ServiceTargetProtocolHttp,
						Enabled:    true,
					},
				},
				Auth: api.ServiceAuthConfig{
					LinkAuth: &api.LinkAuthConfig{
						Enabled: true,
					},
				},
			},
		},
		{
			name: "service with target options",
			service: &api.Service{
				Id:      "svc5",
				Name:    "options-service",
				Domain:  "opts.example.com",
				Enabled: true,
				Targets: []api.ServiceTarget{
					{
						TargetId:   "peer1",
						TargetType: api.ServiceTargetTargetTypePeer,
						Port:       8080,
						Protocol:   api.ServiceTargetProtocolHttps,
						Enabled:    true,
						Options: &api.ServiceTargetOptions{
							SkipTlsVerify:  valPtr(true),
							RequestTimeout: valPtr("30s"),
							PathRewrite:    valPtr(api.ServiceTargetOptionsPathRewritePreserve),
							CustomHeaders:  &map[string]string{"X-Custom": "value"},
						},
					},
				},
				Auth: api.ServiceAuthConfig{
					PasswordAuth: &api.PasswordAuthConfig{
						Enabled:  true,
						Password: "secret",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out ReverseProxyServiceModel
			ctx := context.Background()
			outDiag := reverseProxyServiceAPIToTerraform(ctx, c.service, &out)
			if outDiag.HasError() {
				t.Fatalf("Expected no error diagnostics, found %d errors", outDiag.ErrorsCount())
			}

			if out.Id.ValueString() != c.service.Id {
				t.Errorf("Id mismatch: expected %s, got %s", c.service.Id, out.Id.ValueString())
			}
			if out.Name.ValueString() != c.service.Name {
				t.Errorf("Name mismatch: expected %s, got %s", c.service.Name, out.Name.ValueString())
			}
			if out.Domain.ValueString() != c.service.Domain {
				t.Errorf("Domain mismatch: expected %s, got %s", c.service.Domain, out.Domain.ValueString())
			}
			if out.Enabled.ValueBool() != c.service.Enabled {
				t.Errorf("Enabled mismatch: expected %v, got %v", c.service.Enabled, out.Enabled.ValueBool())
			}

			expectedPassHost := false
			if c.service.PassHostHeader != nil {
				expectedPassHost = *c.service.PassHostHeader
			}
			if out.PassHostHeader.ValueBool() != expectedPassHost {
				t.Errorf("PassHostHeader mismatch: expected %v, got %v", expectedPassHost, out.PassHostHeader.ValueBool())
			}

			expectedRewrite := false
			if c.service.RewriteRedirects != nil {
				expectedRewrite = *c.service.RewriteRedirects
			}
			if out.RewriteRedirects.ValueBool() != expectedRewrite {
				t.Errorf("RewriteRedirects mismatch: expected %v, got %v", expectedRewrite, out.RewriteRedirects.ValueBool())
			}
		})
	}
}

func Test_reverseProxyServiceRoundtrip(t *testing.T) {
	ctx := context.Background()

	original := &api.Service{
		Id:               "svc-rt",
		Name:             "roundtrip-service",
		Domain:           "rt.example.com",
		Enabled:          true,
		PassHostHeader:   valPtr(true),
		RewriteRedirects: valPtr(false),
		Targets: []api.ServiceTarget{
			{
				TargetId:   "peer1",
				TargetType: api.ServiceTargetTargetTypePeer,
				Port:       8080,
				Protocol:   api.ServiceTargetProtocolHttp,
				Enabled:    true,
				Host:       valPtr("10.0.0.1"),
				Path:       valPtr("/v1"),
			},
		},
		Auth: api.ServiceAuthConfig{
			PasswordAuth: &api.PasswordAuthConfig{
				Enabled:  true,
				Password: "pass123",
			},
		},
	}

	// API -> Terraform
	var model ReverseProxyServiceModel
	d := reverseProxyServiceAPIToTerraform(ctx, original, &model)
	if d.HasError() {
		t.Fatalf("APIToTerraform failed with %d errors", d.ErrorsCount())
	}

	// Terraform -> API
	req, d := reverseProxyServiceTerraformToAPI(ctx, &model)
	if d.HasError() {
		t.Fatalf("TerraformToAPI failed with %d errors", d.ErrorsCount())
	}

	if req.Name != original.Name {
		t.Errorf("Name mismatch: expected %s, got %s", original.Name, req.Name)
	}
	if req.Domain != original.Domain {
		t.Errorf("Domain mismatch: expected %s, got %s", original.Domain, req.Domain)
	}
	if req.Enabled != original.Enabled {
		t.Errorf("Enabled mismatch: expected %v, got %v", original.Enabled, req.Enabled)
	}
	if req.PassHostHeader == nil || *req.PassHostHeader != *original.PassHostHeader {
		t.Errorf("PassHostHeader mismatch")
	}
	if req.RewriteRedirects == nil || *req.RewriteRedirects != *original.RewriteRedirects {
		t.Errorf("RewriteRedirects mismatch")
	}
	if req.Targets == nil {
		t.Fatalf("Expected 1 target, got nil")
	}
	if len(*req.Targets) != 1 {
		t.Fatalf("Expected 1 target, got %d", len(*req.Targets))
	}

	target := (*req.Targets)[0]
	origTarget := original.Targets[0]
	if target.TargetId != origTarget.TargetId {
		t.Errorf("Target.TargetId mismatch: expected %s, got %s", origTarget.TargetId, target.TargetId)
	}
	if target.TargetType != origTarget.TargetType {
		t.Errorf("Target.TargetType mismatch")
	}
	if target.Port != origTarget.Port {
		t.Errorf("Target.Port mismatch: expected %d, got %d", origTarget.Port, target.Port)
	}
	if target.Protocol != origTarget.Protocol {
		t.Errorf("Target.Protocol mismatch")
	}
	if target.Host == nil || *target.Host != *origTarget.Host {
		t.Errorf("Target.Host mismatch")
	}
	if target.Path == nil || *target.Path != *origTarget.Path {
		t.Errorf("Target.Path mismatch")
	}

	if req.Auth.PasswordAuth == nil {
		t.Fatal("Expected PasswordAuth to be set")
	}
	if req.Auth.PasswordAuth.Enabled != original.Auth.PasswordAuth.Enabled {
		t.Errorf("PasswordAuth.Enabled mismatch")
	}
	if req.Auth.PasswordAuth.Password != original.Auth.PasswordAuth.Password {
		t.Errorf("PasswordAuth.Password mismatch")
	}
}

func Test_reverseProxyServiceRoundtrip_withOptions(t *testing.T) {
	ctx := context.Background()

	preserve := api.ServiceTargetOptionsPathRewritePreserve
	original := &api.Service{
		Id:      "svc-opts-rt",
		Name:    "options-roundtrip",
		Domain:  "opts-rt.example.com",
		Enabled: true,
		Targets: []api.ServiceTarget{
			{
				TargetId:   "peer1",
				TargetType: api.ServiceTargetTargetTypePeer,
				Port:       8443,
				Protocol:   api.ServiceTargetProtocolHttps,
				Enabled:    true,
				Options: &api.ServiceTargetOptions{
					SkipTlsVerify:  valPtr(true),
					RequestTimeout: valPtr("45s"),
					PathRewrite:    &preserve,
					CustomHeaders:  &map[string]string{"X-Env": "prod"},
				},
			},
		},
		Auth: api.ServiceAuthConfig{
			PasswordAuth: &api.PasswordAuthConfig{
				Enabled:  true,
				Password: "test",
			},
		},
	}

	var model ReverseProxyServiceModel
	d := reverseProxyServiceAPIToTerraform(ctx, original, &model)
	if d.HasError() {
		t.Fatalf("APIToTerraform failed with %d errors", d.ErrorsCount())
	}

	req, d := reverseProxyServiceTerraformToAPI(ctx, &model)
	if d.HasError() {
		t.Fatalf("TerraformToAPI failed with %d errors", d.ErrorsCount())
	}

	if req.Targets == nil || len(*req.Targets) != 1 {
		t.Fatalf("Expected 1 target")
	}
	target := (*req.Targets)[0]
	if target.Options == nil {
		t.Fatal("Expected Options to be set")
	}
	if target.Options.SkipTlsVerify == nil || !*target.Options.SkipTlsVerify {
		t.Error("SkipTlsVerify should be true")
	}
	if target.Options.RequestTimeout == nil || *target.Options.RequestTimeout != "45s" {
		t.Errorf("RequestTimeout mismatch: got %v", target.Options.RequestTimeout)
	}
	if target.Options.PathRewrite == nil || *target.Options.PathRewrite != preserve {
		t.Error("PathRewrite should be preserve")
	}
	if target.Options.CustomHeaders == nil || (*target.Options.CustomHeaders)["X-Env"] != "prod" {
		t.Error("CustomHeaders mismatch")
	}
}

func Test_reverseProxyServiceRoundtrip_l4Options(t *testing.T) {
	ctx := context.Background()

	original := &api.Service{
		Id:      "svc-l4-rt",
		Name:    "l4-roundtrip",
		Domain:  "l4-rt.example.com",
		Enabled: true,
		Targets: []api.ServiceTarget{
			{
				TargetId:   "res1",
				TargetType: api.ServiceTargetTargetType("subnet"),
				Port:       5432,
				Protocol:   api.ServiceTargetProtocolTcp,
				Enabled:    true,
				Options: &api.ServiceTargetOptions{
					ProxyProtocol:  valPtr(true),
					RequestTimeout: valPtr("60s"),
				},
			},
			{
				TargetId:   "res2",
				TargetType: api.ServiceTargetTargetType("subnet"),
				Port:       53,
				Protocol:   api.ServiceTargetProtocolUdp,
				Enabled:    true,
				Options: &api.ServiceTargetOptions{
					SessionIdleTimeout: valPtr("2m"),
				},
			},
		},
		Auth: api.ServiceAuthConfig{
			PasswordAuth: &api.PasswordAuthConfig{
				Enabled:  true,
				Password: "test",
			},
		},
	}

	var model ReverseProxyServiceModel
	d := reverseProxyServiceAPIToTerraform(ctx, original, &model)
	if d.HasError() {
		t.Fatalf("APIToTerraform failed with %d errors", d.ErrorsCount())
	}

	req, d := reverseProxyServiceTerraformToAPI(ctx, &model)
	if d.HasError() {
		t.Fatalf("TerraformToAPI failed with %d errors", d.ErrorsCount())
	}

	if req.Targets == nil || len(*req.Targets) != 2 {
		t.Fatalf("Expected 2 targets")
	}

	tcp := (*req.Targets)[0]
	if tcp.Options == nil {
		t.Fatal("Expected TCP target options")
	}
	if tcp.Options.ProxyProtocol == nil || !*tcp.Options.ProxyProtocol {
		t.Error("TCP ProxyProtocol should be true")
	}
	if tcp.Options.RequestTimeout == nil || *tcp.Options.RequestTimeout != "60s" {
		t.Error("TCP RequestTimeout mismatch")
	}

	udp := (*req.Targets)[1]
	if udp.Options == nil {
		t.Fatal("Expected UDP target options")
	}
	if udp.Options.SessionIdleTimeout == nil || *udp.Options.SessionIdleTimeout != "2m" {
		t.Error("UDP SessionIdleTimeout mismatch")
	}
}

func Test_reverseProxyServiceTargetModelTFType(t *testing.T) {
	tfType := ReverseProxyServiceTargetModel{}.TFType()
	expectedKeys := []string{"target_id", "target_type", "host", "port", "protocol", "path", "enabled", "access_action", "options"}
	for _, key := range expectedKeys {
		if _, ok := tfType.AttrTypes[key]; !ok {
			t.Errorf("Expected key %s in TFType, not found", key)
		}
	}
	if len(tfType.AttrTypes) != len(expectedKeys) {
		t.Errorf("Expected %d keys in TFType, got %d", len(expectedKeys), len(tfType.AttrTypes))
	}
}

func Test_reverseProxyServiceAuthModelTFType(t *testing.T) {
	tfType := ReverseProxyServiceAuthModel{}.TFType()
	expectedKeys := []string{"password_auth", "pin_auth", "bearer_auth", "link_auth", "header_auths"}
	for _, key := range expectedKeys {
		if _, ok := tfType.AttrTypes[key]; !ok {
			t.Errorf("Expected key %s in TFType, not found", key)
		}
	}
}

func Test_reverseProxyTargetOptionsModelTFType(t *testing.T) {
	tfType := ReverseProxyTargetOptionsModel{}.TFType()
	expectedKeys := []string{"skip_tls_verify", "request_timeout", "path_rewrite", "custom_headers", "proxy_protocol", "session_idle_timeout", "direct_upstream"}
	for _, key := range expectedKeys {
		if _, ok := tfType.AttrTypes[key]; !ok {
			t.Errorf("Expected key %s in TFType, not found", key)
		}
	}
	if len(tfType.AttrTypes) != len(expectedKeys) {
		t.Errorf("Expected %d keys in TFType, got %d", len(expectedKeys), len(tfType.AttrTypes))
	}
}

func Test_reverseProxyServiceAPIToTerraform_nullAuth(t *testing.T) {
	ctx := context.Background()
	svc := &api.Service{
		Id:      "svc-null",
		Name:    "null-auth-service",
		Domain:  "null.example.com",
		Enabled: true,
		Targets: []api.ServiceTarget{
			{
				TargetId:   "peer1",
				TargetType: api.ServiceTargetTargetTypePeer,
				Port:       80,
				Protocol:   api.ServiceTargetProtocolHttp,
				Enabled:    true,
			},
		},
		Auth: api.ServiceAuthConfig{},
	}

	var out ReverseProxyServiceModel
	d := reverseProxyServiceAPIToTerraform(ctx, svc, &out)
	if d.HasError() {
		t.Fatalf("Expected no error diagnostics, found %d errors", d.ErrorsCount())
	}

	if out.Auth.IsNull() {
		t.Error("Auth should not be null")
	}

	authAttrs := out.Auth.Attributes()
	pwObj, ok := authAttrs["password_auth"].(types.Object)
	if !ok || !pwObj.IsNull() {
		t.Error("password_auth should be null when not set in API")
	}
	pinObj, ok := authAttrs["pin_auth"].(types.Object)
	if !ok || !pinObj.IsNull() {
		t.Error("pin_auth should be null when not set in API")
	}
	bearerObj, ok := authAttrs["bearer_auth"].(types.Object)
	if !ok || !bearerObj.IsNull() {
		t.Error("bearer_auth should be null when not set in API")
	}
	linkObj, ok := authAttrs["link_auth"].(types.Object)
	if !ok || !linkObj.IsNull() {
		t.Error("link_auth should be null when not set in API")
	}
}

func Test_reverseProxyServiceAPIToTerraform_nullOptions(t *testing.T) {
	cases := []struct {
		name    string
		options *api.ServiceTargetOptions
	}{
		{name: "nil options", options: nil},
		// The server serializes proxy_protocol only when true, so a false here
		// means the option was never configured and must not materialize an
		// options object in state.
		{name: "default-valued options", options: &api.ServiceTargetOptions{ProxyProtocol: valPtr(false)}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			svc := &api.Service{
				Id:      "svc-no-opts",
				Name:    "no-options",
				Domain:  "noopts.example.com",
				Enabled: true,
				Targets: []api.ServiceTarget{
					{
						TargetId:   "peer1",
						TargetType: api.ServiceTargetTargetTypePeer,
						Port:       80,
						Protocol:   api.ServiceTargetProtocolHttp,
						Enabled:    true,
						Options:    c.options,
					},
				},
				Auth: api.ServiceAuthConfig{},
			}

			var out ReverseProxyServiceModel
			d := reverseProxyServiceAPIToTerraform(ctx, svc, &out)
			if d.HasError() {
				t.Fatalf("Expected no error diagnostics, found %d errors", d.ErrorsCount())
			}

			var targets []ReverseProxyServiceTargetModel
			d = out.Targets.ElementsAs(ctx, &targets, false)
			if d.HasError() {
				t.Fatal("Failed to extract targets")
			}
			if len(targets) != 1 {
				t.Fatalf("Expected 1 target, got %d", len(targets))
			}
			if !targets[0].Options.IsNull() {
				t.Error("Options should be null when not set in API")
			}
		})
	}
}

func Test_reverseProxyServiceAPIToTerraform_bearerWithNilGroups(t *testing.T) {
	ctx := context.Background()
	svc := &api.Service{
		Id:      "svc-bearer-nil",
		Name:    "bearer-nil-groups",
		Domain:  "bearer.example.com",
		Enabled: true,
		Targets: []api.ServiceTarget{
			{
				TargetId:   "peer1",
				TargetType: api.ServiceTargetTargetTypePeer,
				Port:       80,
				Protocol:   api.ServiceTargetProtocolHttp,
				Enabled:    true,
			},
		},
		Auth: api.ServiceAuthConfig{
			BearerAuth: &api.BearerAuthConfig{
				Enabled:            true,
				DistributionGroups: nil,
			},
		},
	}

	var out ReverseProxyServiceModel
	d := reverseProxyServiceAPIToTerraform(ctx, svc, &out)
	if d.HasError() {
		t.Fatalf("Expected no error diagnostics, found %d errors", d.ErrorsCount())
	}

	authAttrs := out.Auth.Attributes()
	bearerObj, ok := authAttrs["bearer_auth"].(types.Object)
	if !ok {
		t.Fatal("bearer_auth is not types.Object")
	}
	if bearerObj.IsNull() {
		t.Fatal("bearer_auth should not be null")
	}
	bearerAttrs := bearerObj.Attributes()
	groupsList, ok := bearerAttrs["distribution_groups"].(types.List)
	if !ok {
		t.Fatal("distribution_groups is not types.List")
	}
	if !groupsList.IsNull() {
		t.Error("distribution_groups should be null when API returns nil")
	}
}

func Test_reverseProxyServiceAPIToTerraform_multipleTargets(t *testing.T) {
	ctx := context.Background()
	svc := &api.Service{
		Id:      "svc-multi",
		Name:    "multi-target",
		Domain:  "multi.example.com",
		Enabled: true,
		Targets: []api.ServiceTarget{
			{
				TargetId:   "peer1",
				TargetType: api.ServiceTargetTargetTypePeer,
				Port:       8080,
				Protocol:   api.ServiceTargetProtocolHttp,
				Enabled:    true,
			},
			{
				TargetId:   "res1",
				TargetType: api.ServiceTargetTargetType("subnet"),
				Port:       443,
				Protocol:   api.ServiceTargetProtocolHttps,
				Enabled:    false,
				Host:       valPtr("backend.local"),
				Path:       valPtr("/api/v2"),
			},
		},
		Auth: api.ServiceAuthConfig{
			PinAuth: &api.PINAuthConfig{
				Enabled: true,
				Pin:     "9999",
			},
		},
	}

	var out ReverseProxyServiceModel
	d := reverseProxyServiceAPIToTerraform(ctx, svc, &out)
	if d.HasError() {
		t.Fatalf("Expected no error diagnostics, found %d errors", d.ErrorsCount())
	}

	var targets []ReverseProxyServiceTargetModel
	d = out.Targets.ElementsAs(ctx, &targets, false)
	if d.HasError() {
		t.Fatalf("Failed to extract targets: %d errors", d.ErrorsCount())
	}
	if len(targets) != 2 {
		t.Fatalf("Expected 2 targets, got %d", len(targets))
	}

	if targets[0].TargetId.ValueString() != "peer1" {
		t.Errorf("First target ID mismatch: expected peer1, got %s", targets[0].TargetId.ValueString())
	}
	if targets[1].TargetId.ValueString() != "res1" {
		t.Errorf("Second target ID mismatch: expected res1, got %s", targets[1].TargetId.ValueString())
	}
	if targets[1].Host.ValueString() != "backend.local" {
		t.Errorf("Second target host mismatch")
	}
	if targets[1].Path.ValueString() != "/api/v2" {
		t.Errorf("Second target path mismatch")
	}
	if targets[1].Enabled.ValueBool() != false {
		t.Errorf("Second target should be disabled")
	}
}

func Test_reverseProxyServiceTerraformToAPI_targets(t *testing.T) {
	ctx := context.Background()

	targetModels := []ReverseProxyServiceTargetModel{
		{
			TargetId:   types.StringValue("peer1"),
			TargetType: types.StringValue("peer"),
			Port:       types.Int64Value(8080),
			Protocol:   types.StringValue("http"),
			Enabled:    types.BoolValue(true),
			Host:       types.StringNull(),
			Path:       types.StringNull(),
			Options:    types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		},
	}

	targetsList, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), targetModels)
	if d.HasError() {
		t.Fatalf("Failed to build targets list")
	}

	pwAuth, d := types.ObjectValueFrom(ctx, ReverseProxyPasswordAuthModel{}.TFType().AttrTypes, ReverseProxyPasswordAuthModel{
		Enabled:  types.BoolValue(false),
		Password: types.StringValue(""),
	})
	if d.HasError() {
		t.Fatalf("Failed to build password auth object")
	}

	authModel := ReverseProxyServiceAuthModel{
		PasswordAuth: pwAuth,
		PinAuth:      types.ObjectNull(ReverseProxyPinAuthModel{}.TFType().AttrTypes),
		BearerAuth:   types.ObjectNull(ReverseProxyBearerAuthModel{}.TFType().AttrTypes),
		LinkAuth:     types.ObjectNull(ReverseProxyLinkAuthModel{}.TFType().AttrTypes),
		HeaderAuths:  types.ListNull(ReverseProxyHeaderAuthModel{}.TFType()),
	}
	authObj, d := types.ObjectValueFrom(ctx, ReverseProxyServiceAuthModel{}.TFType().AttrTypes, authModel)
	if d.HasError() {
		t.Fatalf("Failed to build auth object")
	}

	model := &ReverseProxyServiceModel{
		Id:               types.StringValue("test-id"),
		Name:             types.StringValue("test"),
		Domain:           types.StringValue("test.com"),
		Enabled:          types.BoolValue(true),
		PassHostHeader:   types.BoolValue(false),
		RewriteRedirects: types.BoolValue(false),
		Targets:          targetsList,
		Auth:             authObj,
	}

	req, d := reverseProxyServiceTerraformToAPI(ctx, model)
	if d.HasError() {
		t.Fatalf("TerraformToAPI failed with %d errors", d.ErrorsCount())
	}

	if req.Name != "test" {
		t.Errorf("Name mismatch: expected test, got %s", req.Name)
	}
	if req.Targets == nil {
		t.Fatalf("Expected 1 target, got nil")
	}
	if len(*req.Targets) != 1 {
		t.Fatalf("Expected 1 target, got %d", len(*req.Targets))
	}
	firstTarget := (*req.Targets)[0]
	if firstTarget.TargetId != "peer1" {
		t.Errorf("Target ID mismatch")
	}
	if firstTarget.Host != nil {
		t.Errorf("Target Host should be nil for null input")
	}
	if firstTarget.Path != nil {
		t.Errorf("Target Path should be nil for null input")
	}
	if firstTarget.Options != nil {
		t.Errorf("Target Options should be nil for null input")
	}
	if req.Auth == nil || req.Auth.PasswordAuth == nil {
		t.Fatal("Expected PasswordAuth to be set")
	}
	if req.Auth.PinAuth != nil {
		t.Error("Expected PinAuth to be nil")
	}
	if req.Auth.BearerAuth != nil {
		t.Error("Expected BearerAuth to be nil")
	}
	if req.Auth.LinkAuth != nil {
		t.Error("Expected LinkAuth to be nil")
	}
}

func Test_reverseProxyServiceSchemaValidation(t *testing.T) {
	r := &ReverseProxyService{}

	var metaResp resource.MetadataResponse
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "netbird"}, &metaResp)
	if metaResp.TypeName != "netbird_reverse_proxy_service" {
		t.Errorf("Expected type name netbird_reverse_proxy_service, got %s", metaResp.TypeName)
	}

	var schemaResp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("Schema returned errors")
	}

	attrs := schemaResp.Schema.Attributes
	requiredAttrs := []string{"name", "domain", "targets", "auth"}
	for _, a := range requiredAttrs {
		if _, ok := attrs[a]; !ok {
			t.Errorf("Expected required attribute %s in schema", a)
		}
	}

	computedAttrs := []string{"id", "proxy_cluster"}
	for _, a := range computedAttrs {
		if _, ok := attrs[a]; !ok {
			t.Errorf("Expected computed attribute %s in schema", a)
		}
	}

	var _ resource.Resource = &ReverseProxyService{}
	var _ resource.ResourceWithImportState = &ReverseProxyService{}
}

func Test_reverseProxyServiceTFTypeConsistency(t *testing.T) {
	targetType := ReverseProxyServiceTargetModel{}.TFType()
	authType := ReverseProxyServiceAuthModel{}.TFType()
	pwType := ReverseProxyPasswordAuthModel{}.TFType()
	pinType := ReverseProxyPinAuthModel{}.TFType()
	bearerType := ReverseProxyBearerAuthModel{}.TFType()
	linkType := ReverseProxyLinkAuthModel{}.TFType()
	optsType := ReverseProxyTargetOptionsModel{}.TFType()

	if !reflect.DeepEqual(authType.AttrTypes["password_auth"], pwType) {
		t.Error("Auth password_auth type doesn't match PasswordAuth TFType")
	}
	if !reflect.DeepEqual(authType.AttrTypes["pin_auth"], pinType) {
		t.Error("Auth pin_auth type doesn't match PinAuth TFType")
	}
	if !reflect.DeepEqual(authType.AttrTypes["bearer_auth"], bearerType) {
		t.Error("Auth bearer_auth type doesn't match BearerAuth TFType")
	}
	if !reflect.DeepEqual(authType.AttrTypes["link_auth"], linkType) {
		t.Error("Auth link_auth type doesn't match LinkAuth TFType")
	}

	if !reflect.DeepEqual(targetType.AttrTypes["options"], optsType) {
		t.Error("Target options type doesn't match TargetOptions TFType")
	}

	if len(targetType.AttrTypes) != 9 {
		t.Errorf("Expected 9 target attributes, got %d", len(targetType.AttrTypes))
	}
}

func Test_preserveAuthSecrets(t *testing.T) {
	ctx := context.Background()

	priorAuthModel := ReverseProxyServiceAuthModel{
		PasswordAuth: mustObjectValue(ctx, ReverseProxyPasswordAuthModel{}.TFType().AttrTypes, ReverseProxyPasswordAuthModel{
			Enabled:  types.BoolValue(true),
			Password: types.StringValue("real-password"),
		}),
		PinAuth: mustObjectValue(ctx, ReverseProxyPinAuthModel{}.TFType().AttrTypes, ReverseProxyPinAuthModel{
			Enabled: types.BoolValue(true),
			Pin:     types.StringValue("1234"),
		}),
		BearerAuth:  types.ObjectNull(ReverseProxyBearerAuthModel{}.TFType().AttrTypes),
		LinkAuth:    types.ObjectNull(ReverseProxyLinkAuthModel{}.TFType().AttrTypes),
		HeaderAuths: types.ListNull(ReverseProxyHeaderAuthModel{}.TFType()),
	}
	priorAuth := mustObjectValue(ctx, ReverseProxyServiceAuthModel{}.TFType().AttrTypes, priorAuthModel)

	currentAuthModel := ReverseProxyServiceAuthModel{
		PasswordAuth: mustObjectValue(ctx, ReverseProxyPasswordAuthModel{}.TFType().AttrTypes, ReverseProxyPasswordAuthModel{
			Enabled:  types.BoolValue(true),
			Password: types.StringValue(""),
		}),
		PinAuth: mustObjectValue(ctx, ReverseProxyPinAuthModel{}.TFType().AttrTypes, ReverseProxyPinAuthModel{
			Enabled: types.BoolValue(true),
			Pin:     types.StringValue(""),
		}),
		BearerAuth:  types.ObjectNull(ReverseProxyBearerAuthModel{}.TFType().AttrTypes),
		LinkAuth:    types.ObjectNull(ReverseProxyLinkAuthModel{}.TFType().AttrTypes),
		HeaderAuths: types.ListNull(ReverseProxyHeaderAuthModel{}.TFType()),
	}
	currentAuth := mustObjectValue(ctx, ReverseProxyServiceAuthModel{}.TFType().AttrTypes, currentAuthModel)

	result, d := preserveAuthSecrets(priorAuth, currentAuth)
	if d.HasError() {
		t.Fatalf("preserveAuthSecrets failed with %d errors", d.ErrorsCount())
	}

	authAttrs := result.Attributes()
	pwObj, ok := authAttrs["password_auth"].(types.Object)
	if !ok {
		t.Fatal("password_auth is not types.Object")
	}
	pwAttrs := pwObj.Attributes()
	password, ok := pwAttrs["password"].(types.String)
	if !ok {
		t.Fatal("password is not types.String")
	}
	if password.ValueString() != "real-password" {
		t.Errorf("Expected password to be preserved as 'real-password', got %q", password.ValueString())
	}

	pinObj, ok := authAttrs["pin_auth"].(types.Object)
	if !ok {
		t.Fatal("pin_auth is not types.Object")
	}
	pinAttrs := pinObj.Attributes()
	pin, ok := pinAttrs["pin"].(types.String)
	if !ok {
		t.Fatal("pin is not types.String")
	}
	if pin.ValueString() != "1234" {
		t.Errorf("Expected pin to be preserved as '1234', got %q", pin.ValueString())
	}
}

func Test_preserveAuthSecrets_nullPrior(t *testing.T) {
	ctx := context.Background()

	currentAuthModel := ReverseProxyServiceAuthModel{
		PasswordAuth: mustObjectValue(ctx, ReverseProxyPasswordAuthModel{}.TFType().AttrTypes, ReverseProxyPasswordAuthModel{
			Enabled:  types.BoolValue(true),
			Password: types.StringValue("new-password"),
		}),
		PinAuth:     types.ObjectNull(ReverseProxyPinAuthModel{}.TFType().AttrTypes),
		BearerAuth:  types.ObjectNull(ReverseProxyBearerAuthModel{}.TFType().AttrTypes),
		LinkAuth:    types.ObjectNull(ReverseProxyLinkAuthModel{}.TFType().AttrTypes),
		HeaderAuths: types.ListNull(ReverseProxyHeaderAuthModel{}.TFType()),
	}
	currentAuth := mustObjectValue(ctx, ReverseProxyServiceAuthModel{}.TFType().AttrTypes, currentAuthModel)

	result, d := preserveAuthSecrets(types.ObjectNull(ReverseProxyServiceAuthModel{}.TFType().AttrTypes), currentAuth)
	if d.HasError() {
		t.Fatalf("preserveAuthSecrets failed with %d errors", d.ErrorsCount())
	}

	if result.IsNull() {
		t.Error("Result should not be null when only prior is null")
	}

	authAttrs := result.Attributes()
	pwObj, ok := authAttrs["password_auth"].(types.Object)
	if !ok {
		t.Fatal("password_auth is not types.Object")
	}
	pwAttrs := pwObj.Attributes()
	password, ok := pwAttrs["password"].(types.String)
	if !ok {
		t.Fatal("password is not types.String")
	}
	if password.ValueString() != "new-password" {
		t.Errorf("Expected password to remain 'new-password', got %q", password.ValueString())
	}
}

func Test_preserveTargetPlanValues(t *testing.T) {
	ctx := context.Background()

	planModels := []ReverseProxyServiceTargetModel{
		{
			TargetId:   types.StringValue("peer1"),
			TargetType: types.StringValue("peer"),
			Port:       types.Int64Value(8080),
			Protocol:   types.StringValue("http"),
			Enabled:    types.BoolValue(true),
			Host:       types.StringValue("10.0.0.1"),
			Path:       types.StringValue("/custom"),
			Options:    types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		},
	}
	planTargets, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), planModels)
	if d.HasError() {
		t.Fatal("Failed to build plan targets")
	}

	apiModels := []ReverseProxyServiceTargetModel{
		{
			TargetId:   types.StringValue("peer1"),
			TargetType: types.StringValue("peer"),
			Port:       types.Int64Value(8080),
			Protocol:   types.StringValue("http"),
			Enabled:    types.BoolValue(true),
			Host:       types.StringValue("100.0.219.86"),
			Path:       types.StringValue("/custom"),
			Options:    types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		},
	}
	apiTargets, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), apiModels)
	if d.HasError() {
		t.Fatal("Failed to build API targets")
	}

	result, d := preserveTargetPlanValues(ctx, planTargets, apiTargets)
	if d.HasError() {
		t.Fatalf("preserveTargetPlanValues failed with %d errors", d.ErrorsCount())
	}

	var resultModels []ReverseProxyServiceTargetModel
	d = result.ElementsAs(ctx, &resultModels, false)
	if d.HasError() {
		t.Fatal("Failed to extract result targets")
	}

	if len(resultModels) != 1 {
		t.Fatalf("Expected 1 target, got %d", len(resultModels))
	}
	if resultModels[0].Host.ValueString() != "10.0.0.1" {
		t.Errorf("Expected host to be preserved as '10.0.0.1', got %q", resultModels[0].Host.ValueString())
	}
	if resultModels[0].Path.ValueString() != "/custom" {
		t.Errorf("Expected path to be preserved as '/custom', got %q", resultModels[0].Path.ValueString())
	}
	if resultModels[0].Port.ValueInt64() != 8080 {
		t.Errorf("Expected port 8080, got %d", resultModels[0].Port.ValueInt64())
	}
}

func Test_preserveTargetPlanValues_nullPlanHost(t *testing.T) {
	ctx := context.Background()

	planModels := []ReverseProxyServiceTargetModel{
		{
			TargetId:   types.StringValue("peer1"),
			TargetType: types.StringValue("peer"),
			Port:       types.Int64Value(80),
			Protocol:   types.StringValue("http"),
			Enabled:    types.BoolValue(true),
			Host:       types.StringNull(),
			Path:       types.StringNull(),
			Options:    types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		},
	}
	planTargets, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), planModels)
	if d.HasError() {
		t.Fatal("Failed to build plan targets")
	}

	apiModels := []ReverseProxyServiceTargetModel{
		{
			TargetId:   types.StringValue("peer1"),
			TargetType: types.StringValue("peer"),
			Port:       types.Int64Value(80),
			Protocol:   types.StringValue("http"),
			Enabled:    types.BoolValue(true),
			Host:       types.StringValue("100.0.219.86"),
			Path:       types.StringValue("/"),
			Options:    types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		},
	}
	apiTargets, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), apiModels)
	if d.HasError() {
		t.Fatal("Failed to build API targets")
	}

	result, d := preserveTargetPlanValues(ctx, planTargets, apiTargets)
	if d.HasError() {
		t.Fatalf("preserveTargetPlanValues failed with %d errors", d.ErrorsCount())
	}

	var resultModels []ReverseProxyServiceTargetModel
	d = result.ElementsAs(ctx, &resultModels, false)
	if d.HasError() {
		t.Fatal("Failed to extract result targets")
	}

	if resultModels[0].Host.ValueString() != "100.0.219.86" {
		t.Errorf("Expected API host '100.0.219.86' when plan is null, got %q", resultModels[0].Host.ValueString())
	}
	if resultModels[0].Path.ValueString() != "/" {
		t.Errorf("Expected API path '/' when plan is null, got %q", resultModels[0].Path.ValueString())
	}
}

func Test_preserveTargetPlanValues_extraAPITarget(t *testing.T) {
	ctx := context.Background()

	planModels := []ReverseProxyServiceTargetModel{
		{
			TargetId:   types.StringValue("peer1"),
			TargetType: types.StringValue("peer"),
			Port:       types.Int64Value(80),
			Protocol:   types.StringValue("http"),
			Enabled:    types.BoolValue(true),
			Host:       types.StringValue("10.0.0.1"),
			Path:       types.StringNull(),
			Options:    types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		},
	}
	planTargets, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), planModels)
	if d.HasError() {
		t.Fatal("Failed to build plan targets")
	}

	apiModels := []ReverseProxyServiceTargetModel{
		{
			TargetId:   types.StringValue("peer1"),
			TargetType: types.StringValue("peer"),
			Port:       types.Int64Value(80),
			Protocol:   types.StringValue("http"),
			Enabled:    types.BoolValue(true),
			Host:       types.StringValue("100.0.219.86"),
			Path:       types.StringValue("/"),
			Options:    types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		},
		{
			TargetId:   types.StringValue("peer2"),
			TargetType: types.StringValue("peer"),
			Port:       types.Int64Value(443),
			Protocol:   types.StringValue("https"),
			Enabled:    types.BoolValue(true),
			Host:       types.StringValue("100.0.34.47"),
			Path:       types.StringValue("/"),
			Options:    types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		},
	}
	apiTargets, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), apiModels)
	if d.HasError() {
		t.Fatal("Failed to build API targets")
	}

	result, d := preserveTargetPlanValues(ctx, planTargets, apiTargets)
	if d.HasError() {
		t.Fatalf("preserveTargetPlanValues failed with %d errors", d.ErrorsCount())
	}

	var resultModels []ReverseProxyServiceTargetModel
	d = result.ElementsAs(ctx, &resultModels, false)
	if d.HasError() {
		t.Fatal("Failed to extract result targets")
	}

	if len(resultModels) != 2 {
		t.Fatalf("Expected 2 targets, got %d", len(resultModels))
	}
	if resultModels[0].Host.ValueString() != "10.0.0.1" {
		t.Errorf("Expected peer1 host preserved as '10.0.0.1', got %q", resultModels[0].Host.ValueString())
	}
	if resultModels[1].Host.ValueString() != "100.0.34.47" {
		t.Errorf("Expected peer2 API host '100.0.34.47', got %q", resultModels[1].Host.ValueString())
	}
}

func Test_preserveTargetPlanValues_nullPlan(t *testing.T) {
	ctx := context.Background()

	apiModels := []ReverseProxyServiceTargetModel{
		{
			TargetId:   types.StringValue("peer1"),
			TargetType: types.StringValue("peer"),
			Port:       types.Int64Value(80),
			Protocol:   types.StringValue("http"),
			Enabled:    types.BoolValue(true),
			Host:       types.StringValue("100.0.219.86"),
			Path:       types.StringValue("/"),
			Options:    types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		},
	}
	apiTargets, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), apiModels)
	if d.HasError() {
		t.Fatal("Failed to build API targets")
	}

	result, d := preserveTargetPlanValues(ctx, types.ListNull(ReverseProxyServiceTargetModel{}.TFType()), apiTargets)
	if d.HasError() {
		t.Fatalf("preserveTargetPlanValues failed with %d errors", d.ErrorsCount())
	}

	var resultModels []ReverseProxyServiceTargetModel
	d = result.ElementsAs(ctx, &resultModels, false)
	if d.HasError() {
		t.Fatal("Failed to extract result targets")
	}

	if resultModels[0].Host.ValueString() != "100.0.219.86" {
		t.Errorf("Expected API host when plan is null, got %q", resultModels[0].Host.ValueString())
	}
}

func Test_reverseProxyServiceRoundtrip_bearerAuth(t *testing.T) {
	ctx := context.Background()

	original := &api.Service{
		Id:      "svc-bearer-rt",
		Name:    "bearer-roundtrip",
		Domain:  "bearer-rt.example.com",
		Enabled: true,
		Targets: []api.ServiceTarget{
			{
				TargetId:   "peer1",
				TargetType: api.ServiceTargetTargetTypePeer,
				Port:       8080,
				Protocol:   api.ServiceTargetProtocolHttp,
				Enabled:    true,
			},
		},
		Auth: api.ServiceAuthConfig{
			BearerAuth: &api.BearerAuthConfig{
				Enabled:            true,
				DistributionGroups: &[]string{"group1", "group2"},
			},
		},
	}

	var model ReverseProxyServiceModel
	d := reverseProxyServiceAPIToTerraform(ctx, original, &model)
	if d.HasError() {
		t.Fatalf("APIToTerraform failed with %d errors", d.ErrorsCount())
	}

	req, d := reverseProxyServiceTerraformToAPI(ctx, &model)
	if d.HasError() {
		t.Fatalf("TerraformToAPI failed with %d errors", d.ErrorsCount())
	}

	if req.Auth.BearerAuth == nil {
		t.Fatal("Expected BearerAuth to be set")
	}
	if req.Auth.BearerAuth.Enabled != true {
		t.Error("BearerAuth.Enabled should be true")
	}
	if req.Auth.BearerAuth.DistributionGroups == nil {
		t.Fatal("Expected DistributionGroups to be set")
	}
	groups := *req.Auth.BearerAuth.DistributionGroups
	if len(groups) != 2 {
		t.Fatalf("Expected 2 distribution groups, got %d", len(groups))
	}
	if groups[0] != "group1" || groups[1] != "group2" {
		t.Errorf("Distribution groups mismatch: got %v", groups)
	}
	if req.Auth.PasswordAuth != nil {
		t.Error("PasswordAuth should be nil")
	}
	if req.Auth.PinAuth != nil {
		t.Error("PinAuth should be nil")
	}
}

func Test_reverseProxyServiceDataSourceSchema(t *testing.T) {
	d := &ReverseProxyServiceDataSource{}

	var metaResp datasource.MetadataResponse
	d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "netbird"}, &metaResp)
	if metaResp.TypeName != "netbird_reverse_proxy_service" {
		t.Errorf("Expected type name netbird_reverse_proxy_service, got %s", metaResp.TypeName)
	}

	var schemaResp datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("Schema returned errors")
	}

	attrs := schemaResp.Schema.Attributes
	expectedAttrs := []string{"id", "name", "domain", "enabled", "pass_host_header", "rewrite_redirects", "proxy_cluster", "targets", "auth"}
	for _, attr := range expectedAttrs {
		if _, ok := attrs[attr]; !ok {
			t.Errorf("Expected attribute %s in schema", attr)
		}
	}

	var _ datasource.DataSource = &ReverseProxyServiceDataSource{}
}

func Test_reverseProxyServiceRoundtrip_headerAuth(t *testing.T) {
	ctx := context.Background()

	original := &api.Service{
		Id:      "svc-header-rt",
		Name:    "header-roundtrip",
		Domain:  "header-rt.example.com",
		Enabled: true,
		Targets: []api.ServiceTarget{
			{
				TargetId:   "peer1",
				TargetType: api.ServiceTargetTargetTypePeer,
				Port:       8080,
				Protocol:   api.ServiceTargetProtocolHttp,
				Enabled:    true,
			},
		},
		Auth: api.ServiceAuthConfig{
			HeaderAuths: &[]api.HeaderAuthConfig{
				{Enabled: true, Header: "X-API-Key", Value: "secret-key"},
				{Enabled: true, Header: "Authorization", Value: "Bearer token123"},
			},
		},
	}

	var model ReverseProxyServiceModel
	d := reverseProxyServiceAPIToTerraform(ctx, original, &model)
	if d.HasError() {
		t.Fatalf("APIToTerraform failed with %d errors", d.ErrorsCount())
	}

	req, d := reverseProxyServiceTerraformToAPI(ctx, &model)
	if d.HasError() {
		t.Fatalf("TerraformToAPI failed with %d errors", d.ErrorsCount())
	}

	if req.Auth.HeaderAuths == nil {
		t.Fatal("Expected HeaderAuths to be set")
	}
	headers := *req.Auth.HeaderAuths
	if len(headers) != 2 {
		t.Fatalf("Expected 2 header auths, got %d", len(headers))
	}
	if headers[0].Header != "X-API-Key" || headers[0].Value != "secret-key" {
		t.Errorf("First header auth mismatch: got %+v", headers[0])
	}
	if headers[1].Header != "Authorization" || headers[1].Value != "Bearer token123" {
		t.Errorf("Second header auth mismatch: got %+v", headers[1])
	}
}

func Test_reverseProxyServiceRoundtrip_accessRestrictions(t *testing.T) {
	ctx := context.Background()

	original := &api.Service{
		Id:      "svc-ar-rt",
		Name:    "access-restrictions-roundtrip",
		Domain:  "ar-rt.example.com",
		Enabled: true,
		Targets: []api.ServiceTarget{
			{
				TargetId:   "peer1",
				TargetType: api.ServiceTargetTargetTypePeer,
				Port:       8080,
				Protocol:   api.ServiceTargetProtocolHttp,
				Enabled:    true,
			},
		},
		Auth: api.ServiceAuthConfig{
			PasswordAuth: &api.PasswordAuthConfig{
				Enabled:  true,
				Password: "test",
			},
		},
		AccessRestrictions: &api.AccessRestrictions{
			AllowedCountries: &[]string{"US", "DE"},
			BlockedCidrs:     &[]string{"192.168.0.0/16", "10.0.0.0/8"},
		},
	}

	var model ReverseProxyServiceModel
	d := reverseProxyServiceAPIToTerraform(ctx, original, &model)
	if d.HasError() {
		t.Fatalf("APIToTerraform failed with %d errors", d.ErrorsCount())
	}

	if model.AccessRestrictions.IsNull() {
		t.Fatal("AccessRestrictions should not be null")
	}

	req, d := reverseProxyServiceTerraformToAPI(ctx, &model)
	if d.HasError() {
		t.Fatalf("TerraformToAPI failed with %d errors", d.ErrorsCount())
	}

	if req.AccessRestrictions == nil {
		t.Fatal("Expected AccessRestrictions to be set")
	}
	if req.AccessRestrictions.AllowedCountries == nil || len(*req.AccessRestrictions.AllowedCountries) != 2 {
		t.Fatalf("Expected 2 allowed countries, got %v", req.AccessRestrictions.AllowedCountries)
	}
	countries := *req.AccessRestrictions.AllowedCountries
	if countries[0] != "DE" || countries[1] != "US" {
		t.Errorf("Allowed countries mismatch: got %v", countries)
	}
	if req.AccessRestrictions.BlockedCidrs == nil || len(*req.AccessRestrictions.BlockedCidrs) != 2 {
		t.Fatalf("Expected 2 blocked CIDRs, got %v", req.AccessRestrictions.BlockedCidrs)
	}
	if req.AccessRestrictions.AllowedCidrs != nil {
		t.Error("AllowedCidrs should be nil when not set")
	}
	if req.AccessRestrictions.BlockedCountries != nil {
		t.Error("BlockedCountries should be nil when not set")
	}
}

func Test_reverseProxyServiceAPIToTerraform_nullAccessRestrictions(t *testing.T) {
	ctx := context.Background()
	svc := &api.Service{
		Id:      "svc-no-ar",
		Name:    "no-access-restrictions",
		Domain:  "noar.example.com",
		Enabled: true,
		Targets: []api.ServiceTarget{
			{
				TargetId:   "peer1",
				TargetType: api.ServiceTargetTargetTypePeer,
				Port:       80,
				Protocol:   api.ServiceTargetProtocolHttp,
				Enabled:    true,
			},
		},
		Auth: api.ServiceAuthConfig{},
	}

	var out ReverseProxyServiceModel
	d := reverseProxyServiceAPIToTerraform(ctx, svc, &out)
	if d.HasError() {
		t.Fatalf("Expected no error diagnostics, found %d errors", d.ErrorsCount())
	}
	if !out.AccessRestrictions.IsNull() {
		t.Error("AccessRestrictions should be null when not set in API")
	}
}

func mustObjectValue(ctx context.Context, attrTypes map[string]attr.Type, val any) types.Object {
	obj, d := types.ObjectValueFrom(ctx, attrTypes, val)
	if d.HasError() {
		panic("failed to create object value in test helper")
	}
	return obj
}

func Test_reverseProxyNewFieldsRoundTrip(t *testing.T) {
	ctx := context.Background()
	inherit, block := api.ServiceTargetAccessActionInherit, api.ServiceTargetAccessActionBlock
	mode := api.AccessRestrictionsCrowdsecModeObserve
	yes, no := true, false
	svc := &api.Service{
		Id: "s", Name: "n", Domain: "d.example", Enabled: true,
		Targets: []api.ServiceTarget{
			{TargetId: "a", TargetType: "peer", Port: 80, Protocol: "http", Enabled: true, AccessAction: &inherit, Options: &api.ServiceTargetOptions{DirectUpstream: &no}},
			{TargetId: "b", TargetType: "peer", Port: 80, Protocol: "http", Enabled: true, AccessAction: &block, Options: &api.ServiceTargetOptions{DirectUpstream: &yes}},
		},
		AccessRestrictions: &api.AccessRestrictions{CrowdsecMode: &mode},
	}
	var data ReverseProxyServiceModel
	if d := reverseProxyServiceAPIToTerraform(ctx, svc, &data); d.HasError() {
		t.Fatal(d)
	}
	var targets []ReverseProxyServiceTargetModel
	if d := data.Targets.ElementsAs(ctx, &targets, false); d.HasError() {
		t.Fatal(d)
	}
	if targets[0].AccessAction.ValueString() != "inherit" || targets[1].AccessAction.ValueString() != "block" {
		t.Errorf("access_action: found %v, %v", targets[0].AccessAction, targets[1].AccessAction)
	}
	// direct_upstream=false is the default and reads as no options at all.
	if !targets[0].Options.IsNull() {
		t.Errorf("options with only direct_upstream=false should read as null, found %v", targets[0].Options)
	}
	if v := targets[1].Options.Attributes()["direct_upstream"]; !v.Equal(types.BoolValue(true)) {
		t.Errorf("direct_upstream: found %v", v)
	}
	if v := data.AccessRestrictions.Attributes()["crowdsec_mode"]; !v.Equal(types.StringValue("observe")) {
		t.Errorf("crowdsec_mode: found %v", v)
	}

	// Back to a request: configured values are sent, an unknown action is not.
	data.Targets, _ = types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), []ReverseProxyServiceTargetModel{
		dataTarget(targets[0], types.StringUnknown()), dataTarget(targets[1], types.StringValue("block")),
	})
	data.Auth = types.ObjectNull(ReverseProxyServiceAuthModel{}.TFType().AttrTypes)
	req, d := reverseProxyServiceTerraformToAPI(ctx, &data)
	if d.HasError() {
		t.Fatal(d)
	}
	got := *req.Targets
	if got[0].AccessAction != nil {
		t.Errorf("an unknown access_action must not be sent, found %v", *got[0].AccessAction)
	}
	if got[1].AccessAction == nil || *got[1].AccessAction != block {
		t.Errorf("configured access_action: found %v", got[1].AccessAction)
	}
	if got[1].Options == nil || got[1].Options.DirectUpstream == nil || !*got[1].Options.DirectUpstream {
		t.Errorf("direct_upstream not sent: %+v", got[1].Options)
	}
	if req.AccessRestrictions == nil || req.AccessRestrictions.CrowdsecMode == nil || *req.AccessRestrictions.CrowdsecMode != mode {
		t.Errorf("crowdsec_mode not sent: %+v", req.AccessRestrictions)
	}
}

func dataTarget(m ReverseProxyServiceTargetModel, action types.String) ReverseProxyServiceTargetModel {
	m.AccessAction = action
	return m
}

func Test_preserveTargetPlanValues_pathAndDirectUpstream(t *testing.T) {
	ctx := context.Background()
	optsType := ReverseProxyTargetOptionsModel{}.TFType()
	options := func(direct types.Bool) types.Object {
		attrs := map[string]attr.Value{}
		for name, typ := range optsType.AttrTypes {
			switch typ {
			case types.BoolType:
				attrs[name] = types.BoolNull()
			case types.StringType:
				attrs[name] = types.StringNull()
			default:
				attrs[name] = types.MapNull(types.StringType)
			}
		}
		attrs["direct_upstream"] = direct
		return types.ObjectValueMust(optsType.AttrTypes, attrs)
	}
	target := func(path string, action string, opts types.Object) ReverseProxyServiceTargetModel {
		return ReverseProxyServiceTargetModel{
			TargetId: types.StringValue("peer"), TargetType: types.StringValue("peer"), Host: types.StringNull(),
			Port: types.Int64Value(80), Protocol: types.StringValue("http"), Path: types.StringValue(path),
			Enabled: types.BoolValue(true), AccessAction: types.StringValue(action), Options: opts,
		}
	}
	list := func(targets ...ReverseProxyServiceTargetModel) types.List {
		l, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), targets)
		if d.HasError() {
			t.Fatal(d)
		}
		return l
	}
	nullOpts := types.ObjectNull(optsType.AttrTypes)
	for name, tc := range map[string]struct {
		plannedDirect, liveDirect types.Object
		want                      types.Bool
	}{
		"false as written while off":  {options(types.BoolValue(false)), nullOpts, types.BoolValue(false)},
		"switched on outside":         {options(types.BoolValue(false)), options(types.BoolValue(true)), types.BoolValue(true)},
		"switched off outside":        {options(types.BoolValue(true)), nullOpts, types.BoolNull()}, // options null
		"left out, switched on":       {options(types.BoolNull()), options(types.BoolValue(true)), types.BoolValue(true)},
		"left out, off stays omitted": {options(types.BoolNull()), nullOpts, types.BoolNull()},
	} {
		t.Run(name, func(t *testing.T) {
			// One backend serving two paths with different actions.
			planned := list(target("/public", "inherit", nullOpts), target("/admin", "block", tc.plannedDirect))
			live := list(target("/admin", "block", tc.liveDirect), target("/public", "inherit", nullOpts))
			got, d := preserveTargetPlanValues(ctx, planned, live)
			if d.HasError() {
				t.Fatal(d)
			}
			var out []ReverseProxyServiceTargetModel
			got.ElementsAs(ctx, &out, false)
			// Both pair, so the planned order stands.
			if out[1].Path.ValueString() != "/admin" || out[1].AccessAction.ValueString() != "block" || out[0].Path.ValueString() != "/public" {
				t.Fatalf("targets matched to the wrong path: %+v", out)
			}
			if !out[0].Options.IsNull() {
				t.Errorf("/public must keep no options, found %v", out[0].Options)
			}
			// With no options left on the server the parent is null unless the
			// planned object sets only defaults.
			wantNull := tc.liveDirect.IsNull() && tc.plannedDirect.Attributes()["direct_upstream"].Equal(types.BoolValue(true))
			if out[1].Options.IsNull() != wantNull {
				t.Fatalf("options: expected null %v, found %v", wantNull, out[1].Options)
			}
			if wantNull {
				return
			}
			if v := out[1].Options.Attributes()["direct_upstream"]; !v.Equal(tc.want) {
				t.Errorf("direct_upstream: expected %v, found %v", tc.want, v)
			}
		})
	}
}

// A target whose path the server reports differently (moved outside
// Terraform) is not paired: its server values stay, so the plan shows it.
func Test_preserveTargetPlanValues_movedPathStaysVisible(t *testing.T) {
	ctx := context.Background()
	target := func(path, host string) ReverseProxyServiceTargetModel {
		return ReverseProxyServiceTargetModel{
			TargetId: types.StringValue("peer1"), TargetType: types.StringValue("peer"), Port: types.Int64Value(80),
			Protocol: types.StringValue("http"), Enabled: types.BoolValue(true), Host: types.StringValue(host),
			Path: types.StringValue(path), AccessAction: types.StringValue("block"),
			Options: types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		}
	}
	planned, _ := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), []ReverseProxyServiceTargetModel{target("/admin", "10.0.0.1")})
	live, _ := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), []ReverseProxyServiceTargetModel{target("/old-admin", "100.0.0.9")})
	got, d := preserveTargetPlanValues(ctx, planned, live)
	if d.HasError() {
		t.Fatal(d)
	}
	var out []ReverseProxyServiceTargetModel
	got.ElementsAs(ctx, &out, false)
	if out[0].Path.ValueString() != "/old-admin" || out[0].Host.ValueString() != "100.0.0.9" {
		t.Errorf("a moved target must keep its server values, found %+v", out[0])
	}
}

// Two targets of one backend on the same path pair one to one.
func Test_preserveTargetPlanValues_samePathPairsOnce(t *testing.T) {
	ctx := context.Background()
	target := func(port int64, host string) ReverseProxyServiceTargetModel {
		return ReverseProxyServiceTargetModel{
			TargetId: types.StringValue("peer1"), TargetType: types.StringValue("peer"), Port: types.Int64Value(port),
			Protocol: types.StringValue("http"), Enabled: types.BoolValue(true), Host: types.StringValue(host),
			Path: types.StringValue("/"), AccessAction: types.StringValue("inherit"),
			Options: types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		}
	}
	planned, _ := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), []ReverseProxyServiceTargetModel{target(80, "a.example"), target(81, "b.example")})
	live, _ := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), []ReverseProxyServiceTargetModel{target(80, "100.0.0.1"), target(81, "100.0.0.2")})
	got, d := preserveTargetPlanValues(ctx, planned, live)
	if d.HasError() {
		t.Fatal(d)
	}
	var out []ReverseProxyServiceTargetModel
	got.ElementsAs(ctx, &out, false)
	if out[0].Host.ValueString() != "a.example" || out[1].Host.ValueString() != "b.example" {
		t.Errorf("each planned target pairs once: found %s, %s", out[0].Host.ValueString(), out[1].Host.ValueString())
	}
}

// Targets of one backend that share a path pair on port and protocol: on
// create with the paths left out, and on refresh after one was removed.
func Test_matchPlanTargets_portDecides(t *testing.T) {
	target := func(port int64, path types.String, host string) ReverseProxyServiceTargetModel {
		return ReverseProxyServiceTargetModel{
			TargetId: types.StringValue("peer1"), Port: types.Int64Value(port), Protocol: types.StringValue("http"),
			Path: path, Host: types.StringValue(host),
		}
	}
	unknown, root := types.StringUnknown(), types.StringValue("/")
	// Create: both paths unknown in the plan.
	got := matchPlanTargets(
		[]ReverseProxyServiceTargetModel{target(8080, unknown, "a"), target(8081, unknown, "b")},
		[]ReverseProxyServiceTargetModel{target(8081, root, ""), target(8080, root, "")})
	if got[0].Host.ValueString() != "b" || got[1].Host.ValueString() != "a" {
		t.Errorf("create: expected 8081->b and 8080->a, found %v", got)
	}
	// Refresh after 8080 was removed outside Terraform.
	got = matchPlanTargets(
		[]ReverseProxyServiceTargetModel{target(8080, root, "a"), target(8081, root, "b")},
		[]ReverseProxyServiceTargetModel{target(8081, root, "")})
	if len(got) != 1 || got[0].Host.ValueString() != "b" {
		t.Errorf("refresh: the survivor must pair with its own prior target, found %v", got)
	}
}

// Targets of one subnet resource that share path, port and protocol pair on
// their host.
func Test_matchPlanTargets_hostDecides(t *testing.T) {
	target := func(path types.String, host string) ReverseProxyServiceTargetModel {
		return ReverseProxyServiceTargetModel{
			TargetId: types.StringValue("subnet"), Port: types.Int64Value(80), Protocol: types.StringValue("http"),
			Path: path, Host: types.StringValue(host), TargetType: types.StringValue("subnet"),
		}
	}
	unknown, root := types.StringUnknown(), types.StringValue("/")
	got := matchPlanTargets(
		[]ReverseProxyServiceTargetModel{target(root, "10.0.0.1"), target(root, "10.0.0.2")},
		[]ReverseProxyServiceTargetModel{target(root, "10.0.0.2")})
	if len(got) != 1 || got[0].Host.ValueString() != "10.0.0.2" {
		t.Errorf("refresh: the survivor must pair with its own prior target, found %v", got)
	}
	got = matchPlanTargets(
		[]ReverseProxyServiceTargetModel{target(unknown, "10.0.0.1"), target(unknown, "10.0.0.2")},
		[]ReverseProxyServiceTargetModel{target(root, "10.0.0.2"), target(root, "10.0.0.1")})
	if len(got) != 2 || got[0].Host.ValueString() != "10.0.0.2" || got[1].Host.ValueString() != "10.0.0.1" {
		t.Errorf("create: expected pairs by host, found %v", got)
	}
}

// Targets equal in every compared field pair in list order; a survivor whose
// port changed still pairs on its host.
func Test_matchPlanTargets_duplicatesAndMovedPort(t *testing.T) {
	target := func(port int64, host types.String, tag string) ReverseProxyServiceTargetModel {
		return ReverseProxyServiceTargetModel{
			TargetId: types.StringValue("x"), Port: types.Int64Value(port), Protocol: types.StringValue("http"),
			Path: types.StringValue("/"), Host: host, TargetType: types.StringValue(tag),
		}
	}
	null := types.StringNull()
	got := matchPlanTargets(
		[]ReverseProxyServiceTargetModel{target(80, null, "first"), target(80, null, "second")},
		[]ReverseProxyServiceTargetModel{target(80, types.StringValue("100.0.0.1"), ""), target(80, types.StringValue("100.0.0.1"), "")})
	if len(got) != 2 || got[0].TargetType.ValueString() != "first" || got[1].TargetType.ValueString() != "second" {
		t.Errorf("duplicates: expected pairs in list order, found %v", got)
	}
	got = matchPlanTargets(
		[]ReverseProxyServiceTargetModel{target(80, types.StringValue("10.0.0.1"), "A"), target(80, types.StringValue("10.0.0.2"), "B")},
		[]ReverseProxyServiceTargetModel{target(81, types.StringValue("10.0.0.2"), "")})
	if len(got) != 1 || got[0].TargetType.ValueString() != "B" {
		t.Errorf("moved port: expected the survivor to pair with B, found %v", got)
	}
}

// A subnet target keeps the host the server has, so a host changed outside
// Terraform shows; among equal targets the same position pairs.
func Test_preserveTargetPlanValues_subnetHostAndPosition(t *testing.T) {
	ctx := context.Background()
	target := func(typ string, path types.String, host, timeout string) ReverseProxyServiceTargetModel {
		opts := types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes)
		if timeout != "" {
			attrs := map[string]attr.Value{}
			for name, at := range (ReverseProxyTargetOptionsModel{}).TFType().AttrTypes {
				switch at {
				case types.BoolType:
					attrs[name] = types.BoolNull()
				case types.StringType:
					attrs[name] = types.StringNull()
				default:
					attrs[name] = types.MapNull(types.StringType)
				}
			}
			attrs["request_timeout"] = types.StringValue(timeout)
			opts = types.ObjectValueMust(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes, attrs)
		}
		return ReverseProxyServiceTargetModel{
			TargetId: types.StringValue("x"), TargetType: types.StringValue(typ), Port: types.Int64Value(80),
			Protocol: types.StringValue("http"), Enabled: types.BoolValue(true), Path: path,
			Host: types.StringValue(host), AccessAction: types.StringValue("inherit"), Options: opts,
		}
	}
	list := func(ts ...ReverseProxyServiceTargetModel) types.List {
		l, _ := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), ts)
		return l
	}
	root := types.StringValue("/")
	got, d := preserveTargetPlanValues(ctx,
		list(target("subnet", root, "10.0.0.1", ""), target("subnet", root, "10.0.0.2", "")),
		list(target("subnet", root, "10.0.0.3", ""), target("subnet", root, "10.0.0.4", "")))
	if d.HasError() {
		t.Fatal(d)
	}
	var out []ReverseProxyServiceTargetModel
	got.ElementsAs(ctx, &out, false)
	if out[0].Host.ValueString() != "10.0.0.3" || out[1].Host.ValueString() != "10.0.0.4" {
		t.Errorf("subnet hosts changed outside Terraform must show, found %s, %s", out[0].Host.ValueString(), out[1].Host.ValueString())
	}
	// Create: the first path left out (unknown), the second "/", both on one
	// peer; the API answers in request order.
	got, d = preserveTargetPlanValues(ctx,
		list(target("peer", types.StringUnknown(), "", "5s"), target("peer", root, "", "10s")),
		list(target("peer", types.StringNull(), "100.0.0.1", "5s"), target("peer", root, "100.0.0.1", "10s")))
	if d.HasError() {
		t.Fatal(d)
	}
	got.ElementsAs(ctx, &out, false)
	timeout := func(m ReverseProxyServiceTargetModel) string {
		v, _ := m.Options.Attributes()["request_timeout"].(types.String)
		return v.ValueString()
	}
	if timeout(out[0]) != "5s" || timeout(out[1]) != "10s" {
		t.Errorf("options must stay with their own target, found %s, %s", timeout(out[0]), timeout(out[1]))
	}
}

// After an earlier target was removed outside Terraform, equal survivors keep
// their relative order.
func Test_matchPlanTargets_removalKeepsRelativeOrder(t *testing.T) {
	target := func(id, tag string) ReverseProxyServiceTargetModel {
		return ReverseProxyServiceTargetModel{
			TargetId: types.StringValue(id), Port: types.Int64Value(80), Protocol: types.StringValue("http"),
			Path: types.StringValue("/"), Host: types.StringNull(), TargetType: types.StringValue(tag),
		}
	}
	got := matchPlanTargets(
		[]ReverseProxyServiceTargetModel{target("a", "A"), target("p", "B"), target("p", "C")},
		[]ReverseProxyServiceTargetModel{target("p", ""), target("p", "")})
	if len(got) != 2 || got[0].TargetType.ValueString() != "B" || got[1].TargetType.ValueString() != "C" {
		t.Errorf("expected B, C in order, found %v", got)
	}
}

// Planned options keep their spelling where they mean the server's value and
// take the server's value where they do not.
func Test_reconcileOptions(t *testing.T) {
	optsType := ReverseProxyTargetOptionsModel{}.TFType()
	options := func(timeout, idle types.String, skip types.Bool) types.Object {
		attrs := map[string]attr.Value{}
		for name, typ := range optsType.AttrTypes {
			switch typ {
			case types.BoolType:
				attrs[name] = types.BoolNull()
			case types.StringType:
				attrs[name] = types.StringNull()
			default:
				attrs[name] = types.MapNull(types.StringType)
			}
		}
		attrs["request_timeout"], attrs["session_idle_timeout"], attrs["skip_tls_verify"] = timeout, idle, skip
		return types.ObjectValueMust(optsType.AttrTypes, attrs)
	}
	s, n := types.StringValue, types.StringNull()
	got, d := reconcileOptions(options(s("2m"), s("5s"), types.BoolValue(false)), options(s("2m0s"), s("10s"), types.BoolNull()))
	if d.HasError() {
		t.Fatal(d)
	}
	a := got.Attributes()
	if !a["request_timeout"].Equal(s("2m")) {
		t.Errorf("an equal duration keeps its planned spelling, found %v", a["request_timeout"])
	}
	if !a["session_idle_timeout"].Equal(s("10s")) {
		t.Errorf("a different duration takes the server value, found %v", a["session_idle_timeout"])
	}
	if !a["skip_tls_verify"].Equal(types.BoolValue(false)) {
		t.Errorf("a planned false stays while the server has none, found %v", a["skip_tls_verify"])
	}
	got, _ = reconcileOptions(options(s("5s"), n, types.BoolValue(true)), types.ObjectNull(optsType.AttrTypes))
	if !got.IsNull() {
		t.Errorf("with no options on the server, options that set more than defaults must read as null, found %v", got)
	}
	got, _ = reconcileOptions(options(n, n, types.BoolValue(false)), types.ObjectNull(optsType.AttrTypes))
	if got.IsNull() || !got.Attributes()["skip_tls_verify"].Equal(types.BoolValue(false)) {
		t.Errorf("options that set only defaults stay as planned, found %v", got)
	}
}

// The server loads targets without an order: a reordered response keeps the
// planned order when every target pairs, and the API order otherwise.
func Test_preserveTargetPlanValues_order(t *testing.T) {
	ctx := context.Background()
	target := func(id string) ReverseProxyServiceTargetModel {
		return ReverseProxyServiceTargetModel{
			TargetId: types.StringValue(id), TargetType: types.StringValue("peer"), Host: types.StringNull(),
			Port: types.Int64Value(80), Protocol: types.StringValue("http"), Path: types.StringValue("/" + id),
			Enabled: types.BoolValue(true), AccessAction: types.StringValue("inherit"),
			Options: types.ObjectNull(ReverseProxyTargetOptionsModel{}.TFType().AttrTypes),
		}
	}
	list := func(ids ...string) types.List {
		var targets []ReverseProxyServiceTargetModel
		for _, id := range ids {
			targets = append(targets, target(id))
		}
		l, d := types.ListValueFrom(ctx, ReverseProxyServiceTargetModel{}.TFType(), targets)
		if d.HasError() {
			t.Fatal(d)
		}
		return l
	}
	for name, tc := range map[string]struct {
		planned, live []string
		want          []string
	}{
		"reordered":       {[]string{"a", "b", "c"}, []string{"c", "a", "b"}, []string{"a", "b", "c"}},
		"one added":       {[]string{"a", "b"}, []string{"c", "b", "a"}, []string{"c", "b", "a"}},
		"one removed":     {[]string{"a", "b", "c"}, []string{"c", "a"}, []string{"c", "a"}},
		"one replaced":    {[]string{"a", "b"}, []string{"x", "a"}, []string{"x", "a"}},
		"same order kept": {[]string{"a", "b"}, []string{"a", "b"}, []string{"a", "b"}},
	} {
		got, d := preserveTargetPlanValues(ctx, list(tc.planned...), list(tc.live...))
		if d.HasError() {
			t.Fatalf("%s: %v", name, d)
		}
		var out []ReverseProxyServiceTargetModel
		got.ElementsAs(ctx, &out, false)
		var ids []string
		for _, o := range out {
			ids = append(ids, o.TargetId.ValueString())
		}
		if !slices.Equal(ids, tc.want) {
			t.Errorf("%s: expected %v, found %v", name, tc.want, ids)
		}
	}
}
