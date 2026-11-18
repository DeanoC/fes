#!/usr/bin/env bash
# What the vendored condition-3 check actually catches, by construction.
#
# Reasoning about what a check catches is how holes survive, so each case
# here perturbs one thing in a *copy* of reports-cond3 and runs the real
# seals_at_head.py against it. The exit code is captured from that command
# directly: piping it into tail would report tail's status instead, which
# has made a failing run read rc=0 in this record before.
#
#   bash counterexamples_cond3.sh <checkout> <scratch dir>
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
TREE="${1:?the checkout whose head the seals must match}"
WORK="${2:?a scratch directory}"
ANCHORS=(--design-move "$HERE/reports-preserve/seal-to-head-design.json"
         --route-inputs "$HERE/reports-preserve/seal-to-head-route-inputs.json"
                        "$HERE/reports-preserve/seal2-to-head-route-inputs.json")

rm -rf "$WORK"; mkdir -p "$WORK"
for case in control edited-c64 removed-c64 edited-zx81 unpinned unvendored; do
    cp -r "$HERE/reports-cond3" "$WORK/$case"
done
python3 - "$WORK" <<'PY'
import json, os, sys
work = sys.argv[1]
# A summary edited after it was vendored: same content, reformatted.
path = f"{work}/edited-c64/summaries/c64/build-summary.json"
text = json.dumps(json.loads(open(path).read())) + "\n"
open(path, "w").write(text)
# A summary that is simply gone.
os.remove(f"{work}/removed-c64/summaries/c64/build-summary.json")
# A summary edited in a row that also has a design anchor behind it.
path = f"{work}/edited-zx81/summaries/zx81/build-summary.json"
text = open(path).read().replace("}", " }", 1)
open(path, "w").write(text)
# Vendored, but with the pin dropped.
path = f"{work}/unpinned/cond3.json"
rows = json.loads(open(path).read())
for row in rows.values():
    row.pop("source_sha256", None)
open(path, "w").write(json.dumps(rows, indent=2, sort_keys=True) + "\n")
# The record as #1862 merged it: every row pointing back into the deleted
# campaign scratch.
path = f"{work}/unvendored/cond3.json"
rows = json.loads(open(path).read())
for row in rows.values():
    row["source"] = row["original_source"]
open(path, "w").write(json.dumps(rows, indent=2, sort_keys=True) + "\n")
PY

for case in control edited-c64 removed-c64 edited-zx81 unpinned unvendored; do
    echo "=== $case ==="
    out="$WORK/$case.out"
    python3 "$HERE/seals_at_head.py" --cond3 "$WORK/$case/cond3.json" \
        --tree "$TREE" "${ANCHORS[@]}" > "$out" 2>&1
    rc=$?
    grep -E "SUBSTITUTED|unpinned|unverifiable|MOVED|seals still match" "$out" \
        | cut -c1-140
    echo "seals_at_head.py rc=$rc"
    echo
done
