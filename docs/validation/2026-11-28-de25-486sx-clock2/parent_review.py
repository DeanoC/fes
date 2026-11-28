#!/usr/bin/env python3
"""Compare a generated pc386 Qsys parent with a reviewed one (#2264's review, SX references).

Supply the candidate and reference combined/hps directories. Read the
retained raw shared-reset inputs if derivation ran, or project/ if the
producer stopped at the parent pin. Print a candidate pin only on success.
"""

import argparse
import hashlib
import re
from pathlib import Path
import sys


# #2228's SX parent and #2264's reviewed DX parents at the same C0 (parent-c26.txt, parent-c24.txt there).
REFERENCE_PINS = {
    34: "fefb39d68d09b2fad37d35be5d2686d557b9f5c8c97d2eb41bbd80400a31f7e7",
    28: "0cccfdc88260c9bbde8c5fa1cbfbe1d8d0b689b69808ab7fd912ec42fae12435",
    26: "b900cf95959b71cb6496a9fd4df8c09ac67faac2897f0667562b488c24cd752c",
    24: "66d997f6a6a919f6e105fb8917e10e936f9442f2249cbf690c297c6b96091e2d",
}
INTERCONNECT_SYNTH = "peripheral_subsys/peripheral_subsys/altera_mm_interconnect_1920/synth"
WINDOW_INSTANCE = re.compile(
    rb"(?m)^[ \t]*(peripheral_subsys_altera_mm_interconnect_1920_[a-z0-9]{7})[ \t]+mm_interconnect_4[ \t]*\("
)


def raw_inputs(directory):
    retained = directory / "shared-reset-originals"
    return retained if retained.is_dir() else directory / "project"


def window_module(parent):
    names = WINDOW_INSTANCE.findall(parent)
    if len(names) != 1 or parent.count(names[0]) != 1:
        raise ValueError("expected one uniquely named window interconnect instance")
    return names[0]


def digest(data):
    return hashlib.sha256(data).hexdigest()


def review(candidate, reference, reference_pin, parent_path, original_pins):
    reference_raw = raw_inputs(reference)
    candidate_raw = raw_inputs(candidate)
    old = (reference_raw / parent_path).read_bytes()
    new = (candidate_raw / parent_path).read_bytes()
    if digest(old) != reference_pin:
        raise ValueError(f"reference parent {reference_raw / parent_path} does not match {reference_pin}")

    old_name, new_name = window_module(old), window_module(new)
    if new.replace(new_name, old_name) != old:
        raise ValueError("candidate parent differs beyond the window interconnect name")
    print(f"PASS reference parent {reference_pin}, window {old_name.decode()} -> {new_name.decode()}")

    for name, pin in sorted(original_pins.items()):
        if name == parent_path:
            continue
        for raw in (reference_raw, candidate_raw):
            actual = digest((raw / name).read_bytes())
            if actual != pin:
                raise ValueError(f"{raw / name} has SHA-256 {actual}, expected {pin}")
        print(f"PASS generated input {pin} {name}")

    old_module = reference / "project" / INTERCONNECT_SYNTH / (old_name.decode() + ".v")
    new_module = candidate / "project" / INTERCONNECT_SYNTH / (new_name.decode() + ".v")
    if new_module.read_bytes().replace(new_name, old_name) != old_module.read_bytes():
        raise ValueError("candidate window interconnect body differs beyond its name")
    print("PASS window interconnect module bytes after the same name replacement")
    return digest(new)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("misteross", type=Path, help="checkout's sources/misteross directory")
    parser.add_argument("candidate", type=Path, help="candidate combined/hps directory")
    parser.add_argument("reference", type=Path, help="reviewed combined/hps directory")
    parser.add_argument("--reference-c0", type=int, choices=tuple(REFERENCE_PINS), required=True)
    args = parser.parse_args()

    sys.path.insert(0, str(args.misteross.resolve()))
    from boards.de25nano.scanout import shared_resets

    try:
        pin = review(args.candidate, args.reference, REFERENCE_PINS[args.reference_c0],
                     shared_resets.PARENT, shared_resets.ORIGINAL_SHA256)
    except (OSError, ValueError) as error:
        parser.exit(1, f"FAIL {error}\n")
    print(f"candidate parent sha256 {pin}")


if __name__ == "__main__":
    main()
