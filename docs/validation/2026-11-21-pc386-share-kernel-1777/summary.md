# pc386: FreeDOS kernel 2046 and SHARE built from source, 21 November 2026

Issue: [#1777](https://github.com/DeanoC/fes/issues/1777). The
diagnosis record ([#2073](https://github.com/DeanoC/fes/pull/2073), `docs/validation/2026-11-21-pc386-share-1777/`) found two
FreeDOS defects behind it. Together they made every delete and rename fail
under SHARE on the games image's Windows boot entry (entry 4):

- **Kernel 2046 and SHARE 08/2006a don't match.** The kernel asks SHARE
  before each delete and rename (`INT 2Fh AX=10A6h`). FreeDOS 1.4's SHARE
  08/2006a predates that call
  ([FDOS/share#27](https://github.com/FDOS/share/issues/27)).
- **The kernel's 10A6h stub returns with SI clobbered.** `DosRenameTrue`
  keeps its path in SI across the call, so a SHARE that answers the call
  sends the rename to a garbage path
  ([FDOS/kernel#276](https://github.com/FDOS/kernel/issues/276)).

Deano chose to report both upstream (done) and to carry fixed builds until
upstream ships them. This record covers the fixed builds.

**Result.**
- **The kernel and SHARE are built from the ke2046 release's source
  archive** (`ke2046s.zip`, already pinned):
  - **Kernel:** 386 FAT12/FAT16 with `WIN31SUPPORT`, built with Open Watcom
    v2 (the 2026-07-13 build), plus one 18-byte patch. The patch makes the
    stub keep SI and DS on the stack.
  - **SHARE:** the archive's `share` tree (FDOS/share 47f3d425, which has
    `is_file_open`), built with ia16-gcc 6.3.0 from tkchia/build-ia16.
- **Builds are pinned and reproducible.** `freedos.lock` pins both
  toolchains by archive SHA-256 and both outputs by SHA-256; a build that
  makes other bytes fails. Building twice gives the same bytes.
- **The unpatched source build matches the release.** Its symbol map
  equals the release kernel's (`kwc38616.map`) at 565 of 567 symbols; two
  DGROUP symbols sit 2 bytes higher. Its HMA code equals the code the
  release kernel runs, apart from bytes the kernel fills at run time and
  the 2-byte DGROUP offsets (below). The patched build differs from the
  unpatched one in 13 bytes, all inside the 18-byte stub.
- **`--suite share` is the regression check.** `SHRTEST.COM` runs on entry
  4 and checks both directions:
  - a file still open must not delete or rename;
  - once closed, it must rename and delete;
  - a second delete must return 0002h (file not found).

  It passes on the oracle and on the RTL CPU (`--cpu rtl`: the oracle boots,
  fes_i386_cpu runs from the prompt with no oracle step or stop). On the old
  fixture it fails at rename, delete and gone with error 5.
- **Memory is unchanged.** Entry 4 still shows 622K (637,152 bytes) free.
  The new SHARE takes 6,768 bytes (7K) of upper memory; 08/2006a took 9K.
- **No sealed design moves.** The pc386 seal producer (`pc386_quartus.py`)
  imports neither changed script. The PC/XT producer reads its own core's
  `freedos.lock`, not pc386's.

## The patch

[`ke2046-share-is-file-open-si.patch`](../../../sources/misteross/cores/fes-pc386/sim/freedos-patches/ke2046-share-is-file-open-si.patch)
(CRLF, as `int2f.asm` is):

```
SHARE_IS_FILE_OPEN:
        push    si              ; callers keep values in si (DosRenameTrue: path1)
        push    ds
        mov     bx, sp
        lds     si, [ss:bx+6]   ; filename, past ds, si and the return address
        mov     ax, 0x10a6
        int     0x2f            ; returns ax
        pop     ds
        pop     si
        ret     4
```

BX is in the Watcom `pascal` convention's modify list. The new stub takes the
old one's 18 bytes, so the rest of the kernel keeps its layout. Drop the patch
once FDOS/kernel releases a fix.

## Checks

| Check | Result |
| --- | --- |
| `--suite share`, oracle | pass: create, del-open, ren-open, close, rename, delete, gone, DONE |
| `--suite share --cpu rtl` | pass, no oracle step or stop after the handover |
| `SHRTEST.COM` with the new SHARE and an unpatched kernel 2046 built from the same source, entry 4 | stops at the rename: "Error reading from drive A: DOS area: drive not ready" |
| `SHRTEST.COM` on the old fixture (release kernel, SHARE 08/2006a), entry 4 | rename, delete and gone FAIL with AX=0005h, as #1777 found |
| unit tests (`tests.test_pc386_machine`, `tests.test_pc386_game_drive`) | pass |
| MEM on entry 4 | 622K (637,152 bytes) free; SHARE 6,768 bytes in upper memory |
| `--suite boot --image games --type 'mem\|'`, oracle (entry 1) | pass: 582K (595,808 bytes), as before; the Serpent Isle and Frontier memory check passes; VGA replay 0 mismatches |
| `--suite boot` (boot image, `dir`), oracle | pass; VGA replay 0 mismatches |
| `--suite game --game wolf3d --drive real` (rebuilt `real`, `bb473ed9…`), oracle | pass (24 s run) |
| WfW 3.11 Notepad on entry 4 (`win /3 notepad c:\save1.txt`), oracle: type, File Save, then File Save As `C:\SAVE2.TXT` | pass: `SAVE1.TXT` goes from "ORIGINAL" (10 bytes) to "EDITED" + "ORIGINAL" (18 bytes); `SAVE2.TXT` is created with the same 18 bytes; SHARE's open and close calls for `C:\SAVE2.TXT` return CF=0 |

## Method

- **Toolchains.** Fetched from the lock's URLs, checked by SHA-256, and
  unpacked under `build/sim/fes-pc386/freedos/toolchains`, so nothing is
  installed on the host. Each archive lists its upstream URL first and then
  the byte-identical mirror release
  [DeanoC/fes-pc386-toolchains v1](https://github.com/DeanoC/fes-pc386-toolchains/releases/tag/v1),
  where each asset's name starts with its SHA-256; GitHub also turns `~` into
  `.` in asset names. Deano approved the mirror. The README there names each
  archive's upstream source and licence. With every upstream URL made
  unreachable, the builder fetched all five archives from the mirror, each
  matching its pin. The archives are:
  - Open Watcom v2's `2026-07-13-Build` `ow-snapshot.tar.xz`
    (`35e9c56c…`);
  - `binutils-ia16-elf`, `gcc-ia16-elf`, `libnewlib-ia16-elf` and
    `libi86-ia16-elf` from tkchia/build-ia16's noble pool, unpacked with
    `dpkg-deb -x`. Their payloads are zstd, which Python's `tarfile`
    cannot read.
- **Kernel build.** `make all COMPILER=owlinux XCPU=386 XFAT=16 XUPX=
  ALLCFLAGS=-DWIN31SUPPORT NASMFLAGS=-DWIN31SUPPORT` with the test386
  runner's NASM 2.16.03. The kernel ships uncompressed (68,386 bytes, no UPX).
  UPX only packs the file: the kernel unpacks itself at boot, so its memory
  use is the same.
- **SHARE build.** `make -C src UPXARGS=true` with `share/build.sh`'s gcc
  settings, without UPX. `SHARE.COM` is 11,682 bytes.
- **Comparing with the release.**
  - The memory dump from the diagnosis record holds the release kernel's
    HMA_TEXT at FFFF0h.
  - It differs from the unpatched build's HMA_TEXT in 47 of 39,129 bytes:
    - the first D4h bytes, zero in the file, which the kernel fills at
      run time;
    - DGROUP offsets 2 higher (`_Dyn` moved from 007a:1910 to 1912);
    - a few run-time patches (`CDh` to `E9h`).
- **Licences.** The kernel and SHARE are GPL. The image carries
  `C:\FDOS\DOC\SOURCES.TXT`, which names each source archive and its
  SHA-256, the toolchain and the upstream issue, plus the patch as
  `KERNEL1.DIF`. The image's JSON record lists the same. The images are
  test fixtures and are not distributed.
