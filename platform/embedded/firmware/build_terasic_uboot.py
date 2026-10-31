#!/usr/bin/env python3
"""Assemble a vendor U-Boot FIT around a locally built experimental BL31.

This produces files only. It does not program flash, alter media, or supply
an EL1 handoff for the native runtime.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import zlib

import build_terasic
import prepare_tfa

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
UBOOT_REVISION = "d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413"


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git(repo, *args):
    return subprocess.check_output(["git", "-C", str(repo), *args],
                                   env=dict(os.environ, GIT_OPTIONAL_LOCKS="0", GIT_NO_LAZY_FETCH="1"))


def private_path(path):
    path = path.absolute()
    return (".." not in path.parts and path.is_relative_to(ROOT / "out") and
            not any(p.is_symlink() for p in (path, *path.parents)))


def checked_bl31(bl31_build):
    receipt_path = bl31_build / "result.json"
    component = bl31_build / "source/build/agilex5/release/bl31.bin"
    elf = bl31_build / "source/build/agilex5/release/bl31/bl31.elf"
    if any(p.is_symlink() for p in (receipt_path, component, elf, *elf.parents)):
        raise ValueError("BL31 component or receipt is a symlink")
    receipt_bytes = receipt_path.read_bytes()
    receipt = json.loads(receipt_bytes)
    blob = component.read_bytes()
    image_elf = elf.read_bytes()
    if (receipt.get("status") != "built" or receipt.get("reference_revision") != prepare_tfa.TERASIC_REVISION or
            receipt.get("reference_git_blob") != prepare_tfa.TERASIC_BLOB or not blob or
            receipt.get("bl31_bin_size") != len(blob) or receipt.get("bl31_bin_sha256") != hashlib.sha256(blob).hexdigest() or
            receipt.get("bl31_elf_sha256") != hashlib.sha256(image_elf).hexdigest()):
        raise ValueError("BL31 component differs from its pinned vendor build receipt")
    entry, segments = build_terasic.bl31_load_segments(image_elf)
    if receipt.get("bl31_entry") != entry or receipt.get("bl31_load_segments") != [list(s) for s in segments]:
        raise ValueError("BL31 ELF memory ranges differ from its receipt")
    sources = (HERE / "build_terasic.py", HERE / "prepare_tfa.py", HERE / "generated/fes_embedded_control.hpp")
    if any(receipt.get("producer_sources", {}).get(str(p.relative_to(ROOT))) != sha(p) for p in sources):
        raise ValueError("BL31 producer sources differ from its receipt")
    return receipt_path, receipt_bytes, receipt, component, blob, sources, elf, image_elf, segments


def check_uboot_load(segments, size):
    """Check the initial U-Boot FIT payload against BL31's live ELF ranges."""
    start = 0x80200000
    if not size or start + size > 0xc0000000:
        raise ValueError("U-Boot load is outside the Nano's 1 GiB DRAM window")
    if any(start < end and begin < start + size for begin, end in segments):
        raise ValueError("U-Boot FIT payload overlaps BL31 loaded memory")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--reference-repo", required=True, type=Path)
    parser.add_argument("--bl31-build", required=True, type=Path,
                        help="completed build_terasic.py output in this worktree")
    parser.add_argument("--cross-prefix", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--jobs", type=int, default=2)
    args = parser.parse_args()
    repo = args.reference_repo.resolve()
    bl31_build = args.bl31_build.absolute()
    output = args.output.absolute()
    if not private_path(output) or output.exists() or not private_path(bl31_build) or not bl31_build.is_dir():
        parser.error("BL31 build and fresh output must be private paths under this worktree's out/")
    if not Path(args.cross_prefix).is_absolute() or not args.cross_prefix.endswith("-") or not 1 <= args.jobs <= 16:
        parser.error("cross prefix must be absolute and end in '-'; jobs must be 1..16")
    receipt_path, receipt_bytes, receipt, component, blob, sources, elf, image_elf, segments = checked_bl31(bl31_build)
    revision = git(repo, "rev-parse", "--verify", "HEAD^{commit}").decode().strip()
    if revision != UBOOT_REVISION or git(repo, "status", "--porcelain", "--untracked-files=all"):
        raise ValueError("Terasic U-Boot checkout must be clean and at the pinned revision")
    epoch = git(repo, "show", "-s", "--format=%ct", revision).decode().strip()
    if not epoch.isdigit():
        raise ValueError("invalid source timestamp")
    env = {key: os.environ[key] for key in ("PATH", "LD_LIBRARY_PATH", "PYTHONPATH", "SWIG_LIB", "TMPDIR") if key in os.environ}
    env.update(LC_ALL="C", SOURCE_DATE_EPOCH=epoch)
    tools = {}
    for name in ("gcc", "ld", "as", "ar", "objcopy", "objdump", "readelf", "nm"):
        path = Path(args.cross_prefix + name).resolve()
        if not path.is_file() or not os.access(path, os.X_OK):
            raise ValueError("missing executable cross tool " + name)
        tools[name] = {"path": str(path), "sha256": sha(path)}
    make = Path(shutil.which("make", path=env.get("PATH")) or "").resolve()
    if not make.is_file():
        raise ValueError("missing make")
    tools["make"] = {"path": str(make), "sha256": sha(make)}
    for name in ("python3", "dtc", "fdtget", "swig", "bison", "flex", "gcc"):
        path = Path(shutil.which(name, path=env.get("PATH")) or "").resolve()
        if not path.is_file():
            raise ValueError("missing host build tool " + name)
        tools["host_" + name] = {"path": str(path), "sha256": sha(path)}
    for name, option in (("cc1", "-print-prog-name=cc1"), ("libgcc", "-print-libgcc-file-name")):
        path = Path(subprocess.check_output([args.cross_prefix + "gcc", option], env=env, text=True).strip()).resolve()
        if not path.is_file():
            raise ValueError("missing compiler component " + name)
        tools[name] = {"path": str(path), "sha256": sha(path)}
    compiler_version = subprocess.check_output([args.cross_prefix + "gcc", "-v"], env=env, stderr=subprocess.STDOUT, text=True)
    actual_gcc = re.findall(r"(?m)^COLLECT_GCC=(.+)$", compiler_version)
    if len(actual_gcc) != 1 or not Path(actual_gcc[0]).is_file():
        raise ValueError("cannot identify underlying cross compiler")
    actual = Path(actual_gcc[0]).resolve()
    tools["gcc_binary"] = {"path": str(actual), "sha256": sha(actual)}
    producer_hash = sha(Path(__file__).resolve())
    output.mkdir(parents=True)
    build = output / "build"
    build.mkdir()
    (build / "bl31.bin").write_bytes(blob)
    commands = []
    def run(label, argv):
        argv = [str(x) for x in argv]
        commands.append(argv)
        with (output / (label + ".log")).open("wb") as log:
            subprocess.run(argv, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
    base = [make, "-C", repo, "O=" + str(build), "CROSS_COMPILE=" + args.cross_prefix]
    run("defconfig", [*base, "socfpga_agilex5_de25_nano_defconfig"])
    config = (build / ".config").read_text()
    if ("CONFIG_SPL_ATF=y\n" not in config or "CONFIG_CMD_ELF=y\n" not in config or
            "# CONFIG_ARMV8_SWITCH_TO_EL1 is not set\n" not in config):
        raise ValueError("unexpected vendor bootloader configuration")
    run("build", [*base, "-j", str(args.jobs)])
    fit = build / "u-boot.itb"
    spl = build / "spl/u-boot-spl-dtb.hex"
    dump = build / "tools/dumpimage"
    if not fit.is_file() or not fit.stat().st_size or not spl.is_file() or not spl.stat().st_size or not dump.is_file():
        raise ValueError("missing vendor U-Boot output")
    run("fit-info", [dump, "-l", fit])
    info = (output / "fit-info.log").read_text()
    positions = re.findall(r"(?m)^ Image (\d+) \(atf\)$", info)
    if len(positions) != 1 or not re.search(r"(?m)^\s+Firmware:\s+atf\s*$", info):
        raise ValueError("FIT does not select exactly one ATF image")
    fdtget = tools["host_fdtget"]["path"]
    def prop(node, name):
        return subprocess.check_output([fdtget, "-t", "s", str(fit), node, name], env=env, text=True).strip()
    names = subprocess.check_output([fdtget, "-l", str(fit), "/images"], env=env, text=True).splitlines()
    if (names != ["uboot", "atf", "fdt-0"] or int(positions[0]) != names.index("atf") or
            prop("/configurations", "default") != "board-0" or
            prop("/configurations/board-0", "firmware") != "atf" or
            prop("/configurations/board-0", "loadables") != "uboot" or
            prop("/configurations/board-0", "fdt") != "fdt-0" or
            prop("/images/atf", "type") != "firmware" or prop("/images/atf", "arch") != "arm64"):
        raise ValueError("unexpected FIT image selection")
    for name, address in (("atf", 0x80000000), ("uboot", 0x80200000)):
        recorded = subprocess.check_output([fdtget, "-t", "x", str(fit), "/images/" + name, "load"], env=env, text=True).strip()
        if int(recorded, 16) != address:
            raise ValueError("unexpected FIT load address: " + name)
    parts = {}
    for index, name in enumerate(names):
        extracted = output / ("extracted-bl31.bin" if name == "atf" else "extracted-" + name + ".bin")
        run("fit-extract-" + name, [dump, "-T", "flat_dt", "-p", str(index), "-o", extracted, fit])
        if prop("/images/" + name + "/hash", "algo") != "crc32":
            raise ValueError("unsupported FIT checksum")
        recorded = subprocess.check_output([fdtget, "-t", "x", str(fit), "/images/" + name + "/hash", "value"], env=env, text=True).strip()
        data = extracted.read_bytes()
        if not data or int(recorded, 16) != zlib.crc32(data):
            raise ValueError("FIT component checksum differs: " + name)
        parts[name] = {"sha256": hashlib.sha256(data).hexdigest(), "size": len(data), "crc32": recorded}
        if name == "atf" and data != blob:
            raise ValueError("FIT ATF bytes differ from the selected BL31 component")
        if name == "uboot":
            check_uboot_load(segments, len(data))
    board_model = subprocess.check_output([fdtget, "-t", "s", str(output / "extracted-fdt-0.bin"), "/", "model"], env=env, text=True).strip()
    if board_model != "SoCFPGA Agilex5 Terasic DE25-Nano":
        raise ValueError("FIT device tree is not the expected Nano model")
    if (component.read_bytes() != blob or elf.read_bytes() != image_elf or receipt_path.read_bytes() != receipt_bytes or
            sha(Path(__file__).resolve()) != producer_hash or
            git(repo, "rev-parse", "HEAD^{commit}").decode().strip() != revision or
            git(repo, "status", "--porcelain", "--untracked-files=all") or
            any(sha(Path(v["path"])) != v["sha256"] for v in tools.values()) or
            any(receipt["producer_sources"][str(p.relative_to(ROOT))] != sha(p) for p in sources)):
        raise ValueError("U-Boot build inputs changed")
    report = {"status": "built", "classification": "off-board Terasic U-Boot FIT and SPL; no native handoff or hardware acceptance",
              "uboot_revision": revision, "uboot_tree": git(repo, "rev-parse", revision + "^{tree}").decode().strip(),
              "bl31_sha256": hashlib.sha256(blob).hexdigest(), "bl31_receipt_sha256": sha(receipt_path),
              "fit_sha256": sha(fit), "fit_size": fit.stat().st_size, "spl_sha256": sha(spl), "spl_size": spl.stat().st_size,
              "fit_atf_index": int(positions[0]), "fit_atf_verified": True, "fit_components": parts,
              "bl31_load_segments": segments, "uboot_initial_load_nonoverlap": True,
              "device_tree_model": board_model, "cross_tools": tools,
              "producer_sha256": producer_hash,
              "compiler_version": compiler_version, "environment": env, "commands": commands,
              "limitations": ["FIT and SPL are not paired with an FPGA phase-one image",
                              "default bootelf is not an EL1/cache/ownership handoff",
                              "no native payload, media image, firmware signature or board test"]}
    (output / "result.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print("Built experimental FIT:", fit)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, json.JSONDecodeError, subprocess.SubprocessError) as error:
        raise SystemExit("Terasic U-Boot build: " + str(error))
