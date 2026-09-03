# Mega Drive 20260603 native-profile provenance

This record freezes evidence for later implementation. It does not claim that
Mega Drive is software-supported or hardware-supported by this runtime.

## Immutable inputs

The core authority is
[`MiSTer-devel/MegaDrive_MiSTer`](https://github.com/MiSTer-devel/MegaDrive_MiSTer)
at commit
[`7365a137cfd8fa6f041e964d8b953159c0ec42d9`](https://github.com/MiSTer-devel/MegaDrive_MiSTer/commit/7365a137cfd8fa6f041e964d8b953159c0ec42d9),
whose commit message is `Release 20260603.` The RBF is
[`releases/MegaDrive_20260603.rbf`](https://github.com/MiSTer-devel/MegaDrive_MiSTer/blob/7365a137cfd8fa6f041e964d8b953159c0ec42d9/releases/MegaDrive_20260603.rbf).
Fetching that immutable path reproduces the designated-kit identity exactly:

```text
size=4296864
sha256=0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839
```

The matching host authority is
[`MiSTer-devel/Main_MiSTer`](https://github.com/MiSTer-devel/Main_MiSTer) at
commit
[`c73802332ff9c73659410084b6319ccd29f0b3aa`](https://github.com/MiSTer-devel/Main_MiSTer/commit/c73802332ff9c73659410084b6319ccd29f0b3aa),
also committed as `Release 20260603.` The executable guard downloads every
cited file from these commit-qualified paths. It additionally fixes the
downloaded source-file SHA-256 values so the check cannot pass by reading its
own fixture as its only authority.

## Derived core and media contract

The following facts come from
[`MegaDrive.sv`](https://github.com/MiSTer-devel/MegaDrive_MiSTer/blob/7365a137cfd8fa6f041e964d8b953159c0ec42d9/MegaDrive.sv):

- Lines 77-79 begin `CONF_STR` with `MegaDrive` and declare `FS1` for
  `BIN`, `GEN`, and `MD`, establishing core identity `MegaDrive` and cartridge
  file index 1.
- Lines 218-220 declare the 128-bit status and 12-bit joystick buses. Lines
  252-278 instantiate `hps_io` with `WIDE(1)` and a 16-bit `ioctl_data` path.
- Lines 307-318 admit cartridge downloads on indices 1 and 2 and select the
  normal Mega Drive cartridge mode for index 1. This slice deliberately
  freezes the normal cartridge path only.

Matching Main reads the core's file-I/O-width flag at bit 16 in
[`fpga_io.cpp`, lines 553-556](https://github.com/MiSTer-devel/Main_MiSTer/blob/c73802332ff9c73659410084b6319ccd29f0b3aa/fpga_io.cpp#L553-L556),
retains it during core initialization in
[`user_io.cpp`, lines 1400-1402](https://github.com/MiSTer-devel/Main_MiSTer/blob/c73802332ff9c73659410084b6319ccd29f0b3aa/user_io.cpp#L1400-L1402),
and passes it to file transfer in
[`user_io.cpp`, lines 2042-2047](https://github.com/MiSTer-devel/Main_MiSTer/blob/c73802332ff9c73659410084b6319ccd29f0b3aa/user_io.cpp#L2042-L2047).
[`spi.cpp`, lines 180-193](https://github.com/MiSTer-devel/Main_MiSTer/blob/c73802332ff9c73659410084b6319ccd29f0b3aa/spi.cpp#L180-L193)
sends the wide path as successive native 16-bit words. On the target's
little-endian Arm ABI, each word is the next little-endian byte pair. The
frozen wire contract is therefore `little_endian_byte_pairs`.

## Derived reset and status words

[`MegaDrive.sv`, lines 132-135](https://github.com/MiSTer-devel/MegaDrive_MiSTer/blob/7365a137cfd8fa6f041e964d8b953159c0ec42d9/MegaDrive.sv#L132-L135)
declares reset as status bit 0. Matching Main:

- asserts that bit after identifying the generic core in
  [`user_io.cpp`, lines 1427-1433](https://github.com/MiSTer-devel/Main_MiSTer/blob/c73802332ff9c73659410084b6319ccd29f0b3aa/user_io.cpp#L1427-L1433);
- starts the no-saved-configuration status from zero and reasserts bit 0 in
  [`user_io.cpp`, lines 1515-1530](https://github.com/MiSTer-devel/Main_MiSTer/blob/c73802332ff9c73659410084b6319ccd29f0b3aa/user_io.cpp#L1515-L1530); and
- releases bit 0 last in
  [`user_io.cpp`, lines 1676-1679](https://github.com/MiSTer-devel/Main_MiSTer/blob/c73802332ff9c73659410084b6319ccd29f0b3aa/user_io.cpp#L1676-L1679).

With this slice accepting no settings, those operations produce these exact
low status words:

```text
reset_assert_word=0x0001
initial_status_word=0x0001
reset_release_word=0x0000
```

The initial word retains reset while applying the all-zero option baseline;
it is not an early release.

## Derived player-one input contract

Matching Main defines player one's user-I/O command as `UIO_JOYSTICK0 = 0x02`
in
[`user_io.h`, line 16](https://github.com/MiSTer-devel/Main_MiSTer/blob/c73802332ff9c73659410084b6319ccd29f0b3aa/user_io.h#L16).
[`user_io.cpp`, lines 1789-1799](https://github.com/MiSTer-devel/Main_MiSTer/blob/c73802332ff9c73659410084b6319ccd29f0b3aa/user_io.cpp#L1789-L1799)
sends the digital map immediately after that command.

The core maps the player-one bus in
[`MegaDrive.sv`, lines 1020-1048](https://github.com/MiSTer-devel/MegaDrive_MiSTer/blob/7365a137cfd8fa6f041e964d8b953159c0ec42d9/MegaDrive.sv#L1020-L1048).
The resulting masks are:

```text
up=0x0008 down=0x0004 left=0x0002 right=0x0001
a=0x0010 b=0x0020 c=0x0040 start=0x0080
```

All eight masks are nonzero and disjoint. Although the upstream core exposes
more buttons and players, the vertical slice intentionally freezes one
three-button player only. `player_count=1` and `video_recipe=menu_720p60` are
slice bounds from the approved design, not claims about upstream limits.

## Executable evidence

`tests/profile_provenance_test.sh` validates exactly one assignment for every
v1 fixture key, downloads the immutable RBF and cited source files, checks
their independent hashes, derives the core name, cartridge index, width,
reset/status values, player command, and input masks, and compares those
results with `tests/fixtures/megadrive-profile-v1.txt`. It also rejects zero or
overlapping supported input masks.
