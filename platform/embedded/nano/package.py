#!/usr/bin/env python3
"""Prepare a matched Nano probe SOF, SPL/FIT and test-SD image as files only."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
VENDOR_SOF = "e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09"
PARTITION_SECTOR, PARTITION_SECTORS = 32768, 262144
FIT_SECTOR = 512


def sha(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--uboot-build", required=True, type=Path)
    p.add_argument("--entry-build", required=True, type=Path)
    p.add_argument("--vendor-sof", required=True, type=Path)
    p.add_argument("--quartus-pfg", required=True, type=Path)
    p.add_argument("--output", required=True, type=Path)
    args = p.parse_args()
    output = args.output.absolute()
    if (output.exists() or not output.is_relative_to(ROOT / "out") or ".." in output.parts or
            any(x.is_symlink() for x in (output, *output.parents)) or any(c in str(output) for c in (",", "@"))):
        p.error("output must be a fresh non-symlink directory below this worktree's out/, without commas or @")
    uboot, entry = args.uboot_build.resolve(strict=True), args.entry_build.resolve(strict=True)
    vendor, pfg = args.vendor_sof.resolve(strict=True), args.quartus_pfg.resolve(strict=True)
    fit, spl, image = uboot / "build/u-boot.itb", uboot / "build/spl/u-boot-spl-dtb.hex", entry / "entry.img"
    u = json.loads((uboot / "result.json").read_text())
    e = json.loads((entry / "result.json").read_text())
    configuration = (uboot / "build/.config").read_text()
    if (not vendor.is_file() or sha(vendor) != VENDOR_SOF or u["status"] != "built" or
            u["fit_sha256"] != sha(fit) or u["spl_sha256"] != sha(spl) or not u["fit_atf_verified"] or
            u["native_handoff"]["entry_image_sha256"] != sha(image) or
            u["native_handoff"]["entry_receipt_sha256"] != sha(entry / "result.json") or
            u["native_handoff"].get("bl31_sha256") != u["bl31_sha256"] or
            u["native_handoff"]["profile_sha256"] != sha(HERE / "probe-profile.json") or
            e["entry_image_sha256"] != sha(image) or e["target"] != "de25-nano"):
        raise ValueError("FPGA reference, SPL, FIT, native entry or handoff profile differs from its receipt")
    if (sha(uboot / "build/.config") != u["config_sha256"] or
            "CONFIG_SYS_MMCSD_RAW_MODE_U_BOOT_SECTOR=0x200\n" not in configuration or
            "CONFIG_ENV_IS_NOWHERE=y\n" not in configuration or
            any("CONFIG_" + name + "=y\n" in configuration for name in
                ("NET", "USB", "MMC_SDHCI_ADMA", "MMC_SDHCI_SDMA", "SPL_MMC_SDHCI_ADMA", "SPL_MMC_SDHCI_SDMA"))):
        raise ValueError("bootloader media offset, environment or PIO-only handoff configuration differs")
    if fit.stat().st_size > (PARTITION_SECTOR - FIT_SECTOR) * 512:
        raise ValueError("FIT overlaps the FAT32 test partition")
    for name, expected in u["native_handoff"]["producer_sources"].items():
        if sha(ROOT / name) != expected:
            raise ValueError("handoff source changed since U-Boot build: " + name)
    if u["producer_sha256"] != sha(HERE.parent / "firmware/build_terasic_uboot.py"):
        raise ValueError("U-Boot producer source changed since the selected build")
    tool_ids = {}
    for command in ("mformat", "mmd", "mcopy"):
        tool = shutil.which(command)
        if not tool:
            raise ValueError("mtools is required: " + command)
        tool_ids[command] = {"path": str(Path(tool).resolve()), "sha256": sha(Path(tool))}
    inputs = {str(f): sha(f) for f in (fit, spl, image, vendor, pfg, uboot / "result.json", entry / "result.json",
                                     uboot / "build/.config", HERE / "probe-profile.json", Path(__file__))}
    inputs.update({str(ROOT / name): expected for name, expected in u["native_handoff"]["producer_sources"].items()})
    output.mkdir(parents=True)
    commands = []
    def run(label, command):
        argv = [str(x) for x in command]
        commands.append(argv)
        with (output / (label + ".log")).open("wb") as log:
            subprocess.run(argv, stdout=log, stderr=subprocess.STDOUT, check=True)
    shutil.copyfile(fit, output / "u-boot.itb")
    shutil.copyfile(spl, output / "u-boot-spl-dtb.hex")
    shutil.copyfile(image, output / "ENTRY.IMG")
    shutil.copyfile(vendor, output / "recovery-reference.sof")
    candidate = output / "native-probe.sof"
    run("pfg-combine", [pfg, "-c", vendor, candidate, "-o", "hps_path=" + str(spl)])
    run("pfg-info", [pfg, "-i", candidate])
    info = (output / "pfg-info.log").read_text(encoding="latin-1")
    if "Device name: A5EB013BB23BE4SCS" not in info or "HPS/FPGA configuration order: HPS_AFTER_INIT_DONE" not in info:
        raise ValueError("combined SOF has an unexpected Nano device or boot order")
    manifest = {"format": 1, "classification": "matched serial-only Nano probe candidate; physical boot gate pending",
                "board_serial": "TRWU8CBL", "native_entry_sha256": sha(image), "fit_sha256": sha(fit),
                "spl_sha256": sha(spl), "bl31_sha256": u["bl31_sha256"], "sof_sha256": sha(candidate),
                "recovery_reference_sof_sha256": VENDOR_SOF, "profile_sha256": sha(HERE / "probe-profile.json"),
                "native_handoff": u["native_handoff"], "fit_raw_sector": FIT_SECTOR,
                "fat32_start_sector": PARTITION_SECTOR, "fat32_sectors": PARTITION_SECTORS,
                "embedded_spl_independently_decoded": False,
                "limitations": ["gate compiled for Nano but not physically executed", "no native SD/USB/network/display/FPGA activation",
                                "test image is a regular file; no card, flash, kit or boot configuration was changed"]}
    (output / "BOOTCHAIN.JSON").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    disk = output / "nano-probe-sd.img"
    with disk.open("xb") as stream:
        stream.truncate((PARTITION_SECTOR + PARTITION_SECTORS) * 512)
    mbr = bytearray(512)
    mbr[446:462] = struct.pack("<B3sB3sII", 0, b"\xfe\xff\xff", 0x0c, b"\xfe\xff\xff", PARTITION_SECTOR, PARTITION_SECTORS)
    mbr[510:] = b"\x55\xaa"
    with disk.open("r+b") as stream:
        stream.write(mbr)
        stream.seek(FIT_SECTOR * 512)
        stream.write(fit.read_bytes())
    fat = str(disk) + "@@" + str(PARTITION_SECTOR * 512)
    run("fat-format", ["mformat", "-i", fat, "-F", "-H", str(PARTITION_SECTOR), "-v", "FESNANOTEST", "::"])
    run("fat-dir", ["mmd", "-i", fat, "::FES"])
    for source, target in ((output / "ENTRY.IMG", "::FES/ENTRY.IMG"),
                           (output / "u-boot.itb", "::u-boot.itb"), (output / "BOOTCHAIN.JSON", "::FES/BOOTCHAIN.JSON")):
        run("fat-write-" + source.name, ["mcopy", "-i", fat, source, target])
        recovered = output / (source.name + ".readback")
        run("fat-read-" + source.name, ["mcopy", "-i", fat, target, recovered])
        if sha(source) != sha(recovered):
            raise ValueError("FAT32 readback differs: " + source.name)
    with disk.open("rb") as stream:
        if stream.read(512) != mbr:
            raise ValueError("MBR changed during FAT assembly")
        stream.seek(FIT_SECTOR * 512)
        if stream.read(fit.stat().st_size) != fit.read_bytes():
            raise ValueError("raw FIT differs from selected firmware")
        stream.seek(PARTITION_SECTOR * 512)
        boot_sector = stream.read(512)
        if (struct.unpack_from("<H", boot_sector, 11)[0] != 512 or
                struct.unpack_from("<I", boot_sector, 28)[0] != PARTITION_SECTOR or
                struct.unpack_from("<I", boot_sector, 32)[0] != PARTITION_SECTORS or boot_sector[510:] != b"\x55\xaa"):
            raise ValueError("FAT32 BPB differs from the test partition geometry")
        backup = struct.unpack_from("<H", boot_sector, 50)[0]
        if not 0 < backup < struct.unpack_from("<H", boot_sector, 14)[0]:
            raise ValueError("invalid FAT32 backup boot sector")
        stream.seek((PARTITION_SECTOR + backup) * 512)
        if stream.read(512) != boot_sector:
            raise ValueError("FAT32 backup boot sector differs")
    if (any(sha(Path(name)) != expected for name, expected in inputs.items()) or
            any(sha(Path(tool["path"])) != tool["sha256"] for tool in tool_ids.values())):
        raise ValueError("boot-chain inputs changed during packaging")
    manifest.update(status="prepared", sd_image_sha256=sha(disk), sd_image_size=disk.stat().st_size,
                    commands=commands, inputs=inputs, tools=tool_ids, producer_sha256=sha(Path(__file__)))
    (output / "result.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print("Prepared Nano test files:", output)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        raise SystemExit("Nano boot-chain packaging: " + str(error))
