#!/usr/bin/env bash
# Prints one version's section of a Keep a Changelog file, without its
# heading and without the blank lines around it:
#
#   bash tools/release/changelog-section.sh 0.1.0 [CHANGELOG.md]
#
# Fails when the version has no section or the section is empty, so a
# release tag cannot go out without its notes.
set -euo pipefail

version="${1:?usage: changelog-section.sh <version> [file]}"
file="${2:-CHANGELOG.md}"
[[ -f "$file" ]] || { echo "changelog-section: $file not found" >&2; exit 1; }

# The heading is "## [<version>]" followed by the end of the line or " - date";
# the section ends at the next "## [" heading or the link references.
section="$(awk -v heading="## [$version]" '
  index($0, "## [") == 1 {
    if (found) exit
    if ($0 == heading || index($0, heading " ") == 1) { found = 1; next }
  }
  found && /^\[[^]]+\]: / { exit }
  found { print }
' "$file" | sed -e '/./,$!d')" # $(...) drops the trailing blank lines

if [[ ! "$section" =~ [^[:space:]] ]]; then
  echo "changelog-section: $file has no notes for $version" >&2
  exit 1
fi
printf '%s\n' "$section"
