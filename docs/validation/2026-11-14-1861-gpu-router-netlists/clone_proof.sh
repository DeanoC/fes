#!/usr/bin/env bash
# Does this record verify from nothing but a clone?
#
# The condition-3 check used to open files in a build scratch directory, so
# "the record verifies" quietly meant "it verifies on the machine that ran
# the campaign". This runs the checks inside a fresh clone from the remote,
# from a working directory of / so that no relative path can resolve against
# the checkout they were prepared in, and with --single-branch so that
# #1862's merged branch is absent -- the state after someone deletes it.
#
#   bash clone_proof.sh <branch> <clone dir>
set -u
BRANCH="${1:?the branch to clone}"
CLONE="${2:?where to put the clone}"
rm -rf "$CLONE"
git clone -q --single-branch --branch "$BRANCH" https://github.com/DeanoC/fes "$CLONE"
echo "clone rc=$? (--single-branch: no sibling refs, so #1862's merged branch is absent)"
R="$CLONE/docs/validation/2026-11-14-1861-gpu-router-netlists"
cd /
echo "head $(git -C "$CLONE" rev-parse HEAD)"
echo "refs in the clone: $(git -C "$CLONE" for-each-ref --format='%(refname)' refs/remotes | tr '\n' ' ')"
echo
echo "== vendor_cond3.py --check =="
python3 "$R/vendor_cond3.py" --cond3 "$R/reports-cond3/cond3.json" --into "$R/reports-cond3" --check > /tmp/vendor.$$ 2>&1
echo "vendor_cond3.py rc=$?"
tail -1 /tmp/vendor.$$
echo
echo "== seals_at_head.py =="
python3 "$R/seals_at_head.py" --cond3 "$R/reports-cond3/cond3.json" --tree "$CLONE" \
  --design-move "$R/reports-preserve/seal-to-head-design.json" \
  --route-inputs "$R/reports-preserve/seal-to-head-route-inputs.json" \
                 "$R/reports-preserve/seal2-to-head-route-inputs.json" > /tmp/seals.$$ 2>&1
echo "seals_at_head.py rc=$?"
cat /tmp/seals.$$
echo "rows matching their own input digests: $(grep -c 'input digests match this head' /tmp/seals.$$)"
echo "rows carried by the design anchor:     $(grep -c 'design identity unchanged between the seal tree' /tmp/seals.$$)"
echo "lines of this run naming the deleted scratch: $(cat /tmp/vendor.$$ /tmp/seals.$$ | grep -c '/data/fes1861')"
echo
echo "== check_shas.py, with #1862's branch absent =="
python3 "$R/check_shas.py" --root "$R" --repo "$CLONE" > /tmp/shas.$$ 2>&1
echo "check_shas.py rc=$?"
echo "objects this record cites that do not resolve here: $(grep -c 'DOES NOT RESOLVE' /tmp/shas.$$)"
grep 'DOES NOT RESOLVE' /tmp/shas.$$
echo
echo "== the same, after fetching #1862's merged branch =="
git -C "$CLONE" fetch -q origin refs/heads/deano/gpu-router-default
echo "fetch rc=$?"
python3 "$R/check_shas.py" --root "$R" --repo "$CLONE" > /tmp/shas2.$$ 2>&1
echo "check_shas.py rc=$?"
tail -1 /tmp/shas2.$$
rm -f /tmp/vendor.$$ /tmp/seals.$$ /tmp/shas.$$ /tmp/shas2.$$
