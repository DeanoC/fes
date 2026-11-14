# Physical SError after MMU/cache activation

Candidate07e1 passed qualified recovery, thirteen uploads/fresh SD reads and
native programming. Provider setup passed. The original fifteen steps are
followed by a complete raw enabled record: SCTLR `cd183f`, VBAR `80003800`.
The next raw record is a terminal exception with ESR `be000011`, ELR
`80017098`, TCR `80803520`, root `8003f000` and MAIR `4400ff`. These MMU
registers match the selected image. No returned write-after checkpoint, private
memory test, vector-restored record, loader or tape transfer follows.

ESR.EC is `0x2f`, the pinned TF-A `EC_SERROR` value; ISS/DFSC is `0x11`.
This is an asynchronous SError, not a diagnosed translation-permission fault.
The matching ELF places ELR at `tf_log+0x24`, whose instruction is
`STR X7,[SP,#152]` while saving printf arguments. Its preceding instructions
also store stack pairs and read the private DDR log-level word. Control has
therefore reached C after the stack-free enable record. Because delivery is
asynchronous, ELR does not establish the responsible access. The cause could
involve cached memory traffic or an earlier pending error; it remains unknown.

Raw FAR is `5c780`; it is not established as a valid failing address for this
SError. Raw SP is the handler's SP_EL3 `58000`. SPSR mode `0xc` confirms
interrupted EL3t/SP_EL0, whose stack pointer this version does not snapshot.
The pinned TF-A common entry explicitly selects SP_EL0 for C, and `el3_exit`
expects it on entry. The earlier SP-bank experiment and normal SP_EL0 case
correctly describe these limits. No raw diagnostic field grants authority.

Arm's [FAR_EL3 register description](https://support.arm.com/documentation/ddi0500/d/system-control/aarch64-register-descriptions/fault-address-register--el3)
defines it for synchronous aborts and misaligned-PC exceptions. The
[Cortex-A55 reference manual](https://documentation-service.arm.com/static/5e7e1405b471823cb9de57ae)
and the pinned TF-A exception/RAS definitions support the SError classification;
this report makes no unsupported severity or exact-address attribution.

The source-bound stack-free handler captured the frame and parked. Host cleanup
independently verified15M, original USB permissions, no owned private server and
free UART. Complete terminal evidence is retained with full archive readback.
Both programming grants remain consumed and07e1 is retired. The spare remains
installed under the continuing exclusive claim; the factory restore waiver
stands, and the bottom kit is untouched.

Next: capture bounded read-only CCU/OCRAM/ECC status before activation and after
the fault, plus guarded architectural error records. Existing reviewed status
addresses and CPU gates provide the basis. Preserve sticky/error status; do not
acknowledge, scrub memory or relax permissions based on this trace. Include the
interrupted SP_EL0 in the next diagnostic. Validate the register-only paths and
nested-failure behavior, then build a genuinely changed matched image and one-use
procedure. General cache/coherency, core loads and appliance acceptance remain
open; the one-second tape deadline and all existing guards remain.
