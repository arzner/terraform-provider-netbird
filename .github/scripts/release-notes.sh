#!/bin/sh
# Print the CHANGELOG.md section of the version given as vX.Y.Z (the tag).
# Fails when the section is missing or empty, so a release cannot go out
# without its notes.
set -eu
version="${1#v}"
notes=$(awk -v v="$version" '
  /^## \[/ { if (found) exit; found = index($0, "## [" v "]") == 1; next }
  /^\[[^]]+\]: / { if (found) exit; next }
  found { print }
' CHANGELOG.md)
if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
  echo "CHANGELOG.md has no notes for $version" >&2
  exit 1
fi
printf '%s\n' "$notes"
