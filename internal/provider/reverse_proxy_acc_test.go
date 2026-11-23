//go:build e2e

package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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
		},
	})
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
