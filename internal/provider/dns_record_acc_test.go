//go:build e2e

package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func Test_DNSRecord_Create(t *testing.T) {
	testE2E(t)
	zoneName := "z" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rName := "r" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_dns_record." + rName
	var zoneID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().DNSZones.GetZone, &zoneID),
		Steps: []resource.TestStep{
			{
				Config: testDNSRecordResource(zoneName, "test.local", rName, "www", "A", "192.168.1.1", 300),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordAttr(rNameFull, "zone_id", &zoneID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttrSet(rNameFull, "zone_id"),
					resource.TestCheckResourceAttr(rNameFull, "name", "www.test.local"),
					resource.TestCheckResourceAttr(rNameFull, "type", "A"),
					resource.TestCheckResourceAttr(rNameFull, "content", "192.168.1.1"),
					resource.TestCheckResourceAttr(rNameFull, "ttl", "300"),
				),
			},
			{
				ResourceName:      rNameFull,
				ImportState:       true,
				ImportStateIdFunc: testDNSRecordImportStateIdFunc(rNameFull),
				ImportStateVerify: true,
			},
		},
	})
}

func Test_DNSRecord_Update(t *testing.T) {
	testE2E(t)
	zoneName := "z" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rName := "r" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_dns_record." + rName
	var zoneID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().DNSZones.GetZone, &zoneID),
		Steps: []resource.TestStep{
			{
				Config: testDNSRecordResource(zoneName, "test.local", rName, "www", "A", "192.168.1.1", 300),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordAttr(rNameFull, "zone_id", &zoneID),
					resource.TestCheckResourceAttr(rNameFull, "name", "www.test.local"),
					resource.TestCheckResourceAttr(rNameFull, "content", "192.168.1.1"),
					resource.TestCheckResourceAttr(rNameFull, "ttl", "300"),
				),
			},
			{
				Config: testDNSRecordResource(zoneName, "test.local", rName, "api", "A", "192.168.1.2", 600),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "name", "api.test.local"),
					resource.TestCheckResourceAttr(rNameFull, "content", "192.168.1.2"),
					resource.TestCheckResourceAttr(rNameFull, "ttl", "600"),
				),
			},
			{
				// TTLs outside 60 to 86400, which the server accepts.
				Config: testDNSRecordResource(zoneName, "test.local", rName, "api", "A", "192.168.1.2", 30),
				Check:  testDNSRecordTTLOnServer(rNameFull, &zoneID, 30),
			},
			{
				Config: testDNSRecordResource(zoneName, "test.local", rName, "api", "A", "192.168.1.2", 172800),
				Check:  testDNSRecordTTLOnServer(rNameFull, &zoneID, 172800),
			},
			{
				Config:      testDNSRecordResource(zoneName, "test.local", rName, "api", "A", "192.168.1.2", -1),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`at least 0`),
			},
		},
	})
}

// testDNSRecordTTLOnServer checks the record's TTL in state and on the
// server.
func testDNSRecordTTLOnServer(name string, zoneID *string, want int) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(name, "ttl", strconv.Itoa(want)),
		func(s *terraform.State) error {
			id := s.RootModule().Resources[name].Primary.ID
			zone, err := testClient().DNSZones.GetZone(context.Background(), *zoneID)
			if err != nil {
				return err
			}
			for _, rec := range zone.Records {
				if rec.Id == id {
					if rec.Ttl != want {
						return fmt.Errorf("TTL on the server: expected %d, found %d", want, rec.Ttl)
					}
					return nil
				}
			}
			return fmt.Errorf("record %s is not in zone %s", id, *zoneID)
		},
	)
}

func Test_DNSRecord_CNAME(t *testing.T) {
	testE2E(t)
	zoneName := "z" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rName := "r" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_dns_record." + rName
	var zoneID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().DNSZones.GetZone, &zoneID),
		Steps: []resource.TestStep{
			{
				Config: testDNSRecordResource(zoneName, "test.local", rName, "mail", "CNAME", "mail.example.com", 300),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordAttr(rNameFull, "zone_id", &zoneID),
					resource.TestCheckResourceAttr(rNameFull, "name", "mail.test.local"),
					resource.TestCheckResourceAttr(rNameFull, "type", "CNAME"),
					resource.TestCheckResourceAttr(rNameFull, "content", "mail.example.com"),
				),
			},
		},
	})
}

func Test_DNSRecord_DataSource(t *testing.T) {
	testE2E(t)
	zoneName := "z" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rName := "r" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	dsNameFull := "data.netbird_dns_record." + rName
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testDNSRecordResourceAndDataSource(zoneName, "datasource.local", rName, "test", "A", "10.0.0.1", 300),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(dsNameFull, "id"),
					resource.TestCheckResourceAttrSet(dsNameFull, "zone_id"),
					resource.TestCheckResourceAttr(dsNameFull, "name", "test.datasource.local"),
					resource.TestCheckResourceAttr(dsNameFull, "type", "A"),
					resource.TestCheckResourceAttr(dsNameFull, "content", "10.0.0.1"),
				),
			},
		},
	})
}

func testDNSRecordResource(zoneName, domain, recordName, recordSubdomain, recordType, recordContent string, ttl int) string {
	return fmt.Sprintf(`
resource "netbird_dns_zone" "%s" {
  name                = "%s"
  domain              = "%s"
  distribution_groups = [%q]
}

resource "netbird_dns_record" "%s" {
  zone_id = netbird_dns_zone.%s.id
  name    = "%s.%s"
  type    = "%s"
  content = "%s"
  ttl     = %d
}
`, zoneName, zoneName, domain, e2eGroupAllID(), recordName, zoneName, recordSubdomain, domain, recordType, recordContent, ttl)
}

func testDNSRecordResourceAndDataSource(zoneName, domain, recordName, recordSubdomain, recordType, recordContent string, ttl int) string {
	return testDNSRecordResource(zoneName, domain, recordName, recordSubdomain, recordType, recordContent, ttl) + fmt.Sprintf(`
data "netbird_dns_record" "%s" {
  zone_id = netbird_dns_zone.%s.id
  name    = netbird_dns_record.%s.name
  type    = netbird_dns_record.%s.type
}
`, recordName, zoneName, recordName, recordName)
}

func testDNSRecordImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}

		zoneId := rs.Primary.Attributes["zone_id"]
		id := rs.Primary.ID

		if zoneId == "" || id == "" {
			return "", fmt.Errorf("zone_id or id is not set")
		}

		return fmt.Sprintf("%s:%s", zoneId, id), nil
	}
}
