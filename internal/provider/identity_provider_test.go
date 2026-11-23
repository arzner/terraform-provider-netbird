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
			// The functions must keep the shapes read here; any other shape
			// fails, so a change in how the server decides is looked at.
			switch d.Name.Name {
			case "IsValid":
				accepted = isValidCases(t, d)
			case "HasBuiltInIssuer":
				builtIn = builtInIssuerTypes(t, d)
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

// isValidCases reads `switch t { case A, B: return true }; return false`.
func isValidCases(t *testing.T, d *ast.FuncDecl) []string {
	t.Helper()
	recv := d.Recv.List[0].Names
	if len(d.Body.List) != 2 || len(recv) != 1 || !returnsBool(d.Body.List[1], "false") {
		t.Fatal("IsValid is not a switch followed by return false; update this test")
	}
	sw, ok := d.Body.List[0].(*ast.SwitchStmt)
	if !ok {
		t.Fatal("IsValid does not start with a switch; update this test")
	}
	if tag, isIdent := sw.Tag.(*ast.Ident); sw.Init != nil || !isIdent || tag.Name != recv[0].Name {
		t.Fatal("IsValid does not switch on its receiver; update this test")
	}
	var names []string
	for _, stmt := range sw.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok || len(cc.List) == 0 || len(cc.Body) != 1 || !returnsBool(cc.Body[0], "true") {
			t.Fatal("IsValid has a case that does not just return true, or a default; update this test")
		}
		for _, e := range cc.List {
			id, ok := e.(*ast.Ident)
			if !ok {
				t.Fatal("IsValid has a case value that is not a constant name; update this test")
			}
			names = append(names, id.Name)
		}
	}
	return names
}

// builtInIssuerTypes reads `return t == A || t == B ...`.
func builtInIssuerTypes(t *testing.T, d *ast.FuncDecl) []string {
	t.Helper()
	recv := d.Recv.List[0].Names
	if len(d.Body.List) != 1 || len(recv) != 1 {
		t.Fatal("HasBuiltInIssuer is not a single return; update this test")
	}
	ret, ok := d.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		t.Fatal("HasBuiltInIssuer is not a single return; update this test")
	}
	var names []string
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		be, ok := e.(*ast.BinaryExpr)
		if !ok {
			t.Fatal("HasBuiltInIssuer returns something other than comparisons; update this test")
		}
		switch be.Op {
		case token.LOR:
			walk(be.X)
			walk(be.Y)
		case token.EQL:
			x, xok := be.X.(*ast.Ident)
			y, yok := be.Y.(*ast.Ident)
			if !xok || !yok || x.Name != recv[0].Name {
				t.Fatal("HasBuiltInIssuer compares something other than its receiver with a constant; update this test")
			}
			names = append(names, y.Name)
		default:
			t.Fatal("HasBuiltInIssuer uses an operator other than || and ==; update this test")
		}
	}
	walk(ret.Results[0])
	return names
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
		"http issuer":                        {idpType: "keycloak", issuer: "http://sso.example.test/realms/main", wantError: true},
		"http issuer, type not known yet":    {idpType: tftypes.UnknownValue, issuer: "http://sso.example.test", wantError: true},
		"issuer without a host":              {idpType: "oidc", issuer: "https:///realms/main", wantError: true},
		"issuer with a port but no host":     {idpType: "oidc", issuer: "https://:8443/realms/main", wantError: true},
		"issuer with a port out of range":    {idpType: "oidc", issuer: "https://sso.example.com:65536", wantError: true},
		"issuer with a port":                 {idpType: "oidc", issuer: "https://sso.example.com:8443/realms/main"},
		"issuer with a stray bracket":        {idpType: "oidc", issuer: "https://sso.example.com]/realms/main", wantError: true},
		"issuer with an IPv6 host":           {idpType: "oidc", issuer: "https://[2001:db8::1]:8443/realms/main"},
		"issuer with user info":              {idpType: "oidc", issuer: "https://u:p@sso.example.test/", wantError: true},
		"issuer with a query":                {idpType: "oidc", issuer: "https://sso.example.test/?a=b", wantError: true},
		"issuer with a fragment":             {idpType: "oidc", issuer: "https://sso.example.test/#x", wantError: true},
		"google with an explicit issuer":     {idpType: "google", issuer: "https://accounts.google.com"},
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

func Test_keepKnownType(t *testing.T) {
	for name, tc := range map[string]struct {
		prior, read, want types.String
		warn              bool
	}{
		"same type":              {types.StringValue("keycloak"), types.StringValue("keycloak"), types.StringValue("keycloak"), false},
		"misread from the ID":    {types.StringValue("keycloak"), types.StringValue("okta"), types.StringValue("keycloak"), true},
		"import takes the read":  {types.StringNull(), types.StringValue("okta"), types.StringValue("okta"), false},
		"unknown takes the read": {types.StringUnknown(), types.StringValue("adfs"), types.StringValue("adfs"), false},
		"oidc misread as adfs":   {types.StringValue("oidc"), types.StringValue("adfs"), types.StringValue("oidc"), true},
		"google to microsoft":    {types.StringValue("google"), types.StringValue("microsoft"), types.StringValue("microsoft"), false},
		"oidc to google":         {types.StringValue("oidc"), types.StringValue("google"), types.StringValue("google"), false},
	} {
		t.Run(name, func(t *testing.T) {
			data := IdentityProviderModel{Type: tc.read}
			d := keepKnownType(tc.prior, &data)
			if !data.Type.Equal(tc.want) {
				t.Errorf("expected type %v, found %v", tc.want, data.Type)
			}
			if (d.WarningsCount() > 0) != tc.warn || d.HasError() {
				t.Errorf("expected a warning %v, found %v", tc.warn, d)
			}
		})
	}
}
