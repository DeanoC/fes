#!/usr/bin/env python3
"""Package a native ELF in an experimental U-Boot handoff image.

This produces an ordinary file only. Nano output requires an explicit firmware
profile and remains unqualified until its physical handoff is established.
"""
import argparse
import base64
import ctypes
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import shlex
import struct
import subprocess

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
QEMU_IMAGE_ADDRESS = 0x58080000
QEMU_RAM_START, QEMU_RAM_END = 0x40000000, 0x50000000
QEMU_UART_BASE = 0x09000000


def window_memory_option(receipt, memory):
    selected=receipt.get('window_memory',False)
    if (type(selected) is not bool or selected !=
            (memory is not None and memory.get('scope')=='sealed-cold-window-reader-memory-v1')):
        raise ValueError('window memory selection differs from its complete compiled map')
    command=receipt.get('command',[])
    try:
        flags=command[command.index('-ldflags')+1]
        tokens=shlex.split(flags)
    except (ValueError,IndexError,AttributeError,TypeError):
        raise ValueError('window memory requires the native link command') from None
    options=[(i,word) for i,word in enumerate(tokens) if 'main.expectedWindowMemory' in word]
    if selected:
        if (len(options)!=1 or options[0][1]!='main.expectedWindowMemory=1' or
                options[0][0]==0 or tokens[options[0][0]-1]!='-X'):
            raise ValueError('window memory selection differs from the native link command')
    elif options:
        raise ValueError('unselected window memory appears in the native link command')
    return selected


def window_read_option(receipt,mapping):
    selected=receipt.get('window_read',False)
    if (type(selected) is not bool or selected!=(mapping is not None) or
            selected and receipt.get('window_memory') is not True):
        raise ValueError('window read selection differs from its complete compiled contract')
    command=receipt.get('command',[])
    try:tokens=shlex.split(command[command.index('-ldflags')+1])
    except (ValueError,IndexError,AttributeError,TypeError):
        raise ValueError('window read requires the native link command') from None
    options=[(i,word) for i,word in enumerate(tokens) if 'main.expectedWindowReadMap' in word]
    if selected:
        raw=(json.dumps(mapping,sort_keys=True,separators=(',',':'))+'\n').encode()
        expected='main.expectedWindowReadMap='+base64.b64encode(raw).decode()
        if len(options)!=1 or options[0][1]!=expected or options[0][0]==0 or tokens[options[0][0]-1]!='-X':
            raise ValueError('window read differs from the exact native link assignment')
    elif options:raise ValueError('unselected window read appears in the native link command')
    return selected


def window_present_option(receipt):
    selected=receipt.get('window_present',False)
    if (type(selected) is not bool or selected and
            (receipt.get('window_read') is not True or receipt.get('window_memory') is not True)):
        raise ValueError('window presentation requires the complete selected read path')
    command=receipt.get('command',[])
    try:tokens=shlex.split(command[command.index('-ldflags')+1])
    except (ValueError,IndexError,AttributeError,TypeError):
        raise ValueError('window presentation requires the native link command') from None
    options=[(i,word) for i,word in enumerate(tokens) if 'main.expectedWindowPresent' in word]
    if selected:
        if len(options)!=1 or options[0][1]!='main.expectedWindowPresent=1' or options[0][0]==0 or tokens[options[0][0]-1]!='-X':
            raise ValueError('window presentation differs from the native link assignment')
    elif options:raise ValueError('unselected window presentation appears in the native link command')
    return selected


class Segment(ctypes.Structure):
    _fields_ = [(name, ctypes.c_uint64) for name in ("address", "memory_size", "file_offset", "file_size")]


class Plan(ctypes.Structure):
    _fields_ = [("entry", ctypes.c_uint64), ("count", ctypes.c_uint32), ("segments", Segment * 16)]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def validate_staging(start, end, lower, upper, excluded):
    """The temporary vectors and stack must survive until Go takes ownership."""
    if (start < lower or end <= start or end > upper or
            any(start < other_end and other_start < end for other_start, other_end in excluded)):
        raise ValueError("loader staging must be disjoint from Go RAM and every declared firmware/device reservation")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", choices=("qemu", "de25-nano"), default="qemu")
    parser.add_argument("--payload", required=True, type=Path, help="static AArch64 native ELF")
    parser.add_argument("--nano-build", type=Path, help="Nano native cross-build receipt directory")
    parser.add_argument("--profile", type=Path, help="explicit Nano firmware handoff JSON")
    parser.add_argument("--image-address", type=lambda value: int(value, 0),
                        help="explicit Nano U-Boot load address, 2 MiB aligned base plus 0x80000")
    parser.add_argument("--cross-prefix", required=True, help="absolute GNU AArch64 prefix ending in -")
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    payload, output = args.payload.absolute(), args.output.absolute()
    if (".." in output.parts or any(p.is_symlink() for p in (output, *output.parents)) or
            not output.is_relative_to(ROOT / "out") or output.exists()):
        parser.error("output must be a fresh non-symlink directory beneath this worktree's out/")
    if payload.is_symlink() or not payload.is_file() or payload.stat().st_size > 32 * 1024 * 1024:
        parser.error("payload must be a regular ELF of at most 32 MiB")
    if not Path(args.cross_prefix).is_absolute() or not args.cross_prefix.endswith("-"):
        parser.error("cross-prefix must be absolute and end in -")
    profile_bytes = receipt_bytes = None
    native_sources = None
    native_context = native_io = native_cpu_map = native_control = native_configuration = native_reader_memory = native_ddr_read = native_computer = native_program_platform = None
    native_selected_programs = False
    native_reload = False
    native_storage_return = False
    native_window_memory = False
    native_window_read = None
    native_window_present = False
    if args.target == "qemu":
        if args.nano_build or args.profile or args.image_address is not None:
            parser.error("Nano receipt, profile and load address are only valid with --target de25-nano")
        image_address, ram_start, ram_end, uart_base = QEMU_IMAGE_ADDRESS, QEMU_RAM_START, QEMU_RAM_END, QEMU_UART_BASE
        staging_lower, staging_upper = 0x58000000, 0x5c000000
        exclusions = [(ram_start, ram_end)]
    else:
        if args.nano_build is None or args.profile is None or args.image_address is None:
            parser.error("Nano output requires its cross-build receipt, explicit profile and load address")
        profile_path = args.profile.resolve(strict=True)
        receipt_path = args.nano_build.resolve(strict=True) / "result.json"
        profile_bytes, receipt_bytes = profile_path.read_bytes(), receipt_path.read_bytes()
        profile, receipt = json.loads(profile_bytes), json.loads(receipt_bytes)
        nano_builder = HERE.parent / "tamago/build.py"
        spec = importlib.util.spec_from_file_location("fes_nano_build", nano_builder)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        start, size, link, _, _, network, display, fpga, uart = module.profile_constants(profile)
        module.validate_native_elf(payload.read_bytes(), start, size, link)
        if (receipt["status"] != "cross-built" or receipt["board"] != "de25-nano" or
                receipt["firmware_profile_sha256"] != sha(profile_bytes) or receipt["elf_sha256"] != sha(payload.read_bytes()) or
                receipt["source_sha256"].get(str(nano_builder.relative_to(ROOT))) != sha(nano_builder.read_bytes())):
            raise ValueError("Nano ELF/profile does not match its cross-build receipt")
        native_sources=receipt["source_sha256"]
        context_spec=importlib.util.spec_from_file_location('fes_entry_hps_context',HERE.parent/'nano/hps_context.py')
        hps=importlib.util.module_from_spec(context_spec);context_spec.loader.exec_module(hps)
        native_context=hps.checked(receipt)
        native_io=hps.checked_hps_io(receipt)
        native_control=hps.checked_control(receipt)
        native_configuration=hps.checked_configuration(receipt)
        native_reader_memory=hps.checked_reader_memory(receipt)
        native_cpu_map=hps.checked_cpu_mapping(receipt)
        native_ddr_read=hps.checked_ddr_read(receipt)
        native_window_read=hps.checked_window_read(receipt)
        native_computer=hps.checked_computer(receipt)
        native_program_platform=hps.checked_program_platform(receipt)
        native_selected_programs=receipt.get('selected_programs',False)
        native_reload=receipt.get('computer_reload',False)
        native_storage_return=receipt.get('computer_storage_return',False)
        native_window_memory=window_memory_option(receipt,native_reader_memory)
        window_read_option(receipt,native_window_read)
        native_window_present=window_present_option(receipt)
        if native_window_present and any(value not in payload.read_bytes() for value in
                (b'FES_NATIVE_FPGA_WINDOW_PRESENT FRAME ',b'FES_NATIVE_FPGA_WINDOW_PRESENT PASS ',
                 b'WindowSession).PresentFrame',b'WindowReader).Drain')):
            raise ValueError('window presentation is absent from the selected native ELF')
        command=receipt.get('command',[])
        flags=command[command.index('-ldflags')+1] if isinstance(command,list) and '-ldflags' in command and command.index('-ldflags')+1<len(command) else ''
        if not isinstance(flags,str) or native_reload!=('-X main.expectedComputerReload=1' in flags):
            raise ValueError('computer reload selection differs from the native link command')
        if native_storage_return!=('-X main.expectedComputerStorageReturn=1' in flags):
            raise ValueError('computer storage-return selection differs from the native link command')
        if native_selected_programs!=('-X main.expectedSelectedPrograms=1' in flags):
            raise ValueError('selected program mode differs from the native link command')
        if native_context is not None and base64.b64encode(hps.encode(native_context)) not in payload.read_bytes():
            raise ValueError('HPS context is absent from the selected native ELF')
        if native_io is not None and base64.b64encode(hps.encode(native_io)) not in payload.read_bytes():
            raise ValueError('HPS I/O map is absent from the selected native ELF')
        if native_control is not None and base64.b64encode(hps.encode(native_control)) not in payload.read_bytes():
            raise ValueError('control map is absent from the selected native ELF')
        if native_configuration is not None and base64.b64encode(hps.encode(native_configuration)) not in payload.read_bytes():
            raise ValueError('configuration map is absent from the selected native ELF')
        if native_reader_memory is not None and base64.b64encode(hps.encode(native_reader_memory)) not in payload.read_bytes():
            raise ValueError('reader memory map is absent from the selected native ELF')
        if native_ddr_read is not None and base64.b64encode(hps.encode(native_ddr_read)) not in payload.read_bytes():
            raise ValueError('DDR read contract is absent from the selected native ELF')
        if native_window_read is not None and base64.b64encode(hps.encode(native_window_read)) not in payload.read_bytes():
            raise ValueError('full-window access contract is absent from the selected native ELF')
        if native_computer is not None and base64.b64encode(hps.encode(native_computer)) not in payload.read_bytes():
            raise ValueError('computer control contract is absent from the selected native ELF')
        if native_program_platform is not None and base64.b64encode(hps.encode(native_program_platform)) not in payload.read_bytes():
            raise ValueError('program platform is absent from the selected native ELF')
        if native_configuration is not None:
            package=hps.package_archive(receipt['hps_project'],native_configuration)
            if (receipt.get('configuration_core_sha256')!=native_configuration['core_sha256'] or
                    receipt.get('configuration_package_sha256')!=sha(package) or
                    receipt.get('configuration_package_size')!=len(package) or
                    (args.nano_build/'package.fcore').read_bytes()!=package):
                raise ValueError('native package archive differs from the sealed build source')
        def check_native_sources():
            if not native_sources:raise ValueError("missing Nano selected-source proof")
            for name,expected in native_sources.items():
                path=ROOT/name
                if (Path(name).is_absolute() or '..' in Path(name).parts or
                        not path.resolve().is_relative_to(ROOT) or sha(path.read_bytes())!=expected):
                    raise ValueError("Nano selected source changed: "+name)
        check_native_sources()
        image_address, ram_start, ram_end = args.image_address, start, start + size
        uart_base = 0x10c02000 + uart * 0x100
        staging_lower, staging_upper = 0x80000000, 0xc0000000
        exclusions = [(ram_start, ram_end)] + [(r["start"], r["start"] + r["size"]) for r in profile["reserved"]]
        if native_configuration is not None:
            exclusions.append((native_configuration['base'],native_configuration['base']+native_configuration['size']))
        if native_reader_memory is not None:
            exclusions.append((native_reader_memory['arena_base'],native_reader_memory['arena_base']+native_reader_memory['arena_bytes']))
        for arena, base, size_key in ((network, "dma_start", "dma_size"),
                                      (display, "base", "size"), (fpga, "base", "size")):
            if arena:
                exclusions.append((arena[base], arena[base] + arena[size_key]))
        if image_address % 0x200000 != 0x80000:
            raise ValueError("Nano image address must be a 2 MiB base plus 0x80000")
        validate_staging(image_address, image_address + 1, staging_lower, staging_upper, exclusions)
    env = {key: os.environ[key] for key in ("PATH", "LD_LIBRARY_PATH", "GCC_EXEC_PREFIX", "COMPILER_PATH", "TMPDIR")
           if key in os.environ}
    env.update(LC_ALL="C", SOURCE_DATE_EPOCH="0")
    tools = {}
    for name in ("gcc", "as", "ld", "objcopy", "nm"):
        path = Path(args.cross_prefix + name).resolve()
        if not path.is_file() or not os.access(path, os.X_OK):
            parser.error("missing cross tool " + name)
        tools[name] = {"path": str(path), "sha256": sha(path.read_bytes())}
    host_cc = Path(shutil.which("cc", path=env.get("PATH")) or "").resolve()
    if not host_cc.is_file():
        parser.error("missing host C compiler")
    tools["host_cc"] = {"path": str(host_cc), "sha256": sha(host_cc.read_bytes())}
    cc1 = Path(subprocess.check_output([args.cross_prefix + "gcc", "-print-prog-name=cc1"], env=env, text=True).strip()).resolve()
    if not cc1.is_file():
        parser.error("missing compiler cc1")
    tools["cc1"] = {"path": str(cc1), "sha256": sha(cc1.read_bytes())}
    files = [HERE / name for name in ("build.py", "entry.S", "entry.c", "entry.ld", "elf_plan.c", "elf_plan.h")]
    if args.target == "de25-nano":
        files.append(nano_builder)
        files.append(HERE.parent/'nano/hps_context.py')
        if native_context is not None:files.extend(hps.inputs())
    frozen = {str(path.relative_to(ROOT)): sha(path.read_bytes()) for path in files}
    payload_bytes = payload.read_bytes()
    output.mkdir(parents=True)
    (output / "payload.elf").write_bytes(payload_bytes)
    (output / "payload.S").write_text('.section .payload,"a"\n.global __payload_start, __payload_end\n'
                                       '__payload_start:\n.incbin "payload.elf"\n__payload_end:\n')
    commands = []
    def run(label, command, *, host=False):
        command = [str(value) for value in command]
        commands.append(command)
        selected_env = dict(env)
        if host:
            selected_env.pop("GCC_EXEC_PREFIX", None)
            selected_env.pop("COMPILER_PATH", None)
        with (output / (label + ".log")).open("wb") as log:
            subprocess.run(command, cwd=output, env=selected_env, stdout=log, stderr=subprocess.STDOUT, check=True)
    common = [args.cross_prefix + "gcc", "-ffreestanding", "-fno-builtin", "-fno-stack-protector",
              "-fno-unwind-tables", "-fno-asynchronous-unwind-tables", "-mgeneral-regs-only", "-O2",
              *( ["-DFES_ENTRY_QEMU_DIAGNOSTIC"] if args.target == "qemu" else [] ),
              "-DFES_ENTRY_RAM_START=" + hex(ram_start),
              "-DFES_ENTRY_RAM_END=" + hex(ram_end), "-DFES_ENTRY_UART_BASE=" + hex(uart_base),
              "-I" + str(HERE)]
    for index, source in enumerate((HERE / "entry.S", HERE / "entry.c", HERE / "elf_plan.c", output / "payload.S")):
        run("compile-" + str(index), [*common, "-c", source, "-o", output / ("part-" + str(index) + ".o")])
    elf, raw = output / "entry.elf", output / "entry.img"
    run("link", [args.cross_prefix + "gcc", "-nostdlib", "-static", "-no-pie", "-Wl,--build-id=none",
                 "-Wl,-T," + str(HERE / "entry.ld"), "-Wl,--defsym=FES_ENTRY_IMAGE_ADDRESS=" + hex(image_address),
                 *(output / ("part-" + str(i) + ".o") for i in range(4)), "-o", elf])
    run("objcopy", [args.cross_prefix + "objcopy", "-O", "binary", elf, raw])
    nm = subprocess.check_output([args.cross_prefix + "nm", "-n", str(elf)], env=env, text=True)
    symbols = {}
    for address, name in re.findall(r"(?m)^([0-9a-fA-F]+)\s+\w\s+(\S+)$", nm):
        symbols[name] = int(address, 16)
    required = ("_start", "__image_start", "__image_end", "__payload_start", "__payload_end", "__stack_top", "fes_entry_vectors")
    if any(name not in symbols for name in required):
        raise ValueError("entry ELF is missing required symbols")
    blob = raw.read_bytes()
    begin, end = symbols["__payload_start"] - image_address, symbols["__payload_end"] - image_address
    if (symbols["_start"] != image_address or symbols["__image_start"] != image_address or
            symbols["__image_end"] != image_address + len(blob) or end - begin != len(payload_bytes) or
            not 0 <= begin < end <= len(blob) or blob[begin:end] != payload_bytes or
            symbols["fes_entry_vectors"] % 2048 or symbols["__stack_top"] % 16 or len(blob) < 64 or
            struct.unpack_from("<Q", blob, 8)[0] != 0x80000 or
            struct.unpack_from("<Q", blob, 16)[0] != len(blob) or
            struct.unpack_from("<Q", blob, 24)[0] != 8 or blob[56:60] != b"ARM\x64"):
        raise ValueError("entry image layout or arm64 Image header differs from the diagnostic contract")
    validate_staging(image_address, image_address + len(blob), staging_lower, staging_upper, exclusions)
    host_lib = output / "elf_plan.so"
    run("host-plan-build", [host_cc, "-std=c11", "-Wall", "-Wextra", "-Werror", "-shared", "-fPIC",
                            "-I" + str(HERE), HERE / "elf_plan.c", "-o", host_lib], host=True)
    library = ctypes.CDLL(str(host_lib))
    validate = library.fes_entry_plan_elf
    validate.argtypes = [ctypes.POINTER(ctypes.c_uint8), ctypes.c_uint64, ctypes.c_uint64, ctypes.c_uint64,
                         ctypes.c_uint64, ctypes.c_uint64, ctypes.POINTER(Plan)]
    data = (ctypes.c_uint8 * len(payload_bytes)).from_buffer_copy(payload_bytes)
    plan = Plan()
    if validate(data, len(payload_bytes), ram_start, ram_end, image_address, image_address + len(blob), ctypes.byref(plan)) != 1:
        raise ValueError("native ELF is not a safe static image within the selected diagnostic RAM window")
    if (payload.read_bytes() != payload_bytes or frozen != {str(path.relative_to(ROOT)): sha(path.read_bytes()) for path in files} or
            any(sha(Path(value["path"]).read_bytes()) != value["sha256"] for value in tools.values()) or
            (profile_bytes is not None and (profile_path.read_bytes() != profile_bytes or receipt_path.read_bytes() != receipt_bytes))):
        raise ValueError("entry build inputs changed")
    if native_sources is not None:check_native_sources()
    result = {"status": "built", "target": args.target,
              "classification": ("QEMU-only arm64 U-Boot handoff; not a Nano boot image" if args.target == "qemu" else
                                 "unqualified Nano handoff candidate; profile obligations and hardware unverified"),
              "payload_sha256": sha(payload_bytes), "entry_image_sha256": sha(blob), "entry_elf_sha256": sha(elf.read_bytes()),
              "entry_image_address": image_address, "entry_image_size": len(blob), "payload_offset": begin,
              "payload_size": len(payload_bytes), "native_entry": plan.entry,
              "segments": [{name: getattr(plan.segments[i], name) for name, _ in Segment._fields_} for i in range(plan.count)],
              "ram_start": ram_start, "ram_end": ram_end, "uart_base": uart_base,
              "staging_start": image_address, "staging_end": image_address + len(blob),
              "staging_exclusions": exclusions,
              "firmware_profile_sha256": sha(profile_bytes) if profile_bytes is not None else None,
              "native_build_sha256": sha(receipt_bytes) if receipt_bytes is not None else None,
              "native_build_sources": native_sources,
              "native_program":receipt['program'] if receipt_bytes is not None else None,
              "hps_context":native_context,
              "hps_context_sha256":hps.digest(native_context) if native_context is not None else None,
              "hps_io":native_io,
              "hps_io_sha256":hps.digest(native_io) if native_io is not None else None,
              "cpu_mapping":native_cpu_map,
              "cpu_mapping_sha256":hps.digest(native_cpu_map) if native_cpu_map is not None else None,
              "hps_control":native_control,
              "hps_control_sha256":hps.digest(native_control) if native_control is not None else None,
              "hps_configuration":native_configuration,
              "hps_configuration_sha256":hps.digest(native_configuration) if native_configuration is not None else None,
              "hps_reader_memory":native_reader_memory,
              "hps_reader_memory_sha256":hps.digest(native_reader_memory) if native_reader_memory is not None else None,
              "hps_ddr_read":native_ddr_read,
              "hps_ddr_read_sha256":hps.digest(native_ddr_read) if native_ddr_read is not None else None,
              "hps_computer":native_computer,
              "hps_computer_sha256":hps.digest(native_computer) if native_computer is not None else None,
              "computer_reload":native_reload,
              "window_memory":native_window_memory,
              "window_read":native_window_read is not None,"hps_window_read":native_window_read,
              "window_present":native_window_present,
              "hps_window_read_sha256":hps.digest(native_window_read) if native_window_read else None,
              "computer_storage_return":native_storage_return,
              "selected_programs":native_selected_programs,
              "hps_program_platform":native_program_platform,
              "hps_program_platform_sha256":hps.digest(native_program_platform) if native_program_platform is not None else None,
              "hps_project":receipt['hps_project'] if native_context is not None else None,
              "sources": frozen, "tools": tools, "environment": env, "commands": commands,
              "limitations": ["no physical Nano, DDR, peripheral, DMA, bridge or reset proof",
                              "payload hashes are not signatures; U-Boot device quiescence is a separate obligation",
                              "all other CPUs and DMA masters must already be quiescent"]}
    (output / "result.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print("Built", args.target, "entry image:", raw)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError, struct.error) as error:
        raise SystemExit("native entry build: " + str(error))
