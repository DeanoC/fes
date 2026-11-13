#!/usr/bin/env python3
"""Create a new 64 MiB FAT32 regular-file fixture; never opens a block device."""
import argparse
import hashlib
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("output", type=Path)
args = parser.parse_args()
path = args.output.resolve()
path.parent.mkdir(parents=True, exist_ok=True)
with path.open("xb") as image:
    image.truncate(64 * 1024 * 1024)
subprocess.run(["mformat", "-i", str(path), "-F", "-v", "FESBASE", "::"], check=True)
with tempfile.TemporaryDirectory(prefix="fes-base-fat32-", dir=path.parent) as temporary:
    config = Path(temporary) / "FESBOOT.CFG"
    config.write_bytes(b"format=fes-base-v1\nlauncher=tenfoot\n")
    subprocess.run(["mcopy", "-i", str(path), str(config), "::FESBOOT.CFG"], check=True)
with path.open("rb") as image:
    print(hashlib.file_digest(image, "sha256").hexdigest(), path)
