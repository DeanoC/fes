# G5: Quake on the DX's RTL with the x87 fix

**Pending.** The run is going on Powerboat (`/data/projects/i486dx-s1/quake/`,
harness `i486dxs1_quake.sh`): the DX head `494479998` with the fix
(`quake-diag/x87-fired-fix.patch`) as a scratch merge, `machine_tb` built by
`sim_fes_pc386.py --suite cpu-profile --cpu-core i486dx`, round 2's command
line, `--x87 1 --rtl-at-prompt 1 --rtl-for 20`, frames every 1.0 s. This file
gets the result against G5's pass rule.
