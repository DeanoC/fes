"""Classify two straight-line loops in a hash-verified DOOM v1.9 DOS executable."""
import argparse
import hashlib
import json
import subprocess
from pathlib import Path
from tempfile import TemporaryDirectory


HERE = Path(__file__).resolve().parent
EXE_HASH = "b8020523561a5ad9706e009a52d61c578f37faafd85ac471962308406292ce27"
PROFILE_HASH = "c2c889cde72e78c2843d88a58a957967b11dcf6c82df717a796725b337d8c34e"
# File offsets, byte lengths and linear addresses in the historical H2 profile.
LOOPS = (("column", 0x5DE0C, 51, 0x18BDF8), ("span", 0x5E02C, 64, 0x18C018))


def checked_file(path, digest):
    data = path.read_bytes()
    if hashlib.sha256(data).hexdigest() != digest:
        raise ValueError(f"unexpected SHA-256 for {path}")
    return data


def run(exe, profile):
    checked_file(exe, EXE_HASH)
    data = json.loads(checked_file(profile, PROFILE_HASH))
    sites = {int(pc, 16): count for pc, count, _, _ in data["sites"]}
    with TemporaryDirectory() as tmp:
        pairs = Path(tmp) / "pairs"
        subprocess.run(["g++", "-std=c++17", "-O2", HERE.parent / "classify_stream.cpp",
                        "-o", pairs], check=True)
        for name, start, length, base in LOOPS:
            listing = subprocess.check_output([
                "objdump", "-Dwb", "binary", "-mi386", "-Mintel",
                f"--start-address={start}", f"--stop-address={start + length}", exe
            ], text=True)
            stream = []
            reported = []
            end = start
            for line in listing.splitlines():
                fields = line.strip().split("\t")
                if len(fields) != 3 or not fields[0].endswith(":"):
                    continue
                at = int(fields[0][:-1], 16)
                code = bytes.fromhex(fields[1])
                if at != end:
                    raise ValueError("disassembly has a gap")
                end = at + len(code)
                pc = base + at - start
                stream.append(f"{pc:x} {code.hex()}\n")
                if pc in sites:
                    reported.append(sites[pc])
            if end != start + length or code != bytes([0x75, (start - end) & 0xff]):
                raise ValueError("loop does not end at its expected backedge")
            out = subprocess.check_output([pairs], input="".join(stream), text=True)
            accepted = out.splitlines()
            print(f"{name}: {len(stream)} instructions, {len(accepted)} pairs",
                  f"({', '.join(accepted) or 'none'})")
            print(f"  historical top-sites: {len(reported)}/{len(stream)} starts,"
                  f" {sum(reported):,}/{data['retired']:,} retirements")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("exe", type=Path)
    parser.add_argument("h2_profile", type=Path)
    args = parser.parse_args()
    run(args.exe, args.h2_profile)
