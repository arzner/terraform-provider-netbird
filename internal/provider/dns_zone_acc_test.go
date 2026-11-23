//go:build e2e

package provider

import (
	"context"
	"fmt"
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
	var firstZone, zoneID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().DNSZones.GetZone, &zoneID),
		Steps: []resource.TestStep{
			{
				Config: testDNSRecordResource(zoneName, "before.local", rName, "www", "A", "192.168.1.1", 300),
				Check:  testRecordID(zoneFull, &firstZone),
			},
			{
				Config: testDNSRecordResource(zoneName, "after.local", rName, "www", "A", "192.168.1.1", 300),
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
						return testCheckGone(testClient().DNSZones.GetZone, &firstZone)(s)
					},
				),
			},
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
