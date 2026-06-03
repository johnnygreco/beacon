#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'EOF'
Usage:
  scripts/publish.sh <version>
  scripts/publish.sh --rollback-plan <version>

Environment:
  GITHUB_TOKEN            GitHub token used by gh and GoReleaser.
  PUBLISH_AUTO_ROLLBACK   Set to 0 to print rollback commands instead of trying them after a release failure.
EOF
}

release_tag() {
    local version="${1#v}"
    printf 'v%s\n' "$version"
}

validate_version() {
    local version="${1#v}"
    if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "Error: version must use x.y.z format, got '$1'."
        exit 1
    fi
}

rollback_plan() {
    local tag
    tag="$(release_tag "$1")"
    cat <<EOF
Rollback steps for ${tag}:
  gh release delete ${tag} --cleanup-tag --yes
  git push origin :refs/tags/${tag}
  git tag -d ${tag}
EOF
}

rollback_pushed_release() {
    local failed=0

    echo ""
    echo "Attempting rollback for ${TAG}..."
    if command -v gh >/dev/null 2>&1; then
        if ! gh release delete "$TAG" --cleanup-tag --yes; then
            failed=1
        fi
    else
        failed=1
    fi

    if [ "$failed" -ne 0 ]; then
        echo "Falling back to deleting the remote tag. If a GitHub release was created, delete it manually."
        if ! git push origin ":refs/tags/${TAG}"; then
            failed=1
        else
            failed=0
        fi
    fi

    git tag -d "$TAG" >/dev/null 2>&1 || true

    if [ "$failed" -ne 0 ]; then
        echo ""
        echo "Automatic rollback did not complete. Run the rollback plan manually:"
        rollback_plan "$VERSION"
        return 1
    fi
    echo "Rolled back ${TAG}. Re-run publish after fixing the failure."
}

need_tool() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "Error: required command not found: $1"
        exit 1
    fi
}

case "${1:-}" in
    -h|--help)
        usage
        exit 0
        ;;
    --rollback-plan)
        if [ $# -ne 2 ]; then
            usage
            exit 1
        fi
        validate_version "$2"
        rollback_plan "$2"
        exit 0
        ;;
    "")
        usage
        exit 1
        ;;
esac

VERSION="$1"
validate_version "$VERSION"

# Use gh CLI token if GITHUB_TOKEN is not set
if [ -z "${GITHUB_TOKEN:-}" ]; then
    if command -v gh &>/dev/null; then
        GITHUB_TOKEN="$(gh auth token 2>/dev/null)" || true
    fi
    if [ -z "${GITHUB_TOKEN:-}" ]; then
        echo "Error: GITHUB_TOKEN is not set and gh CLI is not authenticated."
        echo "Either: export GITHUB_TOKEN=<token>"
        echo "    or: gh auth login"
        exit 1
    fi
    export GITHUB_TOKEN
fi

# Normalize: strip leading 'v' if provided, we'll add it
VERSION="${VERSION#v}"
TAG="$(release_tag "$VERSION")"

need_tool git
need_tool goreleaser
need_tool zig

# Ensure working tree is clean
if [ -n "$(git status --porcelain)" ]; then
    echo "Error: working tree is not clean. Commit or stash changes first."
    exit 1
fi

# Ensure we're on main
BRANCH="$(git branch --show-current)"
if [ "$BRANCH" != "main" ]; then
    echo "Error: must be on main branch (currently on '$BRANCH')."
    exit 1
fi

# Ensure local main is synced with remote
git fetch origin main --quiet
LOCAL="$(git rev-parse HEAD)"
REMOTE="$(git rev-parse origin/main)"
if [ "$LOCAL" != "$REMOTE" ]; then
    echo "Error: local main ($LOCAL) differs from origin/main ($REMOTE). Pull or push first."
    exit 1
fi

# Check tag doesn't already exist
if git rev-parse "$TAG" >/dev/null 2>&1 || git ls-remote --exit-code --tags origin "refs/tags/${TAG}" >/dev/null 2>&1; then
    echo "Error: tag $TAG already exists."
    echo ""
    echo "To delete the tag and retry:"
    rollback_plan "$VERSION"
    echo "  make publish VERSION=${VERSION}"
    exit 1
fi

echo "Publishing beacon $TAG..."

# Create tag locally first (don't push yet)
git tag -a "$TAG" -m "Release $TAG"

# Verify the build succeeds before pushing anything
echo "Verifying build..."
if ! goreleaser build --clean; then
    echo ""
    echo "Error: build failed. Removing local tag $TAG."
    git tag -d "$TAG"
    exit 1
fi

# Build passed — push the tag and publish the release
git push origin "$TAG"
if ! goreleaser release --clean; then
    echo ""
    echo "Error: GoReleaser failed after ${TAG} was pushed."
    if [ "${PUBLISH_AUTO_ROLLBACK:-1}" = "1" ]; then
        rollback_pushed_release || true
    else
        rollback_plan "$VERSION"
    fi
    exit 1
fi

echo ""
echo "Released beacon $TAG"
echo "https://github.com/johnnygreco/beacon/releases/tag/$TAG"
