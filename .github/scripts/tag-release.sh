#!/usr/bin/env bash
set -euo pipefail

# Remote tag creation is atomic. Refetch and retry if another passing build
# publishes the same version first; never force-update an existing tag.
for attempt in 1 2 3 4 5; do
  git fetch --tags origin
  latest=$(git tag --list --sort=-version:refname | awk '/^v[01]\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/ && !found { print; found=1 }')

  if [[ -z "$latest" ]]; then
    next=v0.1.0
  else
    if git merge-base --is-ancestor HEAD "$latest"; then
      echo "This commit is already released or superseded by $latest; skipping."
      exit 0
    fi
    if ! git merge-base --is-ancestor "$latest" HEAD; then
      echo "Latest release $latest is not an ancestor of this commit; refusing to release divergent history." >&2
      exit 1
    fi
    IFS=. read -r major minor patch <<< "${latest#v}"
    next="v${major}.${minor}.$((patch + 1))"
  fi

  git tag "$next" HEAD
  if git push origin "refs/tags/$next"; then
    echo "Published $next for $(git rev-parse HEAD)."
    exit 0
  fi
  git tag --delete "$next"
  echo "Tag push failed (attempt $attempt of 5); refetching before retry." >&2
done

echo "Unable to publish a tag after 5 attempts. Check repository write permissions and tag rules." >&2
exit 1
