#!/usr/bin/env python3
"""make_floppies.py: the F3 kit smoke's three floppy images.

  make_floppies.py OUT [NONCE]

Writes OUT/a144.img (1.44M, label F3A, FESF3A.TXT), OUT/b12.img (1.2M, label
F3B, FESF3B.TXT) and OUT/c12.img (1.2M, label F3C, FESF3C.TXT, the disk the
live-insert step puts in B:), each file "F3 <disk> DRIVE <NONCE>", plus OUT/nonce.
NONCE defaults to 8 random hex digits. Needs mtools.

The boot sector must end in 55AA: the kit drive's FreeDOS kernel ignores a
floppy BPB without it, falls back to a default one and shows an empty drive
(serial 1FC2-0004). mformat leaves the signature off, so it is written here,
with boot code of INT 18h at 3Eh so the BIOS goes on to boot the hard disk.
"""
import os, secrets, subprocess, sys
from pathlib import Path

out = Path(sys.argv[1])
nonce = sys.argv[2] if len(sys.argv) > 2 else secrets.token_hex(4).upper()
out.mkdir(parents=True, exist_ok=True)
for drive, name, size, kib in (('A', 'a144.img', 1474560, 1440), ('B', 'b12.img', 1228800, 1200),
                               ('C', 'c12.img', 1228800, 1200)):
    image = out / name
    image.write_bytes(bytes(size))
    subprocess.run(['mformat', '-i', str(image), '-f', str(kib), '-v', f'F3{drive}', '-N', '42BFD7DB', '::'], check=True)
    with image.open('r+b') as f:
        f.seek(0x3E); f.write(b'\xcd\x18')
        f.seek(510); f.write(b'\x55\xaa')
    text = out / f'FESF3{drive}.TXT'
    text.write_bytes(f'F3 {drive} DRIVE {nonce}\r\n'.encode())
    subprocess.run(['mcopy', '-i', str(image), str(text), f'::FESF3{drive}.TXT'], check=True,
                   env={**os.environ, 'MTOOLS_NO_VFAT': '1'})
(out / 'nonce').write_text(nonce + '\n')
print(nonce)
