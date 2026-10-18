# Working in FES

## Shared kits and Powerboat (READ FIRST)

Outside agent sessions have ignored these rules. Obey them.

1. MUST claim the kit before you touch it, and log the result when you
   finish. There is no separate Powerboat host claim. Record owner, PR,
   and start time in that kit's Powerboat status file:
   `~/kit-status/kitA-STATUS.md` or `~/kit-status/kitB-STATUS.md`. That
   log is the single source of truth. NEVER take a kit that is leased or
   claimed by someone else. NEVER use a Slack reservation, and NEVER
   require a person to be present. GPU jobs on Powerboat go through the
   `gpu-run` wrapper: one `flock` lease per GPU, and `gpu-status` shows
   the holders.
2. Kits A and B are both designated test kits. They are interchangeable.
3. MUST install a PR bundle on the kit's running image. After use, restore
   the designated baseline image and bundle the status file names, and log
   that restore. Confirm the kit is idle and lease-free before you release
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

Read [AGENTS.md](AGENTS.md) for every other working rule.
