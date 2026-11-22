#!/bin/bash
# Make sure the NetBird images the acceptance suite runs exist in the container
# registry for the netbird version pinned in go.mod, and print digest-pinned
# references as step outputs (combined, proxy, client).
#
# An image is built only when its tag is missing, from the module source and
# the Dockerfile the e2e harness itself builds it from. A tag is never
# overwritten. When pushing is not allowed (PUSH=false) or a build or push
# fails, the image's output stays empty and the harness falls back to building
# it locally, so this script cannot make a run fail.
set -uo pipefail

module=github.com/netbirdio/netbird
version=$(go list -m -f '{{.Version}}' "$module") || exit 0
go mod download "$module" || exit 0
root=$(go list -m -f '{{.Dir}}' "$module") || exit 0
registry="ghcr.io/${GITHUB_REPOSITORY,,}"
echo "netbird $version from $root"

ensure() {
  local name=$1 dockerfile=$2 ref digest
  ref="$registry/e2e-$name:$version"
  if ! docker buildx imagetools inspect "$ref" >/dev/null 2>&1; then
    if [ "${PUSH:-false}" != true ]; then
      echo "$ref is missing and this run may not push it; the harness builds $name"
      return
    fi
    echo "building $ref from $dockerfile"
    if ! docker buildx build -f "$root/$dockerfile" -t "$ref" \
        --label "org.opencontainers.image.source=https://github.com/$GITHUB_REPOSITORY" \
        --label "org.opencontainers.image.version=$version" \
        --push "$root"; then
      echo "::warning::building $ref failed; the harness builds $name"
      return
    fi
  fi
  digest=$(docker buildx imagetools inspect "$ref" --format '{{json .Manifest}}' | jq -r .digest) || return
  if [ -z "$digest" ] || [ "$digest" = null ]; then
    echo "::warning::no digest for $ref; the harness builds $name"
    return
  fi
  echo "$name=$registry/e2e-$name@$digest" | tee -a "$GITHUB_OUTPUT"
}

# The same Dockerfiles the harness builds from (e2e/harness/{combined,proxy,client}.go).
ensure combined combined/Dockerfile.multistage
ensure proxy proxy/Dockerfile.multistage
ensure client e2e/harness/Dockerfile.client
