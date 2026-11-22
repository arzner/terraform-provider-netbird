#!/bin/bash
# Make sure the NetBird images the acceptance suite runs exist in the container
# registry for the netbird version pinned in go.mod, and print digest-pinned
# references as step outputs (combined, proxy, client).
#
# Only trusted runs publish (PUBLISH=true: pushes to main and runs dispatched
# on main); everything else, pull requests included, only reads. An image is
# built only when its tag is confirmed absent, from the module source and the
# Dockerfile the e2e harness itself builds it from; a tag that exists, or whose
# existence cannot be confirmed, is never pushed over. A replaced netbird
# module is never published or used, because its tag would not describe its
# source. Whenever an image is not available for any reason, its output stays
# empty and the harness builds it locally, so this script never fails a run.
set -uo pipefail

module=github.com/netbirdio/netbird
finish() { exit 0; }

version=$(go list -m -f '{{.Version}}' "$module") || finish
replaced=$(go list -m -f '{{if .Replace}}yes{{end}}' "$module") || finish
if [ -n "$replaced" ]; then
  echo "::warning::$module is replaced in go.mod; the harness builds the images from the replacement"
  finish
fi
if ! [[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]]; then
  echo "::warning::unexpected $module version '$version'; the harness builds the images"
  finish
fi
go mod download "$module" || finish
root=$(go list -m -f '{{.Dir}}' "$module") || finish
registry="ghcr.io/${GITHUB_REPOSITORY,,}"
echo "netbird $version from $root"

# exists prints present, absent or unknown for an image reference.
exists() {
  local out
  if out=$(docker buildx imagetools inspect "$1" 2>&1); then
    echo present
  elif grep -Eqi 'not found|manifest unknown|name unknown' <<<"$out"; then
    echo absent
  else
    echo unknown
  fi
}

ensure() {
  local name=$1 dockerfile=$2 ref digest state
  ref="$registry/e2e-$name:$version"
  state=$(exists "$ref")
  case "$state" in
    present) ;;
    absent)
      if [ "${PUBLISH:-false}" != true ]; then
        echo "$ref is not published yet and this run does not publish; the harness builds $name"
        return 0
      fi
      echo "building $ref from $dockerfile"
      if ! docker buildx build -f "$root/$dockerfile" -t "$ref" \
          --label "org.opencontainers.image.source=https://github.com/$GITHUB_REPOSITORY" \
          --label "org.opencontainers.image.version=$version" \
          --push "$root"; then
        echo "::warning::building $ref failed; the harness builds $name"
        return 0
      fi
      ;;
    *)
      echo "::warning::could not tell whether $ref exists; the harness builds $name"
      return 0
      ;;
  esac
  digest=$(docker buildx imagetools inspect "$ref" --format '{{json .Manifest}}' 2>/dev/null | jq -r '.digest // empty' 2>/dev/null) || digest=
  if ! [[ $digest =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "::warning::no digest for $ref; the harness builds $name"
    return 0
  fi
  echo "$name=$registry/e2e-$name@$digest" | tee -a "$GITHUB_OUTPUT"
  return 0
}

# The same Dockerfiles the harness builds from (e2e/harness/{combined,proxy,client}.go).
ensure combined combined/Dockerfile.multistage
ensure proxy proxy/Dockerfile.multistage
ensure client e2e/harness/Dockerfile.client
finish
