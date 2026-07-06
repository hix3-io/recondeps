#!/usr/bin/env bash
# Semantic version bumper. Reads VERSION (MAJOR.MINOR.PATCH), bumps the requested
# component, writes it back, and reminds you to update the CHANGELOG.
set -euo pipefail
cd "$(dirname "$0")"

part="${1:-patch}"
cur="$(cat VERSION)"
IFS='.' read -r major minor patch <<< "$cur"

case "$part" in
  major) major=$((major+1)); minor=0; patch=0 ;;
  minor) minor=$((minor+1)); patch=0 ;;
  patch) patch=$((patch+1)) ;;
  *) echo "usage: $0 {major|minor|patch}" >&2; exit 1 ;;
esac

new="${major}.${minor}.${patch}"
echo "$new" > VERSION
echo "VERSION: $cur -> $new"
echo "→ add a '## v$new' section to CHANGELOG.md, then: make build"
