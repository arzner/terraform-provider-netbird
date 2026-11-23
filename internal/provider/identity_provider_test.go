package provider

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_identityProviderAPIToTerraform(t *testing.T) {
	cases := []struct {
		resource *api.IdentityProvider
		expected IdentityProviderModel
	}{
		{
			resource: &api.IdentityProvider{
				Id:       valPtr("idp1"),
				Name:     "OIDC Provider",
				Type:     api.IdentityProviderTypeOidc,
				ClientId: "client-id",
				Issuer:   "https://auth.example.com",
			},
			expected: IdentityProviderModel{
				Id:       types.StringValue("idp1"),
				Name:     types.StringValue("OIDC Provider"),
				Type:     types.StringValue("oidc"),
				ClientId: types.StringValue("client-id"),
				Issuer:   types.StringValue("https://auth.example.com"),
			},
		},
		{
			resource: &api.IdentityProvider{
				Id:       valPtr("idp2"),
				Name:     "Okta",
				Type:     api.IdentityProviderTypeOkta,
				ClientId: "okta-client",
				Issuer:   "https://dev-123.okta.com",
			},
			expected: IdentityProviderModel{
				Id:       types.StringValue("idp2"),
				Name:     types.StringValue("Okta"),
				Type:     types.StringValue("okta"),
				ClientId: types.StringValue("okta-client"),
				Issuer:   types.StringValue("https://dev-123.okta.com"),
			},
		},
	}

	for _, c := range cases {
		var out IdentityProviderModel
		// Preserve client_secret as it's write-only
		out.ClientSecret = types.StringValue("preserved-secret")
		identityProviderAPIToTerraform(c.resource, &out)

		// client_secret should be preserved from existing state
		c.expected.ClientSecret = types.StringValue("preserved-secret")
		if !reflect.DeepEqual(out, c.expected) {
			t.Fatalf("Expected:\n%#v\nFound:\n%#v", c.expected, out)
		}
	}
}

func Test_identityProviderTerraformToAPI(t *testing.T) {
	cases := []struct {
		data     IdentityProviderModel
		expected api.IdentityProviderRequest
	}{
		{
			data: IdentityProviderModel{
				Id:           types.StringValue("idp1"),
				Name:         types.StringValue("OIDC Provider"),
				Type:         types.StringValue("oidc"),
				ClientId:     types.StringValue("client-id"),
				ClientSecret: types.StringValue("secret"),
				Issuer:       types.StringValue("https://auth.example.com"),
			},
			expected: api.IdentityProviderRequest{
				Name:         "OIDC Provider",
				Type:         api.IdentityProviderTypeOidc,
				ClientId:     "client-id",
				ClientSecret: "secret",
				Issuer:       "https://auth.example.com",
			},
		},
		{
			data: IdentityProviderModel{
				Name:         types.StringValue("Entra ID"),
				Type:         types.StringValue("entra"),
				ClientId:     types.StringValue("entra-client"),
				ClientSecret: types.StringValue("entra-secret"),
				Issuer:       types.StringValue("https://login.microsoftonline.com/tenant/v2.0"),
			},
			expected: api.IdentityProviderRequest{
				Name:         "Entra ID",
				Type:         api.IdentityProviderTypeEntra,
				ClientId:     "entra-client",
				ClientSecret: "entra-secret",
				Issuer:       "https://login.microsoftonline.com/tenant/v2.0",
			},
		},
	}

	for _, c := range cases {
		out := identityProviderTerraformToAPI(context.Background(), c.data)
		if !reflect.DeepEqual(out, c.expected) {
			t.Fatalf("Expected:\n%#v\nFound:\n%#v", c.expected, out)
		}
	}
}

// Test_identityProviderTypes compares the accepted types with the ones the
// server's IdentityProviderType.IsValid accepts in the pinned netbird module,
// so a type NetBird adds or drops fails here instead of at plan time.
func Test_identityProviderTypes(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/netbirdio/netbird").Output()
	if err != nil {
		t.Fatalf("locate the netbird module (run `go mod download`): %v", err)
	}
	file := filepath.Join(strings.TrimSpace(string(out)), "management", "server", "types", "identity_provider.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	// The constants by name, then the names IsValid's switch accepts.
	values := map[string]string{}
	var accepted, builtIn []string
	for _, decl := range parsed.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.CONST {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							values[name.Name] = strings.Trim(lit.Value, `"`)
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil || len(d.Recv.List) != 1 {
				continue
			}
			if id, ok := d.Recv.List[0].Type.(*ast.Ident); !ok || id.Name != "IdentityProviderType" {
				continue
			}
			switch d.Name.Name {
			case "IsValid":
				// Expected shape: switch t { case A, B, ...: return true }
				// then return false. Anything else fails, so a change in
				// how the server decides is looked at.
				ast.Inspect(d.Body, func(n ast.Node) bool {
					cc, ok := n.(*ast.CaseClause)
					if !ok {
						return true
					}
					if len(cc.Body) != 1 || !returnsBool(cc.Body[0], "true") || len(cc.List) == 0 {
						t.Fatalf("IsValid has a case that does not just return true; update this test")
					}
					for _, e := range cc.List {
						id, ok := e.(*ast.Ident)
						if !ok {
							t.Fatalf("IsValid has a case value that is not a constant name; update this test")
						}
						accepted = append(accepted, id.Name)
					}
					return true
				})
			case "HasBuiltInIssuer":
				// Expected shape: return t == A || t == B.
				ast.Inspect(d.Body, func(n ast.Node) bool {
					if be, ok := n.(*ast.BinaryExpr); ok && be.Op == token.EQL {
						if id, ok := be.Y.(*ast.Ident); ok {
							builtIn = append(builtIn, id.Name)
						}
					}
					return true
				})
			}
		}
	}
	var server []string
	for _, name := range accepted {
		v, ok := values[name]
		if !ok {
			t.Fatalf("IsValid accepts %s, which is not a string constant in %s", name, file)
		}
		server = append(server, v)
	}
	slices.Sort(server)
	if len(server) == 0 {
		t.Fatalf("IdentityProviderType.IsValid accepts no type in %s", file)
	}
	if !slices.Equal(server, identityProviderTypes) {
		t.Errorf("the server accepts the types %v, the provider %v", server, identityProviderTypes)
	}
	var builtInValues []string
	for _, name := range builtIn {
		builtInValues = append(builtInValues, values[name])
	}
	slices.Sort(builtInValues)
	var provider []string
	for k := range builtInIssuer {
		provider = append(provider, k)
	}
	slices.Sort(provider)
	if len(builtInValues) == 0 || !slices.Equal(builtInValues, provider) {
		t.Errorf("the server gives a built-in issuer to %v, the provider to %v", builtInValues, provider)
	}
}

// returnsBool reports whether stmt is `return <value>` for a bool literal.
func returnsBool(stmt ast.Stmt, value string) bool {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	id, ok := ret.Results[0].(*ast.Ident)
	return ok && id.Name == value
}

func Test_identityProviderValidateConfig(t *testing.T) {
	ctx := context.Background()
	r := &IdentityProvider{}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	typ, ok := sch.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	for name, tc := range map[string]struct {
		idpType, issuer any
		wantError       bool
	}{
		"google with the built-in issuer":    {idpType: "google", issuer: ""},
		"microsoft with the built-in issuer": {idpType: "microsoft", issuer: ""},
		"keycloak without an issuer":         {idpType: "keycloak", issuer: "", wantError: true},
		"oidc without an issuer":             {idpType: "oidc", issuer: "", wantError: true},
		"keycloak with an issuer":            {idpType: "keycloak", issuer: "https://id.example.com/realms/x"},
		"issuer not known yet":               {idpType: "keycloak", issuer: tftypes.UnknownValue},
		"type not known yet":                 {idpType: tftypes.UnknownValue, issuer: ""},
	} {
		t.Run(name, func(t *testing.T) {
			values := map[string]tftypes.Value{}
			for n, at := range typ.AttributeTypes {
				values[n] = tftypes.NewValue(at, nil)
			}
			values["type"] = tftypes.NewValue(tftypes.String, tc.idpType)
			values["issuer"] = tftypes.NewValue(tftypes.String, tc.issuer)
			var resp resource.ValidateConfigResponse
			r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sch.Schema, Raw: tftypes.NewValue(typ, values)}}, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("expected error %v, found %v", tc.wantError, resp.Diagnostics)
			}
		})
	}
}
