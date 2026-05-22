#!/usr/bin/env bash
set -euo pipefail

profile="${1:-coverage.txt}"
thresholds="${2:-coverage.thresholds}"

if [ ! -f "$profile" ]; then
  echo "coverage profile not found: $profile" >&2
  exit 1
fi

if [ ! -f "$thresholds" ]; then
  echo "coverage thresholds not found: $thresholds" >&2
  exit 1
fi

module="$(go list -m)"
summary="$(mktemp)"
trap 'rm -f "$summary"' EXIT

awk '
  NR == 1 { next }
  {
    split($1, location, ":")
    pkg = location[1]
    sub(/\/[^\/]+$/, "", pkg)
    statements = $2 + 0
    count = $3 + 0
    covered = count > 0 ? statements : 0
    pkgStatements[pkg] += statements
    pkgCovered[pkg] += covered
    totalStatements += statements
    totalCovered += covered
  }
  END {
    for (pkg in pkgStatements) {
      printf "%s %.1f\n", pkg, 100 * pkgCovered[pkg] / pkgStatements[pkg]
    }
    if (totalStatements > 0) {
      printf "total %.1f\n", 100 * totalCovered / totalStatements
    }
  }
' "$profile" | sort > "$summary"

failed=0
echo "Coverage thresholds:"
while read -r pkg min rest; do
  case "$pkg" in
    ""|\#*) continue ;;
  esac
  if [ -n "${rest:-}" ]; then
    echo "invalid threshold line: $pkg $min $rest" >&2
    exit 1
  fi
  pkg="${pkg//\$\{MODULE\}/$module}"
  actual="$(awk -v pkg="$pkg" '$1 == pkg { print $2 }' "$summary")"
  if [ -z "$actual" ]; then
    echo "  FAIL $pkg: no coverage data, required >= ${min}%"
    failed=1
    continue
  fi
  if awk -v actual="$actual" -v min="$min" 'BEGIN { exit !(actual + 0.0001 < min) }'; then
    echo "  FAIL $pkg: ${actual}% < ${min}%"
    failed=1
  else
    echo "  PASS $pkg: ${actual}% >= ${min}%"
  fi
done < "$thresholds"

if [ "$failed" -ne 0 ]; then
  echo "coverage thresholds failed" >&2
  exit 1
fi
