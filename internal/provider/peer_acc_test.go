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
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/netbirdio/netbird/shared/management/http/api"
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
				// The agent keeps reporting in, so last_seen can move on
				// between the apply and the import.
				ImportStateVerifyIgnore: []string{"last_seen"},
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
	movedAgain := testUnusedPeerIPv6(t, original, moved)
	outside := testOutsideAccountIPv6(t)
	named := func(name, ipv6 string) string {
		if ipv6 == "" {
			return fmt.Sprintf(`resource "netbird_peer" %q {
  id   = %q
  name = %q
}`, rName, peerID, name)
		}
		return fmt.Sprintf(`resource "netbird_peer" %q {
  id   = %q
  name = %q
  ipv6 = %q
}`, rName, peerID, name, ipv6)
	}
	config := func(ipv6 string) string { return named(peer.Name, ipv6) }
	// Checked in the next step's PreConfig, before its apply could repair a
	// partial write.
	onServerBefore := func(name, ipv6 string) func() {
		return func() {
			p, err := testClient().Peers.Get(context.Background(), peerID)
			if err != nil {
				t.Fatal(err)
			}
			if p.Name != name || p.Ipv6 == nil || *p.Ipv6 != ipv6 {
				t.Fatalf("on the server: expected %q at %s, found %q at %v", name, ipv6, p.Name, p.Ipv6)
			}
		}
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
			// A rename together with an address the server refuses: the address
			// goes first and alone, so the refusal leaves the name as it was.
			{Config: named(peer.Name+"-x", outside), ExpectError: regexp.MustCompile(`not within the account IPv6 range`)},
			// Renamed outside Terraform between plan and apply, with an address
			// move pending: the apply stops before writing anything, and the
			// outside change survives.
			{PreConfig: onServerBefore(peer.Name, moved), Config: named(peer.Name+"-s", movedAgain),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					testExpectUpdateInPlace(rNameFull), testPeerRenamedOutside(peerID, peer.Name+"-o")}},
				ExpectError: regexp.MustCompile(`Peer changed after the plan`)},
			{PreConfig: onServerBefore(peer.Name+"-o", moved), Config: config(original), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(rNameFull, "ipv6", original), onServer(original))},
		},
	})
}

// testOutsideAccountIPv6 returns a canonical IPv6 address just outside the
// account's IPv6 range.
func testOutsideAccountIPv6(t *testing.T) string {
	t.Helper()
	accounts, err := testClient().Accounts.List(context.Background())
	if err != nil || len(accounts) != 1 || accounts[0].Settings.NetworkRangeV6 == nil {
		t.Fatalf("read the account IPv6 range: %v (%d accounts)", err, len(accounts))
	}
	prefix, err := netip.ParsePrefix(*accounts[0].Settings.NetworkRangeV6)
	if err != nil || prefix.Bits() == 0 {
		t.Fatalf("parse the account IPv6 range %q: %v", *accounts[0].Settings.NetworkRangeV6, err)
	}
	b := prefix.Masked().Addr().As16()
	bit := prefix.Bits() - 1 // the last bit of the prefix: flipped, the address is outside
	b[bit/8] ^= 0x80 >> (bit % 8)
	b[15] = 1
	return netip.AddrFrom16(b).String()
}

// testPeerRenamedOutside is a plan check that renames the peer through the API
// after the plan and before the apply, the way someone working outside
// Terraform would.
func testPeerRenamedOutside(peerID, name string) plancheck.PlanCheck {
	return peerRenamedOutside{peerID: peerID, name: name}
}

type peerRenamedOutside struct{ peerID, name string }

func (c peerRenamedOutside) CheckPlan(ctx context.Context, _ plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	p, err := testClient().Peers.Get(ctx, c.peerID)
	if err != nil {
		resp.Error = err
		return
	}
	approval := p.ApprovalRequired
	_, resp.Error = testClient().Peers.Update(ctx, c.peerID, api.PeerRequest{
		Name: c.name, SshEnabled: p.SshEnabled, ApprovalRequired: &approval,
		LoginExpirationEnabled: p.LoginExpirationEnabled, InactivityExpirationEnabled: p.InactivityExpirationEnabled,
	})
}

// testUnusedPeerIPv6 returns an address in the /64 of addr that no peer of
// the account uses and that is not one of except.
func testUnusedPeerIPv6(t *testing.T, addr string, except ...string) string {
	t.Helper()
	peers, err := testClient().Peers.List(context.Background())
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	used := map[string]bool{}
	for _, e := range except {
		used[e] = true
	}
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
