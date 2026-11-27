"""Build separate CPU jitter measurement and finite boot entropy firmware."""
import hashlib
import struct
from pathlib import Path

import nano_firmware_mmu
import nano_firmware_ram
import nano_jitter
import nano_trng

HERE = Path(__file__).resolve().parent
DIR = Path("services/std_svc/trng")
WRAPPER = "nano_boot_jitter.c"
CAPTURE = ("jitter_capture.c", "jitter_capture.h")
MODES = ("capture", "seed")


def local_sources(mode):
    if mode not in MODES:
        raise ValueError("explicit CPU jitter mode required")
    return (WRAPPER, *CAPTURE) if mode == "capture" else (WRAPPER,)


def inputs(mode):
    yield from (HERE / p for p in local_sources(mode))
    yield from (Path(__file__), Path(nano_trng.__file__), Path(nano_jitter.__file__))
    yield from (HERE / p for p in nano_jitter.inputs(mode))


def make_suffix(mode):
    text = "\ninclude services/std_svc/trng/fes_jitter.mk\nDEFINES += -DFES_NANO_CPU_JITTER\n"
    if mode == "capture":
        text += "DEFINES += -DFES_NANO_JITTER_CAPTURE\nBL31_SOURCES += services/std_svc/trng/jitter_capture.c\n"
    return text + "BL31_SOURCES += services/std_svc/trng/nano_boot_jitter.c\n"


def install(source, mode):
    paths = local_sources(mode)
    mk = source / nano_trng.MAKEFILE
    if nano_trng.digest(mk) != nano_trng.DEFAULTS[nano_trng.MAKEFILE]:
        raise ValueError("unexpected vendor BL31 makefile")
    installed = dict(nano_jitter.install(source, mode))
    for name in paths:
        with (source / DIR / name).open("xb") as out:
            out.write((HERE / name).read_bytes())
        installed[str(DIR / name)] = nano_trng.digest(HERE / name)
    if mode == "seed":
        nano_trng.install_pool(source)
        installed[str(nano_trng.POOL)] = nano_trng.digest(source / nano_trng.POOL)
    with mk.open("a") as out:
        out.write(make_suffix(mode))
    installed[str(nano_trng.MAKEFILE)] = nano_trng.digest(mk)
    return installed


def verify(source, image, mode, installed):
    paths = local_sources(mode)
    if not isinstance(installed, dict) or not installed:
        raise ValueError("missing CPU jitter source receipt")
    mk = (source / nano_trng.MAKEFILE).read_bytes()
    suffix = make_suffix(mode).encode()
    if not mk.endswith(suffix) or hashlib.sha256(mk[:-len(suffix)]).hexdigest() != nano_trng.DEFAULTS[nano_trng.MAKEFILE]:
        raise ValueError("unexpected linked CPU jitter makefile")
    proof = nano_jitter.verify(source, image, mode)
    expected = dict(proof["installed"])
    expected[str(nano_trng.MAKEFILE)] = hashlib.sha256(mk).hexdigest()
    for name in paths:
        expected[str(DIR / name)] = nano_trng.digest(HERE / name)
    if mode == "seed":
        expected[str(nano_trng.POOL)] = nano_trng.PATCHED_POOL_SHA256
    if installed != expected or any(nano_trng.digest(source / name) != sha for name, sha in expected.items()):
        raise ValueError("CPU jitter firmware differs from the selected sources")
    table = nano_firmware_ram.symbols(image)
    required = {"fes_nano_boot_jitter", "fes_nano_mmu_platform_boot"}
    capture = {"fes_jitter_capture", "fes_jitter_capture_buffer"}
    seed = {"fes_nano_jitter_seed", "plat_entropy_setup", "plat_get_entropy", "trng_smc_handler"}
    if not (required | (capture if mode == "capture" else seed)) <= table.keys() or (
            seed if mode == "capture" else capture).intersection(table):
        raise ValueError("wrong jitter provider or diagnostic entry in BL31")
    # The cold hook may compile to BL or a tail B. Refuse an unused linked wrapper.
    caller, length = table["fes_nano_mmu_platform_boot"]
    target = table["fes_nano_boot_jitter"][0]
    code, _ = nano_firmware_mmu.file_bytes(image, caller, length)
    offsets = [i for i, (insn,) in enumerate(struct.iter_unpack("<I", code))
               if insn & 0x7c000000 == 0x14000000 and
               ((target - (caller + 4 * i)) % (1 << 28)) == (insn & 0x03ffffff) << 2]
    if len(offsets) != 1:
        raise ValueError("missing unique branch from the cold MMU handoff to CPU jitter")
    regions = (("fes_jitter_capture_buffer", 1000000),) if mode == "capture" else (
        ("fes_nano_boot_entropy", 136), ("fes_nano_trng_pool", 48), ("plat_trng_uuid", 16))
    return dict(mode=mode, protocol="a55-jitterentropy", cold_bytes=128 if mode == "seed" else 0,
                capture_terminal=mode == "capture", library=proof, installed=installed,
                placement=nano_trng.verify_storage(image, table, regions))
