"""Execute the linked IA-32 fixture and compare its observed span path."""
import struct
import subprocess

from unicorn import UC_ARCH_X86, UC_HOOK_CODE, UC_MODE_32, Uc
from unicorn.x86_const import UC_X86_REG_EAX, UC_X86_REG_EBX, UC_X86_REG_EIP, UC_X86_REG_ESP


def check(binary, events, backedge, width):
    elf = binary.read_bytes()
    assert elf[:7] == b"\x7fELF\x01\x01\x01"
    entry, phoff = struct.unpack_from("<II", elf, 24)
    phsize, phnum = struct.unpack_from("<HH", elf, 42)
    cpu = Uc(UC_ARCH_X86, UC_MODE_32)
    for i in range(phnum):
        kind, offset, va, _, filesz, memsz = struct.unpack_from("<6I", elf, phoff + i * phsize)
        if kind == 1:  # PT_LOAD
            page = va & ~0xfff
            cpu.mem_map(page, (va - page + memsz + 0xfff) & ~0xfff)
            cpu.mem_write(va, elf[offset:offset + filesz])
    cpu.mem_map(0x1000000, 65536)
    cpu.reg_write(UC_X86_REG_ESP, 0x100fff0)
    names = {}
    for line in subprocess.check_output(["nm", binary], text=True).splitlines():
        address, _, name = line.split()
        names[name] = int(address, 16)

    pcs = []
    cpu.hook_add(UC_HOOK_CODE, lambda _, pc, size, user_data: pcs.append(pc))
    cpu.emu_start(entry, names["linux_exit"], count=10000)
    if (cpu.reg_read(UC_X86_REG_EIP), cpu.reg_read(UC_X86_REG_EAX),
        cpu.reg_read(UC_X86_REG_EBX)) != (names["linux_exit"], 1, 0):
        raise ValueError("the assembled fixture did not finish its pixel checks successfully")
    if cpu.mem_read(names["pixels"], width + 1) != bytes(range(1, width + 1)) + b"\0":
        raise ValueError("unexpected framebuffer or guard pixel")
    expected = [pc for pc, code, asm in events]
    expected.append(names["LNotLastSegment" if backedge else "LLastSegment"])
    start = pcs.index(names["LNotLastSegment"])
    if pcs[start:start + len(expected)] != expected:
        raise ValueError("emulated program counters differ from the listed span path")
    print(f"  Unicorn verified {width} output pixels, guard byte, {len(events)} PCs and the branch target")
