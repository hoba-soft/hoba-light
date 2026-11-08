#!/usr/bin/env bash
set -euo pipefail
version="$RELEASE_VERSION"
tag="v$version"
mkdir -p collected dist
gh run download "$GITHUB_RUN_ID" --repo "$GITHUB_REPOSITORY" --pattern 'hobalight-*' --dir collected
for platform in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64 windows-arm64; do
  system="${platform%-*}"
  arch="${platform##*-}"
  suffixes=(tar.gz)
  case "$system" in
    linux) suffixes+=(deb rpm) ;;
    darwin) suffixes+=(pkg) ;;
    windows) suffixes=(zip msi) ;;
  esac
  for suffix in "${suffixes[@]}"; do
    name="hobalight_${version}_${system}_${arch}.$suffix"
    cp "collected/hobalight-$platform/$name" "dist/$name"
  done
done
(cd dist && sha256sum hobalight_* > SHA256SUMS)
gh api "repos/$GITHUB_REPOSITORY/commits/$RELEASE_COMMIT/pulls" \
  --jq 'map(select(.merged_at != null and .base.ref == "main")) | sort_by(.merged_at) | last | .body // ""' |
  awk '
    /^## Release notes[[:space:]]*$/ { section = 1; next }
    section && /^## / { section = 0 }
    section { print }
  ' > release-notes.txt
if gh release view "$tag" --repo "$GITHUB_REPOSITORY" >/dev/null 2>&1; then
  gh release upload "$tag" dist/* --repo "$GITHUB_REPOSITORY" --clobber
else
  gh release create "$tag" dist/* --repo "$GITHUB_REPOSITORY" --verify-tag --target "$RELEASE_COMMIT" --title "Hoba Light $version" --notes-file release-notes.txt --draft
fi
gh release edit "$tag" --repo "$GITHUB_REPOSITORY" --notes-file release-notes.txt --draft=false
