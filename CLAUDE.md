# Working in FES

## Rules for all agents

Never rebase, force-push, or amend pushed commits on any PR branch or main. Update a PR branch by merging main into it. Don't push to a PR branch you weren't asked to work on.

All GPU work goes through the `gpu-run` wrapper. Never pin a GPU vendor.

Nobody kills a GPU job, and nobody touches Deano's harness (~/harnesses on Powerboat) or its processes.

Never relax a seal or timing check; stop and report instead.

Delete superseded code instead of keeping compatibility shims or "kept for older kits" branches.

A core's locked nextpnr is only its last known good, not a required pin. If a newer nextpnr closes that core's timing, move the lock forward instead of changing RTL to fit the old version. Timing and seal checks still apply.

## PR scope

- One scope per PR. Don't bundle unrelated fixes.
- The PR body says `Closes #N` for the issue it resolves. Agents may open that issue themselves when opening the PR.
- Small, self-explanatory PRs may skip the issue and the `Closes #N` line.
- Before opening a PR, check for an open PR that already covers the change, and don't duplicate it.
- Stay inside the repo and the scope the task names.

## Shared kits and Powerboat (READ FIRST)

Outside agent sessions have ignored these rules. Obey them.

1. MUST claim the kit before you touch it, and log the result when you
   finish. There is no separate Powerboat host claim. Record owner, PR,
   and start time in that kit's Powerboat status file:
   `~/kit-status/kitA-STATUS.md` or `~/kit-status/kitB-STATUS.md`. That
   log is the single source of truth. NEVER take a kit that is leased or
   claimed by someone else. NEVER use a Slack reservation, and NEVER
   require a person to be present.
2. Kits A and B are both designated test kits. They are interchangeable.
3. When a task installs a bundle, install that PR bundle on the kit's
   running image. A core-only run that does not change the bundle does not
   install one. Software goes before the image: before an image change,
   activate a bundle the new image accepts (`fes-update` refuses an image
   whose menu interfaces the selected bundle lacks). After use, restore the
   designated baseline image and bundle the status file names, and log that
   restore. Activating the baseline bundle does not change the booted image.
   If the booted image is not the named baseline, first activate the
   baseline bundle if the running image accepts it, then restore that image
   and verify the boot; otherwise restore and verify the image, then
   activate the baseline bundle. Verify the active bundle before logging the
   kit restored. Confirm the kit is idle and lease-free before you release
   it.
4. MUST keep Kit B's `u-boot.txt` line `usb-storage.quirks=152d:0583:i`.
5. Destructive actions need Deano's explicit OK: reimage, an image or
   boot-config change, killing other people's processes, and closing PRs.
6. These stay pre-approved: deleting old images (keep the running image
   and the previous one), SD reburn, and bundle install-then-restore.
   Never delete a factory or known-good rollback image. Before an SD
   reburn, verify the device path is that kit's SD card.
7. NEVER use broad `pkill` or `killall` patterns on Powerboat (for example
   `pkill -f run.sh`). Kill only PIDs you started. Give your scripts
   distinctive names.
8. NEVER run interactive OAuth or browser logins on Powerboat. Its harness
   `BROWSER` setting makes `xdg-open` loop and fork-bomb the host.

Firmware, ROMs, media and software may be placed on either kit. Use the
applicable kit lease and supported install, activation or media-delivery path;
these rules do not require keeping those artifacts on a host or Powerboat.

GPU jobs follow the [Shared GPUs](AGENTS.md#shared-gpus) section.

nextpnr routing on HIP, CUDA, Metal and CPU gives bit-identical route outcomes. HIP,
CUDA and Metal timing and route evidence are interchangeable: references, pins, seed
results and replays from any of them remain valid on the others. This is route-result
equivalence only; production builds, promotion and seal evidence still require an
authenticated live HIP or CUDA backend as producer validators require, and they do
not accept Metal yet. Registered producers reject CPU-reference fallback
(`NEXTPNR_REQUIRE_GPU=1`), so CPU replays can inform experiments only. HIP, CUDA
and Metal builds keep distinct `BUILD_ID`s and artifact identities because backend
is part of functional execution identity, even though their timing and route
evidence is interchangeable. Among current core
package producers, Apple II, Atari ST, C64, Coleco socket-v2, SG-1000, SMS,
Spectrum and ZX81 accept `--gpu-router {HIP,CUDA}`. Catch, Menu, Pong, RAM Tester
and RISC-V accept only `--gpu-device`; their authenticated toolchain determines
the backend. On a CUDA host, select CUDA and its matching toolchain for a
router-selectable producer; a device-only producer can use CUDA only with an
authenticated CUDA-built toolchain. `gpu-run` leases a device, not a backend;
record the backend in the build record. Never require a separate HIP run or
discount CUDA or Metal evidence. Never set `GPU_LEASE_IDS` per job (it is a box
inventory override).

Read [AGENTS.md](AGENTS.md) for every other working rule.
