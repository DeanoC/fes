"""Stage one cold Jitter RNG source selection and verify its linked BL31 placement."""
import hashlib
import json
from pathlib import Path
import re
import struct

import nano_firmware_mmu as mmu
import nano_firmware_ram as ram

HERE = Path(__file__).resolve().parent
SERVICE = Path("services/std_svc/trng")
INCLUDE = SERVICE / "fes_jitter.mk"
BUILD = Path("build/agilex5/release/bl31")
VENDOR = "nano_jitter_vendor"
PORT = "nano_jitter_port"
LOCK = "sources.json"
LOCK_SHA256 = "51ba9f6d26f42259aa145eee0874979df32e1601f8b38e80e66793a633567ba1"
UPSTREAM_COMMIT = "fbbe36ed459a9f4eeb6caecfe66efe581a638fba"
HOOKS = {
    "seed": ("fes_nano_jitter_seed",),
    "capture": ("fes_nano_jitter_record_open", "fes_nano_jitter_record_sample",
                "fes_nano_jitter_record_close"),
}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def selected(mode):
    if mode not in HOOKS:
        raise ValueError("select either seed or capture for the Jitter RNG build")
    return mode


def read(path):
    if path.resolve(strict=True) != path or not path.is_file():
        raise ValueError("regular Jitter RNG source required: " + str(path))
    return path.read_bytes()


def inputs(mode):
    """Return firmware-relative names and digests for the complete private build."""
    selected(mode)
    manifest = read(HERE / VENDOR / LOCK)
    if digest(manifest) != LOCK_SHA256:
        raise ValueError("Jitter RNG upstream source manifest changed")
    lock = json.loads(manifest)
    if lock["commit"] != UPSTREAM_COMMIT:
        raise ValueError("different upstream Jitter RNG revision")
    sources = {f"{VENDOR}/{LOCK}": LOCK_SHA256}
    for name, expected in lock["files"].items():
        if digest(read(HERE / VENDOR / name)) != expected:
            raise ValueError("upstream Jitter RNG input changed: " + name)
        sources[f"{VENDOR}/{name}"] = expected
    sources["nano_jitter.h"] = digest(read(HERE / "nano_jitter.h"))
    required = {"arena.c", "arena.h", "adapter.c", "build_jitter.py"}
    for path in sorted((HERE / PORT).rglob("*")):
        if path.suffix in {".c", ".h", ".py", ".json"}:
            name = str(path.relative_to(HERE))
            if not re.fullmatch(r"[a-zA-Z0-9_./-]+", name):
                raise ValueError("invalid portable Jitter RNG source name")
            sources[name] = digest(read(path))
            required.discard(str(path.relative_to(HERE / PORT)))
    if required:
        raise ValueError("missing portable Jitter RNG inputs: " + ", ".join(sorted(required)))
    return sources


def makefile(mode, sources):
    # MAKE_BL expands MODULE_OBJS after setting BL. This makes the object a
    # normal BL31 link prerequisite and puts its libc imports before the archives.
    prerequisites = " ".join(str(SERVICE / name) for name in sorted(sources))
    return (f"FES_JITTER_OBJECT := $(BUILD_PLAT)/bl31/fes_jitter.o\n"
            "MODULE_OBJS += $(if $(filter BL31,$(BL)),$(FES_JITTER_OBJECT))\n"
            f"$(FES_JITTER_OBJECT): {INCLUDE} {prerequisites}\n"
            '\t@mkdir -p "$(@D)"\n'
            f'\tpython3 -I -B {SERVICE}/{PORT}/build_jitter.py '
            f'--cc "$(CROSS_COMPILE)gcc" --output "$@" --mode {selected(mode)}\n').encode()


def installed_files(sources, rules):
    return {**{str(SERVICE / name): sha for name, sha in sources.items()}, str(INCLUDE): digest(rules)}


def install(tfa_source, mode):
    source = Path(tfa_source).resolve(strict=True)
    sources = inputs(mode)
    rules = makefile(mode, sources)
    installed = installed_files(sources, rules)
    for name, expected in installed.items():
        dest = source / name
        if dest.resolve() != dest:
            raise ValueError("redirected private Jitter RNG build input")
        if dest.exists():
            if digest(read(dest)) != expected:
                raise ValueError("conflicting private Jitter RNG build input: " + name)
            continue
        data = rules if name == str(INCLUDE) else read(HERE / Path(name).relative_to(SERVICE))
        if digest(data) != expected:
            raise ValueError("Jitter RNG source changed during installation")
        dest.parent.mkdir(parents=True, exist_ok=True)
        with dest.open("xb") as out:
            out.write(data)
    return installed


def object_layout(data):
    """Read sections and symbols directly from the selected AArch64 relocatable."""
    if (len(data) < 64 or data[:7] != b"\x7fELF\x02\x01\x01" or
            struct.unpack_from("<HH", data, 16) != (1, 183)):
        raise ValueError("AArch64 relocatable Jitter RNG object required")
    start = struct.unpack_from("<Q", data, 40)[0]
    stride, count, strings = struct.unpack_from("<HHH", data, 58)
    if stride != 64 or not 0 < strings < count or start + count * stride > len(data):
        raise ValueError("invalid Jitter RNG section table")
    headers = list(struct.iter_unpack("<IIQQQQIIQQ", data[start:start + count * stride]))
    names = headers[strings]
    if names[1] != 3 or names[4] + names[5] > len(data):
        raise ValueError("invalid Jitter RNG section names")
    names = data[names[4]:names[4] + names[5]]
    def string(table, pos):
        stop = table.find(b"\0", pos)
        if stop < 0:
            raise ValueError("invalid Jitter RNG ELF string")
        return table[pos:stop].decode("ascii")

    section_names = [string(names, h[0]) for h in headers]
    allocated = {}
    symbols = []
    imports = set()
    for name, header in zip(section_names, headers):
        _, kind, flags, _, offset, size, link, _, _, stride = header
        if kind != 8 and offset + size > len(data):
            raise ValueError("truncated Jitter RNG section")
        body = data[offset:offset + size]
        if kind == 2:
            if symbols or stride != 24 or size % 24 or link >= count or headers[link][1] != 3:
                raise ValueError("invalid Jitter RNG symbol table")
            tables = headers[link]
            strings = data[tables[4]:tables[4] + tables[5]]
            for pos, info, _, index, value, _ in struct.iter_unpack("<IBBHQQ", body):
                sym = string(strings, pos)
                if not index and info >> 4:
                    imports.add(sym)
                symbols.append((sym, index, value))
        if not flags & 2 or not size:
            continue
        if flags & 1:
            allowed = name == ".data.fes_jent" and flags == 3
        else:
            allowed = ((name.startswith(".text") and flags == 6) or
                       (name.startswith(".rodata") and flags == 2))
        if kind != 1 or not allowed or name in allocated:
            raise ValueError("unrecognized Jitter RNG allocation: " + name)
        allocated[name] = body
    if not symbols or imports - {"memcpy", "memset", "snprintf", "strlen"}:
        raise ValueError("unexpected Jitter RNG imports")
    if len(allocated.get(".data.fes_jent", b"")) < 192 * 1024:
        raise ValueError("missing private Jitter RNG workspace")
    relocations = {}
    for _, kind, _, _, offset, size, link, target, _, stride in headers:
        if kind != 4:
            continue
        if (stride != 24 or size % 24 or link >= count or headers[link][1] != 2 or
                target >= count or section_names[target] in relocations):
            raise ValueError("invalid Jitter RNG relocation table")
        records = []
        for pos, info, addend in struct.iter_unpack("<QQq", data[offset:offset + size]):
            index = info >> 32
            if index >= len(symbols):
                raise ValueError("invalid Jitter RNG relocation symbol")
            sym, section, value = symbols[index]
            if section >= count:
                raise ValueError("invalid Jitter RNG relocation section")
            records.append((pos, info & 0xffffffff, sym, section_names[section], value + addend))
        relocations[section_names[target]] = records
    return allocated, relocations


def placements(map_text, object_path, sections):
    """Read GNU ld's retained input contributions after its discarded-section list."""
    marker = "\nLinker script and memory map\n"
    if map_text.count(marker) != 1:
        raise ValueError("expected one BL31 link map")
    memory = map_text.split(marker)[1]
    rows = re.finditer(
        r"^ (\.[^\s]+)(?:[ \t]+|\n[ \t]+)(0x[0-9a-f]+)[ \t]+(0x[0-9a-f]+)[ \t]+" +
        re.escape(str(object_path)) + r"[ \t]*$", memory, re.M)
    placed = {}
    for row in rows:
        section = row[1]
        if section not in sections:
            continue
        addr, size = int(row[2], 16), int(row[3], 16)
        if section in placed or size != len(sections[section]):
            raise ValueError("Jitter RNG link contribution changed: " + section)
        placed[section] = addr
    if set(placed) != sections.keys():
        raise ValueError("incomplete Jitter RNG input contributions in BL31 link")
    previous_end = 0
    for name, address in sorted(placed.items(), key=lambda item: item[1]):
        if address < previous_end:
            raise ValueError("overlapping Jitter RNG input contributions")
        previous_end = address + len(sections[name])
    return placed


def relocated_bytes(raw, records, base, placed, symbols):
    """Reapply the three static AArch64 relocation types emitted by this port."""
    if not records:
        return raw
    code = bytearray(raw)
    for pos, kind, symbol, section, offset in records:
        if pos % 4 or pos + 4 > len(code):
            raise ValueError("Jitter RNG relocation outside its section")
        if section:
            target = placed.get(section)
        else:
            target = symbols.get(symbol, (None, 0))[0]
        if target is None:
            raise ValueError("Jitter RNG relocation to an unlinked symbol")
        target += offset
        place = base + pos
        word, = struct.unpack_from("<I", code, pos)
        if kind == 283:  # R_AARCH64_CALL26
            diff = target - place
            if diff % 4 or not -(1 << 27) <= diff < 1 << 27:
                raise ValueError("out-of-range Jitter RNG call relocation")
            word = (word & 0xfc000000) | ((diff >> 2) & 0x3ffffff)
        elif kind == 275:  # R_AARCH64_ADR_PREL_PG_HI21
            pages = (target >> 12) - (place >> 12)
            if not -(1 << 20) <= pages < 1 << 20:
                raise ValueError("out-of-range Jitter RNG page relocation")
            word = (word & ~0x60ffffe0) | ((pages & 3) << 29) | (((pages >> 2) & 0x7ffff) << 5)
        elif kind == 277:  # R_AARCH64_ADD_ABS_LO12_NC
            word = (word & ~(0xfff << 10)) | ((target & 0xfff) << 10)
        else:
            raise ValueError("unsupported Jitter RNG relocation type")
        struct.pack_into("<I", code, pos, word)
    return code


def verify_link(image, mode, object_path, relocatable, map_text):
    if (len(image) < 64 or image[:7] != b"\x7fELF\x02\x01\x01" or
            struct.unpack_from("<HH", image, 16) != (2, 183)):
        raise ValueError("AArch64 executable BL31 required")
    symbols = ram.symbols(image)
    object_symbols = ram.symbols(relocatable)
    bounds = mmu.layout(symbols)
    mmu.verify_table_bytes(image, bounds)
    sections, relocations = object_layout(relocatable)
    placed = placements(map_text, object_path, sections)
    hooks = HOOKS[selected(mode)]
    for options in HOOKS.values():
        for name in options:
            if name not in hooks:
                if name in symbols or name in object_symbols:
                    raise ValueError("other Jitter RNG mode linked: " + name)
                continue
            section = ".text." + name
            addr, size = symbols.get(name, (0, 0))
            if (not size or object_symbols.get(name) != (0, size) or placed.get(section) != addr
                    or len(sections.get(section, b"")) != size):
                raise ValueError("unmatched Jitter RNG hook: " + name)
    for section, address in placed.items():
        expected = sections[section]
        mutable = section == ".data.fes_jent"
        lower, upper = ((bounds["data_start"], bounds["data_end"]) if mutable
                        else (mmu.DDR_BASE, bounds["ro_end"]))
        if not lower <= address < address + len(expected) <= upper:
            raise ValueError("Jitter RNG placement outside protected EL3 region: " + section)
        actual, _ = mmu.file_bytes(image, address, len(expected))
        if actual != relocated_bytes(expected, relocations.get(section, ()), address, placed, symbols):
            raise ValueError("linked Jitter RNG input bytes changed: " + section)
    return {"hooks": {k: {"address": symbols[k][0], "bytes": symbols[k][1]} for k in hooks},
            "mutable": {"address": placed[".data.fes_jent"], "bytes": len(sections[".data.fes_jent"])},
            "mapped_input_sections": len(placed), "elf_sha256": digest(image),
            "object_sha256": digest(relocatable)}


def verify(tfa_source, elf_bytes, mode):
    source = Path(tfa_source).resolve(strict=True)
    sources = inputs(mode)
    expected = installed_files(sources, makefile(mode, sources))
    if any(digest(read(source / name)) != sha for name, sha in expected.items()):
        raise ValueError("installed Jitter RNG build inputs changed")
    path = source / BUILD / "fes_jitter.o"
    map_text = read(source / BUILD / "bl31.map").decode()
    proof = verify_link(elf_bytes, mode, path, read(path), map_text)
    return {"mode": mode, "upstream_revision": UPSTREAM_COMMIT, "installed": expected, **proof}
