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

### Fixed

- `netbird_account_settings` no longer resets the settings it does not manage, such as local MFA,
  IPv6 groups or metrics push (#2; upstream #243, #278). The update starts from the account's
  current settings (upstream #244 by chad-loder) and, beyond that, sends back as they are the
  settings the compiled client library does not know, so a management server newer than the
  library keeps them too.
- `netbird_account_settings` updates and refreshes the account by its ID, and returns an error
  instead of panicking when the token sees no account or the account list fails, in the resource
  and the data source (#4; upstream #118, #277, fix #276 by Tim Rabl).
- Management API responses are bounded to 64 MiB, error responses included, and their
  connections are released.

### Removed

- The upstream-only workflows `agent-acceptance-review.yml` and `pr-metadata.yml`
  and the CodeRabbit configuration: they depend on NetBird's agent network and
  review integrations, which this repository does not have.
