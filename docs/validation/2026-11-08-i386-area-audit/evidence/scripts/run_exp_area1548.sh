#!/bin/bash
# run_exp_area1548.sh REPO COMMIT SEAL_PROJECT
# The parallel_case fit: Quartus on a seal's own Yosys netlist (SEAL_PROJECT/pc386_system.v,
# from the producer at COMMIT), with each Yosys $pmux casez marked with Quartus's
# "// synthesis parallel_case" directive in place of the (* parallel_case *) attribute.
# Writes ./wt-exp (a detached worktree, so the project's relative paths resolve) and
# runs the compile there; the reports land in its build/fes-pc386-quartus/project/output_files.
set -eu
repo=$1 commit=$2 seal=$3
wt=$PWD/wt-exp
git -C "$repo" worktree add --detach "$wt" "$commit"
d=$wt/sources/misteross/build/fes-pc386-quartus; mkdir -p "$d/project"
cp "$seal/top.qsf" "$seal/top.qpf" "$d/project/"
python3 - "$seal/pc386_system.v" "$d/project/pc386_system.v" <<'PY'
import re, sys
t = open(sys.argv[1]).read()
pat = r'\(\* parallel_case \*\)\n(\s*)casez \(s\)\n'
print("casez functions marked:", len(re.findall(pat, t)))   # 613 for seal 416e332f
open(sys.argv[2], "w").write(re.sub(pat, r'casez (s) // synthesis parallel_case\n', t))
PY
export QUARTUS_ROOTDIR=${QUARTUS_ROOTDIR:-/home/deano/intelFPGA_lite/17.0/quartus}
. "$(cd "$(dirname "$0")" && pwd)/guard_area1548.sh"
cd "$d/project"
: > ../quartus.log
set +e
guarded_run ../quartus.log nice -n 5 "$QUARTUS_ROOTDIR/bin/quartus_sh" --flow compile top
exit $?
