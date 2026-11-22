# Changelog

All notable changes of the arzner fork are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/). Each release names the upstream
commit of [netbirdio/terraform-provider-netbird](https://github.com/netbirdio/terraform-provider-netbird)
it is based on.

## [Unreleased]

Based on upstream `4aa1f36` (v0.0.11 plus #263, #222, #224).

### Removed

- The upstream-only workflows `agent-acceptance-review.yml` and `pr-metadata.yml`
  and the CodeRabbit configuration: they depend on NetBird's agent network and
  review integrations, which this repository does not have.
