# One bounded early-exception trial on TRWU8CBL

This freezes a fresh procedure for firmware acca40b3c and the byte-exact retained
native application/entry from 31d716957. The sibling early-exception validation
records the audited private cold vectors, handler and exact matched file chain.
This procedure requires that audit and the matching BL31 ELF digest before live
admission. A failed prior linker layout was never programmed.

The boot decoder and outer result require early RAM entry/pass, all 15 ordered
vendor markers and no exception records before startup can pass. Any exception
is a failure; a partial fault message cannot supply missing boot checkpoints.
The actual UART failure pump stops on the exception marker and only drains
remaining output passively. No resume, retry or containment bypass is added.

One qualified recovery, eight fixed uploads and fresh SD readbacks precede the
native attempt. If startup succeeds, the existing native checkpoints, shared
USB pause/resume and three ROM launch/return/Quit cycles remain required. Bounds
stay prompt50/startup100/launch100/configuration20/control5/native450/child480,
work740/host850 seconds. Stop first failure; permit one attempt on this input.

The source-frozen model rejects missing/invalid guard and vendor traces and
unexpected exceptions, including an otherwise successful outer result. Five
fixture groups plus source provenance rejection tests pass. The prior campaign
retains ten separate procedure stops: one programmer failure, one host setup
stop, four ROM deadlines and four pre-native startup stops on distinct inputs.
The consumed 7e90 candidate is not replayed.

The normal-user shared Quartus lock encloses the test. Revalidate the continuing
claim, exact adapter serial/path, private top-only JTAG, chip ID and free UART.
Use the qualified temporary 6M programming link, restore/read back original15M,
then independently audit exact USB permissions, private JTAG termination and
UART release. Cleanup does not prove CPU/FPGA halt. Retain claim/spare card;
leave the factory card and other kit untouched. Factory restore is waived by
the user. This publication precedes live preflight and hardware evidence.

## Recorded physical result

One attempt on operator b25f85c85 stopped on the early exception marker;
no prompt timeout or hardware retry followed. Recovery, all eight fixed uploads
and eight fresh SD readbacks, private 6M chip admission/programming and SPL FIT
verification passed. The ATF descriptor matches the built BL31 SHA-256
ac2e27db7d084a96b34f95128914f7c10a71c5658a3466ded01881beda71c5d7.

Early guard entry/pass and eight ordered vendor markers preceded this record:

```
vector=7 esr=0xbe000011 elr=0x8001b1f8
r0=0x50240 r1=0x5dec0 r2=0x40 r3=0x3f
```

The captured vector slot and ESR EC=0x2f identify an EL3 SError. The saved PC
extracts from the actual loaded ELF as instruction word 0xd5087620, DC IVAC x0,
in the original inv_dcache_range loop. Working registers show its current
address, end, 64-byte line size and mask. This proves early exception capture
during the NOBITS invalidation interval. SError is asynchronous: the saved PC
and working address do not establish the causing instruction or transaction.
The raw FAR value is retained but is not interpreted as a valid fault address.
Feature-dependent syndrome subfields are not used to claim a specific ECC or
interconnect cause. See the [Arm Cortex-A55 register reference](https://documentation-service.arm.com/static/5e7e09f6a3736a0d2e862d2f)
and the pinned TF-A arch.h/ras_arch.h in the retained compiled source.

The independent raw replay exactly matches the saved boot decoder, including
one exception record, invalid startup and no loader grant. The native launch
command was never sent. There is no BSS/stack/setup completion, writable OCRAM,
native CPU exclusion, ROM completion, CPU Run or return evidence on this input.
The earlier model results remain off-board evidence only.

Independent cleanup verified exact USB permissions, recorded original15M
readback, private JTAG stopped and UART free. No physical CPU/FPGA halt is
claimed. The continuing claim and spare test card remain retained; the factory
card and other kit were untouched. This input is consumed after one attempt.
There are now eleven separately retained procedure stops, including five
pre-native stops on distinct compiled firmware inputs.

Next inspect the documented CPU/CCU/OCRAM fault records, memory attributes and
initialization contract before proposing a correction. The vendor notes that
[ECC-protected memories require initialization before reads](https://docs.altera.com/r/docs/814346/25.3/hard-processor-system-technical-reference-manual-agilextm-5-socs/memory-data-initialization).
That is a candidate investigation, not proof of ECC as the source here; a whole
OCRAM initialization during live SPL would require a separate ownership proof.
Do not remove the cache operation, clear fault evidence, widen timeouts or
replay this input as a workaround.
