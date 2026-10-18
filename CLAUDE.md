# Working in FES

## Shared kits and Powerboat (READ FIRST)

Outside agent sessions have ignored these rules. Obey them.

1. MUST check and claim a kit in its status log before you touch it.
   Kits A and B, and the Powerboat dev host, are shared by several agents.
   Record owner, PR, and start time. The logs are Powerboat
   `~/kit-status/kitA-STATUS.md` and `~/kit-status/kitB-STATUS.md`.
   When you are done, release the kit and log the result.
2. NEVER take a kit that is leased or claimed by someone else.
3. MUST restore the kit's normal image and bundle after any HIL.
   The status log is the source of truth for the current normal IDs.
   Confirm the kit is idle and lease-free before you release it.
4. NEVER swap a full image, delete an image, or edit `u-boot.txt` or the
   FAT boot config without Deano's explicit OK. MUST keep Kit B's
   `u-boot.txt` line `usb-storage.quirks=152d:0583:i`. Do not remove it
   or change it.
5. NEVER use broad `pkill` or `killall` patterns on Powerboat (for example
   `pkill -f run.sh`). Kill only PIDs you started. Give your scripts
   distinctive names.
6. NEVER run interactive OAuth or browser logins on Powerboat. Its harness
   `BROWSER` setting makes `xdg-open` loop and fork-bomb the host.

Read [AGENTS.md](AGENTS.md) for every other working rule.
