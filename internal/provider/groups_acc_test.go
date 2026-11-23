//go:build e2e

package provider

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// Test_Groups_DataSource checks netbird_groups filtering against a live NetBird.
func Test_Groups_DataSource(t *testing.T) {
	testE2E(t)
	rName := "g" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	ds := "data.netbird_groups." + rName

	// Filtered by the name and issuer of a group the same configuration creates: exactly that group.
	byName := fmt.Sprintf(`
data "netbird_groups" "%[1]s" {
  name   = netbird_group.%[1]s.name
  issued = "api"
}
`, rName)
	dsCase(t, testGroupResource(rName, `[]`)+byName, resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(ds, "ids.#", "1"),
		resource.TestCheckResourceAttrPair(ds, "ids.0", "netbird_group."+rName, "id"),
		checkGroupsAgainstAPI(ds, func(g api.Group) bool { return g.Name == rName }),
	))

	// Two groups sharing a name: the API refuses a duplicate name on create
	// but not on an update, which keeps the group's ID, so the second group
	// is created under its own name and renamed. Both are returned, by ID.
	twins := func(second string) string {
		return fmt.Sprintf(`
resource "netbird_group" "%[1]s" {
  name = %[1]q
}

resource "netbird_group" "%[1]s_twin" {
  name = %[2]q
}

data "netbird_groups" "%[1]s" {
  name       = %[1]q
  depends_on = [netbird_group.%[1]s, netbird_group.%[1]s_twin]
}
`, rName, second)
	}
	bothIDs := func(s *terraform.State) error {
		ids := []string{
			s.RootModule().Resources["netbird_group."+rName].Primary.ID,
			s.RootModule().Resources["netbird_group."+rName+"_twin"].Primary.ID,
		}
		slices.Sort(ids)
		attrs := s.RootModule().Resources[ds].Primary.Attributes
		for i, id := range ids {
			if got := attrs["ids."+strconv.Itoa(i)]; got != id {
				return fmt.Errorf("ids.%d: expected %s, found %s", i, id, got)
			}
			if got := attrs["groups."+strconv.Itoa(i)+".id"]; got != id {
				return fmt.Errorf("groups.%d.id: expected %s, found %s", i, id, got)
			}
		}
		return nil
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: twins(rName + "-other")},
			{
				Config: twins(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(ds, "ids.#", "2"),
					resource.TestCheckResourceAttr(ds, "groups.#", "2"),
					bothIDs,
				),
			},
		},
	})

	// A name no group has is an empty result, where netbird_group fails with No match.
	missing := fmt.Sprintf(`
data "netbird_groups" "%[1]s" {
  name = "%[1]s-never-created"
}
`, rName)
	// Empty lists, not null: iterating over the result must work.
	dsCaseState(t, missing,
		statecheck.ExpectKnownValue(ds, tfjsonpath.New("ids"), knownvalue.ListSizeExact(0)),
		statecheck.ExpectKnownValue(ds, tfjsonpath.New("groups"), knownvalue.ListSizeExact(0)),
	)

	// An issuer filter returns every group the server holds with that issuer.
	byIssuer := fmt.Sprintf(`
data "netbird_groups" "%s" {
  issued = "api"
}
`, rName)
	dsCase(t, byIssuer, checkGroupsAgainstAPI(ds, func(g api.Group) bool {
		return g.Issued != nil && *g.Issued == api.GroupIssuedApi
	}))
}

// checkGroupsAgainstAPI asserts the data source returned exactly the API groups
// keep selects, sorted by ID, with every attribute mapped from the API value.
func checkGroupsAgainstAPI(ds string, keep func(api.Group) bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		groups, err := testClient().Groups.List(context.Background())
		if err != nil {
			return fmt.Errorf("list groups: %w", err)
		}
		var want []api.Group
		for _, g := range groups {
			if keep(g) {
				want = append(want, g)
			}
		}
		if len(want) == 0 {
			return fmt.Errorf("no API group matches, the case tests nothing")
		}
		// The data source lists groups by ID; memberships are compared as sets.
		slices.SortFunc(want, func(a, b api.Group) int { return strings.Compare(a.Id, b.Id) })
		checks := []resource.TestCheckFunc{
			resource.TestCheckResourceAttr(ds, "ids.#", strconv.Itoa(len(want))),
			resource.TestCheckResourceAttr(ds, "groups.#", strconv.Itoa(len(want))),
		}
		for i, g := range want {
			p := fmt.Sprintf("groups.%d.", i)
			checks = append(checks,
				resource.TestCheckResourceAttr(ds, fmt.Sprintf("ids.%d", i), g.Id),
				resource.TestCheckResourceAttr(ds, p+"id", g.Id),
				resource.TestCheckResourceAttr(ds, p+"name", g.Name),
				checkOptionalAttr(ds, p+"issued", (*string)(g.Issued)),
				resource.TestCheckResourceAttr(ds, p+"peers.#", strconv.Itoa(len(g.Peers))),
				resource.TestCheckResourceAttr(ds, p+"resources.#", strconv.Itoa(len(g.Resources))),
			)
			for _, peer := range g.Peers {
				checks = append(checks, resource.TestCheckTypeSetElemAttr(ds, p+"peers.*", peer.Id))
			}
			for _, r := range g.Resources {
				checks = append(checks, resource.TestCheckTypeSetElemAttr(ds, p+"resources.*", r.Id))
			}
		}
		return resource.ComposeAggregateTestCheckFunc(checks...)(s)
	}
}
