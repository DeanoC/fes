"""Install the dedicated Nano handoff into a private pinned U-Boot export."""
import hashlib
import json
from pathlib import Path
import shutil

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def prepare(source, entry_build, bl31_sha256):
    if len(bl31_sha256) != 64 or any(c not in "0123456789abcdef" for c in bl31_sha256):
        raise ValueError("missing exact BL31 identity for the matched Nano loader")
    receipt_path = entry_build / "result.json"
    receipt = json.loads(receipt_path.read_text())
    image = entry_build / "entry.img"
    profile = HERE / "probe-profile.json"
    if (receipt["status"] != "built" or receipt["target"] != "de25-nano" or
            receipt["entry_image_address"] != 0x96080000 or
            receipt["ram_start"] != 0x84000000 or receipt["ram_end"] != 0x94000000 or
            receipt["uart_base"] != 0x10c02100 or receipt["entry_image_sha256"] != sha(image) or
            receipt["entry_image_size"] != image.stat().st_size or image.stat().st_size > 32 << 20 or
            receipt["firmware_profile_sha256"] != sha(profile)):
        raise ValueError("entry image is not bound to the source-derived serial-only Nano profile")
    for name, expected in receipt["sources"].items():
        if sha(ROOT / name) != expected:
            raise ValueError("entry producer source changed: " + name)
    bootm = source / "arch/arm/lib/bootm.c"
    text = bootm.read_text()
    anchor = "\tdm_remove_devices_active();\n\n\tcleanup_before_linux();"
    if text.count(anchor) != 1:
        raise ValueError("unexpected pinned U-Boot final cleanup path")
    kick = "static void do_nonsec_virt_switch(void)\n{\n\tsmp_kick_all_cpus();"
    if text.count(kick) != 1:
        raise ValueError("unexpected pinned U-Boot secondary CPU kick path")
    text = text.replace(kick, "static void do_nonsec_virt_switch(void)\n{\n\t/* Dedicated Nano probe: retain verified OFF secondary CPUs. */")
    bootm.write_text(text.replace(anchor, "\tdm_remove_devices_active();\n\t{\n\t\textern void fes_nano_late_handoff(void);\n\t\tfes_nano_late_handoff();\n\t}\n\n\tcleanup_before_linux();"))
    for src, dest in (("handoff.c", "fes_nano_policy.c"), ("handoff.h", "handoff.h"), ("uboot.c", "fes_nano.c")):
        shutil.copyfile(HERE / src, source / "cmd" / dest)
    diag_header = HERE.parent / "firmware/nano_bridge_diag.h"
    shutil.copyfile(diag_header, source / "cmd" / diag_header.name)
    makefile = source / "cmd/Makefile"
    makefile.write_text(makefile.read_text() + "\nobj-y += fes_nano.o fes_nano_policy.o\n")
    producer_sources = {str(p.relative_to(ROOT)): sha(p) for p in
                        (HERE / "prepare_uboot.py", HERE / "uboot.c", HERE / "handoff.c", HERE / "handoff.h", profile, diag_header)}
    build_id = hashlib.sha256(json.dumps({"sources": producer_sources, "entry": sha(image), "bl31": bl31_sha256}, sort_keys=True).encode()).hexdigest()[:16]
    digest = bytes.fromhex(sha(image))
    (source / "cmd/fes_nano_config.h").write_text(
        '/* Generated from the exact entry image and handoff sources. */\n#define FES_NANO_BOOT_ID "' + build_id +
        '"\n#define FES_NANO_IMAGE_SIZE ' + str(image.stat().st_size) +
        "ULL\n#define FES_NANO_IMAGE_SHA256 {" + ",".join(hex(b) for b in digest) + "}\n")
    return {"entry_image_sha256": sha(image), "entry_receipt_sha256": sha(receipt_path), "bl31_sha256": bl31_sha256,
            "profile_sha256": sha(profile), "build_id": build_id,
            "classification": "source-derived serial-only Nano handoff; physical gate not executed",
            "producer_sources": producer_sources}


DISABLE = ("NET", "NETDEVICES", "USB", "ENV_IS_IN_FAT", "ENV_IS_IN_UBI", "ENV_IS_IN_SPI_FLASH",
           "FAT_WRITE", "MMC_WRITE", "CMD_SAVEENV", "MMC_SDHCI_SDMA", "MMC_SDHCI_ADMA",
           "SPL_MMC_SDHCI_SDMA", "SPL_MMC_SDHCI_ADMA", "MMC_SDHCI_ADMA_64BIT")
ENABLE = ("NO_NET", "ENV_IS_NOWHERE", "CMD_BOOTI", "SHA256", "LMB", "CMD_BDI", "CMD_WDT")
