#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:?Usage: $0 <version> (e.g. 0.1.0)}"

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
TAG="v${VERSION}"

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
if git rev-parse "$TAG" >/dev/null 2>&1; then
    echo "Error: tag $TAG already exists."
    echo ""
    echo "To delete the tag and retry:"
    echo "  git tag -d $TAG              # delete local tag"
    echo "  git push origin :refs/tags/$TAG   # delete remote tag"
    echo "  make publish VERSION=${VERSION}"
    exit 1
fi

echo "Publishing beacon $TAG..."

git tag -a "$TAG" -m "Release $TAG"
git push origin "$TAG"

goreleaser release --clean

echo ""
echo "Released beacon $TAG"
echo "https://github.com/johnnygreco/beacon/releases/tag/$TAG"
