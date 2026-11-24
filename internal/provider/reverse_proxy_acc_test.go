//go:build e2e

package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_ReverseProxyClusters_DataSource(t *testing.T) {
	// A cluster only appears once a proxy has connected and is heartbeating, so
	// take one before asserting on the listing: without it the count is zero and
	// the assertion holds without proving anything.
	cluster := testRequireProxyCluster(t)
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testEnsureManagementRunning(t)
			// testRequireProxyCluster has already listed clusters and found an
			// online one, so the endpoint is known to work by the time this runs;
			// an error here is a real failure rather than an absent feature.
			if _, err := testClient().ReverseProxyClusters.List(context.Background()); err != nil {
				t.Fatalf("listing reverse proxy clusters: %v", err)
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "netbird_reverse_proxy_clusters" "all" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.netbird_reverse_proxy_clusters.all", "clusters.#"),
					resource.TestCheckTypeSetElemNestedAttrs("data.netbird_reverse_proxy_clusters.all", "clusters.*", map[string]string{
						"address":           cluster.Address,
						"connected_proxies": "1",
					}),
				),
			},
		},
	})
}

func Test_ReverseProxyDomain_DataSource(t *testing.T) {
	// Free domains are not stored records: management derives one from every
	// online proxy cluster. So there is a free domain to look up only once a
	// proxy is connected, and its name is that cluster's address.
	cluster := testRequireProxyCluster(t)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "netbird_reverse_proxy_domain" "free" {
  type = "free"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.netbird_reverse_proxy_domain.free", "type", "free"),
					resource.TestCheckResourceAttr("data.netbird_reverse_proxy_domain.free", "validated", "true"),
					resource.TestCheckResourceAttr("data.netbird_reverse_proxy_domain.free", "domain", cluster.Address),
				),
			},
		},
	})
}

func Test_ReverseProxyDomain_CRUD(t *testing.T) {
	testE2E(t)
	cluster := testRequireProxyCluster(t)
	rName := "d" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domainName := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_domain." + rName

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			domains, err := testClient().ReverseProxyDomains.List(context.Background())
			if err != nil {
				return err
			}
			for _, d := range domains {
				if d.Domain == domainName {
					return fmt.Errorf("domain %s still exists after destroy", domainName)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: testReverseProxyDomainResource(rName, domainName, cluster.Address),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "domain", domainName),
					resource.TestCheckResourceAttr(rNameFull, "target_cluster", cluster.Address),
					resource.TestCheckResourceAttr(rNameFull, "type", "custom"),
					func(s *terraform.State) error {
						domains, err := testClient().ReverseProxyDomains.List(context.Background())
						if err != nil {
							return err
						}
						id := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						for _, d := range domains {
							if d.Id == id {
								if d.Domain != domainName {
									return fmt.Errorf("domain mismatch: expected %s, got %s", domainName, d.Domain)
								}
								return nil
							}
						}
						return fmt.Errorf("domain %s not found in API", id)
					},
				),
			},
			{
				ResourceName:      rNameFull,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: testReverseProxyDomainImportID(rNameFull),
			},
		},
	})
}

func Test_ReverseProxyService_DataSource(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	dsNameFull := "data.netbird_reverse_proxy_service.lookup"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Create a service, then look it up by name via the data source
				Config: testReverseProxyServiceWithDataSource(rName, domain, testPeerID(t, "peer1")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(dsNameFull, "name", rName),
					resource.TestCheckResourceAttr(dsNameFull, "domain", domain),
					resource.TestCheckResourceAttr(dsNameFull, "enabled", "true"),
					resource.TestCheckResourceAttr(dsNameFull, "targets.#", "1"),
					resource.TestCheckResourceAttr(dsNameFull, "targets.0.port", "8080"),
					resource.TestCheckResourceAttr(dsNameFull, "targets.0.protocol", "http"),
					resource.TestCheckResourceAttrSet(dsNameFull, "id"),
					resource.TestCheckResourceAttrSet(dsNameFull, "auth.%"),
				),
			},
		},
	})
}

func Test_ReverseProxyService_PasswordAuth(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	peerID := testPeerID(t, "peer1")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			services, err := testClient().ReverseProxyServices.List(context.Background())
			if err != nil {
				return err
			}
			for _, svc := range services {
				if svc.Name == rName {
					return fmt.Errorf("service %s still exists", rName)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: testReverseProxyServicePasswordAuth(rName, domain, peerID, "secret123"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "name", rName),
					resource.TestCheckResourceAttr(rNameFull, "domain", domain),
					resource.TestCheckResourceAttr(rNameFull, "enabled", "true"),
					resource.TestCheckResourceAttr(rNameFull, "targets.#", "1"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.target_id", peerID),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.target_type", "peer"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.port", "8080"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.protocol", "http"),
					resource.TestCheckResourceAttr(rNameFull, "auth.password_auth.enabled", "true"),
					resource.TestCheckResourceAttr(rNameFull, "auth.password_auth.password", "secret123"),
					resource.TestCheckResourceAttrSet(rNameFull, "proxy_cluster"),
					func(s *terraform.State) error {
						id := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						svc, err := testClient().ReverseProxyServices.Get(context.Background(), id)
						if err != nil {
							return fmt.Errorf("get service: %w", err)
						}
						if svc.Name != rName {
							return fmt.Errorf("name mismatch: expected %s, got %s", rName, svc.Name)
						}
						if !svc.Enabled {
							return fmt.Errorf("expected service to be enabled")
						}
						if len(svc.Targets) != 1 {
							return fmt.Errorf("expected 1 target, got %d", len(svc.Targets))
						}
						if svc.Targets[0].TargetId != peerID {
							return fmt.Errorf("target_id mismatch")
						}
						if svc.Auth.PasswordAuth == nil || !svc.Auth.PasswordAuth.Enabled {
							return fmt.Errorf("expected password auth to be enabled")
						}
						return nil
					},
				),
			},
			{
				Config: testReverseProxyServicePasswordAuthUpdated(rName, domain, peerID, "newsecret456"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "enabled", "false"),
					resource.TestCheckResourceAttr(rNameFull, "auth.password_auth.password", "newsecret456"),
					resource.TestCheckResourceAttr(rNameFull, "pass_host_header", "true"),
					resource.TestCheckResourceAttr(rNameFull, "rewrite_redirects", "true"),
				),
			},
			{
				ResourceName:            rNameFull,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"auth.password_auth.password"},
			},
		},
	})
}

func Test_ReverseProxyService_PinAuth(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	var createdID string
	peerID := testPeerID(t, "peer1")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().ReverseProxyServices.Get, &createdID),
		Steps: []resource.TestStep{
			{
				Config: testReverseProxyServicePinAuth(rName, domain, peerID, "9876"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "auth.pin_auth.enabled", "true"),
					resource.TestCheckResourceAttr(rNameFull, "auth.pin_auth.pin", "9876"),
					func(s *terraform.State) error {
						id := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						svc, err := testClient().ReverseProxyServices.Get(context.Background(), id)
						if err != nil {
							return fmt.Errorf("get service: %w", err)
						}
						if svc.Auth.PinAuth == nil || !svc.Auth.PinAuth.Enabled {
							return fmt.Errorf("expected pin auth to be enabled")
						}
						return nil
					},
				),
			},
		},
	})
}

func Test_ReverseProxyService_BearerAuth(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	var createdID string
	peerID := testPeerID(t, "peer1")
	groupID := e2eGroupAllID()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().ReverseProxyServices.Get, &createdID),
		Steps: []resource.TestStep{
			{
				Config: testReverseProxyServiceBearerAuth(rName, domain, peerID, groupID),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "auth.bearer_auth.enabled", "true"),
					resource.TestCheckResourceAttr(rNameFull, "auth.bearer_auth.distribution_groups.#", "1"),
					resource.TestCheckResourceAttr(rNameFull, "auth.bearer_auth.distribution_groups.0", groupID),
					func(s *terraform.State) error {
						id := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						svc, err := testClient().ReverseProxyServices.Get(context.Background(), id)
						if err != nil {
							return fmt.Errorf("get service: %w", err)
						}
						if svc.Auth.BearerAuth == nil || !svc.Auth.BearerAuth.Enabled {
							return fmt.Errorf("expected bearer auth to be enabled")
						}
						if svc.Auth.BearerAuth.DistributionGroups == nil || len(*svc.Auth.BearerAuth.DistributionGroups) != 1 {
							return fmt.Errorf("expected 1 distribution group")
						}
						return nil
					},
				),
			},
		},
	})
}

func Test_ReverseProxyService_MultipleTargets(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	var createdID string
	peerID1 := testPeerID(t, "peer1")
	peerID2 := testPeerID(t, "peer2")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().ReverseProxyServices.Get, &createdID),
		Steps: []resource.TestStep{
			{
				Config: testReverseProxyServiceMultiTarget(rName, domain, peerID1, peerID2),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "targets.#", "2"),
					func(s *terraform.State) error {
						id := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						svc, err := testClient().ReverseProxyServices.Get(context.Background(), id)
						if err != nil {
							return fmt.Errorf("get service: %w", err)
						}
						if len(svc.Targets) != 2 {
							return fmt.Errorf("expected 2 targets, got %d", len(svc.Targets))
						}
						return nil
					},
				),
			},
		},
	})
}

func Test_AccountSettings_PeerExpose(t *testing.T) {
	testE2E(t)
	groupID := e2eGroupAllID()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccountSettingsPeerExpose(true, groupID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("netbird_account_settings.test", "peer_expose_enabled", "true"),
					resource.TestCheckResourceAttr("netbird_account_settings.test", "peer_expose_groups.#", "1"),
					resource.TestCheckResourceAttr("netbird_account_settings.test", "peer_expose_groups.0", groupID),
				),
			},
			{
				Config: testAccountSettingsPeerExpose(false, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("netbird_account_settings.test", "peer_expose_enabled", "false"),
					resource.TestCheckResourceAttr("netbird_account_settings.test", "peer_expose_groups.#", "0"),
				),
			},
		},
	})
}

// --- Config helpers ---

func testReverseProxyDomainResource(rName, domain, cluster string) string {
	return fmt.Sprintf(`resource "netbird_reverse_proxy_domain" "%s" {
  domain         = %q
  target_cluster = %q
}`, rName, domain, cluster)
}

func testReverseProxyDomainImportID(rNameFull string) func(*terraform.State) (string, error) {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[rNameFull]
		if !ok {
			return "", fmt.Errorf("resource not found: %s", rNameFull)
		}
		return rs.Primary.Attributes["id"], nil
	}
}

func testReverseProxyServicePasswordAuth(rName, domain, peerID, password string) string {
	return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "%s" {
  name    = %q
  domain  = %q
  enabled = true

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 8080
    protocol    = "http"
  }]

  auth = {
    password_auth = {
      enabled  = true
      password = %q
    }
  }
}`, rName, rName, domain, peerID, password)
}

func testReverseProxyServicePasswordAuthUpdated(rName, domain, peerID, password string) string {
	return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "%s" {
  name              = %q
  domain            = %q
  enabled           = false
  pass_host_header  = true
  rewrite_redirects = true

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 8080
    protocol    = "http"
  }]

  auth = {
    password_auth = {
      enabled  = true
      password = %q
    }
  }
}`, rName, rName, domain, peerID, password)
}

func testReverseProxyServicePinAuth(rName, domain, peerID, pin string) string {
	return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "%s" {
  name   = %q
  domain = %q

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 80
    protocol    = "http"
  }]

  auth = {
    pin_auth = {
      enabled = true
      pin     = %q
    }
  }
}`, rName, rName, domain, peerID, pin)
}

func testReverseProxyServiceBearerAuth(rName, domain, peerID, groupID string) string {
	return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "%s" {
  name   = %q
  domain = %q

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 8080
    protocol    = "http"
  }]

  auth = {
    bearer_auth = {
      enabled             = true
      distribution_groups = [%q]
    }
  }
}`, rName, rName, domain, peerID, groupID)
}

func testReverseProxyServiceMultiTarget(rName, domain, peerID1, peerID2 string) string {
	return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "%s" {
  name   = %q
  domain = %q

  targets = [
    {
      target_id   = %q
      target_type = "peer"
      port        = 8080
      protocol    = "http"
    },
    {
      target_id   = %q
      target_type = "peer"
      port        = 9090
      protocol    = "http"
    },
  ]

  auth = {
    password_auth = {
      enabled  = true
      password = "multitest"
    }
  }
}`, rName, rName, domain, peerID1, peerID2)
}

func testReverseProxyServiceWithDataSource(rName, domain, peerID string) string {
	return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "test" {
  name   = %q
  domain = %q

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 8080
    protocol    = "http"
  }]

  auth = {
    link_auth = {
      enabled = true
    }
  }
}

data "netbird_reverse_proxy_service" "lookup" {
  name = netbird_reverse_proxy_service.test.name
}`, rName, domain, peerID)
}

func Test_ReverseProxyService_TargetOptions(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	peerID := testPeerID(t, "peer1")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testReverseProxyServiceDestroyed(rName),
		Steps: []resource.TestStep{
			{
				Config: testReverseProxyServiceTargetOptions(rName, domain, peerID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "targets.#", "1"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.protocol", "https"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.options.skip_tls_verify", "true"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.options.request_timeout", "30s"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.options.path_rewrite", "preserve"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.options.custom_headers.X-Custom", "test-value"),
					func(s *terraform.State) error {
						id := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						svc, err := testClient().ReverseProxyServices.Get(context.Background(), id)
						if err != nil {
							return fmt.Errorf("get service: %w", err)
						}
						if len(svc.Targets) != 1 {
							return fmt.Errorf("expected 1 target, got %d", len(svc.Targets))
						}
						opts := svc.Targets[0].Options
						if opts == nil {
							return fmt.Errorf("expected target options to be set")
						}
						if opts.SkipTlsVerify == nil || !*opts.SkipTlsVerify {
							return fmt.Errorf("expected skip_tls_verify to be true")
						}
						if opts.RequestTimeout == nil || *opts.RequestTimeout != "30s" {
							return fmt.Errorf("expected request_timeout to be 30s")
						}
						return nil
					},
				),
			},
			{
				ResourceName:            rNameFull,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"auth.password_auth.password"},
			},
		},
	})
}

func testReverseProxyServiceTargetOptions(rName, domain, peerID string) string {
	return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "%s" {
  name   = %q
  domain = %q

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 8443
    protocol    = "https"

    options = {
      skip_tls_verify = true
      request_timeout = "30s"
      path_rewrite    = "preserve"
      custom_headers = {
        "X-Custom" = "test-value"
      }
    }
  }]

  auth = {
    password_auth = {
      enabled  = true
      password = "options-test"
    }
  }
}`, rName, rName, domain, peerID)
}

func Test_ReverseProxyService_TCPMode(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	peerID := testPeerID(t, "peer1")

	// The update step's configuration leaves mode out.
	withoutMode := strings.Replace(testReverseProxyServiceL4(rName, domain, peerID, "tcp", 15432, `
    options = {
      proxy_protocol  = true
      request_timeout = "2m0s"
    }`), "  mode        = \"tcp\"\n", "", 1)
	if strings.Contains(withoutMode, "mode ") {
		t.Fatal("the configuration without mode still sets it")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testReverseProxyServiceDestroyed(rName),
		Steps: []resource.TestStep{
			{
				// Durations use the canonical Go form ("1m0s", not "60s") so the
				// server's round-trip matches the config and import verification.
				Config: testReverseProxyServiceL4(rName, domain, peerID, "tcp", 15432, `
    options = {
      proxy_protocol  = true
      request_timeout = "1m0s"
    }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "mode", "tcp"),
					resource.TestCheckResourceAttr(rNameFull, "listen_port", "15432"),
					resource.TestCheckResourceAttr(rNameFull, "port_auto_assigned", "false"),
					resource.TestCheckResourceAttr(rNameFull, "targets.#", "1"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.protocol", "tcp"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.options.proxy_protocol", "true"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.options.request_timeout", "1m0s"),
					func(s *terraform.State) error {
						id := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						svc, err := testClient().ReverseProxyServices.Get(context.Background(), id)
						if err != nil {
							return fmt.Errorf("get service: %w", err)
						}
						if svc.Mode == nil || *svc.Mode != "tcp" {
							return fmt.Errorf("expected mode tcp, got %v", svc.Mode)
						}
						if svc.ListenPort == nil || *svc.ListenPort != 15432 {
							return fmt.Errorf("expected listen_port 15432, got %v", svc.ListenPort)
						}
						if len(svc.Targets) != 1 {
							return fmt.Errorf("expected 1 target, got %d", len(svc.Targets))
						}
						opts := svc.Targets[0].Options
						if opts == nil || opts.ProxyProtocol == nil || !*opts.ProxyProtocol {
							return fmt.Errorf("expected proxy_protocol to be true")
						}
						return nil
					},
				),
			},
			{
				ResourceName:      rNameFull,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// The server does not change a service's mode: the plan
				// passes with a warning (a replacement would be fine), and the
				// update stops before writing.
				Config: testReverseProxyServiceL4(rName, domain, peerID, "udp", 15432, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(rNameFull, plancheck.ResourceActionUpdate),
				}},
				ExpectError: regexp.MustCompile(`(?s)Error running apply.*Mode cannot be changed`),
			},
			{
				// With mode left out, an update keeps (and sends) the TCP
				// mode; a request without one would be refused as a change
				// to HTTP.
				Config: withoutMode,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(rNameFull, plancheck.ResourceActionUpdate),
					plancheck.ExpectKnownValue(rNameFull, tfjsonpath.New("mode"), knownvalue.StringExact("tcp")),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "mode", "tcp"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.options.request_timeout", "2m0s"),
					func(s *terraform.State) error {
						svc, err := testClient().ReverseProxyServices.Get(context.Background(), s.RootModule().Resources[rNameFull].Primary.Attributes["id"])
						if err != nil {
							return err
						}
						if svc.Mode == nil || *svc.Mode != "tcp" {
							return fmt.Errorf("expected mode tcp on the server, found %v", svc.Mode)
						}
						if len(svc.Targets) != 1 || svc.Targets[0].Options == nil || svc.Targets[0].Options.RequestTimeout == nil ||
							*svc.Targets[0].Options.RequestTimeout != "2m0s" {
							return fmt.Errorf("expected request_timeout 2m0s on the server, found %+v", svc.Targets)
						}
						return nil
					},
				),
			},
		},
	})
}

func Test_ReverseProxyService_UDPMode(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	peerID := testPeerID(t, "peer1")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testReverseProxyServiceDestroyed(rName),
		Steps: []resource.TestStep{
			{
				Config: testReverseProxyServiceL4(rName, domain, peerID, "udp", 19053, `
    options = {
      session_idle_timeout = "2m0s"
    }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "mode", "udp"),
					resource.TestCheckResourceAttr(rNameFull, "listen_port", "19053"),
					resource.TestCheckResourceAttr(rNameFull, "targets.#", "1"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.protocol", "udp"),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.options.session_idle_timeout", "2m0s"),
					func(s *terraform.State) error {
						id := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						svc, err := testClient().ReverseProxyServices.Get(context.Background(), id)
						if err != nil {
							return fmt.Errorf("get service: %w", err)
						}
						if svc.Mode == nil || *svc.Mode != "udp" {
							return fmt.Errorf("expected mode udp, got %v", svc.Mode)
						}
						if len(svc.Targets) != 1 {
							return fmt.Errorf("expected 1 target, got %d", len(svc.Targets))
						}
						opts := svc.Targets[0].Options
						if opts == nil || opts.SessionIdleTimeout == nil || *opts.SessionIdleTimeout != "2m0s" {
							return fmt.Errorf("expected session_idle_timeout 2m0s, got %v", opts)
						}
						return nil
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

func testReverseProxyServiceDestroyed(name string) func(*terraform.State) error {
	return func(s *terraform.State) error {
		services, err := testClient().ReverseProxyServices.List(context.Background())
		if err != nil {
			return err
		}
		for _, svc := range services {
			if svc.Name == name {
				return fmt.Errorf("service %s still exists", name)
			}
		}
		return nil
	}
}

func testReverseProxyServiceL4(rName, domain, peerID, mode string, listenPort int, options string) string {
	return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "%s" {
  name        = %q
  domain      = %q
  mode        = %q
  listen_port = %d

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 5001
    protocol    = %q
%s
  }]

  auth = {}
}`, rName, rName, domain, mode, listenPort, peerID, mode, options)
}

func Test_ReverseProxyService_HeaderAuth(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	peerID := testPeerID(t, "peer1")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testReverseProxyServiceDestroyed(rName),
		Steps: []resource.TestStep{
			{
				Config: testReverseProxyServiceHeaderAuth(rName, domain, peerID, "X-API-Key", "my-secret-key"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "auth.header_auths.#", "1"),
					resource.TestCheckResourceAttr(rNameFull, "auth.header_auths.0.enabled", "true"),
					resource.TestCheckResourceAttr(rNameFull, "auth.header_auths.0.header", "X-API-Key"),
					resource.TestCheckResourceAttr(rNameFull, "auth.header_auths.0.value", "my-secret-key"),
					func(s *terraform.State) error {
						id := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						svc, err := testClient().ReverseProxyServices.Get(context.Background(), id)
						if err != nil {
							return fmt.Errorf("get service: %w", err)
						}
						if svc.Auth.HeaderAuths == nil || len(*svc.Auth.HeaderAuths) != 1 {
							return fmt.Errorf("expected 1 header auth")
						}
						h := (*svc.Auth.HeaderAuths)[0]
						if !h.Enabled || h.Header != "X-API-Key" {
							return fmt.Errorf("header auth mismatch")
						}
						return nil
					},
				),
			},
			{
				ResourceName:            rNameFull,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"auth.header_auths.0.value"},
			},
		},
	})
}

func Test_ReverseProxyService_AccessRestrictions(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	peerID := testPeerID(t, "peer1")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testReverseProxyServiceDestroyed(rName),
		Steps: []resource.TestStep{
			{
				Config: testReverseProxyServiceAccessRestrictions(rName, domain, peerID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rNameFull, "id"),
					resource.TestCheckResourceAttr(rNameFull, "access_restrictions.allowed_countries.#", "2"),
					resource.TestCheckResourceAttr(rNameFull, "access_restrictions.allowed_countries.0", "DE"),
					resource.TestCheckResourceAttr(rNameFull, "access_restrictions.allowed_countries.1", "US"),
					resource.TestCheckResourceAttr(rNameFull, "access_restrictions.blocked_cidrs.#", "1"),
					resource.TestCheckResourceAttr(rNameFull, "access_restrictions.blocked_cidrs.0", "192.168.0.0/16"),
					func(s *terraform.State) error {
						id := s.RootModule().Resources[rNameFull].Primary.Attributes["id"]
						svc, err := testClient().ReverseProxyServices.Get(context.Background(), id)
						if err != nil {
							return fmt.Errorf("get service: %w", err)
						}
						if svc.AccessRestrictions == nil {
							return fmt.Errorf("expected access restrictions")
						}
						if svc.AccessRestrictions.AllowedCountries == nil || len(*svc.AccessRestrictions.AllowedCountries) != 2 {
							return fmt.Errorf("expected 2 allowed countries")
						}
						if svc.AccessRestrictions.BlockedCidrs == nil || len(*svc.AccessRestrictions.BlockedCidrs) != 1 {
							return fmt.Errorf("expected 1 blocked CIDR")
						}
						return nil
					},
				),
			},
			{
				ResourceName:            rNameFull,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"auth.password_auth.password"},
			},
		},
	})
}

func testReverseProxyServiceHeaderAuth(rName, domain, peerID, header, value string) string {
	return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "%s" {
  name   = %q
  domain = %q

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 8080
    protocol    = "http"
  }]

  auth = {
    header_auths = [{
      enabled = true
      header  = %q
      value   = %q
    }]
  }
}`, rName, rName, domain, peerID, header, value)
}

func testReverseProxyServiceAccessRestrictions(rName, domain, peerID string) string {
	return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "%s" {
  name   = %q
  domain = %q

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 8080
    protocol    = "http"
  }]

  auth = {
    password_auth = {
      enabled  = true
      password = "test123"
    }
  }

  access_restrictions = {
    allowed_countries = ["DE", "US"]
    blocked_cidrs     = ["192.168.0.0/16"]
  }
}`, rName, rName, domain, peerID)
}

func testAccountSettingsPeerExpose(enabled bool, groupID string) string {
	groups := "[]"
	if groupID != "" {
		groups = fmt.Sprintf("[%q]", groupID)
	}
	return fmt.Sprintf(`resource "netbird_account_settings" "test" {
  peer_expose_enabled = %t
  peer_expose_groups  = %s
}`, enabled, groups)
}

// Test_ReverseProxyService_Private creates a NetBird-only service for a group,
// checks the server and the data source, imports it, expects the plan-time
// errors of the private-service contract, stops an update whose access
// settings changed on the server after the plan, and makes the service public
// again.
func Test_ReverseProxyService_Private(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	dsName := "data.netbird_reverse_proxy_service.test"
	var createdID string
	peerID := testPeerID(t, "peer1")
	groupID := e2eGroupAllID()
	type svc struct {
		private string // HCL value; empty leaves the attribute out
		groups  string // HCL value; empty leaves the attribute out
		bearer  bool
		port    int
		extra   string
	}
	config := func(c svc) string {
		if c.port == 0 {
			c.port = 8080
		}
		lines := ""
		if c.private != "" {
			lines += "\n  private       = " + c.private
		}
		if c.groups != "" {
			lines += "\n  access_groups = " + c.groups
		}
		auth := `
  auth = {}`
		if c.bearer {
			auth = fmt.Sprintf(`
  auth = {
    bearer_auth = {
      enabled             = true
      distribution_groups = [%q]
    }
  }`, groupID)
		}
		return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" %q {
  name          = %q
  domain        = %q
  mode          = "http"%s

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = %d
    protocol    = "http"
  }]%s
}

data "netbird_reverse_proxy_service" "test" {
  id = netbird_reverse_proxy_service.%s.id
}
%s`, rName, rName, domain, lines, peerID, c.port, auth, rName, c.extra)
	}
	group := fmt.Sprintf("[%q]", groupID)
	otherGroupID := e2eGroupNotAllID()
	twoGroups := fmt.Sprintf("[%q, %q]", groupID, otherGroupID)
	onServer := func(private bool, groups []string, port int) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			svc, err := testClient().ReverseProxyServices.Get(context.Background(), s.RootModule().Resources[rNameFull].Primary.Attributes["id"])
			if err != nil {
				return fmt.Errorf("get service: %w", err)
			}
			if got := svc.Private != nil && *svc.Private; got != private {
				return fmt.Errorf("private on the server: expected %t, found %t", private, got)
			}
			var got []string
			if svc.AccessGroups != nil {
				got = *svc.AccessGroups
			}
			if !sameStrings(got, groups) {
				return fmt.Errorf("access groups on the server: expected %v, found %v", groups, got)
			}
			if len(svc.Targets) != 1 || svc.Targets[0].Port != port {
				return fmt.Errorf("target port on the server: expected %d, found %v", port, svc.Targets)
			}
			return nil
		}
	}
	outside := func(change func(*api.ServiceRequest)) resource.ConfigPlanChecks {
		return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
			testExpectUpdateInPlace(rNameFull), serviceChangedOutside{id: &createdID, change: change}}}
	}
	// onServerBefore checks, before a step, that a stopped apply wrote nothing.
	onServerBefore := func(private bool, groups []string, port int) func() {
		return func() {
			check := onServer(private, groups, port)
			if err := check(&terraform.State{Modules: []*terraform.ModuleState{{Path: []string{"root"}, Resources: map[string]*terraform.ResourceState{
				rNameFull: {Primary: &terraform.InstanceState{Attributes: map[string]string{"id": createdID}}},
			}}}}); err != nil {
				t.Error(err)
			}
		}
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().ReverseProxyServices.Get, &createdID),
		Steps: []resource.TestStep{
			{
				Config: config(svc{private: "true", groups: group}),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttr(rNameFull, "private", "true"),
					resource.TestCheckResourceAttr(rNameFull, "access_groups.#", "1"),
					resource.TestCheckResourceAttr(rNameFull, "access_groups.0", groupID),
					resource.TestCheckResourceAttr(dsName, "private", "true"),
					resource.TestCheckResourceAttr(dsName, "access_groups.#", "1"),
					resource.TestCheckResourceAttr(dsName, "access_groups.0", groupID),
					onServer(true, []string{groupID}, 8080),
				),
			},
			{ResourceName: rNameFull, ImportState: true, ImportStateVerify: true},
			{Config: config(svc{private: "true", groups: group, bearer: true}), ExpectError: regexp.MustCompile(`Bearer auth not allowed`)},
			{
				// The service stays private from its state; bearer auth is still refused.
				Config:      config(svc{groups: group, bearer: true}),
				ExpectError: regexp.MustCompile(`Bearer auth not allowed`),
			},
			{Config: config(svc{private: "true", groups: "[null]"}), ExpectError: regexp.MustCompile(`(?s)access_groups.*null`)},
			{Config: config(svc{private: "true", groups: `[""]`}), ExpectError: regexp.MustCompile(`(?s)access_groups.*must not be blank`)},
			{Config: config(svc{private: "true", groups: `[" "]`}), ExpectError: regexp.MustCompile(`(?s)access_groups.*must not be blank`)},
			{
				// A new private service has no groups to keep.
				Config: config(svc{private: "true", groups: group, extra: fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "other" {
  name    = "%s-other"
  domain  = "%s-other.%s"
  mode    = "http"
  private = true
  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 8080
    protocol    = "http"
  }]
  auth = {}
}`, rName, rName, cluster.Address, peerID)}),
				ExpectError: regexp.MustCompile(`Access groups required`),
			},
			{
				// The plan changes the port only; the service is made public
				// on the server before the apply, so the apply stops.
				Config:           config(svc{private: "true", groups: group, port: 8081}),
				ConfigPlanChecks: outside(func(r *api.ServiceRequest) { off := false; r.Private = &off }),
				ExpectError:      regexp.MustCompile(`(?s)changed after the plan.*private: true when planned, now false`),
			},
			{
				// The outside change was kept; a new plan restores the configuration.
				PreConfig: onServerBefore(false, []string{groupID}, 8080),
				Config:    config(svc{private: "true", groups: twoGroups}),
				Check:     onServer(true, []string{groupID, otherGroupID}, 8080),
			},
			{
				// Only a group is revoked on the server after a port-only plan.
				Config: config(svc{private: "true", groups: twoGroups, port: 8081}),
				ConfigPlanChecks: outside(func(r *api.ServiceRequest) {
					r.AccessGroups = &[]string{groupID}
				}),
				ExpectError: regexp.MustCompile(`(?s)changed after the plan.*access_groups: `),
			},
			{
				PreConfig: onServerBefore(true, []string{groupID}, 8080),
				Config:    config(svc{private: "true", groups: group}),
				Check:     onServer(true, []string{groupID}, 8080),
			},
			{
				Config:           config(svc{private: "false", groups: group}),
				ConfigPlanChecks: updatesInPlace(rNameFull),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "private", "false"),
					onServer(false, []string{groupID}, 8080),
				),
			},
			{
				// A public service with no access groups: the empty list stays empty.
				Config: config(svc{private: "false", groups: "[]"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "access_groups.#", "0"),
					resource.TestCheckResourceAttr(dsName, "private", "false"),
					resource.TestCheckResourceAttr(dsName, "access_groups.#", "0"),
					onServer(false, nil, 8080),
				),
			},
			{
				// Imported, the cleared list reads as the applied one.
				ResourceName: rNameFull, ImportState: true, ImportStateVerify: true,
			},
			{
				// Made private with the groups left out: the empty list from
				// state is refused at plan time.
				Config:      config(svc{private: "true"}),
				ExpectError: regexp.MustCompile(`Access groups required`),
			},
			{
				// Both access settings left out: the service is made private
				// on the server after the plan, so the apply stops rather
				// than reopening it.
				Config: config(svc{port: 8081}),
				ConfigPlanChecks: outside(func(r *api.ServiceRequest) {
					on := true
					r.Private, r.AccessGroups = &on, &[]string{groupID}
				}),
				ExpectError: regexp.MustCompile(`(?s)changed after the plan.*private: false when planned, now true`),
			},
			{
				// A new plan adopts the live settings.
				PreConfig: onServerBefore(true, []string{groupID}, 8080),
				Config:    config(svc{port: 8081}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "private", "true"),
					onServer(true, []string{groupID}, 8081),
				),
			},
			{
				// A group created in the same apply: its ID is unknown at
				// plan time and reaches state and the server once known.
				Config: config(svc{private: "true", groups: "[netbird_group.allowed.id]", port: 8081, extra: fmt.Sprintf(`
resource "netbird_group" "allowed" {
  name = "%s-allowed"
}`, rName)}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(rNameFull, "access_groups.0", "netbird_group.allowed", "id"),
					resource.TestCheckResourceAttr(rNameFull, "access_groups.#", "1"),
					func(s *terraform.State) error {
						return onServer(true, []string{s.RootModule().Resources["netbird_group.allowed"].Primary.ID}, 8081)(s)
					},
				),
			},
		},
	})
}

// Test_ReverseProxyService_AccessActionCrowdsecDirectUpstream covers the
// per-target access action, the CrowdSec mode and direct upstream targets.
func Test_ReverseProxyService_AccessActionCrowdsecDirectUpstream(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	var createdID string
	peerID1 := testPeerID(t, "peer1")
	groupID := e2eGroupAllID()
	// The server stores every CrowdSec mode whatever the cluster supports,
	// but only a capable cluster gets an active one: enforce without a
	// checker denies every request from outside the tunnel.
	crowdsec, crowdsecNext := "off", "off"
	if cluster.SupportsCrowdsec != nil && *cluster.SupportsCrowdsec {
		crowdsec, crowdsecNext = "observe", "enforce"
	}
	// Both targets are one backend (the shared read-only peer1) on two paths,
	// which is how access actions are meant to be used.
	type svc struct {
		adminAction string // the /admin target's access_action line, "" leaves it out
		adminPort   int    // 0 leaves the /admin target out
		firstAction string // the first target's access_action line
		direct      bool   // direct_upstream on the first target of a private service
		crowdsec    string // "" leaves access_restrictions out, "null" sets crowdsec_mode = null
		emptyCidrs  bool   // allowed_cidrs = [] beside the mode
		adminNoPath bool   // the /admin target leaves path out (kept from state)
		nameFrom    string // name from terraform_data.name with this input, not known until it is applied
		extra       string // more configuration
	}
	config := func(c svc) string {
		options, private := "", "false"
		if c.direct {
			options = `
    options     = { direct_upstream = true }`
			private = "true"
		}
		restrictions := ""
		if c.crowdsec != "" {
			mode := fmt.Sprintf("%q", c.crowdsec)
			if c.crowdsec == "null" {
				mode = "null"
			}
			cidrs := ""
			if c.emptyCidrs {
				cidrs = `
    allowed_cidrs = []`
			}
			restrictions = fmt.Sprintf(`
  access_restrictions = {
    crowdsec_mode = %s%s
  }
`, mode, cidrs)
		}
		admin := ""
		if c.adminPort != 0 {
			adminPath := `
    path        = "/admin"`
			if c.adminNoPath {
				adminPath = ""
			}
			admin = fmt.Sprintf(`, {
    target_id   = %q
    target_type = "peer"
    port        = %d
    protocol    = "http"%s
    %s
  }`, peerID1, c.adminPort, adminPath, c.adminAction)
		}
		name := fmt.Sprintf("%q", rName)
		if c.nameFrom != "" {
			name = "terraform_data.name.output"
			c.extra += fmt.Sprintf(`
resource "terraform_data" "name" {
  input = %q
}`, c.nameFrom)
		}
		return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" %q {
  name          = %s
  domain        = %q
  mode          = "http"
  private       = %s
  access_groups = [%q]
  auth          = {}
%s
  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = 8080
    protocol    = "http"
    %s%s
  }%s]
}`, rName, name, domain, private, groupID, restrictions, peerID1, c.firstAction, options, admin) + c.extra
	}
	// onServer checks the service as the server has it: action is the /admin
	// target's ("" for no /admin target), crowdsec the stored mode ("" none).
	onServer := func(action string, direct bool, crowdsec string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			svc, err := testClient().ReverseProxyServices.Get(context.Background(), s.RootModule().Resources[rNameFull].Primary.Attributes["id"])
			if err != nil {
				return fmt.Errorf("get service: %w", err)
			}
			seen := map[string]int{}
			for _, target := range svc.Targets {
				seen[normalizedPath(target.Path)]++
			}
			wantAdmin := 0
			if action != "" {
				wantAdmin = 1
			}
			if len(svc.Targets) != 1+wantAdmin || seen["/"] != 1 || seen["/admin"] != wantAdmin {
				return fmt.Errorf("expected one target at / and %d at /admin, found %v", wantAdmin, seen)
			}
			for _, target := range svc.Targets {
				switch normalizedPath(target.Path) {
				case "/":
					if target.AccessAction == nil || *target.AccessAction != api.ServiceTargetAccessActionInherit {
						return fmt.Errorf("first target: expected inherit, found %v", target.AccessAction)
					}
					if got := target.Options != nil && target.Options.DirectUpstream != nil && *target.Options.DirectUpstream; got != direct {
						return fmt.Errorf("first target: expected direct_upstream %t, found %t", direct, got)
					}
				case "/admin":
					if target.AccessAction == nil || string(*target.AccessAction) != action {
						return fmt.Errorf("admin target: expected %s, found %v", action, target.AccessAction)
					}
				}
			}
			got := ""
			if svc.AccessRestrictions != nil && svc.AccessRestrictions.CrowdsecMode != nil {
				got = string(*svc.AccessRestrictions.CrowdsecMode)
			}
			if got != crowdsec {
				return fmt.Errorf("crowdsec_mode on the server: expected %q, found %q", crowdsec, got)
			}
			return nil
		}
	}
	l4 := fmt.Sprintf(`
resource "netbird_reverse_proxy_service" "l4" {
  name    = "%s-l4"
  domain  = "%s-l4.%s"
  mode    = "tcp"
  auth    = {}
  targets = [{
    target_id     = %q
    target_type   = "peer"
    port          = 8080
    protocol      = "tcp"
    access_action = "block"
  }]
}`, rName, rName, cluster.Address, peerID1)
	private := svc{adminAction: `access_action = "block"`, adminPort: 8082, direct: true, crowdsec: crowdsec}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().ReverseProxyServices.Get, &createdID),
		Steps: []resource.TestStep{
			{
				Config: config(svc{adminAction: `access_action = "block"`, adminPort: 8081, crowdsec: crowdsec}),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.access_action", "inherit"),
					resource.TestCheckResourceAttr(rNameFull, "targets.1.access_action", "block"),
					resource.TestCheckResourceAttr(rNameFull, "access_restrictions.crowdsec_mode", crowdsec),
					onServer("block", false, crowdsec),
				),
			},
			{ResourceName: rNameFull, ImportState: true, ImportStateVerify: true},
			{
				// Values outside the enums are refused at plan time.
				Config:      config(svc{adminAction: `access_action = "deny"`, adminPort: 8081, crowdsec: crowdsec}),
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Value Match.*targets\[1\]\.access_action`),
			},
			{
				Config:      config(svc{adminAction: `access_action = "block"`, adminPort: 8081, crowdsec: "strict"}),
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Value Match.*access_restrictions\.crowdsec_mode`),
			},
			{
				// A path left out is kept from state when nothing changes, so it
				// is not a second target at "/".
				Config:   config(svc{adminAction: `access_action = "block"`, adminPort: 8081, crowdsec: crowdsec, adminNoPath: true}),
				PlanOnly: true,
			},
			{
				// A name not known until apply makes the first plan an update
				// that may not happen. It resolves to the current name: nothing
				// is written and /admin stays.
				Config: config(svc{adminAction: `access_action = "block"`, adminPort: 8081, crowdsec: crowdsec, adminNoPath: true, nameFrom: rName}),
				Check:  onServer("block", false, crowdsec),
			},
			{
				// It resolves to another name: the final plan sends no path for
				// /admin, which would join "/", and refuses before writing.
				Config:      config(svc{adminAction: `access_action = "block"`, adminPort: 8081, crowdsec: crowdsec, adminNoPath: true, nameFrom: rName + "x"}),
				ExpectError: regexp.MustCompile(`Duplicate target path`),
			},
			{
				// The service is unchanged: with its own name and /admin it
				// plans nothing (terraform_data keeps the input it was given).
				Config: config(svc{adminAction: `access_action = "block"`, adminPort: 8081, crowdsec: crowdsec}) + fmt.Sprintf(`
resource "terraform_data" "name" {
  input = %q
}`, rName+"x"),
				PlanOnly: true,
			},
			{
				// On an update it is not sent and the server would move the
				// target to "/", next to the first one: the plan says so.
				Config:      config(svc{adminAction: `access_action = "block"`, adminPort: 8082, crowdsec: crowdsec, adminNoPath: true}),
				ExpectError: regexp.MustCompile(`Duplicate target path`),
			},
			{
				// The action and, on a capable cluster, the CrowdSec mode change
				// in place; an empty list beside the mode stays empty.
				Config:           config(svc{adminAction: `access_action = "bypass"`, adminPort: 8081, crowdsec: crowdsecNext, emptyCidrs: true}),
				ConfigPlanChecks: updatesInPlace(rNameFull),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "targets.1.access_action", "bypass"),
					resource.TestCheckResourceAttr(rNameFull, "access_restrictions.crowdsec_mode", crowdsecNext),
					onServer("bypass", false, crowdsecNext),
				),
			},
			{
				// Left out, the action stays as it is: no change planned.
				Config:   config(svc{adminPort: 8081, crowdsec: crowdsecNext, emptyCidrs: true}),
				PlanOnly: true,
			},
			{
				// Left out while the target changes: the update does not send
				// it, and the server keeps the action of the target at /admin.
				// crowdsec_mode = null clears the mode; the server then reports
				// no restrictions, and the object that sets nothing stays.
				Config:           config(svc{adminPort: 8082, crowdsec: "null"}),
				ConfigPlanChecks: updatesInPlace(rNameFull),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "targets.1.access_action", "bypass"),
					resource.TestCheckResourceAttr(rNameFull, "targets.1.port", "8082"),
					resource.TestCheckNoResourceAttr(rNameFull, "access_restrictions.crowdsec_mode"),
					onServer("bypass", false, ""),
				),
			},
			{
				Config:   config(svc{adminPort: 8082, crowdsec: "null"}),
				PlanOnly: true,
			},
			{
				// Turning the service private while /admin keeps bypass from
				// the server is refused at plan time, left out or configured.
				Config:      config(svc{adminPort: 8082, direct: true, crowdsec: crowdsec}),
				ExpectError: regexp.MustCompile(`(?s)Bypass not allowed on a private service.*current target at this path`),
			},
			{
				Config:      config(svc{adminAction: `access_action = "bypass"`, adminPort: 8082, direct: true, crowdsec: crowdsec}),
				ExpectError: regexp.MustCompile(`Bypass not allowed on a private service`),
			},
			{
				// A private service refuses bypass; block with a direct upstream
				// target is fine on the harness cluster (private mode).
				Config: config(svc{adminAction: private.adminAction, adminPort: 8082, direct: true, crowdsec: crowdsec,
					extra: fmt.Sprintf(`
data "netbird_reverse_proxy_service" "test" {
  id = netbird_reverse_proxy_service.%s.id
}`, rName)}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.netbird_reverse_proxy_service.test", "targets.0.options.direct_upstream", "true"),
					resource.TestCheckResourceAttr("data.netbird_reverse_proxy_service.test", "targets.1.access_action", "block"),
					resource.TestCheckResourceAttr("data.netbird_reverse_proxy_service.test", "access_restrictions.crowdsec_mode", crowdsec),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.options.direct_upstream", "true"),
					resource.TestCheckResourceAttr(rNameFull, "targets.1.access_action", "block"),
					onServer("block", true, crowdsec),
				),
			},
			{ResourceName: rNameFull, ImportState: true, ImportStateVerify: true},
			{
				// direct_upstream cleared outside Terraform: the refresh shows
				// it, and the apply sets it again.
				PreConfig: func() {
					change := serviceChangedOutside{id: &createdID, change: func(req *api.ServiceRequest) {
						for i := range *req.Targets {
							if t := &(*req.Targets)[i]; normalizedPath(t.Path) == "/" && t.Options != nil {
								off := false
								t.Options.DirectUpstream = &off
							}
						}
					}}
					var resp plancheck.CheckPlanResponse
					change.CheckPlan(context.Background(), plancheck.CheckPlanRequest{}, &resp)
					if resp.Error != nil {
						t.Fatalf("clear direct_upstream: %v", resp.Error)
					}
				},
				Config:           config(private),
				ConfigPlanChecks: updatesInPlace(rNameFull),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "targets.0.options.direct_upstream", "true"),
					onServer("block", true, crowdsec),
				),
			},
			{
				// Outside HTTP mode only inherit is accepted.
				Config:      config(private) + l4,
				ExpectError: regexp.MustCompile(`Access action needs HTTP`),
			},
			{
				// Dropping /admin (block) with no action set anywhere is refused
				// by the server, so the plan refuses it.
				Config:      config(svc{direct: true, crowdsec: crowdsec}),
				ExpectError: regexp.MustCompile(`(?s)Access action needed when a controlled path goes.*Current target \d+ `),
			},
			{
				// Also drops access_restrictions: the server then has none.
				Config:           config(svc{firstAction: `access_action = "inherit"`, direct: true}),
				ConfigPlanChecks: updatesInPlace(rNameFull),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rNameFull, "targets.#", "1"),
					resource.TestCheckNoResourceAttr(rNameFull, "access_restrictions.crowdsec_mode"),
					onServer("", true, ""),
				),
			},
		},
	})
}

// normalizedPath is the server's reading of a target path: left out is "/".
func normalizedPath(p *string) string {
	if p == nil || *p == "" {
		return "/"
	}
	return *p
}

// serviceChangedOutside is a plan check that changes the service on the
// server between plan and apply, as an operator would.
type serviceChangedOutside struct {
	id     *string
	change func(*api.ServiceRequest)
}

func (c serviceChangedOutside) CheckPlan(ctx context.Context, _ plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	svc, err := testClient().ReverseProxyServices.Get(ctx, *c.id)
	if err != nil {
		resp.Error = err
		return
	}
	auth := svc.Auth
	targets := svc.Targets
	req := api.ServiceRequest{
		Name: svc.Name, Domain: svc.Domain, Enabled: svc.Enabled, ListenPort: svc.ListenPort,
		PassHostHeader: svc.PassHostHeader, RewriteRedirects: svc.RewriteRedirects,
		Private: svc.Private, AccessGroups: svc.AccessGroups,
		AccessRestrictions: svc.AccessRestrictions, Auth: &auth, Targets: &targets,
	}
	if svc.Mode != nil {
		mode := api.ServiceRequestMode(*svc.Mode)
		req.Mode = &mode
	}
	c.change(&req)
	_, resp.Error = testClient().ReverseProxyServices.Update(ctx, *c.id, req)
}

// Test_ReverseProxyService_SubnetHost covers the host of a subnet target,
// which the server keeps as configured: a missing one is refused at plan
// time, and a change made on the server shows in the next plan.
func Test_ReverseProxyService_SubnetHost(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	rNameFull := "netbird_reverse_proxy_service." + rName
	var createdID string
	// extra holds more target lines (path, port, access_action); the port
	// defaults to 8080.
	config := func(host string, extra ...string) string {
		hostLine := ""
		if host != "" {
			hostLine = fmt.Sprintf("\n    host        = %q", host)
		}
		port := "8080"
		for _, line := range extra {
			if strings.HasPrefix(line, "port") {
				port = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "port")), "="))
				continue
			}
			hostLine += "\n    " + line
		}
		return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" %q {
  name   = %q
  domain = "%s.%s"
  mode   = "http"
  auth   = {}

  targets = [{
    target_id   = %q
    target_type = "subnet"%s
    port        = %s
    protocol    = "http"
    options     = { request_timeout = "5s", direct_upstream = false }
  }]
}`, rName, rName, rName, cluster.Address, e2eResourceSubnetID(), hostLine, port)
	}
	hostOnServer := func(want string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			svc, err := testClient().ReverseProxyServices.Get(context.Background(), s.RootModule().Resources[rNameFull].Primary.ID)
			if err != nil {
				return fmt.Errorf("get service: %w", err)
			}
			if len(svc.Targets) != 1 || svc.Targets[0].Host == nil || *svc.Targets[0].Host != want {
				return fmt.Errorf("expected one target with host %q, found %+v", want, svc.Targets)
			}
			return nil
		}
	}
	// pathOnServer checks the target's path as the server has it ("" none).
	pathOnServer := func(want string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			svc, err := testClient().ReverseProxyServices.Get(context.Background(), s.RootModule().Resources[rNameFull].Primary.ID)
			if err != nil {
				return fmt.Errorf("get service: %w", err)
			}
			got := ""
			if len(svc.Targets) == 1 && svc.Targets[0].Path != nil {
				got = *svc.Targets[0].Path
			}
			if len(svc.Targets) != 1 || got != want {
				return fmt.Errorf("expected one target with path %q, found %+v", want, svc.Targets)
			}
			return nil
		}
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().ReverseProxyServices.Get, &createdID),
		Steps: []resource.TestStep{
			{
				Config:      config(""),
				ExpectError: regexp.MustCompile(`(?s)Host not usable.*a subnet target needs a host`),
			},
			{
				Config: config("192.168.10.5"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttr(rNameFull, "targets.0.host", "192.168.10.5"),
					hostOnServer("192.168.10.5"),
				),
			},
			{
				// Changed on the server: the refresh shows it and the apply
				// sets the configured host again.
				PreConfig: func() {
					change := serviceChangedOutside{id: &createdID, change: func(req *api.ServiceRequest) {
						other := "192.168.10.6"
						(*req.Targets)[0].Host = &other
					}}
					var resp plancheck.CheckPlanResponse
					change.CheckPlan(context.Background(), plancheck.CheckPlanRequest{}, &resp)
					if resp.Error != nil {
						t.Fatalf("change the host: %v", resp.Error)
					}
				},
				Config:           config("192.168.10.5"),
				ConfigPlanChecks: updatesInPlace(rNameFull),
				Check:            hostOnServer("192.168.10.5"),
			},
			{
				Config: config("192.168.10.5", `path = "/admin"`),
				Check:  pathOnServer("/admin"),
			},
			{
				// Left out, the path is not sent: the plan shows null, the
				// server keeps none and the next plan is empty.
				Config: config("192.168.10.5", "port = 8081", `access_action = "inherit"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					testExpectUpdateInPlace(rNameFull),
					plancheck.ExpectKnownValue(rNameFull, tfjsonpath.New("targets").AtSliceIndex(0).AtMapKey("path"), knownvalue.Null()),
				}},
				Check: pathOnServer(""),
			},
			{
				// The server keeps a subnet host as written, so an import
				// recovers it without prior state; direct_upstream = false,
				// which the server does not report, reads as false.
				ResourceName:      rNameFull,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Test_ReverseProxyService_TLSModeOmitted: a TLS service updated with mode
// left out keeps (and sends) tls; the server requires listen_port on every
// TLS request, so the configuration keeps it.
func Test_ReverseProxyService_TLSModeOmitted(t *testing.T) {
	cluster := testRequireProxyCluster(t)
	rName := "s" + acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
	domain := rName + "." + cluster.Address
	rNameFull := "netbird_reverse_proxy_service." + rName
	peerID := testPeerID(t, "peer1")
	var createdID string
	config := func(mode string, port int) string {
		return fmt.Sprintf(`
resource "netbird_reverse_proxy_service" %q {
  name        = %q
  domain      = %q
  %s
  listen_port = 15443

  targets = [{
    target_id   = %q
    target_type = "peer"
    port        = %d
    protocol    = "tcp"
  }]

  auth = {}
}`, rName, rName, domain, mode, peerID, port)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testEnsureManagementRunning(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testCheckGone(testClient().ReverseProxyServices.Get, &createdID),
		Steps: []resource.TestStep{
			{
				Config: config(`mode = "tls"`, 5001),
				Check: resource.ComposeAggregateTestCheckFunc(
					testRecordID(rNameFull, &createdID),
					resource.TestCheckResourceAttr(rNameFull, "mode", "tls"),
				),
			},
			{
				Config: config("", 5002),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(rNameFull, plancheck.ResourceActionUpdate),
					plancheck.ExpectKnownValue(rNameFull, tfjsonpath.New("mode"), knownvalue.StringExact("tls")),
				}},
				Check: func(*terraform.State) error {
					svc, err := testClient().ReverseProxyServices.Get(context.Background(), createdID)
					if err != nil {
						return err
					}
					if svc.Mode == nil || *svc.Mode != "tls" || len(svc.Targets) != 1 || svc.Targets[0].Port != 5002 {
						return fmt.Errorf("expected a tls service with target port 5002 on the server, found mode %v and %+v", svc.Mode, svc.Targets)
					}
					return nil
				},
			},
		},
	})
}
