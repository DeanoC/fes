#!/usr/bin/env python3
"""parent_review.py MISTEROSS NEW_PROJECT OLD_PROJECT: method.md's G2, the fresh review of the Qsys parent.

NEW_PROJECT is the 486sx project the producer generated at the new clock and left before derivation
(it stopped at the parent check). OLD_PROJECT is main's 61.44 MHz pc386 board generation, derived.
MISTEROSS is a checkout of main (its shared_resets.py and its pc386/build.py, whose PARENT_SHA256 is
the reviewed 61.44 MHz parent). Checks, and prints PASS/FAIL for each:
1. NEW's parent with its window machine-clock interconnect's name replaced by OLD's name hashes to the
   reviewed 61.44 MHz digest;
2. every other pinned generated input of NEW has its pin;
3. NEW's interconnect module equals OLD's apart from its name (OLD's module is not rewritten by the
   derivation).
Prints the new parent's SHA-256 last. Exit 1 on any FAIL.
"""
import hashlib
import importlib.util
import re
import sys
from pathlib import Path

PARENT = 'peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v'
SYNTH = 'peripheral_subsys/peripheral_subsys/altera_mm_interconnect_1920/synth'
NAME = re.compile(rb'peripheral_subsys_altera_mm_interconnect_1920_[a-z0-9]{7}')


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main(root, new, old):
    root, new, old = Path(root), Path(new), Path(old)
    sys.path.insert(0, str(root))
    resets = load(root / 'boards/de25nano/scanout/shared_resets.py', 'shared_resets')
    reviewed = re.search(r'^PARENT_SHA256 = "([0-9a-f]{64})"', (root / 'boards/de25nano/pc386/build.py').read_text(), re.M).group(1)
    ok = True
    new_names = {p.stem.encode() for p in (new / SYNTH).glob('*.v')}
    old_names = {p.stem.encode() for p in (old / SYNTH).glob('*.v')}
    a, b = new_names - old_names, old_names - new_names
    if len(a) != 1 or len(b) != 1:
        print(f'FAIL interconnect names: new-only {sorted(a)}, old-only {sorted(b)}')
        return 1
    new_name, old_name = a.pop(), b.pop()
    print(f'interconnect: {new_name.decode()} (new) for {old_name.decode()} (61.44 MHz)')
    parent = (new / PARENT).read_bytes()
    renamed = parent.replace(new_name, old_name)
    check = hashlib.sha256(renamed).hexdigest() == reviewed and parent.count(new_name) >= 1
    ok &= check
    print(f"{'PASS' if check else 'FAIL'} 1 parent with the 61.44 MHz name: {hashlib.sha256(renamed).hexdigest()} "
          f"against reviewed {reviewed} ({parent.count(new_name)} occurrence(s) renamed)")
    for name, pin in sorted(resets.ORIGINAL_SHA256.items()):
        if name == resets.PARENT:
            continue
        digest = hashlib.sha256((new / name).read_bytes()).hexdigest()
        ok &= digest == pin
        print(f"{'PASS' if digest == pin else 'FAIL'} 2 pin {name}")
    body_new = (new / SYNTH / (new_name.decode() + '.v')).read_bytes().replace(new_name, b'X')
    body_old = (old / SYNTH / (old_name.decode() + '.v')).read_bytes().replace(old_name, b'X')
    ok &= body_new == body_old
    print(f"{'PASS' if body_new == body_old else 'FAIL'} 3 interconnect module equal apart from its name")
    print('new parent sha256', hashlib.sha256(parent).hexdigest())
    return 0 if ok else 1


if __name__ == '__main__':
    sys.exit(main(*sys.argv[1:]))
