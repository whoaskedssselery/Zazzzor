#!/usr/bin/env bash
# Tests for changelog-section.sh: bash tools/release/changelog-section.test.sh
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/CHANGELOG.md" <<'EOF'
# Changelog

## [Unreleased]

## [0.2.0-rc.1] - 2026-12-01

- Candidate.

## [0.2.0] - 2026-12-02

### Fixed

- Second.

## [0.1.0] - 2026-11-21

### Added

- First.

[Unreleased]: https://example.org/compare/v0.2.0...HEAD
EOF

failures=0
check() { # name, expected, actual
  if [[ "$2" == "$3" ]]; then echo "ok   $1"; else
    echo "FAIL $1"; printf '  expected: %q\n  actual:   %q\n' "$2" "$3"; failures=$((failures + 1))
  fi
}
section() { bash "$here/changelog-section.sh" "$1" "$tmp/CHANGELOG.md" 2>/dev/null; }

check "middle section, trimmed" $'### Fixed\n\n- Second.' "$(section 0.2.0)"
check "prerelease is its own section" '- Candidate.' "$(section 0.2.0-rc.1)"
check "last section stops at links" $'### Added\n\n- First.' "$(section 0.1.0)"
if section 0.3.0 >/dev/null; then check "missing version fails" fail ok; else check "missing version fails" ok ok; fi
if section Unreleased >/dev/null; then check "empty section fails" fail ok; else check "empty section fails" ok ok; fi
if section 0.1 >/dev/null; then check "a prefix is not a version" fail ok; else check "a prefix is not a version" ok ok; fi

((failures == 0)) || { echo "$failures failed"; exit 1; }
