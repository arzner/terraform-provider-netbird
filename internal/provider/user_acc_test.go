//go:build e2e

package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
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

// Test_User_ServiceUserNameAndKind: the API does not change a user's name,
// email or kind, so changes are refused at plan time, or at apply for a value
// known only then; is_service_user left out keeps what the user is.
func Test_User_ServiceUserNameAndKind(t *testing.T) {
	testE2E(t)
	rName := "u" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_user." + rName
	var createdID string
	config := func(name, extra string) string {
		return fmt.Sprintf(`resource "netbird_user" %q {
  name = %q
  role = "user"
  %s
}`, rName, name, extra)
	}
	// fromApply sets the name from a value Terraform knows only at apply.
	fromApply := func(name, role string) string {
		return fmt.Sprintf(`resource "terraform_data" "name" {
  input = %q
}

resource "netbird_user" %q {
  name            = terraform_data.name.output
  role            = %q
  is_service_user = true
}`, name, rName, role)
	}
	roleOnServer := func(want string) error {
		users, err := testClient().Users.List(context.Background())
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.Id == createdID {
				if u.Name != rName || u.Role != want {
					return fmt.Errorf("user %s on the server: expected %s with role %s, found %s with role %s", createdID, rName, want, u.Name, u.Role)
				}
				return nil
			}
		}
		return fmt.Errorf("user %s is not on the server", createdID)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckAbsentFromList(testClient().Users.List, func(u api.User) string { return u.Id }, &createdID),
		Steps: []resource.TestStep{
			{
				Config: config(rName, "is_service_user = true"),
				Check:  testRecordID(rNameFull, &createdID),
			},
			{
				// Left out, the user stays a service user.
				Config:   config(rName, ""),
				PlanOnly: true,
			},
			{
				PlanOnly:    true,
				Config:      config(rName, `email = "someone@example.com"`),
				ExpectError: regexp.MustCompile(`Email cannot be changed`),
			},
			{
				// NetBird does not rename users: refused at plan time.
				PlanOnly:    true,
				Config:      config(rName+"-renamed", "is_service_user = true"),
				ExpectError: regexp.MustCompile(`Name cannot be changed`),
			},
			{
				PlanOnly:    true,
				Config:      config(rName, "is_service_user = false\n  email = \"someone@example.com\""),
				ExpectError: regexp.MustCompile(`User kind cannot be changed`),
			},
			{
				// A name known only at apply that resolves to the current one
				// applies, with the role change next to it.
				Config: fromApply(rName, "auditor"),
				Check:  func(*terraform.State) error { return roleOnServer("auditor") },
			},
			{
				// One that resolves to another name is refused at apply,
				// before anything is written (the role change too).
				Config:      fromApply(rName+"-renamed", "network_admin"),
				ExpectError: regexp.MustCompile(`Name cannot be changed`),
			},
			{
				PreConfig: func() {
					if err := roleOnServer("auditor"); err != nil {
						t.Fatal(err)
					}
				},
				Config: config(rName, "is_service_user = true"),
				Check:  func(*terraform.State) error { return roleOnServer("user") },
			},
			{ResourceName: rNameFull, ImportState: true, ImportStateVerify: true},
			{
				// An import block with the kind left out plans the import and
				// nothing else.
				Config:          config(rName, ""),
				ResourceName:    rNameFull,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportPlanChecks: resource.ImportPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(rNameFull, plancheck.ResourceActionNoop),
				}},
			},
		},
	})
}

// Test_User_CreateChecks: a new user is planned as the create will make it,
// and one the create cannot make as configured is refused at plan time. No
// step applies, so no invitation is sent.
func Test_User_CreateChecks(t *testing.T) {
	testE2E(t)
	rName := "u" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_user." + rName
	config := func(body string) string {
		return fmt.Sprintf(`resource "netbird_user" %q {
  name = %q
  %s
}`, rName, rName, body)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Left out, the kind is planned as a regular user.
				Config:             config(`email = "` + rName + `@example.com"`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(rNameFull, plancheck.ResourceActionCreate),
					plancheck.ExpectKnownValue(rNameFull, tfjsonpath.New("is_service_user"), knownvalue.Bool(false)),
				}},
			},
			{Config: config(`email = "` + rName + `@example.com"` + "\n  is_blocked = true"), PlanOnly: true, ExpectError: regexp.MustCompile(`Cannot create a blocked user`)},
			{Config: config("is_service_user = true\n  role = \"owner\""), PlanOnly: true, ExpectError: regexp.MustCompile(`Cannot create an owner`)},
			{Config: config("is_service_user = true\n  email = \"" + rName + "@example.com\""), PlanOnly: true, ExpectError: regexp.MustCompile(`Service users have no email`)},
			{Config: config(""), PlanOnly: true, ExpectError: regexp.MustCompile(`Name and email required`)},
			{
				// The kind known only at apply, an email and no name: neither
				// kind can be created.
				Config: fmt.Sprintf(`resource "terraform_data" "kind" {
  input = false
}

resource "netbird_user" %q {
  email           = %q
  is_service_user = terraform_data.kind.output
}`, rName, rName+"@example.com"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Name and email required`),
			},
			{
				// An invitation with an email but no name.
				Config:      fmt.Sprintf("resource \"netbird_user\" %q {\n  email = %q\n}", rName, rName+"@example.com"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Name and email required`),
			},
		},
	})
}
