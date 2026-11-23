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

func Test_User_Create(t *testing.T) {
	testE2E(t)
	rName := "u" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_user." + rName
	var createdID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: testCheckAbsentFromList(testClient().Users.List,
			func(u api.User) string { return u.Id }, &createdID),
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testUserResource(rName, fmt.Sprintf("[%q]", e2eGroupNotAllID()), `false`, `user`),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "name", rName),
					resource.TestCheckResourceAttr(rNameFull, "is_service_user", "true"),
					resource.TestCheckResourceAttr(rNameFull, "auto_groups.#", "1"),
					resource.TestCheckResourceAttr(rNameFull, "auto_groups.0", e2eGroupNotAllID()),
					resource.TestCheckResourceAttr(rNameFull, "is_blocked", "false"),
					resource.TestCheckResourceAttr(rNameFull, "role", "user"),
					func(s *terraform.State) error {
						uID := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						users, err := testClient().Users.List(context.Background())
						if err != nil {
							return err
						}
						var user *api.User
						for _, u := range users {
							if u.Id == uID {
								user = &u
							}
						}
						if user == nil {
							return fmt.Errorf("User not found")
						}
						// IsServiceUser is an optional *bool, so a server that
						// omits it would panic the dereference below rather than
						// failing the assertion.
						if user.IsServiceUser == nil {
							return fmt.Errorf("User %s has no is_service_user on management server", uID)
						}
						return matchPairs(map[string][]any{
							"name":            {rName, user.Name},
							"is_service_user": {true, *user.IsServiceUser},
							"auto_groups.#":   {int(1), len(user.AutoGroups)},
							"auto_groups.0":   {e2eGroupNotAllID(), user.AutoGroups[0]},
							"is_blocked":      {false, user.IsBlocked},
							"role":            {"user", user.Role},
						})
					},
				),
			},
			{
				ResourceName:      rNameFull,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Users are always created as service users: inviting a regular user needs an
// IdP that can deliver the invitation, which a self-hosted test deployment has
// no way to satisfy.

func testUserResource(rName, groups, blocked, role string) string {
	return fmt.Sprintf(`resource "netbird_user" "%s" {
	name            = "%s"
  is_service_user = true
  auto_groups     = %s
  is_blocked      = %s
  role            = "%s"
}`, rName, rName, groups, blocked, role)
}

// Test_User_ServiceUserNameAndKind: the API does not change a user's name or
// email. A service user's new name replaces it, an email change is refused at
// plan time, and is_service_user left out keeps what the user is.
func Test_User_ServiceUserNameAndKind(t *testing.T) {
	testE2E(t)
	rName := "u" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_user." + rName
	var firstID, createdID string
	config := func(name, extra string) string {
		return fmt.Sprintf(`resource "netbird_user" %q {
  name = %q
  role = "user"
  %s
}`, rName, name, extra)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckAbsentFromList(testClient().Users.List, func(u api.User) string { return u.Id }, &createdID),
		Steps: []resource.TestStep{
			{
				Config: config(rName, "is_service_user = true"),
				Check:  testRecordID(rNameFull, &firstID),
			},
			{
				// Left out, the user stays a service user.
				Config:   config(rName, ""),
				PlanOnly: true,
			},
			{
				Config:      config(rName, `email = "someone@example.com"`),
				ExpectError: regexp.MustCompile(`Email cannot be changed`),
			},
			{
				// A rename replaces the user, and the new one is created as
				// the configuration says, so the kind must be stated.
				Config:      config(rName+"-renamed", ""),
				ExpectError: regexp.MustCompile(`Service user kind required`),
			},
			{
				Config: config(rName+"-renamed", "is_service_user = true"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(rNameFull, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttr(rNameFull, "name", rName+"-renamed"),
					func(*terraform.State) error {
						if createdID == firstID {
							return fmt.Errorf("the service user kept its ID %s; expected a replacement", createdID)
						}
						users, err := testClient().Users.List(context.Background())
						if err != nil {
							return err
						}
						found := false
						for _, u := range users {
							if u.Id == firstID {
								return fmt.Errorf("the replaced service user %s still exists", firstID)
							}
							if u.Id == createdID {
								found = u.Name == rName+"-renamed" && u.IsServiceUser != nil && *u.IsServiceUser
							}
						}
						if !found {
							return fmt.Errorf("user %s is not a service user named %s-renamed on the server", createdID, rName)
						}
						return nil
					},
				),
			},
			// Imported with the kind left out, it plans no change.
			{ResourceName: rNameFull, ImportState: true, ImportStateVerify: true},
			{
				Config:   config(rName+"-renamed", ""),
				PlanOnly: true,
			},
		},
	})
}
