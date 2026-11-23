//go:build e2e

package provider

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// Test_Users_DataSource checks netbird_users filtering against a live NetBird.
func Test_Users_DataSource(t *testing.T) {
	testE2E(t)
	rName := "u" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	ds := "data.netbird_users." + rName

	// testUserResource creates a service user: every selector
	// it satisfies, combined, finds only it.
	allSelectors := fmt.Sprintf(`
data "netbird_users" "%[1]s" {
  name            = netbird_user.%[1]s.name
  role            = "user"
  status          = "active"
  issued          = "api"
  is_service_user = true
  is_blocked      = false
  auto_groups     = [%[2]q]
}
`, rName, e2eGroupNotAllID())
	cfg := testUserResource(rName, fmt.Sprintf("[%q]", e2eGroupNotAllID()), `false`, `user`)
	dsCase(t, cfg+allSelectors, resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(ds, "ids.#", "1"),
		resource.TestCheckResourceAttrPair(ds, "ids.0", "netbird_user."+rName, "id"),
		checkUsersAgainstAPI(ds, func(u api.User) bool { return u.Name == rName }),
	))

	// Selectors that would match nothing or everything by mistake are refused
	// before any request: a null or empty auto group, an empty list, a role
	// the server does not know.
	refused := func(selector, want string) resource.TestStep {
		return resource.TestStep{
			Config: fmt.Sprintf(`
data "netbird_users" "%s" {
  %s
}
`, rName, selector),
			ExpectError: regexp.MustCompile(want),
		}
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			refused(`auto_groups = [null]`, `Null List Value`),
			refused(`auto_groups = []`, `(?s)Invalid Attribute Value.*auto_groups`),
			refused(`auto_groups = [""]`, `(?s)Invalid Attribute Value Length.*auto_groups`),
			refused(`role = "admn"`, `(?s)Invalid Attribute Value Match.*role`),
			{
				// A read deferred to apply by depends_on is still refused at
				// plan time, before the dependency is created.
				Config: fmt.Sprintf(`
resource "netbird_group" "%[1]s" {
  name = %[1]q
}

data "netbird_users" "%[1]s" {
  depends_on = [netbird_group.%[1]s]
}
`, rName),
				ExpectError: regexp.MustCompile(`(?s)Missing Attribute Configuration.*At least one of these attributes must be configured`),
			},
		},
	})

	// The server filters on is_service_user itself (service_user query);
	// the account's owner is a regular user.
	regularUsers := fmt.Sprintf(`
data "netbird_users" "%s" {
  is_service_user = false
}
`, rName)
	dsCase(t, regularUsers, checkUsersAgainstAPI(ds, func(u api.User) bool { return u.IsServiceUser != nil && !*u.IsServiceUser }))

	// An email no user has is an empty result, where netbird_user fails with No match.
	missing := fmt.Sprintf(`
data "netbird_users" "%[1]s" {
  email = "%[1]s@never-created.invalid"
}
`, rName)
	// Empty lists, not null: iterating over the result must work.
	dsCaseState(t, missing,
		statecheck.ExpectKnownValue(ds, tfjsonpath.New("ids"), knownvalue.ListSizeExact(0)),
		statecheck.ExpectKnownValue(ds, tfjsonpath.New("users"), knownvalue.ListSizeExact(0)),
	)

	// A role filter returns every user the server holds with that role.
	byRole := fmt.Sprintf(`
data "netbird_users" "%s" {
  role = "owner"
}
`, rName)
	dsCase(t, byRole, checkUsersAgainstAPI(ds, func(u api.User) bool { return u.Role == "owner" }))
}

// checkUsersAgainstAPI asserts the data source returned exactly the API users
// keep selects, sorted by ID, with every attribute mapped from the API value.
func checkUsersAgainstAPI(ds string, keep func(api.User) bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		users, err := testClient().Users.List(context.Background())
		if err != nil {
			return fmt.Errorf("list users: %w", err)
		}
		var want []api.User
		for _, u := range users {
			if keep(u) {
				want = append(want, u)
			}
		}
		if len(want) == 0 {
			return fmt.Errorf("no API user matches, the case tests nothing")
		}
		// The data source lists users by ID; auto groups are compared as a set.
		slices.SortFunc(want, func(a, b api.User) int { return strings.Compare(a.Id, b.Id) })
		checks := []resource.TestCheckFunc{
			resource.TestCheckResourceAttr(ds, "ids.#", strconv.Itoa(len(want))),
			resource.TestCheckResourceAttr(ds, "users.#", strconv.Itoa(len(want))),
		}
		for i, u := range want {
			p := fmt.Sprintf("users.%d.", i)
			var lastLogin *string
			if u.LastLogin != nil {
				v := u.LastLogin.Format(time.RFC3339)
				lastLogin = &v
			}
			checks = append(checks,
				resource.TestCheckResourceAttr(ds, fmt.Sprintf("ids.%d", i), u.Id),
				resource.TestCheckResourceAttr(ds, p+"id", u.Id),
				resource.TestCheckResourceAttr(ds, p+"name", u.Name),
				resource.TestCheckResourceAttr(ds, p+"email", u.Email),
				resource.TestCheckResourceAttr(ds, p+"role", u.Role),
				resource.TestCheckResourceAttr(ds, p+"status", string(u.Status)),
				resource.TestCheckResourceAttr(ds, p+"is_blocked", strconv.FormatBool(u.IsBlocked)),
				resource.TestCheckResourceAttr(ds, p+"auto_groups.#", strconv.Itoa(len(u.AutoGroups))),
				checkOptionalAttr(ds, p+"last_login", lastLogin),
				checkOptionalAttr(ds, p+"issued", u.Issued),
				checkOptionalAttr(ds, p+"is_current", boolString(u.IsCurrent)),
				checkOptionalAttr(ds, p+"is_service_user", boolString(u.IsServiceUser)),
			)
			for _, g := range u.AutoGroups {
				checks = append(checks, resource.TestCheckTypeSetElemAttr(ds, p+"auto_groups.*", g))
			}
		}
		return resource.ComposeAggregateTestCheckFunc(checks...)(s)
	}
}

// checkOptionalAttr expects key to equal *want, or to be null when want is nil.
func checkOptionalAttr(ds, key string, want *string) resource.TestCheckFunc {
	if want == nil {
		return resource.TestCheckNoResourceAttr(ds, key)
	}
	return resource.TestCheckResourceAttr(ds, key, *want)
}

// boolString formats an optional bool, keeping nil as nil.
func boolString(b *bool) *string {
	if b == nil {
		return nil
	}
	v := strconv.FormatBool(*b)
	return &v
}
