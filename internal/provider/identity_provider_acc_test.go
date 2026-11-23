//go:build e2e

package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_IdentityProvider_Create(t *testing.T) {
	testE2E(t)
	rName := "idp" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_identity_provider." + rName
	var createdID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().IdentityProviders.Get, &createdID),
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testIdentityProviderResource(rName, "jumpcloud", "oidc", "client-id", "client-secret", "https://oauth.id.jumpcloud.com/"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "name", "jumpcloud"),
					resource.TestCheckResourceAttr(rNameFull, "type", "oidc"),
					resource.TestCheckResourceAttr(rNameFull, "client_id", "client-id"),
					resource.TestCheckResourceAttr(rNameFull, "client_secret", "client-secret"),
					resource.TestCheckResourceAttr(rNameFull, "issuer", "https://oauth.id.jumpcloud.com/"),
					func(s *terraform.State) error {
						pID := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						idp, err := testClient().IdentityProviders.Get(context.Background(), pID)
						if err != nil {
							return err
						}

						return matchPairs(map[string][]any{
							"name":      {"jumpcloud", idp.Name},
							"type":      {api.IdentityProviderTypeOidc, idp.Type},
							"client_id": {"client-id", idp.ClientId},
							"issuer":    {"https://oauth.id.jumpcloud.com/", idp.Issuer},
						})
					},
				),
			},
			{
				ResourceName:            rNameFull,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"client_secret"},
			},
		},
	})
}

func Test_IdentityProvider_Update(t *testing.T) {
	testE2E(t)
	rName := "idp" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_identity_provider." + rName
	var createdID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().IdentityProviders.Get, &createdID),
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testIdentityProviderResource(rName, "jumpcloud", "oidc", "client-id", "client-secret", "https://oauth.id.jumpcloud.com/"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
				),
			},
			{
				ResourceName: rName,
				Config:       testIdentityProviderResource(rName, "updated-jumpcloud", "oidc", "new-client-id", "new-secret", "https://oauth.id.jumpcloud.com/"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "name", "updated-jumpcloud"),
					resource.TestCheckResourceAttr(rNameFull, "type", "oidc"),
					resource.TestCheckResourceAttr(rNameFull, "client_id", "new-client-id"),
					resource.TestCheckResourceAttr(rNameFull, "client_secret", "new-secret"),
					resource.TestCheckResourceAttr(rNameFull, "issuer", "https://oauth.id.jumpcloud.com/"),
					func(s *terraform.State) error {
						pID := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						idp, err := testClient().IdentityProviders.Get(context.Background(), pID)
						if err != nil {
							return err
						}
						return matchPairs(map[string][]any{
							"name":      {"updated-jumpcloud", idp.Name},
							"type":      {api.IdentityProviderTypeOidc, idp.Type},
							"client_id": {"new-client-id", idp.ClientId},
							"issuer":    {"https://oauth.id.jumpcloud.com/", idp.Issuer},
						})
					},
				),
			},
		},
	})
}

// Test_IdentityProvider_TypesAddedInNetBird covers the types the provider did
// not accept before: each is created, checked on the server, given a
// type-prefixed ID and imported. adfs also takes its own path on the server
// (other scopes).
func Test_IdentityProvider_TypesAddedInNetBird(t *testing.T) {
	testE2E(t)
	for _, idpType := range []string{"keycloak", "authentik", "adfs"} {
		t.Run(idpType, func(t *testing.T) {
			rName := "idp" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
			rNameFull := "netbird_identity_provider." + rName
			var createdID string
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { testEnsureManagementRunning(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             testCheckGone(testClient().IdentityProviders.Get, &createdID),
				Steps: []resource.TestStep{
					{
						Config: testIdentityProviderResource(rName, idpType, idpType, "client-id", "client-secret", "https://oauth.id.jumpcloud.com/"),
						Check: resource.ComposeAggregateTestCheckFunc(
							testRecordID(rNameFull, &createdID),
							resource.TestCheckResourceAttr(rNameFull, "type", idpType),
							resource.TestMatchResourceAttr(rNameFull, "id", regexp.MustCompile("^"+idpType+"-")),
							idpTypeOnServer(&createdID, idpType),
						),
					},
					{ResourceName: rNameFull, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"client_secret"}},
				},
			})
		})
	}
}

// Test_IdentityProvider_TypeChangeReplaces: the server refuses to change the
// type of a provider, so a new type replaces it.
func Test_IdentityProvider_TypeChangeReplaces(t *testing.T) {
	testE2E(t)
	rName := "idp" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_identity_provider." + rName
	var createdID, firstID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().IdentityProviders.Get, &createdID),
		Steps: []resource.TestStep{
			{
				Config: testIdentityProviderResource(rName, "migrated", "oidc", "client-id", "client-secret", "https://oauth.id.jumpcloud.com/"),
				Check:  testRecordID(rNameFull, &firstID),
			},
			{
				// A type without a built-in issuer and issuer = "" fails at
				// plan time, before the working provider is deleted.
				Config:      testIdentityProviderResource(rName, "migrated", "keycloak", "client-id", "client-secret", ""),
				ExpectError: regexp.MustCompile(`Issuer required`),
			},
			{
				// So does an issuer the server refuses (http).
				Config:      testIdentityProviderResource(rName, "migrated", "keycloak", "client-id", "client-secret", "http://sso.example.test/realms/main"),
				ExpectError: regexp.MustCompile(`(?s)Invalid issuer.*https`),
			},
			{
				Config:   testIdentityProviderResource(rName, "migrated", "oidc", "client-id", "client-secret", "https://oauth.id.jumpcloud.com/"),
				PlanOnly: true,
			},
			{
				Config: testIdentityProviderResource(rName, "migrated", "keycloak", "client-id", "client-secret", "https://oauth.id.jumpcloud.com/"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(rNameFull, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestMatchResourceAttr(rNameFull, "id", regexp.MustCompile("^keycloak-")),
					idpTypeOnServer(&createdID, "keycloak"),
					func(s *terraform.State) error {
						// Only a not-found answer proves the old one is gone.
						return testCheckGone(testClient().IdentityProviders.Get, &firstID)(s)
					},
				),
			},
		},
	})
}

// idpTypeOnServer checks the provider's type as the server stores it.
func idpTypeOnServer(id *string, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		idp, err := testClient().IdentityProviders.Get(context.Background(), *id)
		if err != nil {
			return fmt.Errorf("get identity provider: %w", err)
		}
		if string(idp.Type) != want {
			return fmt.Errorf("type on the server: expected %s, found %s", want, idp.Type)
		}
		return nil
	}
}

func testIdentityProviderResource(rName, name, idpType, clientID, clientSecret, issuer string) string {
	return fmt.Sprintf(`resource "netbird_identity_provider" "%s" {
  name          = "%s"
  type          = "%s"
  client_id     = "%s"
  client_secret = "%s"
  issuer        = "%s"
}
`, rName, name, idpType, clientID, clientSecret, issuer)
}
