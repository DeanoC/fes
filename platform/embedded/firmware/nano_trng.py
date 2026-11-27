"""Install and verify the optional standard TRNG service in private BL31 DDR."""
import hashlib
import struct
from pathlib import Path

import nano_firmware_ram

HERE = Path(__file__).resolve().parent
MAKEFILE = Path("bl31/bl31.mk")
POOL = Path("services/std_svc/trng/trng_entropy_pool.c")
BACKEND = Path("services/std_svc/trng/nano_trng.c")
SOURCE = HERE / BACKEND.name
PATCHED_POOL_SHA256 = "c51bc6dd3aa7b192774ec92b0278d5276e0cc9b455a6af04e25606afedf33d4d"
DEFAULTS = {
    MAKEFILE: "13b10b945ef3c7ccf6ac44a743b2fa4419373c289ddfc1d5adfeae4246eeff53",
    POOL: "435b6978f209e97c0d3d9080248bc42c9dc52baf812ddb0327f4fe5ddf68c30b",
}


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def install_pool(source):
    if digest(source / POOL) != DEFAULTS[POOL]:
        raise ValueError("unexpected vendor TRNG pool")
    pool = source / POOL
    # Keep all of TF-A's pool, cursor and spinlock in the same, already reserved
    # firmware data segment as the platform reservoir. No extra OCRAM BSS.
    implementation = pool.read_text()
    # Native UUID and seed requests use complete 64-bit words. Avoid the
    # vendor C expression shifting by 64 when all bits of a word are wanted.
    implementation = implementation.replace(
        "~0ULL >> (BITS_PER_WORD - (nbits % BITS_PER_WORD))",
        "~0ULL >> ((BITS_PER_WORD - (nbits % BITS_PER_WORD)) % BITS_PER_WORD)")
    begin = implementation.index("static uint64_t entropy[WORDS_IN_POOL];")
    end = implementation.index("static spinlock_t trng_pool_lock;") + len("static spinlock_t trng_pool_lock;")
    pool.write_text(implementation[:begin] + """struct {
    uint64_t words[WORDS_IN_POOL];
    uint32_t first, valid;
    spinlock_t lock;
} fes_nano_trng_pool __attribute__((section(".data.fes_boot_entropy")));
#define entropy fes_nano_trng_pool.words
#define entropy_bit_index fes_nano_trng_pool.first
#define entropy_bit_size fes_nano_trng_pool.valid
#define trng_pool_lock fes_nano_trng_pool.lock""" + implementation[end:])


def install(source):
    if any(digest(source / path) != expected for path, expected in DEFAULTS.items()):
        raise ValueError("unexpected vendor TRNG build or pool implementation")
    mk = source / MAKEFILE
    mk.write_text(mk.read_text().replace("services/std_svc/trng/trng_entropy_pool.c\n",
                                         f"services/std_svc/trng/trng_entropy_pool.c {BACKEND}\n"))
    install_pool(source)
    with (source / BACKEND).open("xb") as output:
        output.write(SOURCE.read_bytes())
    return {str(p): digest(source / p) for p in (MAKEFILE, POOL, BACKEND)}


def verify(image, source, installed):
    expected = {
        str(MAKEFILE): "3a783774a9dbcb22bd4d3b860be33fea2babc25e8a35577b6b2ec257d89e8694",
        str(POOL): PATCHED_POOL_SHA256,
        str(BACKEND): digest(SOURCE),
    }
    if installed != expected:
        raise ValueError("unexpected SDM boot entropy source selection")
    if any(digest(source / path) != expected for path, expected in installed.items()):
        raise ValueError("installed TRNG source changed during compilation")
    table = nano_firmware_ram.symbols(image)
    for name in ("plat_entropy_setup", "plat_get_entropy", "trng_smc_handler",
                 "fes_nano_sdm_control_mailbox_idle", "mailbox_send_cmd"):
        if not table.get(name, (0, 0))[1]:
            raise ValueError("missing linked TRNG dependency " + name)
    placement = verify_storage(image, table, (("fes_nano_boot_entropy", 136),
                                              ("fes_nano_trng_pool", 48), ("plat_trng_uuid", 16)))
    return {"source": "SDM FCS_RANDOM_GEN", "protocol": "agilex5-fcs-session",
            "cold_bytes": 128, "runtime_mailbox_calls": 0,
            "placement": placement, "installed": installed}


def verify_storage(image, table, regions):
    start, end = (table[name][0] for name in ("__DATA_START__", "__DATA_END__"))
    if not 0x80000000 <= start < end <= 0x80200000:
        raise ValueError("TRNG requires the reserved BL31 writable data segment")
    phoff = struct.unpack_from("<Q", image, 32)[0]
    phsize, count = struct.unpack_from("<HH", image, 54)
    if phsize != 56 or not 0 < count < 32:
        raise ValueError("unexpected firmware program headers")
    placement = {}
    for name, size in regions:
        address, actual = table.get(name, (0, 0))
        if actual != size or not start <= address < address + size <= end:
            raise ValueError(f"{name} is outside private BL31 data or has the wrong size")
        for other, (base, length) in table.items():
            if name != other and length and address < base + length and base < address + size:
                raise ValueError(f"{name} aliases {other}")
        loads = []
        for index in range(count):
            kind, flags, offset, va, pa, file_size, _, _ = struct.unpack_from("<IIQQQQQQ", image, phoff + index * phsize)
            if kind == 1 and flags & 2 and va == pa and va <= address and address + size <= va + file_size:
                loads.append(image[offset + address - va:offset + address - va + size])
        if loads != [bytes(size)]:
            raise ValueError(f"{name} must be sealed file-backed zeros")
        placement[name] = {"address": address, "bytes": size}
    return placement
