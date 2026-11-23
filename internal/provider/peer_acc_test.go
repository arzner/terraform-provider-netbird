//go:build e2e

package provider

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func Test_Peer_Create(t *testing.T) {
	testE2E(t)
	rName := "p" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_peer." + rName
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testPeerResource(rName, testPeerID(t, "peer3"), rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "name", rName),
					func(s *terraform.State) error {
						pID := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						peer, err := testClient().Peers.Get(context.Background(), pID)
						if err != nil {
							return err
						}

						if peer.Name != rName {
							return fmt.Errorf("Peer name mismatch, expected %s, found %s on management server", rName, peer.Name)
						}

						// Every attribute the provider maps from the API, compared
						// against what the API says. 19 of the peer schema's 25
						// attributes are computed from the response and only two
						// were asserted anywhere, so a field mapped to the wrong
						// source — or not mapped at all — produced a state Terraform
						// was happy with and no test could see.
						attrs := s.RootModule().Resources[rNameFull].Primary.Attributes
						return matchPairs(map[string][]any{
							"os":                            {attrs["os"], peer.Os},
							"ip":                            {attrs["ip"], peer.Ip},
							"hostname":                      {attrs["hostname"], peer.Hostname},
							"dns_label":                     {attrs["dns_label"], peer.DnsLabel},
							"connection_ip":                 {attrs["connection_ip"], peer.ConnectionIp},
							"kernel_version":                {attrs["kernel_version"], peer.KernelVersion},
							"serial_number":                 {attrs["serial_number"], peer.SerialNumber},
							"ui_version":                    {attrs["ui_version"], peer.UiVersion},
							"version":                       {attrs["version"], peer.Version},
							"user_id":                       {attrs["user_id"], peer.UserId},
							"city_name":                     {attrs["city_name"], peer.CityName},
							"country_code":                  {attrs["country_code"], peer.CountryCode},
							"geoname_id":                    {attrs["geoname_id"], fmt.Sprint(peer.GeonameId)},
							"connected":                     {attrs["connected"], fmt.Sprint(peer.Connected)},
							"login_expired":                 {attrs["login_expired"], fmt.Sprint(peer.LoginExpired)},
							"ssh_enabled":                   {attrs["ssh_enabled"], fmt.Sprint(peer.SshEnabled)},
							"approval_required":             {attrs["approval_required"], fmt.Sprint(peer.ApprovalRequired)},
							"login_expiration_enabled":      {attrs["login_expiration_enabled"], fmt.Sprint(peer.LoginExpirationEnabled)},
							"inactivity_expiration_enabled": {attrs["inactivity_expiration_enabled"], fmt.Sprint(peer.InactivityExpirationEnabled)},
							"groups.#":                      {attrs["groups.#"], fmt.Sprint(len(peer.Groups))},
							"extra_dns_labels.#":            {attrs["extra_dns_labels.#"], fmt.Sprint(len(peer.ExtraDnsLabels))},
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

func Test_Peer_Update(t *testing.T) {
	testE2E(t)
	rName := "p" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_peer." + rName
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testPeerResource(rName, testPeerID(t, "peer4"), "meow"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
				),
			},
			{
				ResourceName: rName,
				Config:       testPeerResource(rName, testPeerID(t, "peer4"), rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "name", rName),
					func(s *terraform.State) error {
						pID := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						peer, err := testClient().Peers.Get(context.Background(), pID)
						if err != nil {
							return err
						}
						if peer.Name != rName {
							return fmt.Errorf("Peer name mismatch, expected %s, found %s on management server", rName, peer.Name)
						}
						return nil
					},
				),
			},
		},
	})
}

// Test_Peer_Delete asserts that destroying a netbird_peer deregisters the device.
//
// This FAILS on the current provider, deliberately: Peer.Delete skips the API
// call whenever TF_ACC is set in the environment, so under the acceptance suite
// the one path this test exists to cover is switched off. The peer survives the
// destroy and the assertion below reports it as still present.
//
// The switch is in shipped code rather than in a test, so it is not only a
// coverage gap: any user with TF_ACC in their environment gets a provider whose
// terraform destroy reports success while the peer stays registered. Removing
// that branch is what makes this test pass.
//
// It consumes peer5, one of the fixtures reserved for tests that manage a peer
// through its own lifecycle: deleting a peer deregisters the device, and the
// shared fixtures are addressed by name elsewhere in the suite.
func Test_Peer_Delete(t *testing.T) {
	testE2E(t)
	rName := "p" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_peer." + rName
	peerID := testPeerID(t, "peer5")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().Peers.Get, &peerID),
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testPeerResource(rName, peerID, rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "id", peerID),
					resource.TestCheckResourceAttr(rNameFull, "name", rName),
				),
			},
		},
	})
}

func testPeerResource(rName, id, name string) string {
	return fmt.Sprintf(`resource "netbird_peer" "%s" {
	id = "%s"
	name = "%s"
}`, rName, id, name)
}

// Test_Peer_IPv6 reads the peer's IPv6 overlay address, moves the peer to an
// unused address in the same /64, imports it, and moves it back. The account
// gives every peer an IPv6 address (ipv6_enabled_groups defaults to All), so a
// missing address fails the test rather than skipping it. It consumes peer7:
// the destroy at the end deregisters the device.
func Test_Peer_IPv6(t *testing.T) {
	testE2E(t)
	rName := "p" + acctest.RandStringFromCharSet(10, acctest.CharSetAlpha)
	rNameFull := "netbird_peer." + rName
	peerID := testPeerID(t, "peer7")
	peer, err := testClient().Peers.Get(context.Background(), peerID)
	if err != nil {
		t.Fatalf("read the peer: %v", err)
	}
	if peer.Ipv6 == nil || *peer.Ipv6 == "" {
		t.Fatal("the fixture peer has no IPv6 address; IPv6 should be enabled for All")
	}
	original := *peer.Ipv6
	moved := testUnusedPeerIPv6(t, original)
	config := func(ipv6 string) string {
		if ipv6 == "" {
			return fmt.Sprintf(`resource "netbird_peer" %q {
  id   = %q
  name = %q
}`, rName, peerID, peer.Name)
		}
		return fmt.Sprintf(`resource "netbird_peer" %q {
  id   = %q
  name = %q
  ipv6 = %q
}`, rName, peerID, peer.Name, ipv6)
	}
	onServer := func(want string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			p, err := testClient().Peers.Get(context.Background(), peerID)
			if err != nil {
				return err
			}
			if p.Ipv6 == nil || *p.Ipv6 != want {
				return fmt.Errorf("IPv6 on the server: expected %s, found %v", want, p.Ipv6)
			}
			return nil
		}
	}
	gone := peerID
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().Peers.Get, &gone),
		Steps: []resource.TestStep{
			{Config: config(""), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(rNameFull, "ipv6", original), onServer(original))},
			{Config: config(moved), ConfigPlanChecks: updatesInPlace(rNameFull), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(rNameFull, "ipv6", moved), onServer(moved))},
			// Moving the address makes the agent reconnect, so its connection
			// status and last-seen time move on between the apply and the import.
			{ResourceName: rNameFull, ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"connected", "last_seen"}},
			{Config: config(strings.ToUpper(moved)), ExpectError: regexp.MustCompile(`not in canonical form`)},
			{Config: config(original), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(rNameFull, "ipv6", original), onServer(original))},
		},
	})
}

// testUnusedPeerIPv6 returns an address in the /64 of addr that no peer of
// the account uses.
func testUnusedPeerIPv6(t *testing.T, addr string) string {
	t.Helper()
	peers, err := testClient().Peers.List(context.Background())
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	used := map[string]bool{}
	for _, p := range peers {
		if p.Ipv6 != nil {
			used[*p.Ipv6] = true
		}
	}
	base, err := netip.ParseAddr(addr)
	if err != nil {
		t.Fatalf("parse %s: %v", addr, err)
	}
	b := base.As16()
	for i := 0x42; i < 0x142; i++ {
		b[12], b[13], b[14], b[15] = 0xfe, 0xed, byte(i>>8), byte(i)
		if candidate := netip.AddrFrom16(b).String(); !used[candidate] {
			return candidate
		}
	}
	t.Fatal("no unused IPv6 address found")
	return ""
}
