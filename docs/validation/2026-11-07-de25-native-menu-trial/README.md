# Bounded native menu procedure, 2026-11-07

This procedure tests the exact matched menu chain in the preceding
[boot-file validation](../2026-11-07-de25-native-menu-boot/README.md).
[operator-source.json](operator-source.json) freezes the operator and fixture
hashes. Preparation is host-only and does not grant physical acceptance.

One owned session on TRWU8CBL boots the previously qualified recovery loader,
uploads the menu package and experimental FIT through the existing two fixed
destinations, and verifies each write plus a fresh final SD readback. It then
loads the matched volatile HPS-first image, requires the complete cold firmware
and loader records, and sends one `fesboot` command. The native observer requires
three acknowledged Tenfoot frames in slots 0/1/0 and independent firmware
master-stop/bridge closure. No physical button action is needed. This procedure
does not qualify HDMI output or the complete menu/core/menu application.

The existing top-only private JTAG service, exact cable admission, clock policy,
recovery boot and bounded log helpers remain byte-identical to the prior
procedure. The native failure collector retains the same receive-only,
two-second tail after the first failure. Clock restoration to 15 MHz, private
server termination and exact USB permission restoration remain independent of
the experiment result. No shared JTAG daemon, bottom kit, factory card, raw SD
area or recovery FIT is changed.

Offline checks exercise the actual upload runner and its real session over a
stateful card/memory model, damaged receipt chains, the actual master
try/finally, device/clock admission, and the native command gate and result
checks. Missing firmware/cache/CPU proof prevents `fesboot`; missing frame or
closure proof cannot complete the procedure. Earlier host preparation failures
are retained separately. The broad software evidence from the matched menu
milestone applies to unchanged deployed sources; it is not rerun for this
operator-only preparation.

The first hardware attempt has a durable one-attempt fuse and stops on its
first failed gate. Fresh claim, exact USB/UART ownership, source, card and
recovery checks are required. Native work is capped at 240 seconds, with
50 seconds for its loader prompt, 100 for storage and 45 for configuration,
presentation and return. The existing 740-second outer and 850-second host
bounds remain. The spare card and continuing top-kit claim may remain afterward
under Deano's existing factory-restoration waiver.
