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

CASES="control edited-c64 removed-c64 edited-zx81 swapped-zx81 unpinned unvendored"
rm -rf "$WORK"; mkdir -p "$WORK"
for case in $CASES; do
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
# One artefact's summary filed under another's row, pin updated to match, so
# that only the BUILD_ID cross-check can catch it.
import hashlib, shutil
work_case = f"{work}/swapped-zx81"
shutil.copy2(f"{work_case}/summaries/zx80/build-summary.json",
             f"{work_case}/summaries/zx81/build-summary.json")
path = f"{work_case}/cond3.json"
rows = json.loads(open(path).read())
rows["zx81"]["source_sha256"] = hashlib.sha256(
    open(f"{work_case}/summaries/zx81/build-summary.json", "rb").read()).hexdigest()
open(path, "w").write(json.dumps(rows, indent=2, sort_keys=True) + "\n")
PY
# A perturbation step that dies leaves a case unperturbed, and an unperturbed
# case passes. That happened here once and read as four clean passes.
[ $? -eq 0 ] || { echo "the perturbation step failed; no case below is trustworthy"; exit 2; }

for case in $CASES; do
    echo "=== $case ==="
    out="$WORK/$case.out"
    python3 "$HERE/seals_at_head.py" --cond3 "$WORK/$case/cond3.json" \
        --tree "$TREE" "${ANCHORS[@]}" > "$out" 2>&1
    rc=$?
    grep -E "SUBSTITUTED|unpinned|unverifiable|MOVED|ABSENT|WRONG SEAL|seals still match" "$out" \
        | cut -c1-140
    echo "seals_at_head.py rc=$rc"
    echo
done

# vendor_cond3.py --check is a claim of its own, so give it a counterexample
# too, and prove the no-argument form is the idempotent operation it claims.
echo "=== vendor_cond3.py --check on the edited copy ==="
python3 "$HERE/vendor_cond3.py" --cond3 "$WORK/edited-c64/cond3.json" \
    --into "$WORK/edited-c64" --check > "$WORK/vendor-edited.out" 2>&1
rc=$?
grep -E "PROBLEMS|rows verified|  c64" "$WORK/vendor-edited.out" | cut -c1-140
echo "vendor_cond3.py rc=$rc"
echo
echo "=== vendor_cond3.py re-run on an already-vendored collection ==="
python3 "$HERE/vendor_cond3.py" --cond3 "$WORK/control/cond3.json" \
    --into "$WORK/control" > "$WORK/vendor-again.out" 2>&1
rc=$?
tail -1 "$WORK/vendor-again.out"
echo "vendor_cond3.py rc=$rc"
diff -q "$HERE/reports-cond3/cond3.json" "$WORK/control/cond3.json" \
    && echo "cond3.json unchanged by the re-run" || echo "cond3.json CHANGED by the re-run"
