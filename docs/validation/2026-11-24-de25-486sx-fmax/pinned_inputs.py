#!/usr/bin/env python3
"""pinned_inputs.py MISTEROSS PROJECT...: each generated project's shared-reset inputs against their pins.

MISTEROSS is a checkout's sources/misteross (for scanout/shared_resets.py's ORIGINAL_SHA256 and
pc386/build.py's PARENT_SHA256, the parent's pin for pc386). Each PROJECT is a rung's generated
project, which the producer left before derivation because it stopped at the parent's check. Prints,
per file, whether its SHA-256 is the pin.
"""
import hashlib
import importlib.util
import sys
from pathlib import Path


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main(root, *projects):
    root = Path(root)
    sys.path.insert(0, str(root))
    resets = load(root / 'boards/de25nano/scanout/shared_resets.py', 'shared_resets')
    parent_pin = None
    for line in (root / 'boards/de25nano/pc386/build.py').read_text().splitlines():
        if line.startswith('PARENT_SHA256 = '):
            parent_pin = line.split('"')[1]
    for project in projects:
        project = Path(project)
        print(project)
        for name, pin in sorted(resets.ORIGINAL_SHA256.items()):
            if name == resets.PARENT:
                pin = parent_pin
            digest = hashlib.sha256((project / name).read_bytes()).hexdigest()
            print(f"  {'pin' if digest == pin else 'DIFFERS'}  {digest[:16]}  {name}")


if __name__ == '__main__':
    main(*sys.argv[1:])
