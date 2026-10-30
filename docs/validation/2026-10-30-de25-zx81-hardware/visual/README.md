# Physical ZX81 HDMI observations

These native captured frames belong to the SOF recorded by the retained
programmer log with Quartus checksum `0x0649BFAE`, FPGA source
`0534e1ec71e04a00b377e4dd0be8d64391b1dbb5`, and computer build
`489319c9b6eeb20f2b582dca10edf3cb`. The retained logs do not bind the
session to a USB serial, board UID or JTAG IDCODE.
The diagnostic AArch64 visual client is separately bound in
[source-audit.json](source-audit.json). Its additional `key`, `media` and
`reset` operations are maintenance test controls, not a production launcher.
The original protocol client's binary/source identities remain preserved.

The first capture uses the user's MS2109 (USB ID `534d:2109`,
`usb-0000:80:14.0-6`) at `/dev/video4`, MJPEG 1280x720 at 30 fps.
After the user starts a shared feed, subsequent frames come from the explicitly
provided `/dev/v4l/by-id/fes-ms2109-hil` (`/dev/video24`), YUYV 1920x1080
at 30 fps. The shared producer supplies that resolution; it does not establish
the FPGA's native HDMI mode. No capture service or other viewer is stopped.
Each still is an unmodified native frame extracted two seconds into a retained
three-second capture; frames are not synthesized or edited. Full captures
remain in the task worktree `out/hdmi/`.

Observed sequence:

1. [Settled ROM prompt](after-settle.png): white paper and the ZX81 `K` cursor.
2. [AArch64 digit input](after-key-actual.png): `1` appears beside the cursor.
3. [Stored program](entered-line-shared.png): `1 PRINT 7` listed with the cursor
   below, after individual P, 7 and ENTER key events.
4. [Actual RUN output](run-seven.png): `7` displayed and completion report
   `0/1`, after R and ENTER. This is physical BASIC execution evidence.

Only the explicit JTAG master is used. Some fresh System Console
instances initially report the selected device absent before any control
write or client start. Failed setup logs are retained; their owned uploads are
removed. A bounded discovery refresh now allows up to ten seconds to find the
same explicit device, then fails closed. It never substitutes another device,
changes FPGA configuration or retries an uncertain control write.

This is physical diagnostic evidence for the loaded image identified by the
retained Quartus checksum. It does not establish the board's USB serial, UID or
JTAG IDCODE, and does not qualify HDMI electrical timing, audio, HPS CPU
MMIO/DMA, PR personas, production FES packaging, or the complete printed FPGA
part family.

The first LOAD attempt is retained as [load-result.png](load-result.png):
the command reads `LOAD PP` with a syntax marker. The real per-row JTAG
transport exposes P before its later SHIFT modifier row; accepted snapshots
alone do not guarantee simultaneous application. The diagnostic therefore
establishes SHIFT alone, then P, releases P while retaining SHIFT, and finally
releases SHIFT. The production protocol and mapper are unchanged. The new
visual client source/binary identity is recorded separately from the successful
initial RUN client. [clean-load-typed.png](clean-load-typed.png) confirms that
the staged sequence types `LOAD ""` on a cleared screen before ENTER.

The staged repeat starts with machine HOLD/RUN reset, reinserts the same frozen
923-byte `.p` image, and enters `LOAD ""`. [clean-load-result.png](clean-load-result.png)
shows the ROM's successful `0/0` LOAD completion. [loaded-run.png](loaded-run.png)
then shows `7` and `0/1` after RUN. [edited-program.png](edited-program.png)
shows the subsequent `2 REM` edit stored alongside `1 PRINT 7`, without a
reset between LOAD, RUN and the edit.

[reload-result.png](reload-result.png) shows successful `0/0` completion after
reinserting the fixture and entering LOAD again, with no intervening reset.
[reloaded-run.png](reloaded-run.png) shows `7` and `0/1` again. Finally,
[restored-list.png](restored-list.png) shows only `1 PRINT 7` and LIST completion
`0/0`: the `2 REM` edit has disappeared. The physical LOAD/run/edit/reload/run
sequence therefore passes, including restoration of the original program.

[proof.json](proof.json) retains the individual actual client/proxy counters
and board/image/binary identities. Its client-level `basic_output_observed`
fields intentionally remain false: those register clients do not observe the
picture. The separately inspected native frames above supply the visual
evidence. [capture-receipt.json](capture-receipt.json) binds native stills to
retained clips and capture paths. Compressed transcripts retain every successful
master closure and the setup failures before control. Both device and master
discovery have separate ten-second startup bounds; neither bound changes a
control command deadline or retries writes.

[cleanup.json](cleanup.json) verifies all 25 exact owned temporary upload
directories absent. All owned clients, SSH forwards, JTAG masters and finite
captures exit. The shared capture producer/viewer remains running under the
user's ownership; its current capture format is left alone. Linux is asked to
halt for the requested factory power cycle; the retained boot log reports an
unclean journal and does not establish a graceful halt. The later factory power
cycle is verified through UART, pinned SSH and the original factory SLD
identity; the final [restoration receipt](../restoration.json) records release
and restored USB permissions.

The frozen `visual-step.py` runner produced the initial RUN receipts;
`visual-step-v2.py` adds explicit input arguments and start/completion times
for the staged LOAD sequence. Both are retained as historical evidence
producers, together with their byte-bound receipts.
