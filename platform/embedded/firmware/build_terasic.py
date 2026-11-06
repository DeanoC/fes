#!/usr/bin/env python3
"""Build the experimental contained BL31 from the pinned Terasic DE25 tree.

The output is a firmware component only. This command does not create a boot
image, write media, access a board, or establish a safe boot handoff.
"""
import argparse
import hashlib
import io
import importlib.util
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tarfile

import prepare_tfa
import nano_firmware_ram

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def digest(data):
    return hashlib.sha256(data).hexdigest()


def git(repo, *args):
    return subprocess.check_output(["git", "-C", str(repo), *args],
                                   env=dict(os.environ, GIT_OPTIONAL_LOCKS="0", GIT_NO_LAZY_FETCH="1"))


def extract_archive(data, output):
    """Accept only ordinary source files from the pinned commit's Git archive."""
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:") as archive:
        for member in archive:
            path = Path(member.name)
            if (path.is_absolute() or ".." in path.parts or not path.parts or
                    not (member.isdir() or member.isfile())):
                raise ValueError("unsupported path or entry in vendor Git archive")
            target = output / path
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            with archive.extractfile(member) as source, target.open("xb") as dest:
                shutil.copyfileobj(source, dest)
            target.chmod(member.mode & 0o777)


def bl31_load_segments(image_elf, *, firmware_ram=None, cold_chain=None):
    """Return the actual loaded memory ranges of an Agilex5 BL31 ELF."""
    if (len(image_elf) < 64 or image_elf[:6] != b"\x7fELF\x02\x01" or
            struct.unpack_from("<H", image_elf, 18)[0] != 183):
        raise ValueError("expected an AArch64 BL31 ELF")
    entry = struct.unpack_from("<Q", image_elf, 24)[0]
    phoff = struct.unpack_from("<Q", image_elf, 32)[0]
    phsize, count = struct.unpack_from("<HH", image_elf, 54)
    if phsize != 56 or not count or phoff + phsize * count > len(image_elf):
        raise ValueError("invalid BL31 program header table")
    executable = False
    segments = []
    selected = firmware_ram is not None
    if selected and (json.dumps(firmware_ram,sort_keys=True)!=json.dumps(nano_firmware_ram.CONTRACT,sort_keys=True) or
            image_elf[:7]!=b'\x7fELF\x02\x01\x01' or struct.unpack_from('<HHI',image_elf,16)!=(2,183,1)):
        raise ValueError('explicit matched firmware RAM contract and static ELF required')
    for i in range(count):
        kind, flags, offset, va, pa, filesz, memsz, _ = struct.unpack_from("<IIQQQQQQ", image_elf, phoff + i * phsize)
        if kind == 3:
            raise ValueError("BL31 must not contain an ELF interpreter")
        if kind != 1:
            continue
        low = selected and va == nano_firmware_ram.BASE and filesz == 0 and flags == 6 and va+memsz <= nano_firmware_ram.LIMIT
        ddr = va >= 0x80000000 and va+memsz <= (0x80200000 if selected else 0x82000000)
        if (va != pa or not (low or ddr) or
                not memsz or filesz > memsz or offset + filesz > len(image_elf)):
            raise ValueError("BL31 load segment exceeds the vendor firmware bounds")
        if flags & ~7 or any(va < end and begin < va+memsz for begin,end in segments):
            raise ValueError('unsupported or overlapping BL31 load segment')
        executable |= bool(flags & 1) and va <= entry < va + filesz
        segments.append((va, va + memsz))
    if not executable:
        raise ValueError("BL31 entry is outside an executable load segment")
    if selected:
        nano_firmware_ram.validate_elf(image_elf,segments,cold_chain)
    return entry, segments


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--reference-repo", required=True, type=Path,
                        help="clean checkout of the pinned Terasic TF-A revision")
    parser.add_argument("--cross-prefix", required=True,
                        help="absolute AArch64 GNU cross-tool prefix ending in '-'")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--jobs", type=int, default=2)
    parser.add_argument("--nano-owned-bridges", action="store_true", help="controlled SiP variant; boot remains NOT_READY without trusted proof")
    parser.add_argument("--nano-cold-serial-entry", type=Path, help="opt-in actual SPL/BL31 cold serial producer, bound to this native entry build")
    parser.add_argument("--nano-firmware-ocram", action="store_true", help="experimental matched shared-ROM SPL/EL3/native OCRAM reservation")
    args = parser.parse_args()
    if args.nano_cold_serial_entry is not None and not args.nano_owned_bridges:
        parser.error("cold serial boot requires --nano-owned-bridges")
    if args.nano_firmware_ocram and args.nano_cold_serial_entry is None:
        parser.error('OCRAM firmware requires the exact cold native entry')
    cold = cold_profile = None
    if args.nano_cold_serial_entry is not None:
        entry_path=args.nano_cold_serial_entry.absolute()
        if ('..' in entry_path.parts or not entry_path.is_relative_to(ROOT/'out') or any(p.is_symlink() for p in (entry_path,*entry_path.parents))):
            parser.error('cold entry must be private under out/')
        spec=importlib.util.spec_from_file_location('fes_nano_cold_profile',HERE.parent/'nano/cold_profile.py')
        cold=importlib.util.module_from_spec(spec);spec.loader.exec_module(cold)
        cold_profile=cold.make(entry_path,firmware_ram=args.nano_firmware_ocram)
    repo = args.reference_repo.resolve()
    output = args.output.absolute()
    if (".." in output.parts or any(p.is_symlink() for p in (output, *output.parents)) or
            not output.is_relative_to(ROOT / "out") or output.exists()):
        parser.error("output must be a fresh private directory beneath this worktree's out/")
    prefix = Path(args.cross_prefix)
    if not prefix.is_absolute() or not args.cross_prefix.endswith("-") or not 1 <= args.jobs <= 16:
        parser.error("cross prefix must be absolute and end in '-'; jobs must be 1..16")
    tools = {}
    for name in ("gcc", "ld", "as", "ar", "objcopy", "objdump"):
        path = Path(args.cross_prefix + name).resolve()
        if not path.is_file() or not os.access(path, os.X_OK):
            parser.error("missing executable cross tool " + name)
        tools[name] = {"path": str(path), "sha256": digest(path.read_bytes())}
    revision = git(repo, "rev-parse", "--verify", "HEAD^{commit}").decode().strip()
    if revision != prepare_tfa.TERASIC_REVISION or git(repo, "status", "--porcelain", "--untracked-files=all"):
        raise ValueError("Terasic TF-A checkout must be clean and at the pinned revision")
    tree = git(repo, "rev-parse", revision + "^{tree}").decode().strip()
    epoch = git(repo, "show", "-s", "--format=%ct", revision).decode().strip()
    if not epoch.isdigit():
        raise ValueError("invalid source timestamp")
    env = {key: os.environ[key] for key in ("PATH", "LD_LIBRARY_PATH", "GCC_EXEC_PREFIX", "COMPILER_PATH", "TMPDIR")
           if key in os.environ}
    env.update(LC_ALL="C", SOURCE_DATE_EPOCH=epoch)
    make = Path(shutil.which("make", path=env.get("PATH")) or "").resolve()
    if not make.is_file():
        raise ValueError("missing make")
    tools["make"] = {"path": str(make), "sha256": digest(make.read_bytes())}
    for name, option in (("cc1", "-print-prog-name=cc1"), ("libgcc", "-print-libgcc-file-name")):
        selected = subprocess.check_output([args.cross_prefix + "gcc", option], env=env, text=True).strip()
        path = Path(selected).resolve()
        if not path.is_file():
            raise ValueError("missing selected compiler component " + name)
        tools[name] = {"path": str(path), "sha256": digest(path.read_bytes())}
    archive = git(repo, "archive", "--format=tar", revision)
    output.mkdir(parents=True)
    source = output / "source"
    source.mkdir()
    extract_archive(archive, source)
    producer_inputs = (Path(__file__).resolve(), HERE / "prepare_tfa.py", HERE / "generated/fes_embedded_control.hpp",
                       prepare_tfa.DIAG_HEADER, prepare_tfa.DIAG_HELPERS)
    if args.nano_owned_bridges:
        import prepare_nano_owned
        producer_inputs += tuple(prepare_nano_owned.inputs())
    producer_inputs += (HERE/'nano_firmware_ram.py',HERE/'nano_firmware_entry.inc',HERE/'nano_firmware_fault.inc',HERE/'nano_firmware_ram.h')
    if cold is not None:
        producer_inputs += tuple(cold.sources())
    frozen = {str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in producer_inputs}
    target = source / prepare_tfa.SOURCE
    original = target.read_bytes()
    blob, profile = prepare_tfa.source_identity(original)
    if blob != prepare_tfa.TERASIC_BLOB or profile != "terasic-de25-v2.12":
        raise ValueError("pinned tree does not contain the reviewed Terasic service")
    owned = prepare_nano_owned.install(source,cold_profile,cold.header(cold_profile) if cold else None) if args.nano_owned_bridges else None
    patched = target.read_bytes() if owned else prepare_tfa.contained_source(original)
    target.write_bytes(patched)
    reset_target = source / prepare_tfa.RESET_SOURCE
    reset_original = git(repo, "show", revision + ":" + prepare_tfa.RESET_SOURCE)
    reset_patched = reset_target.read_bytes() if owned else prepare_tfa.contained_terasic_reset(reset_original)
    reset_target.write_bytes(reset_patched)
    diag_header = source / prepare_tfa.DIAG_HEADER_SOURCE
    diag_header.write_bytes(prepare_tfa.DIAG_HEADER.read_bytes())
    label = "fes-terasic-contained-" + revision[:12]
    if owned:
        label = "fes-nano-owned-unready-" + revision[:12]
    # The exported source is inside the FES worktree. Do not let TF-A's
    # default git-describe walk up into FES and embed an unrelated revision.
    command = [str(make), "-j", str(args.jobs), "PLAT=agilex5", "BUILD_STRING=" + label,
               "CROSS_COMPILE=" + args.cross_prefix, "bl31"]
    if args.nano_firmware_ocram:command.append('SEPARATE_NOBITS_REGION=1')
    with (output / "build.log").open("wb") as log:
        subprocess.run(command, cwd=source, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
    component = source / "build/agilex5/release/bl31.bin"
    elf = source / "build/agilex5/release/bl31/bl31.elf"
    image, image_elf = component.read_bytes(), elf.read_bytes()
    if not image or label.encode() not in image:
        raise ValueError("expected nonempty Agilex5 BL31 and AArch64 ELF")
    entry, segments = bl31_load_segments(image_elf,
        firmware_ram=nano_firmware_ram.checked(cold_profile),
        cold_chain=cold_profile['chain_sha256'] if args.nano_firmware_ocram else None)
    nano_firmware_ram.verify_installed(source,owned.get('firmware_ram') if owned else None)
    if owned and owned.get("configuration_mailbox_observer") is not None:
        import nano_mailbox
        binding = owned["configuration_mailbox_observer"]
        if (digest((source / nano_mailbox.SOURCE).read_bytes()) != binding["generated_sha256"] or
                digest((source / nano_mailbox.HEADER).read_bytes()) != binding["header_sha256"] or
                digest((source / nano_mailbox.HEADER).with_name("nano_mailbox_observe.h").read_bytes()) != binding["observer_header_sha256"]):
            raise ValueError("bound mailbox observation source changed during compilation")
    if (target.read_bytes() != patched or reset_target.read_bytes() != reset_patched or
            diag_header.read_bytes() != prepare_tfa.DIAG_HEADER.read_bytes() or
            revision != git(repo, "rev-parse", "HEAD^{commit}").decode().strip() or
            git(repo, "status", "--porcelain", "--untracked-files=all") or
            frozen != {str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in producer_inputs} or
            any(digest(Path(value["path"]).read_bytes()) != value["sha256"] for value in tools.values())):
        raise ValueError("build inputs changed during compilation")
    report = {"status": "built", "classification": "contained Terasic Agilex5 BL31 component; no boot or hardware acceptance",
              "reference_revision": revision, "reference_tree": tree,
              "archive_sha256": digest(archive), "reference_git_blob": blob,
              "reference_sha256": digest(original), "patched_sha256": digest(patched),
              "reset_reference_git_blob": prepare_tfa.TERASIC_RESET_BLOB,
              "reset_reference_sha256": digest(reset_original), "reset_patched_sha256": digest(reset_patched),
              "producer_sources": frozen, "cross_tools": tools, "command": command, "environment": env,
              "bl31_bin_sha256": digest(image), "bl31_bin_size": len(image),
              "bl31_elf_sha256": digest(image_elf), "bl31_entry": entry, "bl31_load_segments": segments, "build_label": label,
              "limitations": ["not a complete HPS-first boot chain or board handoff",
                              "vendor SPL, U-Boot, CPU entry, memory ownership and physical containment remain unqualified",
                              "firmware source transformation is not a signature or production attestation"]}
    report["bridge_mode"] = "owned-unready" if owned else "strict"
    if owned:
        report["owned_bridge_binding"] = owned
        report["classification"] = "controlled owned Nano BL31 component; boot proof unavailable, service remains NOT_READY; no board acceptance"
    if cold is not None:
        if cold.make(entry_path,firmware_ram=args.nano_firmware_ocram)!=cold_profile: raise ValueError('cold chain changed during build')
        report['bridge_mode']='owned-cold-serial'
        report['native_boot_profile']=cold_profile
        report['classification']='source-bound cold serial SPL record consumer and master-stop provider; no physical qualification'
    (output / "result.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print("Built experimental BL31:", component)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError, tarfile.TarError, struct.error) as error:
        raise SystemExit("Terasic firmware build: " + str(error))
