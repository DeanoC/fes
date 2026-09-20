# Catch and Coleco playable-package diagnostic

The designated-kit run `playable-compositions-20260921-09` passed the normal
library sequences below. This is exact-package hardware diagnostic evidence,
not a native-image release, permanent deployment, or ZX81 expansion acceptance.

## Exact selection

Host, agent, Kit and ARM runtime binaries were built and frozen from FES
`47a8462cbb87b5496217c4ed71de4fbb5680bf9e`. Their hashes are recorded in
`software-inputs.json` and verified against disk and running target processes.

| Artifact | Identity |
| --- | --- |
| Catch package | `9bacda9374ebcb666b0552bdc1d5e26ddf71d8a2af91504e6d5f1250e9e538db` |
| Catch RBF SHA-256 | `18570a286dc372092e2d8b33a2e514b9ab963723f1918c31a97d59cbfed2e050` |
| Coleco package | `b8871406779a334b0c35d9e64e6bd5638bc1a6f4c0fd27a04a5fa8be273b5ff0` |
| Coleco RBF SHA-256 | `ae5b1bfc6c5700b1112a180c1e9d9c609e61f4183618b0a48fc1f917af994b85` |

Catch preparation selected FES `24764b796c3335c162d60637b4dbe8db522384a2`;
Coleco preparation selected `6992eb47fff9d0fca29170d47d0740527e282f75`.
Each preparation receipt binds its source selection, archive, manifest and
payload. Catch uses reviewed nextpnr `30ac6f47`, including the routing-buffer
LUT-mask fix. Coleco retains the separately qualified registered-memory lock
and nextpnr `0fad53a7`.

## Observed behavior

Catch imported and launched as an ordinary ROM-less library entry. Right input
moved its blue paddle from x=159.5 to x=249.5 in the 320-pixel analysis frame.
Both HDMI audio channels carried a bounded approximately 110 ms, 1 kHz chime
with peak-block RMS 4096. Stop returned idle. Restarting the private host retained
the entry; relaunch restored the centered paddle and Stop succeeded again.
Bounded read-only frame observation allowed HDMI capture to become visible;
it did not replay launch or input. Visible frames were also reviewed manually.

Coleco rejected the firmware-required Frogger entry before activation while
the household BIOS slot was empty. The same package launched BIOS-free
Graphics I, showing the expected grid and exactly zero RMS in both captured
audio channels. A one-byte BIOS selection was rejected. Importing and selecting
the household BIOS then booted Frogger through the normal library path.

The captured Frogger playfield was recognizable, keypad Start entered play,
and Up input changed the displayed score from 0 to 10. The later frame does
not establish collision-free progress or complete game correctness. Both audio
channels had RMS approximately 7925, peak 24573 and 2399 zero crossings over
the capture; means below 7 distinguish the signal from a DC level.

Stop, switching back to Graphics I, host restart and Frogger relaunch all
passed. Clearing the BIOS restored the readiness block and launch rejection.
The final redundant idle Stop succeeded. Input used the ordinary session API;
this is not a physical USB button or multiplayer acceptance claim.

## Restoration and retained evidence

The operator temporarily overlaid the tested binaries and used an isolated
host catalog. Original disk and live-process hashes, boot identity and target
configuration were restored/checked. The target ended ready, idle and unleased;
the private container, temporary target files and private configuration were
removed. No card or permanent image was changed.

Powerboat retains receipts, captures and logs under
`/home/deano/fes/out/hardware/playable-compositions-20260921-09/`.
`operator-result.json` records completion and restoration; `catch/result.json`
records the Catch measurements. `frogger-audio-signal.json` records the audio
analysis. Private BIOS/ROM bytes and credentials are not committed.

Earlier failures remain recorded: run 06 sampled black video immediately
after relaunch; bounded observation resolved that in runs 08 and 09. Run 07
refused an occupied lease without mutation. Run 08 completed Catch but was
interrupted when another operator stopped its private host; that operator
confirmed the action in the coordination channel. None is relabeled as an
overall pass.

The optional ZX81 RAM composition still needs its own final routed cartridge
and same-shell 1 KiB/16 KiB hardware acceptance. These results do not qualify
later compiler changes, later software artifacts, or the assembled appliance.
