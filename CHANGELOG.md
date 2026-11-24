# Changelog

All notable changes of the arzner fork are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/). Each release names the upstream
commit of [netbirdio/terraform-provider-netbird](https://github.com/netbirdio/terraform-provider-netbird)
it is based on.

## [Unreleased]

### Added

- `netbird_account_settings` can be imported with the ID `current`: the account of the token,
  when the token sees exactly one account (#19).
- `netbird_account_settings`: `metrics_push_enabled`, `auto_update_always`,
  `peer_hostname_collision_rejected`, `agent_network_only` and `dashboard_features`, also in
  the data source (#23). `agent_network_only = true` together with
  `dashboard_features.agent_network = false` is rejected at plan time.
- `netbird_posture_check`: `certificate_check` block, requiring a peer certificate that chains to
  one of the given PEM CA certificates (iOS and Android always fail it). Entries are checked at
  plan time with the server's own parser, and a block needs at least one. Also in the data
  source (#26).
- `netbird_scim`: `connector_id`, the Dex connector for setups with the embedded identity
  provider; also in the data source (#27).
- `netbird_peer`: `ipv6`, the peer's IPv6 overlay address; left out, the server-assigned address
  is kept, set, the peer moves to it (canonical form only). Also in the `netbird_peer` data
  source (#24).
- `netbird_reverse_proxy_service`: `private` and `access_groups` for NetBird-only services
  (reachable over the tunnel by the given groups, with a server-generated access policy);
  the private-service rules (HTTP mode, at least one group, no blank group IDs, no bearer
  auth) are checked at plan time, also for a service that stays private from its state. An
  update stops before writing when `private` or `access_groups`, shown unchanged by the plan,
  changed on the server after the plan. Peers reach a private service only through a proxy with
  private access enabled (`NB_PROXY_PRIVATE=true`). Also in the data source (#25).
- `netbird_reverse_proxy_service`: `access_restrictions.crowdsec_mode` (CrowdSec IP reputation),
  `targets[].access_action` (`inherit`, `bypass` or `block` for a target's path; left out, the
  server keeps the current action) and `targets[].options.direct_upstream` (dial through the
  proxy host's network). Also in the data source (#45). The plan refuses what the server would
  reject: a `bypass` target on a private service; `bypass` or `block` outside HTTP mode or on a
  path that is not a canonical prefix; two enabled targets on one path once a target has `bypass`
  or `block`; and a loopback,
  multicast, link-local or malformed host with `direct_upstream` (the server's rules for the
  service's mode and target type). Options cleared on the server read as no options, not as an object
  of nulls that plans an update. `direct_upstream` on a TCP, UDP
  or TLS service, which NetBird stores but does not apply, gets a plan warning. What only an
  in-place update meets, because the server keeps a left-out action from the current target at
  that path (a kept `bypass` on a private service, a dropped `bypass` or `block` path while no
  target sets an action, a left-out action while two current targets share a path), is a plan
  warning, since the plan cannot tell an update from a replacement, and the update stops before
  writing.

### Changed

- NetBird 0.82 is supported too: the acceptance suite also runs against the newest release of the
  previous NetBird minor line (v0.82.1 today), and passes with the 0.1.0 provider code (#32).
- `netbird_account_settings` sends only the settings the configuration sets; the others keep
  the server's current values even if they changed between plan and apply (previously the
  planned prior state was sent back).
- `netbird_peer` updates start from the live peer and change only the attributes the
  configuration sets. When a setting the plan shows unchanged changed after the plan, an
  in-place update stops before writing anything and asks for a new plan (#74 extends this to
  the other resources). An
  address the server refuses (outside the account range, peer not in an IPv6-enabled group,
  already in use) changes nothing else, and changing login or inactivity expiration on a peer
  added with a setup key is refused before anything is sent.

### Fixed

- A delete answered with 404 counts as done when the object's own lookup answers 404 too and the
  token still works (NetBird also answers 404 for a revoked token or for another object the delete
  touches); a destroy failed when the object disappeared between the refresh and the delete (#100;
  upstream [#196](https://github.com/netbirdio/terraform-provider-netbird/pull/196)).
- `netbird_reverse_proxy_service`: an `access_restrictions` block that sets nothing (for example
  `crowdsec_mode = null`), or an empty list in it such as `allowed_cidrs = []`, no longer fails
  the apply with an inconsistent result. A subnet target without a host, or with a host the
  server refuses, is refused at plan time.
- `netbird_reverse_proxy_service`: a target that leaves `path` out plans `path = null` (the
  server's `/`) on a create or an update instead of "known after apply", so a target kept at
  another path, for example after an import, shows that it moves to `/`.

## [0.1.0] - 2026-11-23

First release of the fork. Based on upstream `4aa1f36` (v0.0.11 plus [#263](https://github.com/netbirdio/terraform-provider-netbird/pull/263)),
built against NetBird v0.83.2 and tested with Terraform 1.14 to 1.17 and OpenTofu 1.12
to 1.13.

### Added

- `netbird_account_settings`: `local_mfa_enabled` (upstream [#227](https://github.com/netbirdio/terraform-provider-netbird/pull/227) by Nathan).
- `netbird_account_settings`: `network_range_v6` and `ipv6_enabled_groups`, also in the data source
  (#3; upstream [#272](https://github.com/netbirdio/terraform-provider-netbird/issues/272)).

### Changed

- `netbird_account_settings`: `network_range` and `network_range_v6` must be written in canonical
  CIDR form (host bits zero, lower case, shortest IPv6 form), the way the server returns them;
  any other spelling used to fail the apply with an inconsistent result.
- The provider address is `registry.terraform.io/arzner/netbird` (#6). Releases are signed
  with the key in `release-signing-key.asc` and their notes come from this file.
- The acceptance suite runs with OpenTofu as well as Terraform: Terraform 1.14 to 1.17 and
  OpenTofu 1.12 to 1.13 on `main`, the newest line of each on pull requests (#16). Terraform
  1.12 and 1.13 are no longer tested.
- Compatibility policy with NetBird releases in the README, and a daily acceptance run against
  the newest NetBird release that opens an issue when it fails (#17).
- Built against the NetBird client library v0.83.2 (was v0.81.0-canary.pr-7662.1); the acceptance
  suite runs against a NetBird v0.83.2 server (#3; upstream [#280](https://github.com/netbirdio/terraform-provider-netbird/pull/280) bumps to 0.83.1).

### Removed

- The upstream-only workflows `agent-acceptance-review.yml` and `pr-metadata.yml`
  and the CodeRabbit configuration: they depend on NetBird's agent network and
  review integrations, which this repository does not have.

### Fixed

- `netbird_account_settings` no longer resets the settings it does not manage, such as local MFA,
  IPv6 groups or metrics push (#2; upstream [#243](https://github.com/netbirdio/terraform-provider-netbird/issues/243), [#278](https://github.com/netbirdio/terraform-provider-netbird/issues/278)). The update starts from the account's
  current settings (upstream [#244](https://github.com/netbirdio/terraform-provider-netbird/pull/244) by chad-loder) and, beyond that, sends back as they are the
  settings the compiled client library does not know, so a management server newer than the
  library keeps them too.
- `netbird_account_settings` updates and refreshes the account by its ID and no longer panics on
  an empty or failed account list (#4; upstream [#118](https://github.com/netbirdio/terraform-provider-netbird/issues/118), [#277](https://github.com/netbirdio/terraform-provider-netbird/issues/277), fix [#276](https://github.com/netbirdio/terraform-provider-netbird/pull/276) by Tim Rabl): a failed list
  is an error everywhere; an empty list is an error on create, update and in the data source,
  while refresh removes a resource whose account is no longer listed.
  Importing `netbird_account_settings` therefore needs the real account ID; any other ID now
  fails as a missing object instead of binding to the token's account.
- Management API responses are bounded to 64 MiB, error responses included, and their
  connections are released.
- `netbird_group` with attached network resources can be updated again; a plan without
  `resources` keeps the attached ones instead of detaching them (#5; upstream [#104](https://github.com/netbirdio/terraform-provider-netbird/issues/104), [#150](https://github.com/netbirdio/terraform-provider-netbird/issues/150),
  fix [#268](https://github.com/netbirdio/terraform-provider-netbird/pull/268) by Andreas Wapf).

[Unreleased]: https://github.com/arzner/terraform-provider-netbird/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/arzner/terraform-provider-netbird/releases/tag/v0.1.0
