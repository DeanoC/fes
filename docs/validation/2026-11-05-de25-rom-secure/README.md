# ROM transfer inside secure computer control opening

The opt-in cold ROM path now connects the actual ROM transfer driver to the
strong firmware provider and existing computer control-opening gate. The gate
returns a successful opening only after ROM transfer, private-image SHA-384,
identity, counters, CRC and held completion succeed. A failure clears successful
reply provenance, retires the activation and uses the existing containment path.
It does not add a new SMC, release data bridges or execute CPU Run.

Implementation: `fcd60e3cc`, based on `fa448e43f`. The boot-identity source-inventory
fix is `0e027ed17`. All 55 frozen C/RTL model inputs match the committed code.
The firmware installer includes the ROM sources, header and provider; the cold
chain identity includes the same files.

A ROM profile captures a version-2 program binding while all bridges are closed.
The existing 640-byte header binds package/build, owner/generation and bitstream
hashes. A fixed 128-byte extension binds the 4/8 KiB ROM's aligned offset, length,
CRC, SHA-256 and SHA-384. The image follows the aligned bitstream within the
existing reservation and cannot overlap its metadata page. The provider samples
the extension twice and keeps its secure copy. Legacy profiles require version 1;
ROM profiles require version 2. These requests supply integrity, not package
or boot authentication, physical addresses or new platform permissions.

Before endpoint access, the provider hashes the driver's private byte snapshot.
The existing secure lock, finished configuration, captured build, same owner,
next generation, control-only scope, contained masters, held CPU and qualified
400 MHz timer remain required. The 8 KiB buffer stays in static secure memory.
A later successful configuration may construct a fresh object after a completed
ROM activation; a failed or uncertain transfer cannot reset or replay it.

Both image sizes pass C/RTL tests through the actual provider include and control
gate into the real ZX80/ZX81 machine model. Synthetic ROM execution and held return
pass. Bad digest/source/scope/timer, changed identity, counters/CRC, failed reads
and writes, lost ownership/Hold, deadlines, poll limits and corrupt hardware
payloads cannot publish a successful open or run the guest. Platform operations
and an exact-image digest oracle are injected; this is not physical execution or
qualification of the modeled digest primitive.

Native encoding is checked against the actual C descriptor decoder. The
native/Tenfoot, firmware, producer and DE25 control integrations pass. The FPGA
software suite completes 1,577 cases with 75 skips. The parent aggregate ran
1,476 cases and failed one real invariant: installed ROM sources were absent
from the cold boot identity inventory. The inventory fix passes all 13 affected
context tests; the other 1,473 parent cases passed with two skips. The original
failed aggregate remains retained and is not described as a passing exact-head
full-suite run.

The provider translation unit compiles for ARM64 with the real vendor headers,
SHA API and normal warning flags. An isolated 58-file dependency closure
reproduces its object byte for byte. ARM64 driver/descriptor compilation and
per-function stack reports pass. The compile fixture uses a placeholder aperture;
it is not a ROM-capable boot image or verified physical address. Per-function
frames do not prove the complete EL3 call-chain stack bound.

Artifacts, failed preparation logs, exact compile inputs, validation logs and
source bundles are retained at
`/home/deano/fes-retained/de25-rom-secure-20261105-r1/`. Source restoration and
strict fsck use the recorded retained prerequisite bundles; the archive is read
back against its recorded file hashes. No hardware action occurs in this change.
TRWU8CBL's last tested state remains the earlier parked controller/core-cycle run.

No generated cold profile or physical producer currently exports the required
ROM aperture/conduits. The native launch publisher does not select the new ROM
encoder, and ordinary ROM-linked packages remain rejected. The next integration
needs an explicit update of the frozen 100 MHz machine source, a matched physical
endpoint/identity/Hold connection and sealed map, then native retained-ROM launch
and its receipt before a bounded board test. Full DE10-equivalent loading, media,
native video/Ethernet and broader playable acceptance remain open.
