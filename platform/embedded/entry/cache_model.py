"""Construct the exact target table bytes after the loader layout is linked."""
import ctypes
from pathlib import Path
import re
import subprocess
import zlib

TABLE_BYTES = 22 * 4096


def symbols(nm, elf, environment):
    raw = subprocess.check_output([str(nm), '-n', str(elf)], env=environment, text=True)
    return {name: int(address, 16) for address, name in
            re.findall(r'(?m)^([0-9a-fA-F]+)\s+\w\s+(\S+)$', raw)}


def tables(library, base, ram_start, ram_end, image_start, image_end, uart):
    lib = ctypes.CDLL(str(library))
    plan = lib.fes_entry_map_plan
    plan.argtypes = [ctypes.c_void_p] + [ctypes.c_uint64] * 6
    plan.restype = ctypes.c_int
    result = (ctypes.c_uint64 * (TABLE_BYTES // 8))()
    if not plan(result, base, ram_start, ram_end, image_start, image_end, uart):
        raise ValueError('loader cache map exceeds its exact owned RAM/image/UART geometry')
    return bytes(result)


def link(run, gcc, nm, environment, objects, linker, address, library, ram_start, ram_end, uart, output):
    output = Path(output)
    def command(path, crc):
        return [str(gcc), '-nostdlib', '-static', '-no-pie', '-Wl,--build-id=none',
                '-Wl,-T,' + str(linker), '-Wl,--defsym=FES_ENTRY_IMAGE_ADDRESS=' + hex(address),
                '-Wl,--defsym=FES_ENTRY_MAP_CRC=' + hex(crc), *map(str, objects), '-o', str(path)]
    layout = output/'entry-layout.elf'
    run('link-layout', command(layout, 0))
    before = symbols(nm, layout, environment)
    raw = tables(library, before['fes_entry_tables'], ram_start, ram_end,
                 before['__image_start'], before['__image_end'], uart)
    crc = zlib.crc32(raw)
    elf = output/'entry.elf'
    run('link', command(elf, crc))
    after = symbols(nm, elf, environment)
    for name in ('_start', '__image_start', '__image_end', '__payload_start', '__payload_end',
                 '__stack_top', 'fes_entry_tables', 'fes_entry_vectors'):
        if before[name] != after[name]:
            raise ValueError('cache-map CRC relink changed loader layout: ' + name)
    if after['FES_ENTRY_MAP_CRC'] != crc or after['__image_end'] % 4096:
        raise ValueError('cache-map CRC or materialized page boundary differs')
    (output/'cache-map.bin').write_bytes(raw)
    return after, raw, crc
