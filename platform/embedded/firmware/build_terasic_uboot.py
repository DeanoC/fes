#!/usr/bin/env python3
"""Assemble a vendor U-Boot FIT around a locally built experimental BL31.

This produces files only. It does not program flash, alter media, or supply
an EL1 handoff for the native runtime.
"""
import argparse
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
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


def checked_bl31(bl31_build, cold_profile=None):
    receipt_path = bl31_build / "result.json"
    component = bl31_build / "source/build/agilex5/release/bl31.bin"
    elf = bl31_build / "source/build/agilex5/release/bl31/bl31.elf"
    if any(p.is_symlink() for p in (receipt_path, component, elf, *elf.parents)):
        raise ValueError("BL31 component or receipt is a symlink")
    receipt_bytes = receipt_path.read_bytes()
    receipt = json.loads(receipt_bytes)
    if cold_profile is None:
        if receipt.get("bridge_mode") != "strict":
            raise ValueError("U-Boot packaging requires the strict bridge mode; owned-unready is a firmware component only")
    elif (receipt.get('bridge_mode')!='owned-cold-serial' or receipt.get('native_boot_profile')!=cold_profile or
          receipt.get('owned_bridge_binding',{}).get('boot_proof_installed') is not True or
          receipt.get('owned_bridge_binding',{}).get('secondary_release_locked') is not True):
        raise ValueError('native boot requires the matched owned cold serial SPL/BL31 profile')
    blob = component.read_bytes()
    image_elf = elf.read_bytes()
    if (receipt.get("status") != "built" or receipt.get("reference_revision") != prepare_tfa.TERASIC_REVISION or
            receipt.get("reference_git_blob") != prepare_tfa.TERASIC_BLOB or
            receipt.get("reset_reference_git_blob") != prepare_tfa.TERASIC_RESET_BLOB or not blob or
            receipt.get("bl31_bin_size") != len(blob) or receipt.get("bl31_bin_sha256") != hashlib.sha256(blob).hexdigest() or
            receipt.get("bl31_elf_sha256") != hashlib.sha256(image_elf).hexdigest()):
        raise ValueError("BL31 component differs from its pinned vendor build receipt")
    ram = build_terasic.nano_firmware_ram.checked(cold_profile)
    entry, segments = build_terasic.bl31_load_segments(image_elf,firmware_ram=ram,
        cold_chain=cold_profile['chain_sha256'] if ram else None)
    if ram is not None:
        binding=receipt.get('owned_bridge_binding',{}).get('firmware_ram')
        if not isinstance(binding,dict) or binding.get('contract')!=ram or binding.get('chain_sha256')!=cold_profile['chain_sha256']:
            raise ValueError('matched early EL3 memory reservation binding required')
        build_terasic.nano_firmware_ram.verify_installed(bl31_build/'source',binding)
    if receipt.get("bl31_entry") != entry or receipt.get("bl31_load_segments") != [list(s) for s in segments]:
        raise ValueError("BL31 ELF memory ranges differ from its receipt")
    sources = (HERE / "build_terasic.py", HERE / "prepare_tfa.py", HERE / "generated/fes_embedded_control.hpp",
               prepare_tfa.DIAG_HEADER, prepare_tfa.DIAG_HELPERS,
               HERE/'nano_firmware_ram.py',HERE/'nano_firmware_entry.inc',HERE/'nano_firmware_ram.h')
    if any(receipt.get("producer_sources", {}).get(str(p.relative_to(ROOT))) != sha(p) for p in sources):
        raise ValueError("BL31 producer sources differ from its receipt")
    if cold_profile is not None:
        if any(sha(ROOT/name)!=expected for name,expected in receipt['producer_sources'].items()):
            raise ValueError('cold BL31 producer sources differ')
        sources=tuple(ROOT/name for name in receipt['producer_sources'])
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
    parser.add_argument("--native-entry-build", type=Path,
                        help="bind the dedicated serial-only Nano handoff to this entry image")
    parser.add_argument("--nano-firmware-ocram", action="store_true",
                        help="require the selected matched SPL/EL3/native OCRAM reservation")
    parser.add_argument("--network-recovery", action="store_true",
                        help="separate vendor Ethernet/TFTP and SD-writing recovery loader; manual prompt only")
    args = parser.parse_args()
    if args.network_recovery and args.native_entry_build is not None:
        parser.error("network recovery and native handoff are separate boot profiles")
    if args.nano_firmware_ocram and args.native_entry_build is None:
        parser.error('OCRAM firmware requires the exact cold native entry')
    repo = args.reference_repo.resolve()
    bl31_build = args.bl31_build.absolute()
    output = args.output.absolute()
    if not private_path(output) or output.exists() or not private_path(bl31_build) or not bl31_build.is_dir():
        parser.error("BL31 build and fresh output must be private paths under this worktree's out/")
    if not Path(args.cross_prefix).is_absolute() or not args.cross_prefix.endswith("-") or not 1 <= args.jobs <= 16:
        parser.error("cross prefix must be absolute and end in '-'; jobs must be 1..16")
    cold_profile=None
    if args.native_entry_build is not None:
        spec=importlib.util.spec_from_file_location('fes_nano_cold_profile',HERE.parent/'nano/cold_profile.py')
        cold=importlib.util.module_from_spec(spec);spec.loader.exec_module(cold)
        cold_profile=cold.make(args.native_entry_build.absolute(),firmware_ram=args.nano_firmware_ocram)
    receipt_path, receipt_bytes, receipt, component, blob, sources, elf, image_elf, segments = checked_bl31(bl31_build,cold_profile)
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
    native = None
    recovery = None
    recovery_hash = None
    recovery_sources = None
    if args.network_recovery:
        recovery_path = HERE.parent / "nano/recovery.py"
        spec = importlib.util.spec_from_file_location("fes_nano_recovery", recovery_path)
        recovery = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(recovery)
        recovery_hash = sha(recovery_path)
    build_repo = repo
    if args.native_entry_build is not None or recovery is not None:
        build_repo = output / "source"
        build_repo.mkdir()
        with tarfile.open(fileobj=io.BytesIO(git(repo, "archive", revision))) as archive:
            archive.extractall(build_repo, filter="data")
    if recovery is not None:
        recovery_sources = recovery.prepare_phy_sources(build_repo)
    if args.native_entry_build is not None:
        entry_build = args.native_entry_build.absolute()
        if not private_path(entry_build):
            raise ValueError("native entry must be a private build under out/")
        spec = importlib.util.spec_from_file_location("fes_nano_prepare", HERE.parent / "nano/prepare_uboot.py")
        nano_prepare = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(nano_prepare)
        native = nano_prepare.prepare(build_repo, entry_build, hashlib.sha256(blob).hexdigest(), len(blob),firmware_ram=args.nano_firmware_ocram)
        if native['cold_boot_profile']!=cold_profile: raise ValueError('SPL and BL31 cold chain differ')
        (build/'fes-entry.img').write_bytes((entry_build/'entry.img').read_bytes())
    commands = []
    def run(label, argv):
        argv = [str(x) for x in argv]
        commands.append(argv)
        with (output / (label + ".log")).open("wb") as log:
            subprocess.run(argv, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
    base = [make, "-C", build_repo, "O=" + str(build), "CROSS_COMPILE=" + args.cross_prefix]
    run("defconfig", [*base, "socfpga_agilex5_de25_nano_defconfig"])
    if recovery is not None:
        configure = [build_repo / "scripts/config", "--file", build / ".config"]
        for name in recovery.DISABLE:
            run("recovery-disable-" + name, [*configure, "-d", name])
        for name in recovery.ENABLE:
            run("recovery-enable-" + name, [*configure, "-e", name])
        run("recovery-bootcmd", [*configure, "--set-str", "BOOTCOMMAND", ""])
        run("recovery-bootdelay", [*configure, "--set-val", "BOOTDELAY", "-1"])
        run("recovery-localversion", [*configure, "--set-str", "LOCALVERSION", recovery.LOCALVERSION])
        run("recovery-olddefconfig", [*base, "olddefconfig"])
    if native is not None:
        configure = [build_repo / "scripts/config", "--file", build / ".config"]
        for name in nano_prepare.DISABLE:
            run("config-disable-" + name, [*configure, "-d", name])
        for name in nano_prepare.ENABLE:
            run("config-enable-" + name, [*configure, "-e", name])
        run("config-bootcmd", [*configure, "--set-str", "BOOTCOMMAND", "fesboot"])
        run("config-bootdelay", [*configure, "--set-val", "BOOTDELAY", "-1"])
        run("config-localversion", [*configure, "--set-str", "LOCALVERSION", "-fes-nano-" + native["build_id"]])
        run('config-experimental-fit',[*configure,'--set-str','SPL_FS_LOAD_PAYLOAD_NAME',cold.EXPERIMENTAL_FILE])
        run("olddefconfig", [*base, "olddefconfig"])
    config = (build / ".config").read_text()
    if recovery is not None:
        recovery.check(config)
    if ("CONFIG_SPL_ATF=y\n" not in config or "CONFIG_CMD_ELF=y\n" not in config or
            "# CONFIG_ARMV8_SWITCH_TO_EL1 is not set\n" not in config):
        raise ValueError("unexpected vendor bootloader configuration")
    if native is not None and (any("CONFIG_" + name + "=y\n" in config for name in nano_prepare.DISABLE) or
                               any("CONFIG_" + name + "=y\n" not in config for name in nano_prepare.ENABLE)):
        enabled=[name for name in nano_prepare.DISABLE if 'CONFIG_'+name+'=y\n' in config]
        missing=[name for name in nano_prepare.ENABLE if 'CONFIG_'+name+'=y\n' not in config]
        raise ValueError("serial-only bootloader options differ: excluded="+repr(enabled)+" missing="+repr(missing))
    if native is not None and any(line not in config for line in (
            'CONFIG_SPL_BSS_START_ADDR=0xbff00000\n','CONFIG_SPL_BSS_MAX_SIZE=0x100000\n',
            'CONFIG_SPL_ATF_NO_PLATFORM_PARAM=y\n','# CONFIG_SPL_ATF_LOAD_IMAGE_V2 is not set\n')):
        raise ValueError('native ATF argument/BSS configuration differs from its compiled handoff descriptor')
    run("build", [*base, "-j", str(args.jobs)])
    fit = build / "u-boot.itb"
    spl = build / "spl/u-boot-spl-dtb.hex"
    dump = build / "tools/dumpimage"
    if not fit.is_file() or not fit.stat().st_size or not spl.is_file() or not spl.stat().st_size or not dump.is_file():
        raise ValueError("missing vendor U-Boot output")
    if native is not None:
        sealed_fit=fit.read_bytes()
        if not 0<len(sealed_fit)<=cold.MAX_FIT_SIZE:raise ValueError('experimental FIT exceeds the upload/seal bound')
        digest=hashlib.sha256(sealed_fit).hexdigest()
        (build_repo/'arch/arm/mach-socfpga/fes_nano_fit_seal.h').write_text(
            '#define FES_NANO_SEALED_FIT_SIZE '+str(len(sealed_fit))+'UL\n#define FES_NANO_SEALED_FIT_SHA {'+
            ','.join('0x'+digest[i:i+2] for i in range(0,64,2))+'}\n')
        run('seal-spl',[*base,'-j',str(args.jobs),'spl/u-boot-spl-dtb.hex'])
        if fit.read_bytes()!=sealed_fit:raise ValueError('FIT changed while sealing SPL')
        native['sealed_fit_sha256']=digest
        native['sealed_fit_size']=len(sealed_fit)
        native['experimental_fit_path']=cold.EXPERIMENTAL_FILE
    run("fit-info", [dump, "-l", fit])
    info = (output / "fit-info.log").read_text()
    positions = re.findall(r"(?m)^ Image (\d+) \(atf\)$", info)
    if len(positions) != 1 or not re.search(r"(?m)^\s+Firmware:\s+atf\s*$", info):
        raise ValueError("FIT does not select exactly one ATF image")
    fdtget = tools["host_fdtget"]["path"]
    def prop(node, name):
        return subprocess.check_output([fdtget, "-t", "s", str(fit), node, name], env=env, text=True).strip()
    names = subprocess.check_output([fdtget, "-l", str(fit), "/images"], env=env, text=True).splitlines()
    expected_names=["uboot", "atf", "fdt-0"]+(['fes-entry'] if native else [])
    if (names != expected_names or int(positions[0]) != names.index("atf") or
            prop("/configurations", "default") != "board-0" or
            prop("/configurations/board-0", "firmware") != "atf" or
            prop("/configurations/board-0", "loadables") != ("uboot fes-entry" if native else "uboot") or
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
    if native is not None:
        if (parts['fes-entry']['sha256']!=native['entry_image_sha256'] or
                (output/'extracted-fes-entry.bin').read_bytes()!=(entry_build/'entry.img').read_bytes() or
                int(subprocess.check_output([fdtget,'-t','x',str(fit),'/images/fes-entry','load'],env=env,text=True).strip(),16)!=0x96080000):
            raise ValueError('preloaded native FIT entry differs')
        symbols=subprocess.check_output([args.cross_prefix+'nm','-S',str(build/'spl/u-boot-spl')],env=env,text=True)
        record_symbol=re.findall(r'(?m)^([0-9a-f]+) ([0-9a-f]+) [dD] fes_nano_spl_record$',symbols)
        if len(record_symbol)!=1 or not 0x1000<=int(record_symbol[0][0],16)<0x40000-int(record_symbol[0][1],16):
            raise ValueError('SPL cold record is not within loaded OCRAM data')
        native['spl_record_address']=int(record_symbol[0][0],16)
        native['spl_record_size']=int(record_symbol[0][1],16)
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
              "bl31_bridge_mode":receipt['bridge_mode'],
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
    if native is not None:
        if any(sha(ROOT / name) != expected for name, expected in native["producer_sources"].items()):
            raise ValueError("Nano handoff producer source changed during build")
        report["classification"] = "off-board serial-only Nano U-Boot/SPL with executable handoff gate; hardware not run"
        report["native_handoff"] = native
        report["config_sha256"] = sha(build / ".config")
        report["limitations"] = ["live memory/CPU/peripheral gate has not been executed on a Nano",
                                 "requires this exact SPL paired to FPGA SOF and this exact FIT on test media",
                                 "source-derived serial probe only; no native SD, USB, network, display or programming"]
    if recovery is not None:
        if sha(recovery_path) != recovery_hash:
            raise ValueError("recovery profile changed during compilation")
        recovery.check_phy_sources(build_repo, recovery_sources)
        recovery_symbols=subprocess.check_output([tools['readelf']['path'],'-Ws',build/'u-boot'],env=env,text=True,timeout=30)
        recovery.check_linked(recovery_symbols)
        report['uboot_elf_sha256']=sha(build/'u-boot')
        report["classification"] = "off-board Nano Ethernet/SD-writing recovery U-Boot; no native handoff or board acceptance"
        report["network_recovery"] = {"profile_sha256": recovery_hash, "manual_prompt": True,
                                       "automatic_writes": False, "random_mac": False,
                                       "sd_dma": False, "native_handoff": False,
                                       "phy":recovery.PHY, "phy_driver_linked":True,
                                       "phy_sources":recovery_sources}
        report["config_sha256"] = sha(build / ".config")
        report["limitations"] = ["requires matching SPL/SOF and explicit test-card preparation",
                                  "Ethernet PHY/link, supplied stable MAC and SD writes remain untested on Nano",
                                  "SD writes are manual U-Boot commands; no atomic update/rollback implementation",
                                  "JTAG can recover a loader; no direct JTAG SD block writer is supplied"]
    (output / "result.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print("Built experimental FIT:", fit)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, json.JSONDecodeError, subprocess.SubprocessError) as error:
        raise SystemExit("Terasic U-Boot build: " + str(error))
