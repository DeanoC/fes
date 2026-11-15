# First fatal USB channel observation trial

Candidate `833277b3abc25cab`, compiled from `d1c0cf21a`, adds a read-only
observation of 15 registers after a fatal USB channel interrupt and before the
existing halt attempt. It consumes no RX/payload, acknowledges no status and
adds no command, clock sample or retry. The successful transfer path, ownership
rules and timeout bounds remain unchanged.

The preceding image `1059bb64e5cac38b` reached one menu frame, then USB transaction
error `0x80` and a failed 500 ms channel halt, before Ready or any tape/core
request. Its two programming operations are consumed. Corrected full evidence
is retained in the r2 archive; the earlier timing interpretation is explicitly
superseded. Software timing durations do not establish an expired frame budget.

Current driver race tests and all six affected checks pass. A source comparison
of 429 tracked selected inputs shows only the changed fatal-channel dispatch and
new observation helper. Firmware producer sources and FPGA core payloads are
identical, allowing explicitly scoped inherited protocol/FPGA evidence reuse.
The complete earlier campaign is not claimed rerun on this source. The rebuilt
SDK/entry/BL31/SPL/FIT/SOF chain and its current source hashes pass checks on the
builder and Powerboat, under the unchanged 8 MiB seal bound.

This single-use trial retains 13 Ethernet uploads/fresh SD readbacks, seven FPGA
configurations, three core launches and four menu sessions. It uses only top
TRWU8CBL with the installed spare card. Bottom TRWU8J8G is excluded; the factory
card remains safe and restoration remains waived. No physical key action is
requested. Qualified programming is fixed 3 MHz/auto-adjust off, with independent
cleanup required to verify original 15 MHz/auto-adjust, exact USB permissions,
owned server gone and UART free.

Original bounds remain native 450 s, outer 800 s, host 890 s and initial Ready
100 s. The first failure ends the attempt, followed only by a two-second passive
tail. The stepped tape protocol keeps one second per 512-byte call, size-derived
total time, and an independent one-second control/fetch allowance. No consumed
image is replayed. Old-file cleanup remains paused.

The terminal result is recorded below. This diagnostic does not itself repair USB or
establish ordinary core-loading, physical display/input or appliance acceptance.


Both fixed-clock programming operations and all 13 uploads/fresh readbacks
passed. Five FPGA configurations completed. The complete 917-byte tape
transferred in 1.205599140 s; independent control/fetch took 0.092368590 s. The
retained tape identity and CPU Hold-to-Run counter proof passed. ZX81 launched
and completed its App/menu return; ZX80 then launched with its distinct package,
ROM receipt and advancing CPU counter. The first four App commands replay
independently through the actual strict paired client with saved byte/timing
boundaries. This validates this tape fixture on this image, not all media cases.

The second return reached native Ready generation 4 and submitted menu3's first
frame, then failed a `menu-frame` snapshot consistency check. Its last sampled
frame was generation1/sequence1/submitted1/slot0/underflows0; flags `0x6` mean
running and submit-ready, with no FPGA fault bit set. The precise rejecting
comparison/intended next sequence was not logged. It is not yet proven whether
a valid preceding receipt was observed before the next update took effect.
The full ZX80 App return, third launch and final Park did not complete.

Emergency menu master-stop, reader drain and bridge-reset closure passed;
resources remain retained and the terminal service requires reboot. The last
emitted Ready state predates that terminal error. The original first-failure
stop and two-second passive tail were preserved. Independent programming-device
cleanup passed. Firmware return/CPU parking and physical display/input remain
unobserved. The earlier USB startup error did not recur, which does not establish
its cause or general reliability. Candidate `833277b3abc25cab` and both grants
are consumed and retired. Old-file cleanup stays paused.

Next inspect the actual menu/control handshake and reproduce the old-receipt
case off-board before choosing a repair. Preserve exact generation/frame/slot
checks, original deadlines, one submission and containment on ambiguous results.
Full ordinary core-loading and appliance acceptance remain incomplete.
