"""Compare the assembled inner loop with the DOS benchmark executable."""
import hashlib
import subprocess


DOS_SHA256 = "7cdb244122cc607cc6f162524bfcac60f6d11d8ca2ff07473d6cd962311161fb"
DOS_START = 0xfffb


def check(executable, draw_object, events, runner):
    image = executable.read_bytes()
    if hashlib.sha256(image).hexdigest() != DOS_SHA256:
        raise ValueError("unexpected DOS executable hash")
    symbols = subprocess.check_output(["nm", "-n", draw_object], text=True)
    labels = {line.split()[2]: int(line.split()[0], 16)
              for line in symbols.splitlines() if len(line.split()) == 3}
    begin, end = labels["LNotLastSegment"], labels["LLastSegment"]
    flat = draw_object.with_suffix(".text")
    subprocess.run(["objcopy", "-O", "binary", "--only-section=.text", draw_object, flat], check=True)
    source = flat.read_bytes()[begin:end]
    actual = image[DOS_START:DOS_START + len(source)]
    normalized = bytearray(actual)

    # Use the assembler's relocations. All occurrences of a symbol must have
    # the same DOS address after removing the assembler's displacement addend.
    relocs = subprocess.check_output(["objdump", "-r", "-j", ".text", draw_object], text=True)
    addresses = {}
    for line in relocs.splitlines():
        fields = line.split()
        if len(fields) == 3 and fields[1] == "R_386_32":
            at = int(fields[0], 16) - begin
            if 0 <= at < len(source):
                part = slice(at, at + 4)
                base = int.from_bytes(actual[part], "little") - int.from_bytes(source[part], "little")
                if addresses.setdefault(fields[2], base) != base:
                    raise ValueError(f"inconsistent DOS address for {fields[2]}")
                normalized[part] = source[part]
    if len(set(addresses.values())) != len(addresses):
        raise ValueError("distinct assembly symbols alias in the DOS executable")

    # Four clamp targets before this segment are four bytes earlier in DOS.
    # Both measured paths fall through all four. Check opcodes and displacement
    # deltas, retaining every other control-transfer byte for the full comparison.
    for at in (0x4f, 0x57, 0x63, 0x6b):
        part = slice(at + 2, at + 6)
        if (source[at:at + 2] not in (b"\x0f\x8c", b"\x0f\x87")
                or int.from_bytes(source[part], "little") - int.from_bytes(actual[part], "little") != 4):
            raise ValueError("unexpected clamp branch")
        normalized[part] = source[part]
    if normalized != source:
        raise ValueError("DOS segment differs from the assembled source")

    first = events[0][0]
    stream = []
    for index, (pc, code, _) in enumerate(events):
        offset = pc - first
        if not 0 <= offset <= len(actual) - len(code):
            raise ValueError("fixture leaves the verified DOS segment")
        if offset in (0x4f, 0x57, 0x63, 0x6b) and events[index + 1][0] != pc + len(code):
            raise ValueError("fixture takes a clamp branch")
        stream.append(f"{DOS_START + offset:x} {actual[offset:offset + len(code)].hex()}\n")
    pairs = subprocess.check_output([runner], input="".join(stream), text=True)
    print(f"  DOS executable: verified {len(source)} bytes, {len(events)} path instructions,"
          f" {len(pairs.splitlines())} pairs")
