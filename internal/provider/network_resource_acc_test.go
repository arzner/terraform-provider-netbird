//go:build e2e

package provider

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_NetworkResource_Create(t *testing.T) {
	testE2E(t)
	rName := "nre" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_network_resource." + rName
	var createdID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: testCheckGone(func(ctx context.Context, id string) (*api.NetworkResource, error) {
			return testClient().Networks.Resources(e2eNetworkID()).Get(ctx, id)
		}, &createdID),
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testNetworkResourceResource(rName, e2eNetworkID(), `example.com`, fmt.Sprintf("[%q, %q]", e2eGroupNotAllID(), e2eGroupAllID()), rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "address", "example.com"),
					resource.TestCheckResourceAttr(rNameFull, "groups.#", "2"),
					resource.TestCheckResourceAttr(rNameFull, "name", rName),
					func(s *terraform.State) error {
						nreID := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						resource, err := testClient().Networks.Resources(e2eNetworkID()).Get(context.Background(), nreID)
						if err != nil {
							return err
						}

						if resource.Address != "example.com" {
							return fmt.Errorf("NetworkResource Address mismatch, expected example.com, found %s on management server", resource.Address)
						}

						if !sameIDSet(resource.Groups, e2eGroupNotAllID(), e2eGroupAllID()) {
							return fmt.Errorf("NetworkResource Groups mismatch, expected [%s %s], found %#v on management server", e2eGroupNotAllID(), e2eGroupAllID(), resource.Groups)
						}

						if resource.Name != rName {
							return fmt.Errorf("NetworkResource Name mismatch, expected %s, found %s on management server", rName, resource.Name)
						}

						return nil
					},
				),
			},
			{
				ResourceName:      rNameFull,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: testImportIDFrom(rNameFull, "/", "network_id", "id"),
			},
		},
	})
}

func Test_NetworkResource_Update(t *testing.T) {
	testE2E(t)
	rName := "nre" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_network_resource." + rName
	var createdID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: testCheckGone(func(ctx context.Context, id string) (*api.NetworkResource, error) {
			return testClient().Networks.Resources(e2eNetworkID()).Get(ctx, id)
		}, &createdID),
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testNetworkResourceResource(rName, e2eNetworkID(), `example.com`, fmt.Sprintf("[%q]", e2eGroupNotAllID()), rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
				),
			},
			{
				ResourceName: rName,
				Config:       testNetworkResourceResource(rName, e2eNetworkID(), `google.com`, fmt.Sprintf("[%q, %q]", e2eGroupAllID(), e2eGroupNotAllID()), rName+"Updated"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "address", "google.com"),
					resource.TestCheckResourceAttr(rNameFull, "groups.#", "2"),
					resource.TestCheckResourceAttr(rNameFull, "name", rName+"Updated"),
					func(s *terraform.State) error {
						nreID := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						resource, err := testClient().Networks.Resources(e2eNetworkID()).Get(context.Background(), nreID)
						if err != nil {
							return err
						}

						if resource.Address != "google.com" {
							return fmt.Errorf("NetworkResource Address mismatch, expected google.com, found %s on management server", resource.Address)
						}

						if !sameIDSet(resource.Groups, e2eGroupNotAllID(), e2eGroupAllID()) {
							return fmt.Errorf("NetworkResource Groups mismatch, expected [%s %s], found %#v on management server", e2eGroupNotAllID(), e2eGroupAllID(), resource.Groups)
						}

						if resource.Name != rName+"Updated" {
							return fmt.Errorf("NetworkResource Name mismatch, expected %s, found %s on management server", rName+"Updated", resource.Name)
						}

						return nil
					},
				),
			},
		},
	})
}

func testNetworkResourceResource(rName, networkID, address, groups, name string) string {
	return fmt.Sprintf(`resource "netbird_network_resource" "%s" {
	network_id = "%s"
	address = "%s"
	groups = %s
	name = "%s"
}`, rName, networkID, address, groups, name)
}

// Test_NetworkResource_WithoutGroups: a resource can exist without groups,
// and with groups left out its membership is not managed here, so groups
// that add it, through netbird_group.resources or the API after the plan,
// keep it across an update.
func Test_NetworkResource_WithoutGroups(t *testing.T) {
	testE2E(t)
	rName := "nre" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_network_resource." + rName
	var createdID string
	config := func(name, extra string) string {
		return fmt.Sprintf(`resource "netbird_network_resource" "%s" {
  network_id = %q
  address    = "10.77.0.0/24"
  name       = %q
}
%s`, rName, e2eNetworkID(), name, extra)
	}
	// ignored keeps groups out of the plan: a rename then carries the prior
	// set as a known value.
	ignored := func(name, extra string) string {
		return fmt.Sprintf(`resource "netbird_network_resource" "%s" {
  network_id = %q
  address    = "10.77.0.0/24"
  name       = %q
  lifecycle {
    ignore_changes = [groups]
  }
}
%s`, rName, e2eNetworkID(), name, extra)
	}
	// groups attaches the resource to the first group; the second group
	// starts without resources (left out, so the API may add some).
	groups := func(firstResources, secondResources string) string {
		return fmt.Sprintf(`
resource "netbird_group" "%[1]s" {
  name      = "%[1]s"
  resources = %[2]s
}

resource "netbird_group" "%[1]s_second" {
  name = "%[1]s-second"
  %[3]s
}

resource "netbird_group" "%[1]s_third" {
  name = "%[1]s-third"
  %[3]s
}`, rName, firstResources, secondResources)
	}
	attached := fmt.Sprintf("[netbird_network_resource.%s.id]", rName)
	groupsOnServer := func(want ...string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			res, err := testClient().Networks.Resources(e2eNetworkID()).Get(context.Background(), createdID)
			if err != nil {
				return err
			}
			var got []string
			for _, g := range res.Groups {
				got = append(got, g.Id)
			}
			var expected []string
			for _, w := range want {
				expected = append(expected, s.RootModule().Resources[w].Primary.ID)
			}
			slices.Sort(got)
			slices.Sort(expected)
			if !slices.Equal(got, expected) {
				return fmt.Errorf("groups on the server: expected %v, found %v", expected, got)
			}
			return nil
		}
	}
	var secondID, thirdID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: testCheckGone(func(ctx context.Context, id string) (*api.NetworkResource, error) {
			return testClient().Networks.Resources(e2eNetworkID()).Get(ctx, id)
		}, &createdID),
		Steps: []resource.TestStep{
			{
				Config: config(rName, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttr(rNameFull, "groups.#", "0"),
					groupsOnServer(),
				),
			},
			{
				ResourceName: rNameFull, ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) { return e2eNetworkID() + "/" + createdID, nil },
			},
			{
				// A group adds the resource; the resource does not fight it.
				Config: config(rName, groups(attached, "")),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID("netbird_group."+rName+"_second", &secondID),
					testRecordID("netbird_group."+rName+"_third", &thirdID),
					groupsOnServer("netbird_group."+rName),
				),
			},
			{
				// An update with groups left out keeps that membership, and
				// one the API adds after the plan.
				Config: config(rName+"-renamed", groups(attached, "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					testExpectUpdateInPlace(rNameFull),
					groupGetsResource{group: &secondID, name: rName + "-second", resource: &createdID},
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "name", rName+"-renamed"),
					resource.TestCheckResourceAttr(rNameFull, "groups.#", "2"),
					resource.TestCheckTypeSetElemAttrPair(rNameFull, "groups.*", "netbird_group."+rName, "id"),
					resource.TestCheckTypeSetElemAttrPair(rNameFull, "groups.*", "netbird_group."+rName+"_second", "id"),
					groupsOnServer("netbird_group."+rName, "netbird_group."+rName+"_second"),
				),
			},
			{
				// With groups ignored the plan holds the known set; a group
				// added through the API after the plan stops the rename
				// before anything is written.
				Config: ignored(rName+"-ignored", groups(attached, "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectKnownValue(rNameFull, tfjsonpath.New("groups"), knownvalue.SetSizeExact(2)),
					groupGetsResource{group: &thirdID, name: rName + "-third", resource: &createdID},
				}},
				ExpectError: regexp.MustCompile(`groups changed after the plan`),
			},
			{
				// Nothing was written: the old name stays and the new group
				// is still attached.
				PreConfig: func() {
					res, err := testClient().Networks.Resources(e2eNetworkID()).Get(context.Background(), createdID)
					if err != nil {
						t.Fatal(err)
					}
					if res.Name != rName+"-renamed" || len(resourceGroupIDs(res.Groups)) != 3 {
						t.Fatalf("expected the old name and three groups on the server, found %s and %v", res.Name, res.Groups)
					}
				},
				Config: config(rName+"-renamed", groups(attached, "")),
			},
			{
				// Detach before teardown: the server refuses to delete a
				// group that still has resources.
				Config: config(rName+"-renamed", groups("[]", "resources = []")),
				Check:  groupsOnServer(),
			},
		},
	})
}

// groupGetsResource is a plan check that adds a network resource to a group
// through the API between plan and apply, as another tool would.
type groupGetsResource struct {
	group, resource *string
	name            string
}

func (c groupGetsResource) CheckPlan(ctx context.Context, _ plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	resources := []api.Resource{{Id: *c.resource, Type: api.ResourceTypeSubnet}}
	_, resp.Error = testClient().Groups.Update(ctx, *c.group, api.GroupRequest{Name: c.name, Resources: &resources})
}

// Test_NetworkResource_EmptyGroups: groups = [] removes the resource from
// every group.
func Test_NetworkResource_EmptyGroups(t *testing.T) {
	testE2E(t)
	rName := "nre" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_network_resource." + rName
	var createdID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: testCheckGone(func(ctx context.Context, id string) (*api.NetworkResource, error) {
			return testClient().Networks.Resources(e2eNetworkID()).Get(ctx, id)
		}, &createdID),
		Steps: []resource.TestStep{
			{
				Config: testNetworkResourceResource(rName, e2eNetworkID(), "10.78.0.0/24", fmt.Sprintf("[%q]", e2eGroupAllID()), rName),
				Check:  testRecordID(rNameFull, &createdID),
			},
			{
				Config: testNetworkResourceResource(rName, e2eNetworkID(), "10.78.0.0/24", "[]", rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "groups.#", "0"),
					func(*terraform.State) error {
						res, err := testClient().Networks.Resources(e2eNetworkID()).Get(context.Background(), createdID)
						if err != nil {
							return err
						}
						if len(res.Groups) != 0 {
							return fmt.Errorf("expected no groups on the server, found %v", res.Groups)
						}
						return nil
					},
				),
			},
		},
	})
}
