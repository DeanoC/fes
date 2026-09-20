#!/usr/bin/env python3
"""Generate deterministic, synthetic conformance vectors (never kit data)."""
import argparse
import hashlib
import json
from pathlib import Path
import struct

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "testdata/core-persistence-v1"


def record(core="fes.pong", layout="fes.pong.progress", major=1, minor=0, words=(1, 0)):
    data = (b"FESDATA1" + hashlib.sha256(core.encode()).digest()
            + struct.pack("<HHHH", len(layout), major, minor, len(words))
            + layout.encode() + struct.pack("<" + "H" * len(words), *words))
    return data + hashlib.sha256(data).digest()


def records():
    valid = []
    for name, words in (("defaults", (1, 0)), ("fast-rally", (2, 17)), ("saturated-rally", (0, 65535))):
        data = record(words=words)
        valid.append({"name": name, "words": list(words), "hex": data.hex(), "revision": hashlib.sha256(data).hexdigest()})
    default = record()
    invalid = {
        "bad-magic": b"X" + default[1:],
        "bad-checksum": default[:-1] + bytes([default[-1] ^ 1]),
        "wrong-core": record(core="fes.other"),
        "wrong-layout": record(layout="fes.pong.other"),
        "wrong-major": record(major=2),
        "wrong-minor": record(minor=1),
        "wrong-count": record(words=(1,)),
        "invalid-speed": record(words=(3, 0)),
        "truncated": default[:-1],
        "trailing-byte": default + b"\x00",
        "oversize": b"\x00" * 689,
        "empty": b"",
    }
    return {"format": 1, "core_id": "fes.pong", "layout": {"id": "fes.pong.progress", "major": 1, "minor": 0},
            "valid": valid, "invalid": [{"name": name, "hex": data.hex()} for name, data in invalid.items()]}


def exchanges():
    original = json.loads((ROOT / "testdata/fes-gp-v1/exchanges.json").read_text())
    rows = []
    toggle = False

    def add(name, opcode, index=0, argument=0, data=0, error=False):
        nonlocal toggle
        fields = opcode << 24 | index << 16 | argument
        gpo = [fields | (int(toggle) << 31), fields | (int(not toggle) << 31)]
        toggle = not toggle
        gpi = 0xf5000000 | (int(toggle) << 23) | (int(error) << 22) | data
        rows.append(dict(name=name, opcode=opcode, index=index, argument=argument,
                         gpo=gpo, gpi=gpi, data=data, error=error))

    # Same discovery, reset and button exchanges, with this core's four live
    # capabilities. The legacy fixture remains byte-for-byte unchanged.
    for row in original["exchanges"]:
        command = row["gpo"][-1]
        data = 15 if row["name"] == "identity-word-07" else row["data"]
        add(row["name"], (command >> 24) & 127, (command >> 16) & 255,
            command & 65535, data, bool(row["gpi"] & 0x00400000))
    for index, value in enumerate((2, 1, 1, 0)):
        add(f"data-info-{index}", 7, index, data=value)
    add("invalid-info-index", 7, 4, data=2, error=True)
    add("read-before-freeze", 5, data=4, error=True)
    add("begin-restore", 4, argument=1)
    add("write-speed", 6, argument=2)
    add("incomplete-commit", 4, argument=2, data=4, error=True)
    add("invalid-write-index", 6, 2, 7, data=2, error=True)
    add("write-rally", 6, 1, 17)
    # Words are opaque to the transfer; semantic validation is atomic at commit.
    add("stage-invalid-speed", 6, argument=3)
    add("invalid-speed-commit", 4, argument=2, data=3, error=True)
    add("correct-speed", 6, argument=2)
    add("commit", 4, argument=2)
    add("write-after-commit", 6, argument=1, data=4, error=True)
    add("release", 2, argument=1)
    add("begin-while-running", 4, argument=1, data=4, error=True)
    add("freeze", 4)
    add("freeze-again", 4)
    add("snapshot-speed", 5, data=2)
    add("snapshot-rally", 5, 1, data=17)
    add("invalid-read-index", 5, 2, data=2, error=True)
    add("resume", 4, argument=3)
    add("read-after-resume", 5, data=4, error=True)
    return {"initial_request_toggle": False, "build_id": original["build_id"], "exchanges": rows}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    for filename, value in (("records.json", records()), ("exchanges.json", exchanges())):
        path = OUTPUT / filename
        contents = json.dumps(value, indent=2) + "\n"
        if args.check:
            if not path.is_file() or path.read_text() != contents:
                raise SystemExit(f"fixture differs: {path}")
        else:
            OUTPUT.mkdir(parents=True, exist_ok=True)
            path.write_text(contents)


if __name__ == "__main__":
    main()
