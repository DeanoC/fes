# Draft upstream reports (not filed)

There are two reports, one for [FDOS/kernel](https://github.com/FDOS/kernel/issues)
and one for [FDOS/share](https://github.com/FDOS/share/issues), each
linking the other. Filing them is outward-facing, so they wait for Deano's
OK. The kernel report comes first: fixing only SHARE turns the error 5 into a
rename of a garbage path.

---

## FDOS/kernel

**Title:** `SHARE_IS_FILE_OPEN` clobbers SI, and with the released SHARE
every delete and rename fails with error 5

Kernel commit 5b9e8fed ("dosfns: Check share table before delete/rename",
September 2021, first released in ke2044) makes `DosDelete` and
`DosRenameTrue` call `share_is_file_open()`, which is `INT 2Fh AX=10A6h`,
whenever SHARE is installed. This causes two problems in ke2046
(`ke2046_386f16`, Watcom).

**1. The stub does not preserve SI.** `SHARE_IS_FILE_OPEN` in
`kernel/int2f.asm` loads SI with DS to save it in ES, pops the filename
offset into SI, and on return does `mov si, es` / `mov ds, si`. It returns
with SI holding the DS segment value. `share_is_file_open` is declared
`ASMPASCAL` (Watcom `pascal`), so the caller expects SI to be preserved.
`SHARE_OPEN_CHECK` does save it, through DI. `DosRenameTrue` keeps `path1`
in SI across the call and passes it to `dos_rename` afterwards. From the
running ke2046 386 FAT16 kernel (HMA_TEXT at 020c, `kwc38616.map`):

```
DosRenameTrue_ (020c:35f0)
  mov si, ax                 ; path1
  mov di, dx                 ; path2
  ...
  push ds
  push si
  call SHARE_IS_FILE_OPEN    ; 020c:16de
  test ax, ax
  jz   .rename
  mov  ax, 0xfffb            ; DE_ACCESS
  ...
.rename:
  mov  cx, [bp-2]
  mov  dx, di
  mov  ax, si                ; path1, now the DS segment value
  call dos_rename
```

When SHARE answers 10A6h with "not open", `dos_rename` gets a garbage
`path1`. Here that was a critical error, "Error reading from drive A: DOS
area: drive not ready", on a plain rename of a closed file on C:. (We
answered 10A6h with AX=0 from a test TSR to reach this path.) `DosDelete`
uses the global `PriPathName`, so the clobber does not affect it.

**2. The released SHARE does not answer 10A6h.** The newest SHARE release,
08/2006a (FreeDOS 1.4's `SHARE.COM`), handles 10A0h to 10A5h only.
`is_file_open` arrived in FDOS/share 9c8db61c, which has no release. 10A6h
goes on down the INT 2Fh chain and comes back as AX=0001h with CF set. `share_is_file_open()` ignores CF and returns AX, so
`DosDelete` and `DosRenameTrue` read every file as open:

| Step (with SHARE loaded) | MS-DOS 6.22 + SHARE.EXE | ke2046 + SHARE 08/2006a |
| --- | --- | --- |
| create `C:\SHRREPRO.TMP` (3Ch), close (3Eh) | ok | ok |
| rename it (56h), no handle open | ok | AX=0005h CF=1 |
| delete it (41h) | ok | AX=0005h CF=1 |
| delete a file that does not exist (41h) | AX=0002h CF=1 | AX=0005h CF=1 |
| `INT 2Fh AX=10A6h` | AX=0001h CF=1 | AX=0001h CF=1 |

The repro is `shrrepro.asm` (NASM, attached). `DEL` and `REN` at the
prompt fail the same way. Windows 3.1x on ke2046 needs SHARE, so under it
Notepad's Save As to a new name says "This file is on a network drive with
create but no modify privileges". Write's Save As leaves a 0-byte file and
its `~WRI*.TMP` behind (compare https://www.both.org/?p=14420).

**Suggested fix:**
- Preserve SI (and DI) in `SHARE_IS_FILE_OPEN`, as `SHARE_OPEN_CHECK` does.
- Treat CF set on return as "SHARE cannot say": skip the check, as ke2043
  did, rather than reading AX=1 as "open".

## FDOS/share

**Title:** No release has `is_file_open` (2F/10A6h), which kernels 2044 and
later call on every delete and rename

Kernels 2044 to 2046 call `INT 2Fh AX=10A6h` before every delete and
rename while SHARE is installed (FDOS/kernel 5b9e8fed). SHARE gained it in
9c8db61c, but the newest release, 08/2006a, which FreeDOS 1.4 ships,
predates that. With it, every delete and rename fails with error 5 (see
the kernel report). Could you make a release built from current source,
ideally shipped beside the kernel releases? On its own a release brings
the kernel's SI bug into play (the kernel report), so it needs that fix
too.
