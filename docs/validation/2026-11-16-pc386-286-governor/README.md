# Retained 286 governor diagnostic fixtures

These small original source files, captured text outputs, control commands and
hashes support the [diagnostic record](../2026-11-16-pc386-286-governor-diagnostic.md).
They do not include private DOS disks, ROMs, game data, the reference emulator
binary, or a kit image. Reproducing the *reference measurement* requires a
separately provisioned 86Box installation, legally obtained ROMs and a DOS
boot disk. The retained output alone verifies transcription and arithmetic,
not the provenance, clock speed or correctness of the original run.

Run from this directory:

```sh
sha256sum -c SHA256SUMS
python3 verify.py
# Optional: rebuild indexed root and wide fixture sources and compare hashes.
python3 verify.py --nasm /absolute/path/to/nasm
```

The recorded assembler was NASM 2.16.03 (executable SHA-256
`d75404632d151de8438081b817d09f56ab9eb4f688a151f55897f2a5396bf01b`).
The optional rebuild has been checked with that executable. `verify.py` checks
this directory's indexed hashes, repeated results, controls and arithmetic. It
also runs the wide-milestone verifier and reconstructs its candidate patches,
checks the mixed-form and nesting indexes and reconstructs the form-holdout
manifests, checks the CMP and event indexes, and runs the CMP segment evidence
verifier. That last verifier needs the main-history Git base `9037d15e...`;
a full main-history checkout includes it. `--nasm` also rebuilds the wide
milestone's recorded sources, but does not rebuild every sibling fixture. These
checks require Python 3, Git and `patch`. They do not rerun an emulator or
assert a tier threshold.

## Reference setup and procedure

Each reference used the 86Box 6.0 AppImage executable SHA-256
`ae3dc231824cf0c2facd1af4e2164814cbbb15bb1a9fa0603cdd8e9ee0ad2969`
and the following relevant machine settings:

```ini
[Machine]
machine = award286
cpu_family = 286
cpu_multi = 1
cpu_speed = 12500000
cpu_use_dynarec = 0
cpu_waitstates = 1
mem_size = 1024
[Video]
gfxcard = vga
[Sound]
sndcard = adlib
[Storage controllers]
hdc_1 = xtide_at
```

The actual per-run configurations also contained machine identity, disk path,
input and presentation settings. The original pre-run disk/config digests are
listed below for evidence identity; the disks and original ROM content are not
shipped here. `sequence/commands.jsonl`, `rep-verified/commands.jsonl`,
`rep-chunks/commands-chunks.jsonl`, and `forms/commands-*-boot.jsonl` preserve
the original key/sleep/capture commands in order. These reference the original
private driver vocabulary, not a standalone tool included here. Run the
assembled COM under DOS and redirect its output as those commands show. The
programs install a temporary INT 1Ch timer handler, synchronize to a tick and
count completed blocks of 1,000 iterations during 64 ticks; they then restore
the old handler. For count `n`, clocks per loop are estimated as
`12_500_000 * 64 / (18.2065 * 1000 * n)` and the one-block quantization is
approximately `12_500_000 * 64 / (18.2065 * 1000 * n * (n + 1))`. BIOS timer,
interrupt and block overhead remain. Loops are not per-instruction timings.
Check `DS_ES_MATCH` and `RAM_WRITE_VERIFIED` in every REP output before using
it. The forms and sequence outputs do not claim equivalent RAM readback.

The recorded pre-run identities were:

| Run | Disk SHA-256 | 86Box config SHA-256 |
| --- | --- | --- |
| sequence | `9be577b9f2076a51ef9817ff98fbcaefe4393fb2c2f1f1e1974271077813d0da` | `0d1e878c64d1434865dfa6bd1f4c5f63429526dfdcbf9ff23c8bd914c90f0448` |
| forms, first boot | `11d46d94f53eaed0518289a65b0834f901332f6f21a28fa8df09ade18c0b09f3` | `14b51204aad6b3f48b81253d3d9b519f9a68aaebcfed5c3bab776d773cea8ca2` |
| forms, second boot | `5d21976276c545adbbc74be05fabb1a7fdf20d6d85a3dae1b3c3dedafe2dbee4` | `46cfe612783e47cf09058bb8e36a99a4d2d28121072eee4795f5d5fa00333f8e` |
| verified REP | `e431df785ba368df2443246efd7e7a1693ce0de2aca926ad7690dde7b9bb050c` | `2a2d84644df0050af3f7ae0503d5f017117b668bf4ff31aced039cb571768533` |
| REP chunks | `e898da76810a052ff909a74b4a143b8bd125bf539095a7e6718c6edf8851b859` | `894c46a712230c6b2da1e75c6e40f324fc44815f8fdc8c75a4ff59ecc1282ba5` |

The 86Box app bundle and NVR/ROM hash inventories remain in the original local
run directories. These disk/config hashes are not portable replacements for
the missing inputs. Three sequence, verified REP and chunk captures each came
from one boot; the form captures came from two boots. Earlier invalid probes
and the private-disk path accident are described in the parent record.

## CPU side

`forms/reference.asm` and `forms/rtl.asm` contain the paired form loops; the
original timed loop instruction bytes were checked equal. `forms/survey.py` is
the retained historical generator/runner and contains original absolute paths;
it is evidence of the method, not a portable command. `forms/rtl.log` contains
the CPU markers from which `forms/rtl.json` and `forms/comparison.json` were
calculated. The REP CPU programs relocate their data addresses; their timing
loops do not independently check nonzero copied data. `rep-chunks/cpu-v4.log`
contains the raw charge markers and `rep-verified/cpu-result.json` records the
other frozen CPU result.

The frozen CPU executable used the candidate RTL with the diagnostic enabled,
`GOV_N=5`, `GOV_D=3072`, and SHA-256
`bc5c8c551496111bbea3c2a79f56563c4d1f8f7769cc2f47e323e6a07b973516`.
For example, after separately building an equivalent candidate-enabled
`Vfes_i386_cpu` and assembling `rep-chunks/cpu_chunks.asm`, the marker run was:

```sh
FES_I386_MODE=governor FES_I386_LATENCY=0 \
FES_I386_PROGRAM=/absolute/path/to/cpu_chunks.bin \
FES_I386_PRESET=1 FES_I386_FREQUENCY=0 FES_I386_TURBO=0 \
FES_I386_IRQ_PERIOD=0 /absolute/path/to/Vfes_i386_cpu
```

A rebuilt executable is comparable only if its RTL, parameter values, tools,
program bytes and testbench are independently checked. No reproduced emulator
run, package seal or hardware acceptance is implied by the retained fixtures.

## Default-off and oracle differential records

`cpu-differential/default-off-differential.py` and its JSON and text log retain
the original whole-CPU comparison method and outputs. The script contains
original absolute paths and must be adapted for a fresh checkout; its source
identities, binary hashes and parameter values are preserved in the JSON and
parent record. The unmodified clean-main and modified default-off binaries
matched for every recorded field in the four measured modes. This sampled
comparison does not establish netlist identity or physical equivalence.

`cpu-differential/seed41-netlist.log` preserves the failure observed before the
oracle fix; `seed41-netlist-after-merge.log` preserves the subsequent 17/17
pass. The netlist digest was unchanged and the executable was relinked with
the corrected oracle; see the parent record for source identities. This is an
additional seed, not a replacement for the complete producer gate.
