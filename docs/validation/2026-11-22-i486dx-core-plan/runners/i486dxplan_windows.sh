#!/bin/bash
# i486dxplan_windows.sh VARIANT: h1 and h2 windows from the variant's cpu.prof into the record.
V=$1; P=/data/projects/i486dx-plan; R=/home/deano/fes/.claude/worktrees/happy-payne-d3b9ad/docs/validation/2026-11-22-i486dx-core-plan
mkdir -p $R/profiles $R/latency
cd $P/wt-sim-base/sources/misteross
read H M E < <(python3 -c "
import sys;sys.path.insert(0,'.')
from scripts import pc386_cpu_profile as p
s=sorted(p.load(open('$P/sims/$V/cpu.prof').read().splitlines()));print(*s[:3])")
python3 scripts/pc386_cpu_profile.py $P/sims/$V/cpu.prof --from $H --to $M --json $R/profiles/$V-h1.json > $P/sims/$V/h1.txt
python3 scripts/pc386_cpu_profile.py $P/sims/$V/cpu.prof --from $M --to $E --json $R/profiles/$V-h2.json > $P/sims/$V/h2.txt
cp $P/sims/$V/latency-report.json $R/latency/$V.json
head -n 2 $P/sims/$V/h2.txt
