# PR #1182 tenfoot verification on Kit A

On 2026-10-28, the final userspace bundle passed the requested family/variant,
media and UI launch/stop diagnostic on Kit A's existing image. The kit was
restored and released at 10:03:33 Europe/Sofia. No Linux image was built or changed.

FES owns this integration evidence; FogCast owns the two UI corrections found
during verification. Base: `c7d08235308562964f875d2dbb3fb95f6c9c91ec`.
Tested source: `451ade35840b3c2ad261eb899560ac0719c3c83b`.

## Artifacts and method

The [software receipt](software-receipt.json) identifies bundle
`eeccbf60270ba0b809a319057107c3670bfad2ba44b53885a72ca7d33babddd0`.
[Software observations](software.json) confirm it was selected and active at
generation 490, with no pending activation. The existing image remained
`f904961e008a972db8f06f1bb47aff9631123d67215b9f4157ea1fbee83b98d4`.
The retained acceptance receipt represents the supervisor's payload verification,
exact executable-path checks and launcher-ready check. Candidate process hashes
were not independently captured; a separate factory-path hash probe is explicitly
identified as factory bytes in the inventory.

A private Docker host used its own binary, configuration, ports 18782/18783 and
catalog. Its source was `e16f3e9217`; subsequent commits change only room rendering,
hint initialization, tests and documentation. The shared host stayed running.
Deano explicitly approved temporarily changing only the saved launcher API URL.
The supervised tenfoot process used the installed bundle throughout.

Input came through a task-owned USB-bus uinput keyboard and tenfoot's evdev path.
Launch, media selection and Stop were UI operations; API reads recorded results.
HDMI frames came from `/dev/v4l/by-id/fes-shadowcast-hil`. Stop used F12 to open
the live picker, then its existing `S` command. This soft stop retains the host
lease; an explicit API release was used separately for maintenance and cleanup.

## Verified behavior

| Check | Observed result |
| --- | --- |
| Family selector | All 12 families render and cycle once: ZX81, C64, Spectrum, Apple II, Atari ST, Coleco, SMS, SG-1000, Pong, Catch, RAM Tester and RISC-V. Singleton families omit Variant. See [family captures 1](captures/families-1.jpg), [2](captures/families-2.jpg), [3](captures/families-3.jpg). |
| Spectrum variants | 48K, [128K](captures/variant-128k.png), +3 and [Pentagon](captures/variant-pentagon.png) share one workbench. Returning after the family loop remembers +3 and its entry/media. The [reopened +3 page](captures/saved-plus3.png) has the correct hint. |
| Saved media | The +3 UI clears, imports and saves a generated DSK. The 48K entry retains its separate first TAP. Invalid tape-as-disk selection was rejected during the intermediate run. |
| +3 launch/stop | Start machine launches package `88766ab1…` with cassette unit 0 empty and disk unit 1 ready. UI Stop returns host and target to idle with input detached. |
| 48K launch/stop | Start machine launches package `073b93ad…`, generation 2, with tape unit 0 ready and [Spectrum HDMI output](captures/spectrum-48-running.png). UI Stop returns to idle/input detached. |
| Live cassette | Cancel preserves the active session. Selecting the second TAP and [reopening its filename](captures/live-tape.png) preserves session, flight, input session, package and generation. The saved first TAP and +3 disk remain unchanged. |

[Machine-readable observations](observations.json) retain exact identities and
media states. The synthetic fixtures contain no game or firmware bytes; their
hashes and retained bundle/log locations are in the [inventory](inventory.json).

Two defects were fixed and independently reviewed: action rows overlapped native
status/hints (`64f24b6352`), and saved Spectrum reopen used the initial ZX81 hint
(`451ade3584`). Final-bundle HDMI checks confirm both corrections.

## Checks and evidence limits

Both affected runs pass: footer delta 439 parent regressions, final hint delta
392; each also passes 21 planner, 31 runner, affected Go/race, 361 Node and 78 UI
checks. `make check` and `make deadcode` pass, including 21 generated consumers and
37 fixture copies. The hint has a focused saved-family regression. Independent
reviews found no P1/P2 findings. Module/shared contracts are unchanged.

This is an exact-artifact FPGA/HDMI diagnostic for this bundle and two retained
Spectrum packages. Other families have navigation/render coverage. Physical
controller input, ROM-less/cartridge launches, title editions, card fitting,
game-content loading, audio and core fidelity were not qualified here. No ROM-less
artifact was retained and the published core catalog was unavailable. The third synthetic
pad was menu-only, so gameplay Select+Start is unqualified. +3 disk evidence covers
next-start selection and its ready unit, not live disk-unit selection.

## Restoration

The designated baseline bundle `b5f6d8e54731b2b5356f7e475a87771fa88da0902af18083dddde05263b89176`
is selected and active at generation 491, with no pending activation. Image and
boot ID are unchanged. The original launcher file was restored byte-for-byte
(SHA-256 `dbbb3ba7e175334ba814851f91604daf78f022d651976093bb35822fd1d73e8f`).
Target idle, lease free and [normal Home visible](captures/restored-home.png) were
verified before release. Task input devices, helper and staged media were removed;
the private host stopped; shared ports 8787/8789 stayed listening. The authoritative
Kit A status file records restoration and release. Kit B was untouched.

The next integration step is PR review. The coverage limits above remain explicit;
this run does not establish that every core has hardware acceptance.
