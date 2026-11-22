# Changelog

All notable changes of the arzner fork are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/). Each release names the upstream
commit of [netbirdio/terraform-provider-netbird](https://github.com/netbirdio/terraform-provider-netbird)
it is based on.

## [Unreleased]

Based on upstream `4aa1f36` (v0.0.11 plus #263, #222, #224).

### Added

- `netbird_account_settings`: `local_mfa_enabled` (upstream netbirdio/terraform-provider-netbird#227 by Nathan).
- `netbird_account_settings`: `network_range_v6` and `ipv6_enabled_groups`, also in the data source
  (#3; upstream #272).

### Changed

- `netbird_account_settings`: `network_range` and `network_range_v6` must be written in canonical
  CIDR form (host bits zero, lower case, shortest IPv6 form), the way the server returns them;
  any other spelling used to fail the apply with an inconsistent result.
- The provider address is `registry.terraform.io/arzner/netbird` (#6). Releases are signed
  with the key in `release-signing-key.asc` and their notes come from this file.
- The acceptance suite runs with OpenTofu as well as Terraform: Terraform 1.14 to 1.17 and
  OpenTofu 1.12 to 1.13 on `main`, the newest line of each on pull requests (#16). Terraform
  1.12 and 1.13 are no longer tested.
- Compatibility policy with NetBird releases in the README, and a weekly acceptance run against
  the newest NetBird release that opens an issue when it fails (#17).
- Built against the NetBird client library v0.83.2 (was v0.81.0-canary.pr-7662.1); the acceptance
  suite runs against a NetBird v0.83.2 server (#3; upstream #280 bumps to 0.83.1).

### Removed

- The upstream-only workflows `agent-acceptance-review.yml` and `pr-metadata.yml`
  and the CodeRabbit configuration: they depend on NetBird's agent network and
  review integrations, which this repository does not have.

### Fixed

- `netbird_account_settings` no longer resets the settings it does not manage, such as local MFA,
  IPv6 groups or metrics push (#2; upstream #243, #278). The update starts from the account's
  current settings (upstream #244 by chad-loder) and, beyond that, sends back as they are the
  settings the compiled client library does not know, so a management server newer than the
  library keeps them too.
- `netbird_account_settings` updates and refreshes the account by its ID and no longer panics on
  an empty or failed account list (#4; upstream #118, #277, fix #276 by Tim Rabl): a failed list
  is an error everywhere; an empty list is an error on create, update and in the data source,
  while refresh removes a resource whose account is no longer listed.
  Importing `netbird_account_settings` therefore needs the real account ID; any other ID now
  fails as a missing object instead of binding to the token's account.
- Management API responses are bounded to 64 MiB, error responses included, and their
  connections are released.
- `netbird_group` with attached network resources can be updated again; a plan without
  `resources` keeps the attached ones instead of detaching them (#5; upstream #104, #150,
  fix #268 by Andreas Wapf).
