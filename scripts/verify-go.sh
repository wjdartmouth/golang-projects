#!/usr/bin/env bash
# Verify every independent Go module without GitHub Actions.
set -uo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
modules=(holidays-api invoice-gen postal-api jlpt-quiz-api kintai-api price-tracker furigana)
failed=0

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: Go is not installed or not on PATH." >&2
  exit 127
fi

echo "Go toolchain: $(go version)"
for module in "${modules[@]}"; do
  echo
  echo "=== ${module} ==="
  if [[ ! -f "$root/$module/go.mod" ]]; then
    echo "FAIL: missing go.mod in $module" >&2
    failed=$((failed + 1))
    continue
  fi
  for action in build test vet; do
    echo "--- go $action ./... ---"
    if (cd "$root/$module" && go "$action" ./...); then
      echo "PASS: $module $action"
    else
      echo "FAIL: $module $action" >&2
      failed=$((failed + 1))
    fi
  done
done

echo
if ((failed > 0)); then
  echo "Verification failed: $failed check(s)." >&2
  exit 1
fi
echo "Verification passed: all seven modules built, tested and vetted."
