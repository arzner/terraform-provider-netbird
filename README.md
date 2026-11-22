# NetBird Terraform Provider

A Terraform and OpenTofu provider for [NetBird](https://netbird.io), published as
`arzner/netbird` (from v0.1.0). The goal is the most dependable way to manage
NetBird as code.

## Principles

- **An apply changes only what you declare.** Settings, members and attachments
  that are not in your configuration are left as they are, including ones this
  provider does not know yet. No silent resets.
- **Current with NetBird.** The provider is built and tested against the latest
  NetBird server releases, and new server features are tracked as issues.
- **Tested against real servers.** Every change runs the full acceptance suite
  against a NetBird deployment built from the pinned server version.
- **Plans you can trust.** No perpetual diffs, no inconsistent results after
  apply, clear errors instead of crashes.
- **Releases you can verify.** Semantic versions, a changelog for every release,
  GPG-signed checksums, signed commits.
- **Open to upstream.** This project started as a fork of
  [netbirdio/terraform-provider-netbird](https://github.com/netbirdio/terraform-provider-netbird);
  upstream changes are merged in regularly and fixes that apply there are offered
  back. [CHANGELOG.md](CHANGELOG.md) names the upstream commit each release is
  based on.

## Maintenance

- Upstream is merged in through a pull request (`git merge upstream/main`); history
  on `main` is never rewritten.
- A ported upstream pull request keeps its author and links the original.
- Issues and pull requests here track what the fork changes; upstream issues are
  linked from them.
- Releases are tagged `vX.Y.Z` on `main` by the owner; the release workflow builds
  and signs them with GoReleaser.

## About

A Terraform Provider for managing your [NetBird](https://netbird.io) Account and its resources.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.23
- [NetBird Account](https://docs.netbird.io/)

## Building The Provider

1. Clone the repository
1. Enter the repository directory
1. Build the provider using the Go `install` command:

```shell
go install
```

## Adding Dependencies

This provider uses [Go modules](https://github.com/golang/go/wiki/Modules).
Please see the Go documentation for the most up to date information about using Go modules.

To add a new dependency `github.com/author/dependency` to your Terraform provider:

```shell
go get github.com/author/dependency
go mod tidy
```

Then commit the changes to `go.mod` and `go.sum`.

## Using the provider

Check usage in [docs](./docs/index.md).

## Developing the Provider

If you wish to work on the provider, you'll first need [Go](http://www.golang.org) installed on your machine (see [Requirements](#requirements) above).

To compile the provider, run `go install`. This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

To generate or update documentation, run `make generate`.

There are two test suites, separated by the `e2e` build tag.

`make test` runs everything that needs nothing but Go — the conversion functions
each resource maps its API type through, the filter matching every data source
scores on, and the provider's own configuration. It takes about a second and needs
no Docker and no Terraform CLI, because the acceptance tests and the harness that
starts a deployment for them are not compiled into that build at all.

`make testacc` runs the acceptance suite as well, against a live NetBird
deployment that the harness brings up. It needs Docker and a Terraform CLI.

```shell
make test      # fast: no deployment
make testacc   # everything, against a deployment
```

Both switches in `testacc` are load-bearing: `-tags e2e` compiles the harness and
the acceptance tests in, and `TF_ACC=1` is terraform-plugin-testing's own gate.
Dropping either one runs the fast suite and reports success without having tested
a deployment.

*Note:* Acceptance tests create real resources, and often cost money to run
against a hosted NetBird instance.
