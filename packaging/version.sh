#!/usr/bin/env bash
set -euo pipefail
major="${VERSION_MAJOR:-0}"
[[ "$major" =~ ^[0-9]+$ ]] || { echo "Invalid major version" >&2; exit 1; }
version="$major.0.0"
if [[ "$GITHUB_REF" == refs/heads/main && "$GITHUB_EVENT_NAME" != pull_request ]]; then
  # Tag this push, without backfilling historical workflow revisions that the
  # Actions token is not permitted to tag.
  range="$GITHUB_SHA"
  if [[ -n "${BEFORE_SHA:-}" && "$BEFORE_SHA" != 0000000000000000000000000000000000000000 ]]; then
    range="$BEFORE_SHA..$GITHUB_SHA"
  else
    range="$GITHUB_SHA^!"
  fi
  tags=()
  while read -r commit; do
    count=$(git rev-list --first-parent --count "$commit")
    tag="v$major.$count.0"
    if git show-ref --verify --quiet "refs/tags/$tag"; then
      [[ "$(git rev-parse "$tag^{commit}")" == "$commit" ]] || { echo "Conflicting tag: $tag" >&2; exit 1; }
    else
      git tag "$tag" "$commit"
      tags+=("refs/tags/$tag")
    fi
    if [[ "$commit" == "$GITHUB_SHA" ]]; then version="$major.$count.0"; fi
  done < <(git rev-list --first-parent --reverse "$range")
  if (( ${#tags[@]} )); then git push --atomic origin "${tags[@]}"; fi
fi
printf 'version=%s\ncommit=%s\n' "$version" "$GITHUB_SHA" >> "$GITHUB_OUTPUT"
