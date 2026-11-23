//go:build e2e

package provider

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func Test_DNSZone_Create(t *testing.T) {
	testE2E(t)
	rName := "z" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_dns_zone." + rName
	var createdID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().DNSZones.GetZone, &createdID),
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testDNSZoneResource(rName, "test.local", true, false, fmt.Sprintf("[%q]", e2eGroupAllID())),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "name", rName),
					resource.TestCheckResourceAttr(rNameFull, "domain", "test.local"),
					resource.TestCheckResourceAttr(rNameFull, "enabled", "true"),
					resource.TestCheckResourceAttr(rNameFull, "enable_search_domain", "false"),
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

func Test_DNSZone_Update(t *testing.T) {
	testE2E(t)
	rName := "z" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_dns_zone." + rName
	var createdID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().DNSZones.GetZone, &createdID),
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testDNSZoneResource(rName, "test.local", true, false, fmt.Sprintf("[%q]", e2eGroupAllID())),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "name", rName),
					resource.TestCheckResourceAttr(rNameFull, "domain", "test.local"),
					resource.TestCheckResourceAttr(rNameFull, "enabled", "true"),
					resource.TestCheckResourceAttr(rNameFull, "enable_search_domain", "false"),
				),
			},
			{
				ResourceName: rName,
				Config:       testDNSZoneResource(rName, "test.local", false, true, fmt.Sprintf("[%q]", e2eGroupAllID())),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "name", rName),
					resource.TestCheckResourceAttr(rNameFull, "domain", "test.local"),
					resource.TestCheckResourceAttr(rNameFull, "enabled", "false"),
					resource.TestCheckResourceAttr(rNameFull, "enable_search_domain", "true"),
				),
			},
		},
	})
}

func Test_DNSZone_DataSource(t *testing.T) {
	testE2E(t)
	rName := "z" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	dsNameFull := "data.netbird_dns_zone." + rName
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testDNSZoneResourceAndDataSource(rName, "datasource.local", true, false, fmt.Sprintf("[%q]", e2eGroupAllID())),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(dsNameFull, "id"),
					resource.TestCheckResourceAttr(dsNameFull, "name", rName),
					resource.TestCheckResourceAttr(dsNameFull, "domain", "datasource.local"),
					resource.TestCheckResourceAttr(dsNameFull, "enabled", "true"),
					resource.TestCheckResourceAttr(dsNameFull, "enable_search_domain", "false"),
				),
			},
		},
	})
}

// Test_DNSZone_DomainReplaces: the server refuses to change a zone's domain,
// so a new domain replaces the zone, and its records with it.
func Test_DNSZone_DomainReplaces(t *testing.T) {
	testE2E(t)
	zoneName := "z" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rName := "r" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	zoneFull := "netbird_dns_zone." + zoneName
	recordFull := "netbird_dns_record." + rName
	var firstZone, zoneID, firstRecord string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().DNSZones.GetZone, &zoneID),
		Steps: []resource.TestStep{
			{
				// A new zone needs distribution_groups.
				Config:      strings.Replace(testDNSRecordResource(zoneName, "before.local", rName, "www", "A", "192.168.1.1", 300), fmt.Sprintf("distribution_groups = [%q]", e2eGroupAllID()), "", 1),
				ExpectError: regexp.MustCompile(`Distribution groups required`),
			},
			{
				Config: testDNSRecordResource(zoneName, "before.local", rName, "www", "A", "192.168.1.1", 300),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(zoneFull, &firstZone),
					testRecordID(recordFull, &firstRecord),
				),
			},
			{
				// A domain the server refuses fails the plan, so the zone
				// is not deleted for a create that cannot succeed.
				Config:      testDNSRecordResource(zoneName, "after.local.", rName, "www", "A", "192.168.1.1", 300),
				ExpectError: regexp.MustCompile(`Invalid zone domain`),
			},
			{
				// So do no groups, an empty set, a null group and a name the
				// server refuses, on a replacement.
				Config:      strings.Replace(testDNSRecordResource(zoneName, "after.local", rName, "www", "A", "192.168.1.1", 300), fmt.Sprintf("distribution_groups = [%q]", e2eGroupAllID()), "", 1),
				ExpectError: regexp.MustCompile(`Distribution groups required`),
			},
			{
				Config:      strings.Replace(testDNSRecordResource(zoneName, "after.local", rName, "www", "A", "192.168.1.1", 300), fmt.Sprintf("distribution_groups = [%q]", e2eGroupAllID()), "distribution_groups = []", 1),
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Value.*distribution_groups`),
			},
			{
				Config:      strings.Replace(testDNSRecordResource(zoneName, "after.local", rName, "www", "A", "192.168.1.1", 300), fmt.Sprintf("distribution_groups = [%q]", e2eGroupAllID()), "distribution_groups = [null]", 1),
				ExpectError: regexp.MustCompile(`Null Set Value|null`),
			},
			{
				Config:      strings.Replace(testDNSRecordResource(zoneName, "after.local", rName, "www", "A", "192.168.1.1", 300), fmt.Sprintf("name                = %q", zoneName), fmt.Sprintf("name                = %q", strings.Repeat("n", 256)), 1),
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Value Length.*name`),
			},
			{
				// A group that does not exist is found before the replacement.
				Config:      strings.Replace(testDNSRecordResource(zoneName, "after.local", rName, "www", "A", "192.168.1.1", 300), fmt.Sprintf("distribution_groups = [%q]", e2eGroupAllID()), `distribution_groups = ["nosuchgroup0000000000"]`, 1),
				ExpectError: regexp.MustCompile(`Distribution group not found`),
			},
			{
				// Left out on an in-place change, the groups are kept.
				Config:   testDNSRecordResource(zoneName, "before.local", rName, "www", "A", "192.168.1.1", 300),
				PlanOnly: true,
			},
			{
				Config: strings.Replace(testDNSRecordResource(zoneName, "before.local", rName, "www", "A", "192.168.1.1", 300), fmt.Sprintf("distribution_groups = [%q]", e2eGroupAllID()), "enable_search_domain = true", 1),
				Check: func(*terraform.State) error {
					zone, err := testClient().DNSZones.GetZone(context.Background(), firstZone)
					if err != nil {
						return err
					}
					if !zone.EnableSearchDomain || len(zone.DistributionGroups) != 1 || zone.DistributionGroups[0] != e2eGroupAllID() {
						return fmt.Errorf("expected search domain on and the group kept, found %v %v", zone.EnableSearchDomain, zone.DistributionGroups)
					}
					return nil
				},
			},
			{
				// A real group ID with a suffix the lookup URL would drop is
				// refused at plan time without a request.
				Config:      strings.Replace(testDNSRecordResource(zoneName, "after.local", rName, "www", "A", "192.168.1.1", 300), fmt.Sprintf("distribution_groups = [%q]", e2eGroupAllID()), fmt.Sprintf("distribution_groups = [%q]", e2eGroupAllID()+"#old"), 1),
				ExpectError: regexp.MustCompile(`Invalid group ID`),
			},
			{
				// The new zone's group is created by the same apply, so its
				// ID is not known at plan time.
				Config: strings.Replace(testDNSRecordResource(zoneName, "after.local", rName, "www", "A", "192.168.1.1", 300),
					fmt.Sprintf("distribution_groups = [%q]", e2eGroupAllID()), "distribution_groups = [netbird_group.zone.id]", 1) + fmt.Sprintf(`
resource "netbird_group" "zone" {
  name = %q
}
`, zoneName+"-group"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(zoneFull, plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction(recordFull, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(zoneFull, &zoneID),
					resource.TestCheckResourceAttr(zoneFull, "domain", "after.local"),
					resource.TestCheckResourceAttr(recordFull, "name", "www.after.local"),
					func(s *terraform.State) error {
						if zoneID == firstZone {
							return fmt.Errorf("the zone kept its ID %s; expected a replacement", zoneID)
						}
						zone, err := testClient().DNSZones.GetZone(context.Background(), zoneID)
						if err != nil {
							return err
						}
						if zone.Domain != "after.local" {
							return fmt.Errorf("domain on the server: expected after.local, found %s", zone.Domain)
						}
						groupID := s.RootModule().Resources["netbird_group.zone"].Primary.ID
						if !slices.Equal(zone.DistributionGroups, []string{groupID}) {
							return fmt.Errorf("distribution groups on the server: expected [%s], found %v", groupID, zone.DistributionGroups)
						}
						recordID := s.RootModule().Resources[recordFull].Primary.ID
						if recordID == firstRecord {
							return fmt.Errorf("the record kept its ID %s; expected a new one in the new zone", recordID)
						}
						found := false
						for _, rec := range zone.Records {
							found = found || (rec.Id == recordID && rec.Name == "www.after.local")
						}
						if !found {
							return fmt.Errorf("the new zone does not hold record %s (www.after.local): %+v", recordID, zone.Records)
						}
						return testCheckGone(testClient().DNSZones.GetZone, &firstZone)(s)
					},
				),
			},
			{
				// Only the domain changes and the group, known now, stays: the
				// plan looks it up, and the new zone holds it.
				Config: strings.Replace(testDNSRecordResource(zoneName, "again.local", rName, "www", "A", "192.168.1.1", 300),
					fmt.Sprintf("distribution_groups = [%q]", e2eGroupAllID()), "distribution_groups = [netbird_group.zone.id]", 1) + fmt.Sprintf(`
resource "netbird_group" "zone" {
  name = %q
}
`, zoneName+"-group"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(zoneFull, plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("netbird_group.zone", plancheck.ResourceActionNoop),
				}},
				Check: func(s *terraform.State) error {
					id := s.RootModule().Resources[zoneFull].Primary.ID
					if id == zoneID {
						return fmt.Errorf("the zone kept its ID %s; expected a replacement", id)
					}
					zone, err := testClient().DNSZones.GetZone(context.Background(), id)
					if err != nil {
						return err
					}
					groupID := s.RootModule().Resources["netbird_group.zone"].Primary.ID
					if zone.Domain != "again.local" || !slices.Equal(zone.DistributionGroups, []string{groupID}) {
						return fmt.Errorf("new zone on the server: expected again.local with [%s], found %s with %v", groupID, zone.Domain, zone.DistributionGroups)
					}
					return nil
				},
			},
			{ResourceName: zoneFull, ImportState: true, ImportStateVerify: true},
		},
	})
}

func testDNSZoneResource(name, domain string, enabled, enableSearchDomain bool, distributionGroups string) string {
	return fmt.Sprintf(`
resource "netbird_dns_zone" "%s" {
  name                 = "%s"
  domain               = "%s"
  enabled              = %t
  enable_search_domain = %t
  distribution_groups  = %s
}
`, name, name, domain, enabled, enableSearchDomain, distributionGroups)
}

func testDNSZoneResourceAndDataSource(name, domain string, enabled, enableSearchDomain bool, distributionGroups string) string {
	return testDNSZoneResource(name, domain, enabled, enableSearchDomain, distributionGroups) + fmt.Sprintf(`
data "netbird_dns_zone" "%s" {
  name = netbird_dns_zone.%s.name
}
`, name, name)
}
