# Native retirement command snapshots

The preceding v6 physical trial remains failed: its full source transfer
completed, then the command phase exceeded its 100 ms bound before queue
publication. Its later warm baseline return also lacked the fabric drain
acknowledgement. Neither failure is reclassified by these host results.

The selected input-retirement profile now supplies an optional fixed snapshot
to the command-queue engine. It checks the same 24-register superset already
used by retirement, with source/owner and callback checks around a bounded,
read-only batch. Queue slot stores retain their individual guards. Producer
publication still requires the separate fresh 24-register pre-write check.
Arm-only and fault profiles leave the callback unset and retain individual
register reads. The selected input contract advances to format 7 so a v6
profile cannot silently describe the new build.

The command, retirement and source bounds remain 100 ms, one second and
20 seconds. Successful translation retirement still retains memory and grants
neither ordinary activation nor interconnect drain or buffer reuse.

The [model result](model-result.json) records the source hashes, compiler and
injected-cost cases. At zero injected cost, command-phase raw supplier reads
fall from the recorded v6 model's 15,406 to 7,996 (48.1%). All five complete
5,120-word table checks remain, as do the five control writes and twelve queue
word stores. The new model retires with 8 us added to each supplier read; the
recorded v6 model failed that case. At 16 us and above it still fails closed,
and a separately injected slow counter also fails. These are operation-cost
models, not measured hardware latency or proof that the physical failure is fixed.

The sanitized tests include 30 command-snapshot cases covering success,
read failure, changed callbacks, ownership loss and deadline expiry at each
snapshot boundary. They also retain the existing joined transfer/lifecycle,
table/callback mutation, counter-boundary, portable failure and fresh pre-write
tests. Run them without hardware:

```sh
python3 -m unittest discover -s tests -p test_nano_source_retirement.py -v
python3 -m unittest discover -s tests -p test_nano_sdm_input_profile.py -v
```

The complete Nano suite passes 810 tests with 13 toolchain/hardware-dependent
skips. The actual board ROM/CPU/return RTL model also passes after its stale
dependency on a removed bundled ROM file is removed; that model streams only
its synthetic program through the external-ROM transport.

The [AArch64 compilation record](portable-arm64.json) binds the five portable
firmware units and headers to the pinned compiler tools and resulting AArch64
objects. The largest individual static frame is 368 bytes. This is neither a
linked BL31 nor a whole EL3 stack bound.

A new matched AArch64 firmware/boot build, physical retirement evidence and
the separate warm fabric-drain investigation remain required before ordinary
native core-load acceptance. No board was programmed or rebooted for this
change. Historical baseline restoration evidence remains separately bound.
