"""Assemble id Software's unmodified renderer and classify two specified paths."""
import argparse
import hashlib
import re
import subprocess
from pathlib import Path
from tempfile import TemporaryDirectory


HERE = Path(__file__).resolve().parent
SOURCES = {
    "d_draw.s": "6471ec615e7a619e8f8d9447736672c0eb5c4985b9bc3e4710a0e127dd1c62e2",
    "d_varsa.s": "f34636c3cea2672e7a82097cc7d1aad4e408f4909a239a5ee2d97db9ed3a84d8",
    "asm_i386.h": "f27fc0fe4f95a79387006e5c3989a43a679a5674b4818035c734780e520b6e7d",
    "quakeasm.h": "91a98f65c3f30886b02216e19131d23c4ae32b00000a104d3587f68ad1ac8ee4",
    "asm_draw.h": "5c7189a00905e97a434a61aff615245b9948aa20c63af40ad40de127bd23e621",
    "d_ifacea.h": "d6093ccdd43de7397c9eec38bcc6e87004a3cc1c5088e78d91786706022234d3",
}


def output(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout


def path(binary, width):
    listing = output("objdump", "-dw", binary)
    labels, insns = {}, {}
    for line in listing.splitlines():
        label = re.fullmatch(r"([0-9a-f]+) <(\w+)>:", line)
        if label:
            labels[label[2]] = int(label[1], 16)
        fields = line.strip().split("\t")
        if len(fields) == 3 and fields[0].endswith(":"):
            insns[int(fields[0][:-1], 16)] = (bytes.fromhex(fields[1]), fields[2].strip())

    # At u=v=0, s advances one texel/pixel, t remains on row 1, and 1/z is
    # constant. Both coordinates are in bounds. The high integer t step is zero.
    # Evaluate the rest of the branches from counttemp = width - 8; the
    # last-remaining-pixel path decrements that to zero before the final test.
    decisions = {
        "LClampLow2": False, "LClampHigh2": False,
        "LClampLow3": False, "LClampHigh3": False,
        "LZero": True, "LSetupNotLast2": width - 8 > 8,
        "LFDIVInFlight2": width - 8 == 1,
        "LNotLastSegment": width - 8 > 8,
    }
    events = []
    pc = labels["LNotLastSegment"]
    while True:
        code, asm = insns[pc]
        events.append((pc, code, asm))
        next_pc = pc + len(code)
        if asm.startswith("j"):
            target = re.search(r"<(\w+)>$", asm)[1]
            taken = asm.startswith("jmp") or decisions[target]
            if taken:
                next_pc = labels[target]
            if target == "LNotLastSegment":
                assert next_pc == labels["LNotLastSegment" if taken else "LLastSegment"]
                return events, taken
        pc = next_pc
        if len(events) > 200:
            raise ValueError("unexpected path through the span routine")


def run(quake, native, show, dos_exe=None, unicorn=False):
    for name, expected in SOURCES.items():
        if hashlib.sha256((quake / name).read_bytes()).hexdigest() != expected:
            raise ValueError(f"unexpected upstream contents in {name}")
    with TemporaryDirectory() as work:
        d = Path(work)
        for name in ("d_draw", "d_varsa"):
            output("gcc", "-m32", "-DELF", "-x", "assembler-with-cpp", "-I", quake,
                   "-c", quake / (name + ".s"), "-o", d / (name + ".o"))
        output("g++", "-std=c++17", "-O2", HERE.parent / "classify_stream.cpp", "-o", d / "pairs")
        for width in (17, 9):
            output("gcc", "-m32", f"-DSPAN_WIDTH={width}", "-c", HERE / "fixture.S",
                   "-o", d / "fixture.o")
            exe = d / "renderer"
            output("ld", "-m", "elf_i386", "-o", exe, d / "fixture.o", d / "d_draw.o", d / "d_varsa.o")
            if native:
                output(exe)    # independent check of all output pixels and a guard byte
            events, backedge = path(exe, width)
            if unicorn:
                from unicorn_check import check
                check(exe, events, backedge, width)
            stream = "".join(f"{pc:x} {code.hex()}\n" for pc, code, _ in events)
            pairs = output(d / "pairs", input=stream).splitlines()
            x87 = [i for i, (_, code, _) in enumerate(events) if 0xd8 <= code[0] <= 0xdf]
            print(f"width {width}, backedge {'taken' if backedge else 'fall-through'}, "
                  f"{len(events)} instructions, {len(pairs)} pairs, {len(x87)} x87 instructions")
            if dos_exe:
                from dos_check import check
                check(dos_exe, d / "d_draw.o", events, d / "pairs")
            for i, (_, _, asm) in enumerate(events):
                if asm.startswith("fdiv"):
                    print(f"  {len(events) - i - 1} subsequent instructions through the branch; "
                          f"{sum(j > i for j in x87)} of them x87")
            if show:
                first = {int(line.split()[0], 16) for line in pairs}
                for pc, code, asm in events:
                    print(f"  {pc:08x} {code.hex():16} {asm:34} {'U+V' if pc in first else ''}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("winquake", type=Path)
    parser.add_argument("--check-pixels", action="store_true", help="also run the Linux i386 ELF")
    parser.add_argument("--list", action="store_true", help="print ordered path, bytes and pair starts")
    parser.add_argument("--dos-exe", type=Path, help="also verify the benchmark QUAKE.EXE and classify its bytes")
    parser.add_argument("--unicorn", action="store_true", help="execute the pixel and branch checks in Unicorn")
    args = parser.parse_args()
    run(args.winquake, args.check_pixels, args.list, args.dos_exe, args.unicorn)
