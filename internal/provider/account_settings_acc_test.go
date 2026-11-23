//go:build e2e

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_Account_Create(t *testing.T) {
	env := testE2E(t)
	rName := "acc" + acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	rNameFull := "netbird_account_settings." + rName
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testAccountResource(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "id", env.AccountID),
					resource.TestCheckResourceAttr(rNameFull, "jwt_groups_enabled", "false"),
					func(s *terraform.State) error {
						accounts, err := testClient().Accounts.List(context.Background())
						if err != nil {
							return err
						}
						if len(accounts) == 0 {
							return fmt.Errorf("no accounts on the management server")
						}
						// By ID rather than by position: the assertion above is
						// about env.AccountID, so comparing settings from
						// whichever account happened to be listed first would
						// check a different account than the resource manages.
						idx := slices.IndexFunc(accounts, func(a api.Account) bool { return a.Id == env.AccountID })
						if idx < 0 {
							return fmt.Errorf("account %s is not among the %d accounts on the management server", env.AccountID, len(accounts))
						}
						settings := accounts[idx].Settings
						attrs := s.RootModule().Resources[rNameFull].Primary.Attributes
						return matchPairs(map[string][]any{
							"peer_login_expiration":              {attrs["peer_login_expiration"], fmt.Sprint(settings.PeerLoginExpiration)},
							"peer_inactivity_expiration":         {attrs["peer_inactivity_expiration"], fmt.Sprint(settings.PeerInactivityExpiration)},
							"peer_login_expiration_enabled":      {attrs["peer_login_expiration_enabled"], fmt.Sprint(settings.PeerLoginExpirationEnabled)},
							"peer_inactivity_expiration_enabled": {attrs["peer_inactivity_expiration_enabled"], fmt.Sprint(settings.PeerInactivityExpirationEnabled)},
							"regular_users_view_blocked":         {attrs["regular_users_view_blocked"], fmt.Sprint(settings.RegularUsersViewBlocked)},
							"groups_propagation_enabled":         {attrs["groups_propagation_enabled"], fmt.Sprint(valOr(settings.GroupsPropagationEnabled, false))},
							"jwt_groups_enabled":                 {attrs["jwt_groups_enabled"], fmt.Sprint(valOr(settings.JwtGroupsEnabled, false))},
							"routing_peer_dns_resolution_enabled": {attrs["routing_peer_dns_resolution_enabled"],
								fmt.Sprint(valOr(settings.RoutingPeerDnsResolutionEnabled, false))},
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

func Test_Account_Update(t *testing.T) {
	env := testE2E(t)
	rName := "acc" + acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	rNameFull := "netbird_account_settings." + rName
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				ResourceName: rName,
				Config:       testAccountResourceWithJWT(rName, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "id", env.AccountID),
					resource.TestCheckResourceAttr(rNameFull, "jwt_groups_enabled", "true"),
				),
				Destroy: false,
			},
			{
				ResourceName: rName,
				Config:       testAccountResourceWithJWT(rName, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "id", env.AccountID),
					resource.TestCheckResourceAttr(rNameFull, "jwt_groups_enabled", "false"),
				),
			},
		},
	})
}

// Test_Account_Update_KeepsUnmodeledSettings sets settings the resource does
// not model through the API, then applies a change to a modeled one. The
// server rebuilds the settings from the update request, so the apply keeps
// them only if the provider carries them into the request.
func Test_Account_Update_KeepsUnmodeledSettings(t *testing.T) {
	env := testE2E(t)
	rName := "acc" + acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	rNameFull := "netbird_account_settings." + rName
	original, err := testAccountSettings(env.AccountID)
	if err != nil {
		t.Fatalf("could not read the account's settings: %v", err)
	}
	t.Cleanup(func() {
		if err := setUnmodeledAccountSettings(env.AccountID, original.MetricsPushEnabled, original.AutoUpdateAlways); err != nil {
			t.Errorf("could not restore the unmodeled settings: %v", err)
		}
	})
	enabled := true
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccountResourceWithJWT(rName, true),
				Check:  resource.TestCheckResourceAttr(rNameFull, "jwt_groups_enabled", "true"),
			},
			{
				PreConfig: func() {
					if err := setUnmodeledAccountSettings(env.AccountID, &enabled, &enabled); err != nil {
						t.Fatalf("could not set the unmodeled settings: %v", err)
					}
				},
				Config:           testAccountResourceWithJWT(rName, false),
				ConfigPlanChecks: updatesInPlace(rNameFull),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "jwt_groups_enabled", "false"),
					func(s *terraform.State) error {
						settings, err := testAccountSettings(env.AccountID)
						if err != nil {
							return err
						}
						return matchPairs(map[string][]any{
							"jwt_groups_enabled":   {false, valOr(settings.JwtGroupsEnabled, true)},
							"metrics_push_enabled": {true, valOr(settings.MetricsPushEnabled, false)},
							"auto_update_always":   {true, valOr(settings.AutoUpdateAlways, false)},
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

// testAccountSettings reads the account's settings from the server, by ID.
func testAccountSettings(accountID string) (api.AccountSettings, error) {
	accounts, err := testClient().Accounts.List(context.Background())
	if err != nil {
		return api.AccountSettings{}, err
	}
	idx := slices.IndexFunc(accounts, func(a api.Account) bool { return a.Id == accountID })
	if idx < 0 {
		return api.AccountSettings{}, fmt.Errorf("account %s is not among the %d accounts on the management server", accountID, len(accounts))
	}
	return accounts[idx].Settings, nil
}

// setUnmodeledAccountSettings sets metrics_push_enabled and auto_update_always
// through the API, keeping every other setting (see setAccountSettingsRaw).
func setUnmodeledAccountSettings(accountID string, metricsPush, autoUpdateAlways *bool) error {
	return setAccountSettingsRaw(accountID, api.AccountSettings{MetricsPushEnabled: metricsPush, AutoUpdateAlways: autoUpdateAlways})
}

// setAccountSettingsRaw sets the fields of change that are not nil, through the
// same raw-JSON path the provider uses, so settings this client library does
// not know keep their values on the server.
func setAccountSettingsRaw(accountID string, change api.AccountSettings) error {
	client := testClient()
	accounts, err := listAccountsRaw(context.Background(), client)
	if err != nil {
		return err
	}
	current, err := selectAccount(accounts, accountID)
	if err != nil {
		return err
	}
	typed, err := json.Marshal(change)
	if err != nil {
		return err
	}
	var overlay map[string]json.RawMessage
	if err := json.Unmarshal(typed, &overlay); err != nil {
		return err
	}
	settings := maps.Clone(current.settings)
	for _, k := range readOnlySettings {
		delete(settings, k)
	}
	// Only the pointer fields set in change: the typed zero values of the
	// non-pointer fields must not overwrite the account's.
	for k, v := range overlay {
		if _, isPointer := pointerSettings[k]; isPointer {
			settings[k] = v
		}
	}
	body, err := json.Marshal(map[string]any{"settings": settings})
	if err != nil {
		return err
	}
	resp, err := client.NewRequest(context.Background(), http.MethodPut, "/api/accounts/"+accountID, bytes.NewReader(body), nil)
	if err != nil {
		return err
	}
	_, err = readBody(resp)
	return err
}

// pointerSettings are the optional settings setAccountSettingsRaw may change.
var pointerSettings = map[string]struct{}{
	"network_range_v6":                 {},
	"ipv6_enabled_groups":              {},
	"metrics_push_enabled":             {},
	"auto_update_always":               {},
	"peer_hostname_collision_rejected": {},
	"agent_network_only":               {},
	"dashboard_features":               {},
}

func testAccountResource(rName string) string {
	return fmt.Sprintf(`resource "netbird_account_settings" "%s" {}`, rName)
}

func Test_Account_LocalMFA(t *testing.T) {
	env := testE2E(t)
	ctx := context.Background()
	accounts, err := testClient().Accounts.List(ctx)
	if err != nil {
		t.Fatalf("list account settings: %v", err)
	}
	idx := slices.IndexFunc(accounts, func(a api.Account) bool { return a.Id == env.AccountID })
	if idx < 0 {
		t.Fatal("test account is missing")
	}
	initial := accounts[idx].Settings.LocalMfaEnabled
	if !valOr(accounts[idx].Settings.EmbeddedIdpEnabled, false) {
		t.Fatal("local MFA test requires the embedded identity provider")
	}
	t.Cleanup(func() {
		accounts, err := testClient().Accounts.List(ctx)
		if err != nil {
			t.Errorf("list account settings for cleanup: %v", err)
			return
		}
		idx := slices.IndexFunc(accounts, func(a api.Account) bool { return a.Id == env.AccountID })
		if idx < 0 {
			t.Error("test account is missing during cleanup")
			return
		}
		settings := accounts[idx].Settings
		settings.LocalMfaEnabled = initial
		if _, err := testClient().Accounts.Update(ctx, env.AccountID, api.AccountRequest{Settings: settings}); err != nil {
			t.Errorf("restore local MFA setting: %v", err)
		}
	})
	rName := "acc" + acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	rNameFull := "netbird_account_settings." + rName
	checkAPI := func(enabled bool) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			accounts, err := testClient().Accounts.List(ctx)
			if err != nil {
				return fmt.Errorf("list account settings: %w", err)
			}
			idx := slices.IndexFunc(accounts, func(a api.Account) bool { return a.Id == env.AccountID })
			if idx < 0 {
				return fmt.Errorf("test account is missing")
			}
			mfa := accounts[idx].Settings.LocalMfaEnabled
			if mfa == nil || *mfa != enabled {
				return fmt.Errorf("local MFA setting does not match %t", enabled)
			}
			return nil
		}
	}
	config := func(enabled bool) string {
		return fmt.Sprintf(`resource "netbird_account_settings" %q {
  local_mfa_enabled = %t
}
data "netbird_account_settings" "test" {
  depends_on = [netbird_account_settings.%s]
}`, rName, enabled, rName)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Account settings are a singleton; destroying the resource must not delete the account
		// or reset its settings. The cleanup above restores the shared fixture afterward.
		CheckDestroy: checkAPI(false),
		Steps: []resource.TestStep{
			{
				Config: config(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "local_mfa_enabled", "true"),
					resource.TestCheckResourceAttr("data.netbird_account_settings.test", "local_mfa_enabled", "true"),
					checkAPI(true),
				),
			},
			{ResourceName: rNameFull, ImportState: true, ImportStateVerify: true},
			{
				Config: testAccountResource(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "local_mfa_enabled", "true"),
					checkAPI(true),
				),
			},
			{
				Config: config(false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "local_mfa_enabled", "false"),
					resource.TestCheckResourceAttr("data.netbird_account_settings.test", "local_mfa_enabled", "false"),
					checkAPI(false),
				),
			},
		},
	})
}

func testAccountResourceWithJWT(rName string, enabled bool) string {
	return fmt.Sprintf(`resource "netbird_account_settings" "%s" {
jwt_groups_enabled = %v
}`, rName, enabled)
}

func Test_Account_IPv6(t *testing.T) {
	env := testE2E(t)
	rName := "acc" + acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	rNameFull := "netbird_account_settings." + rName
	original, err := testAccountSettings(env.AccountID)
	if err != nil {
		t.Fatalf("could not read the account's settings: %v", err)
	}
	t.Cleanup(func() {
		restore := api.AccountSettings{NetworkRangeV6: original.NetworkRangeV6, Ipv6EnabledGroups: original.Ipv6EnabledGroups}
		if err := setAccountSettingsRaw(env.AccountID, restore); err != nil {
			t.Errorf("could not restore the IPv6 settings: %v", err)
		}
	})
	const v6Range = "fd00:1234:5678::/64"
	config := func(groups string) string {
		return fmt.Sprintf(`resource "netbird_group" "v6" {
  name = %q
}
resource "netbird_account_settings" %q {
  network_range_v6    = %q
  ipv6_enabled_groups = %s
}
data "netbird_account_settings" "test" {
  depends_on = [netbird_account_settings.%s]
}`, rName+"-v6", rName, v6Range, groups, rName)
	}
	checkAPI := func(wantGroups int) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			settings, err := testAccountSettings(env.AccountID)
			if err != nil {
				return err
			}
			if settings.NetworkRangeV6 == nil || *settings.NetworkRangeV6 != v6Range {
				return fmt.Errorf("network_range_v6 on the server: expected %s, found %v", v6Range, settings.NetworkRangeV6)
			}
			if got := len(valOr(settings.Ipv6EnabledGroups, nil)); got != wantGroups {
				return fmt.Errorf("ipv6_enabled_groups on the server: expected %d groups, found %d", wantGroups, got)
			}
			return nil
		}
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("[netbird_group.v6.id]"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "network_range_v6", v6Range),
					resource.TestCheckResourceAttr(rNameFull, "ipv6_enabled_groups.#", "1"),
					resource.TestCheckResourceAttrPair(rNameFull, "ipv6_enabled_groups.0", "netbird_group.v6", "id"),
					resource.TestCheckResourceAttr("data.netbird_account_settings.test", "ipv6_enabled_groups.#", "1"),
					checkAPI(1),
				),
			},
			{ResourceName: rNameFull, ImportState: true, ImportStateVerify: true},
			{
				Config: config("[]"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "ipv6_enabled_groups.#", "0"),
					checkAPI(0),
				),
			},
		},
	})
}

func Test_Account_RemainingSettings(t *testing.T) {
	env := testE2E(t)
	rName := "acc" + acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	rNameFull := "netbird_account_settings." + rName
	// Restore these keys exactly as the server had them, absent ones included.
	keys := []string{"metrics_push_enabled", "auto_update_always", "peer_hostname_collision_rejected", "agent_network_only", "dashboard_features"}
	snapshot, err := snapshotAccountSettingsRaw(env.AccountID)
	if err != nil {
		t.Fatalf("could not read the account's settings: %v", err)
	}
	t.Cleanup(func() {
		if err := restoreAccountSettingsRaw(env.AccountID, snapshot, keys); err != nil {
			t.Errorf("could not restore the account settings: %v", err)
		}
	})
	config := func(on, only, menu bool) string {
		return fmt.Sprintf(`resource "netbird_account_settings" %q {
  metrics_push_enabled             = %t
  auto_update_always               = %t
  peer_hostname_collision_rejected = %t
  agent_network_only               = %t
  dashboard_features = {
    agent_network = %t
  }
}
data "netbird_account_settings" "test" {
  depends_on = [netbird_account_settings.%s]
}`, rName, on, on, on, only, menu, rName)
	}
	checkAPI := func(on, only, menu bool) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			settings, err := testAccountSettings(env.AccountID)
			if err != nil {
				return err
			}
			var gotMenu any
			if settings.DashboardFeatures != nil {
				gotMenu = valOr(settings.DashboardFeatures.AgentNetwork, !menu)
			}
			return matchPairs(map[string][]any{
				"metrics_push_enabled":             {on, valOr(settings.MetricsPushEnabled, !on)},
				"auto_update_always":               {on, valOr(settings.AutoUpdateAlways, !on)},
				"peer_hostname_collision_rejected": {on, valOr(settings.PeerHostnameCollisionRejected, !on)},
				"agent_network_only":               {only, valOr(settings.AgentNetworkOnly, !only)},
				"dashboard_features.agent_network": {menu, gotMenu},
			})
		}
	}
	checkState := func(on, only, menu bool) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(rNameFull, "metrics_push_enabled", fmt.Sprint(on)),
			resource.TestCheckResourceAttr(rNameFull, "auto_update_always", fmt.Sprint(on)),
			resource.TestCheckResourceAttr(rNameFull, "peer_hostname_collision_rejected", fmt.Sprint(on)),
			resource.TestCheckResourceAttr(rNameFull, "agent_network_only", fmt.Sprint(only)),
			resource.TestCheckResourceAttr(rNameFull, "dashboard_features.agent_network", fmt.Sprint(menu)),
			resource.TestCheckResourceAttr("data.netbird_account_settings.test", "metrics_push_enabled", fmt.Sprint(on)),
			resource.TestCheckResourceAttr("data.netbird_account_settings.test", "dashboard_features.agent_network", fmt.Sprint(menu)),
			checkAPI(on, only, menu),
		)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config(true, false, true), Check: checkState(true, false, true)},
			{ResourceName: rNameFull, ImportState: true, ImportStateVerify: true},
			{Config: config(false, false, false), ConfigPlanChecks: updatesInPlace(rNameFull), Check: checkState(false, false, false)},
			{Config: config(false, true, true), Check: checkState(false, true, true)},
			{Config: config(false, true, false), ExpectError: regexp.MustCompile(`Agent Network menu required`)},
			{Config: config(false, false, true), Check: checkState(false, false, true)},
		},
	})
}

// snapshotAccountSettingsRaw returns the account's settings as the server sent them.
func snapshotAccountSettingsRaw(accountID string) (map[string]json.RawMessage, error) {
	accounts, err := listAccountsRaw(context.Background(), testClient())
	if err != nil {
		return nil, err
	}
	current, err := selectAccount(accounts, accountID)
	if err != nil {
		return nil, err
	}
	return maps.Clone(current.settings), nil
}

// restoreAccountSettingsRaw puts keys back as they are in snapshot: present
// keys with their raw value, absent keys removed from the request, which the
// server reads as unset.
func restoreAccountSettingsRaw(accountID string, snapshot map[string]json.RawMessage, keys []string) error {
	client := testClient()
	current, err := snapshotAccountSettingsRaw(accountID)
	if err != nil {
		return err
	}
	for _, k := range readOnlySettings {
		delete(current, k)
	}
	for _, k := range keys {
		if v, ok := snapshot[k]; ok {
			current[k] = v
		} else {
			delete(current, k)
		}
	}
	body, err := json.Marshal(map[string]any{"settings": current})
	if err != nil {
		return err
	}
	resp, err := client.NewRequest(context.Background(), http.MethodPut, "/api/accounts/"+accountID, bytes.NewReader(body), nil)
	if err != nil {
		return err
	}
	_, err = readBody(resp)
	return err
}
