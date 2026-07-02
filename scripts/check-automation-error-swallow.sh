#!/usr/bin/env bash
# LLD-01 §3 CI gate: "в internal/engine запрещён паттерн присваивания
# результата createTask/applyTransition в blank identifiers на путях
# automation" — a side-effecting call whose error is thrown away with `_`
# either never surfaces its failure (createTask) or silently skips a state
# move (applyTransition), the exact silent-loss shape LLD-01/HLD I-3 closes.
#
# Scans internal/engine/*.go (production code only, *_test.go excluded — a
# test faking a swallow to set up a fixture is not the anti-pattern this gate
# is about) for the two blank-assignment shapes:
#   _, _ = e.createTask(        (createTask returns (string, error))
#   _ = e.applyTransition(      (applyTransition returns error only)
#
# A pre-existing hit that is a deliberate, reviewed exception goes in
# scripts/automation-error-swallow-allowlist.txt as "path:line" — one per
# line, '#' comments allowed. New hits not in the allowlist fail the build.
set -euo pipefail
cd "$(dirname "$0")/.."

ALLOWLIST="scripts/automation-error-swallow-allowlist.txt"
PATTERN='_,[[:space:]]*_[[:space:]]*=[[:space:]]*e\.createTask\(|(^|[^_[:alnum:]])_[[:space:]]*=[[:space:]]*e\.applyTransition\('

allowed() {
    # $1 = "path:line"
    [[ -f "$ALLOWLIST" ]] || return 1
    grep -qxF "$1" "$ALLOWLIST"
}

violations=0
while IFS=: read -r file line _; do
    key="$file:$line"
    if allowed "$key"; then
        continue
    fi
    echo "BLANK SIDE-EFFECT ERROR: $key"
    sed -n "${line}p" "$file" | sed 's/^/    /'
    violations=$((violations + 1))
done < <(grep -nE "$PATTERN" internal/engine/*.go 2>/dev/null | grep -v '_test\.go:' || true)

if [[ "$violations" -gt 0 ]]; then
    echo
    echo "$violations blank-assignment side-effect error(s) found in internal/engine."
    echo "Either handle the error, or — for a deliberate, reviewed exception —"
    echo "add \"path:line\" to $ALLOWLIST with a comment explaining why."
    exit 1
fi

echo "check-automation-error-swallow: clean (0 unallowed blank side-effect assignments)"
