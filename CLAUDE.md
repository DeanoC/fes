# Working in FES

## Shared kits and Powerboat (READ FIRST)

Outside agent sessions have ignored these rules. Obey them.

1. MUST claim the kit in its Powerboat status file before you touch it,
   and log the result there when you finish. Record owner, PR, and start
   time. The files are `~/kit-status/kitA-STATUS.md` and
   `~/kit-status/kitB-STATUS.md`. That log is the single source of truth.
   NEVER take a kit that is leased or claimed by someone else. NEVER use a
   Slack reservation, and NEVER require a person to be present.
2. Kits A and B are both designated test kits. They are interchangeable.
3. MUST, after use, restore the kit's designated baseline image and bundle.
   The status file names those IDs. Confirm the kit is idle and lease-free
   before you release it.
4. MUST keep Kit B's `u-boot.txt` line `usb-storage.quirks=152d:0583:i`.
5. Destructive actions need Deano's explicit OK: reimage, an image or
   boot-config change, killing other people's processes, and closing PRs.
6. These stay pre-approved: deleting old images (keep the running image
   and the previous one), SD reburn, and bundle install-then-restore.
7. NEVER use broad `pkill` or `killall` patterns on Powerboat (for example
   `pkill -f run.sh`). Kill only PIDs you started. Give your scripts
   distinctive names.
8. NEVER run interactive OAuth or browser logins on Powerboat. Its harness
   `BROWSER` setting makes `xdg-open` loop and fork-bomb the host.

Read [AGENTS.md](AGENTS.md) for every other working rule.
