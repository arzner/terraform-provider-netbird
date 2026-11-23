#!/bin/bash
# Build the NetBird server, proxy and client images of the newest release of
# the minor line before the one go.mod pins, from that release's module
# source with the e2e harness Dockerfiles, into the local Docker daemon, and
# export references the harness uses as they are (they contain a "/", and
# testcontainers runs a local image without pulling). The provider stays
# compiled against the pinned library: this is the "older server" case.
set -euo pipefail

module=github.com/netbirdio/netbird
pinned=$(go list -m -f '{{.Version}}' "$module")
if ! [[ $pinned =~ ^v0\.([0-9]+)\.[0-9]+$ ]]; then
  echo "::error::unexpected pinned version $pinned"
  exit 1
fi
previous_minor=$((BASH_REMATCH[1] - 1))
version=$(gh api "repos/netbirdio/netbird/releases?per_page=100" --jq '.[] | select(.draft == false and .prerelease == false) | .tag_name' |
  grep -E "^v0\.${previous_minor}\.[0-9]+$" | sort -V | tail -1)
if ! [[ $version =~ ^v0\.${previous_minor}\.[0-9]+$ ]]; then
  echo "::error::no release of v0.${previous_minor}.x found"
  exit 1
fi
echo "pinned $pinned, previous minor release $version"
root=$(go mod download -json "$module@$version" | jq -r .Dir)
test -d "$root"

build() {
  local name=$1 dockerfile=$2 ref="local/netbird-e2e-$1:$version"
  docker build -q -f "$root/$dockerfile" -t "$ref" "$root" >/dev/null
  echo "$3=$ref" >> "$GITHUB_ENV"
  echo "built $ref"
}
build combined combined/Dockerfile.multistage NB_E2E_COMBINED_IMAGE
build proxy proxy/Dockerfile.multistage NB_E2E_PROXY_IMAGE
build client e2e/harness/Dockerfile.client NB_E2E_CLIENT_IMAGE
echo "NB_PREVIOUS_VERSION=$version" >> "$GITHUB_ENV"
