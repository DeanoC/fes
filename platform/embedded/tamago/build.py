#!/usr/bin/env python3
"""Build an explicit board image without executing it or opening devices."""
import hashlib
import base64
import importlib.util
import ipaddress
import re
import json
import os
from pathlib import Path
import struct
import subprocess

APP = Path(__file__).resolve().parent
ROOT = APP.parents[2]


def validate_native_elf(data, start, size, link_address):
    """Reject a malformed or out-of-contract static image before publication."""
    if (len(data) < 64 or data[:7] != b"\x7fELF\x02\x01\x01" or
            struct.unpack_from("<HHI", data, 16) != (2, 183, 1)):
        raise ValueError("expected static little-endian AArch64 ELF")
    elf_entry, phoff = struct.unpack_from("<QQ", data, 24)
    ehsize, phsize, count = struct.unpack_from("<HHH", data, 52)
    if (ehsize != 64 or phsize != 56 or not 1 <= count <= 64 or
            phoff > len(data) or count > (len(data) - phoff) // phsize or
            elf_entry % 4 or elf_entry < link_address or elf_entry >= start + size - 0x20000):
        raise ValueError("invalid ELF header or entry")
    segments = []
    executable_entry = False
    for i in range(count):
        typ, flags, offset, va, pa, filesz, memsz, align = struct.unpack_from("<IIQQQQQQ", data, phoff + i * phsize)
        if typ in (2, 3):
            raise ValueError("dynamic ELF or interpreter is unsupported")
        if typ != 1:
            continue
        if (flags & ~7 or pa != va or va < start + 0x10000 or va >= start + size - 0x20000 or
                not memsz or memsz > start + size - 0x20000 - va or filesz > memsz or
                offset > len(data) or filesz > len(data) - offset or
                (align > 1 and (align & (align - 1) or (va - offset) & (align - 1))) or
                any(va < end and begin < va + memsz for begin, end in segments)):
            raise ValueError("ELF segment overlaps a reservation or falls outside owned RAM")
        segments.append((va, va + memsz))
        if flags & 1 and va <= elf_entry < va + filesz:
            executable_entry = True
    if not segments or not executable_entry:
        raise ValueError("ELF entry is not in file-backed executable memory")


def profile_constants(profile):
    """Validate firmware memory and platform readiness, not a board default."""
    expected = {"schema", "firmware", "ram_start", "ram_size", "entry", "reserved", "sd_input_clock_hz", "usb_phy_clock_hz", "uart_index", "network", "display", "fpga_probe", "sd_control", "handoff"}
    if not isinstance(profile, dict) or set(profile) != expected or type(profile["schema"]) is not int or profile["schema"] != 8:
        raise ValueError("expected exact schema-8 handoff fields")
    if not isinstance(profile["firmware"], str) or not profile["firmware"].strip():
        raise ValueError("firmware identity is required")
    required = {"el1", "single_cpu", "counter_access", "exclusive_devices", "uart_configured", "mmu_off_caches_clean"}
    optional_ready = ("sd_platform_ready", "usb_platform_ready", "ethernet_platform_ready", "display_platform_ready")
    required.update(optional_ready)
    handoff = profile["handoff"]
    if (not isinstance(handoff, dict) or set(handoff) != required or
            any(v is not True for k, v in handoff.items() if k not in optional_ready) or
            any(type(handoff[k]) is not bool for k in optional_ready)):
        raise ValueError("firmware readiness must be explicit; SD, USB, Ethernet and display may be disabled")
    def number(key):
        value = profile[key]
        if type(value) is not int:
            raise ValueError(key + " must be an integer")
        return value
    start, size, entry = (number(k) for k in ("ram_start", "ram_size", "entry"))
    # Board has 1 GiB HPS DDR. TamaGo uses 2 MiB memory block boundaries.
    if start < 0x80000000 or size < 64 << 20 or start + size > 0xc0000000 or start % (2 << 20) or size % (2 << 20):
        raise ValueError("RAM must be an aligned region inside the 1 GiB HPS DDR window")
    if entry != start + 0x20000:
        raise ValueError("link address must leave the first 64 KiB, including the ELF header page, for runtime tables/vectors")
    reserved = profile["reserved"]
    if not isinstance(reserved, list) or not reserved:
        raise ValueError("firmware reservations must be recorded")
    for r in reserved:
        if not isinstance(r, dict) or set(r) != {"start", "size", "owner"} or not isinstance(r["owner"], str) or not r["owner"].strip():
            raise ValueError("invalid reservation")
        a, n = r["start"], r["size"]
        if type(a) is not int or type(n) is not int or a < 0 or n <= 0 or a + n > 1 << 40:
            raise ValueError("invalid reservation bounds")
        if a < start + size and start < a + n:
            raise ValueError("Go RAM overlaps firmware reservation")
    frequency = number("sd_input_clock_hz")
    control = profile["sd_control"]
    # The CPU enters with SD contained. A clock value is not a runtime grant.
    if handoff["sd_platform_ready"] is not False:
        raise ValueError("SD must enter contained; runtime firmware ownership is required")
    if control is None:
        if frequency != 0:
            raise ValueError("disabled SD requires clock 0 and no control service")
    else:
        keys = {"service", "exclusive", "closed_bridges", "synchronous_bounded_return"}
        if (not isinstance(control, dict) or set(control) != keys or
                control["service"] != "fes-owned-sd-pio-v1" or
                any(control[k] is not True for k in keys - {"service"}) or frequency != 200000000):
            raise ValueError("SD requires the explicit owned PIO service and qualified 200 MHz input")
        if (profile["usb_phy_clock_hz"] != 0 or profile["network"] is not None or
                profile["display"] is not None or profile["fpga_probe"] is not None or
                any(handoff[k] for k in ("usb_platform_ready", "ethernet_platform_ready", "display_platform_ready"))):
            raise ValueError("initial closed-bridge SD ownership excludes other active device profiles")
    usb_frequency = number("usb_phy_clock_hz")
    uart_index = number("uart_index")
    if uart_index not in (0, 1):
        raise ValueError("UART index must explicitly select 0 or 1")
    if (profile["handoff"]["usb_platform_ready"] and usb_frequency != 60000000) or (not profile["handoff"]["usb_platform_ready"] and usb_frequency != 0):
        raise ValueError("USB requires 60 MHz ULPI or explicit disabled clock 0")
    # Device discovery never chooses a board MAC, PHY address or DMA window.
    network = profile["network"]
    if profile["handoff"]["ethernet_platform_ready"] != (network is not None):
        raise ValueError("Ethernet readiness must match the explicit network profile")
    if network is not None:
        expected_net = {"mac_index", "phy_address", "phy_id", "mac_address", "csr_clock_hz", "rx_buffer_shift", "address", "gateway", "dma_start", "dma_size", "dma_identity", "rgmii_125mhz_ready"}
        if not isinstance(network, dict) or set(network) != expected_net:
            raise ValueError("invalid network profile fields")
        for key in ("mac_index", "phy_address", "phy_id", "csr_clock_hz", "rx_buffer_shift", "dma_start", "dma_size"):
            if type(network[key]) is not int:
                raise ValueError("network " + key + " must be an integer")
        if not 0 <= network["mac_index"] <= 2 or not 0 <= network["phy_address"] <= 31 or not 0 < network["phy_id"] < 0xffffffff:
            raise ValueError("invalid MAC/PHY identity")
        if not 100000000 <= network["csr_clock_hz"] < 150000000 or network["rx_buffer_shift"] not in (1, 4):
            raise ValueError("unsupported MAC CSR clock/RX register layout")
        mac = network["mac_address"]
        if not isinstance(mac, str) or not re.fullmatch(r"(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}", mac) or int(mac[:2], 16) & 1 or int(mac.replace(":", ""), 16) == 0:
            raise ValueError("require explicit unicast MAC address")
        try:
            address = ipaddress.IPv4Interface(network["address"])
            gateway = ipaddress.IPv4Address(network["gateway"])
        except (ValueError, TypeError) as error:
            raise ValueError("require static IPv4 host/gateway") from error
        subnet = address.network
        if not 1 <= subnet.prefixlen <= 30 or address.ip.is_unspecified or address.ip.is_multicast or address.ip in (subnet.network_address, subnet.broadcast_address) or gateway not in subnet or gateway in (address.ip, subnet.network_address, subnet.broadcast_address) or gateway.is_multicast:
            raise ValueError("invalid static IPv4 host/gateway")
        base, amount = network["dma_start"], network["dma_size"]
        if base < 0x80000000 or amount != 2 << 20 or base % (2 << 20) or base + amount > 0xc0000000 or base >> 30 != start >> 30 or (base < start + size and start < base + amount):
            raise ValueError("require disjoint 2 MiB HPS DMA window in the Go L1 section")
        for r in reserved:
            if base < r["start"] + r["size"] and r["start"] < base + amount:
                raise ValueError("DMA overlaps firmware reservation")
        if network["dma_identity"] is not True or network["rgmii_125mhz_ready"] is not True:
            raise ValueError("Ethernet requires identity DMA and qualified 125 MHz RGMII firmware setup")
    display = profile["display"]
    if profile["handoff"]["display_platform_ready"] != (display is not None):
        raise ValueError("display readiness must match the explicit display profile")
    if display is not None:
        expected_display = {"base", "size", "width", "height", "stride", "format", "lifetime", "firmware_artifact_sha256"}
        if not isinstance(display, dict) or set(display) != expected_display:
            raise ValueError("invalid display profile fields")
        for key in ("base", "size", "width", "height", "stride"):
            if type(display[key]) is not int:
                raise ValueError("display " + key + " must be an integer")
        if (display["width"], display["height"], display["stride"], display["size"]) != (1280, 720, 5120, 4 << 20) or display["format"] != "xrgb8888" or display["lifetime"] != "boot":
            raise ValueError("display requires boot-pinned 720p XRGB8888 in a 4 MiB window")
        artifact = display["firmware_artifact_sha256"]
        if not isinstance(artifact, str) or not re.fullmatch("[0-9a-f]{64}", artifact) or artifact == "0" * 64:
            raise ValueError("display firmware artifact identity is required")
        base, amount = display["base"], display["size"]
        if base < 0x80000000 or base % (2 << 20) or base + amount > 0xc0000000 or base >> 30 != start >> 30 or (base < start + size and start < base + amount):
            raise ValueError("display window must be disjoint HPS DDR in the Go L1 section")
        for r in reserved:
            if base < r["start"] + r["size"] and r["start"] < base + amount:
                raise ValueError("display overlaps firmware reservation")
        if network is not None and base < network["dma_start"] + network["dma_size"] and network["dma_start"] < base + amount:
            raise ValueError("display overlaps Ethernet DMA")
    fpga = profile["fpga_probe"]
    if fpga is not None:
        fields = {"base", "size", "service", "exclusive", "synchronous_bounded_return"}
        if not isinstance(fpga, dict) or set(fpga) != fields:
            raise ValueError("invalid FPGA probe profile fields")
        if fpga["service"] != "intel-sip-mbox-v1-read-only" or fpga["exclusive"] is not True or fpga["synchronous_bounded_return"] is not True:
            raise ValueError("FPGA probe requires exclusive synchronous bounded-return TF-A mailbox v1 service")
        base, amount = fpga["base"], fpga["size"]
        if type(base) is not int or type(amount) is not int or base < 0x82000000 or amount != 2 << 20 or base % (2 << 20) or base + amount > 0xc0000000 or base >> 30 != start >> 30 or (base < start + size and start < base + amount):
            raise ValueError("FPGA probe requires disjoint reserved 2 MiB DDR above BL31")
        for r in reserved:
            if base < r["start"] + r["size"] and r["start"] < base + amount:
                raise ValueError("FPGA probe overlaps firmware reservation")
        if network is not None and base < network["dma_start"] + network["dma_size"] and network["dma_start"] < base + amount:
            raise ValueError("FPGA probe overlaps Ethernet DMA")
        if display is not None and base < display["base"] + display["size"] and display["base"] < base + amount:
            raise ValueError("FPGA probe overlaps display window")
    return start, size, entry, frequency, usb_frequency, network, display, fpga, uart_index


def package_option(program, package_id, profile):
    if program == "sdpackages":
        if (not isinstance(package_id,str) or not re.fullmatch('[0-9a-f]{64}',package_id) or
                profile['sd_control'] is None):
            raise ValueError('sdpackages requires an expected package ID and controlled SD profile')
        return package_id
    if package_id is not None:
        raise ValueError('--package-id requires the sdpackages diagnostic')
    return None


def build(args):
    if args.interactive or args.disk is not None or args.qemu is not None or args.display is not None:
        raise ValueError("Nano selection cross-builds only; QEMU/disk/interactive/display options are not accepted")
    if args.program in ("play", "mapcheck"):
        raise ValueError(args.program+" is a QEMU exercise; use the selected Nano program for a board cross-build")
    if args.profile is None:
        raise ValueError("--board de25-nano requires --profile from the firmware handoff")
    raw = args.profile.resolve().read_bytes()
    profile = json.loads(raw)
    if raw==(APP.parent/'nano/smmu-profile.json').read_bytes() and args.program!='probe':
        raise ValueError('the source-bound SMMU profile permits only the serial probe')
    start, size, entry, frequency, usb_frequency, net, display, fpga, uart_index = profile_constants(profile)
    expected_package_id=package_option(args.program,args.package_id,profile)
    context=io_map=cpu_map=control_map=configuration_map=reader_memory_map=ddr_read_map=computer_map=None
    active_ddr=getattr(args,'ddr_read',False)
    window_memory=getattr(args,'window_memory',False)
    if type(window_memory) is not bool or window_memory and (args.program!='fpgaload' or active_ddr):
        raise ValueError('--window-memory requires Nano fpgaload, separately from --ddr-read')
    window_read=getattr(args,'window_read',False)
    if type(window_read) is not bool or window_read and not window_memory:
        raise ValueError('--window-read requires explicit --window-memory')
    window_read_map=None
    window_present=getattr(args,'window_present',False)
    if type(window_present) is not bool or window_present and not window_read:
        raise ValueError('window presentation requires the complete window-read diagnostic')
    if type(active_ddr) is not bool or active_ddr and args.program!='fpgaload':
        raise ValueError('--ddr-read requires the Nano fpgaload diagnostic')
    computer_control=getattr(args,'computer_control',False)
    if type(computer_control) is not bool or computer_control and (args.program!='fpgaload' or active_ddr or window_memory):
        raise ValueError('--computer-control requires control-only Nano fpgaload, separately from --ddr-read')
    computer_reload=getattr(args,'computer_reload',False)
    if type(computer_reload) is not bool or computer_reload and not computer_control:
        raise ValueError('--computer-reload requires the explicitly selected computer-control diagnostic')
    computer_storage_return=getattr(args,'computer_storage_return',False)
    if type(computer_storage_return) is not bool or computer_storage_return and not computer_reload:
        raise ValueError('--computer-storage-return requires the selected two-load computer diagnostic')
    selected_programs=getattr(args,'selected_programs',False)
    if (type(selected_programs) is not bool or selected_programs and
            (not computer_control or not computer_reload or not computer_storage_return)):
        raise ValueError('--selected-programs requires computer control, rearming and owned SD returns')
    program_platform=program_metadata=None
    package_archive=None
    hps_project=getattr(args,'hps_first_project',None)
    if args.program in ('bootcontext','hpsio','fpgacontext','fpgastatus','fpgaload'):
        required_profile='sd-profile.json' if args.program=='fpgaload' else 'probe-profile.json'
        if hps_project is None or raw!=(APP.parent/'nano'/required_profile).read_bytes():
            raise ValueError('context diagnostics require a sealed HPS-first project and their source-derived serial/SD profile')
        spec=importlib.util.spec_from_file_location('fes_native_hps_context',APP.parent/'nano/hps_context.py')
        hps=importlib.util.module_from_spec(spec);spec.loader.exec_module(hps)
        hps_project=hps_project.resolve(strict=True)
        context=hps.derive(hps_project)
        if (context['programming_profile']=='de25-embedded-hps-first-window-diagnostic-v1')!=window_memory:
            raise ValueError('the window source requires explicit --window-memory and cannot inherit a fixed-reader diagnostic')
        if args.program=='hpsio':io_map=hps.hps_io(hps_project)
        if args.program in ('fpgacontext','fpgastatus','fpgaload'):control_map=hps.control(hps_project)
        if args.program=='fpgaload':
            configuration_map=hps.configuration(hps_project)
            reader_memory_map=hps.reader_memory(hps_project)
            cpu_map=hps.cpu_mapping(control_map,configuration_map,reader_memory_map)
            if active_ddr:ddr_read_map=hps.ddr_read(hps_project)
            if window_read:window_read_map=hps.window_read(hps_project)
            if computer_control:computer_map=hps.computer(hps_project)
            if selected_programs:
                program_platform=hps.program_platform(hps_project)
                program_metadata=hps.selected_program(hps_project)
            package_archive=hps.package_archive(hps_project,configuration_map)
            arena_start,arena_end=configuration_map['base'],configuration_map['base']+configuration_map['size']
            exclusions=[(start,start+size),(0x94000000,0x94800000)]+[(r['start'],r['start']+r['size']) for r in profile['reserved']]
            if any(arena_start<end and begin<arena_end for begin,end in exclusions):
                raise ValueError('configuration arena overlaps heap, sealed FIT or firmware')
            if reader_memory_map is not None:
                begin=reader_memory_map['arena_base'];end=begin+reader_memory_map['arena_bytes']
                if any(begin<stop and first<end for first,stop in exclusions+[(arena_start,arena_end)]):
                    raise ValueError('reader arena overlaps heap, sealed FIT, configuration or firmware')
    elif hps_project is not None:
        raise ValueError('--hps-first-project requires a native context diagnostic')
    if args.program == "fpga" and fpga is None:
        raise ValueError("fpga probe requires an explicit read-only service profile")
    output = args.output.resolve()
    if output.exists() or not output.is_relative_to(ROOT / "out"):
        raise ValueError("output must be a fresh directory beneath this worktree's out/")
    output.mkdir(parents=True)
    (output / "handoff.json").write_bytes(raw)
    if context is not None:(output/'hps-context.json').write_bytes(hps.encode(context))
    if io_map is not None:(output/'hps-io.json').write_bytes(hps.encode(io_map))
    if cpu_map is not None:(output/'cpu-mapping.json').write_bytes(hps.encode(cpu_map))
    if control_map is not None:(output/'hps-control.json').write_bytes(hps.encode(control_map))
    if configuration_map is not None:(output/'hps-configuration.json').write_bytes(hps.encode(configuration_map))
    if reader_memory_map is not None:(output/'hps-reader-memory.json').write_bytes(hps.encode(reader_memory_map))
    if ddr_read_map is not None:(output/'hps-ddr-read.json').write_bytes(hps.encode(ddr_read_map))
    if window_read_map is not None:(output/'hps-window-read.json').write_bytes(hps.encode(window_read_map))
    if computer_map is not None:(output/'hps-computer.json').write_bytes(hps.encode(computer_map))
    if program_platform is not None:
        (output/'program-platform.json').write_bytes(hps.encode(program_platform))
        metadata=hps.encode(program_metadata)
        (output/'PROGRAM.JSN').write_bytes(metadata)
        request={'path':'FES/PKGTEST.BIN','package_id':context['package_id'],
                 'metadata_path':'FES/PROGRAM.JSN','metadata_sha256':hashlib.sha256(metadata).hexdigest()}
        choices={'format':1,'scope':'nano-program-selection-v1','programs':[request]*(2 if computer_reload else 1)}
        (output/'CORESEL.JSN').write_bytes(hps.encode(choices))
    uart_base = 0x10c02000 + uart_index * 0x100
    generated = output / "profile.go"
    generated.write_text("//go:build tamago && arm64 && de25nano\n\npackage de25nano\nconst (\n" +
                         f"profileRAMStart = {start}\nprofileRAMSize = {size}\nprofileSDControlEnabled = {str(profile['sd_control'] is not None).lower()}\nprofileSDInputClockHz = {frequency}\nprofileUSBPHYClockHz = {usb_frequency}\nprofileConsoleUARTBase = {uart_base}\n" +
                         f"profileFPGAProbeEnabled = {str(fpga is not None).lower()}\nprofileFPGAProbeBase = {fpga['base'] if fpga else 0}\nprofileFPGAProbeBytes = {fpga['size'] if fpga else 0}\n)\n")
    with generated.open('a') as stream:
        stream.write('const (\nprofileConfigurationBase = '+str(configuration_map['base'] if configuration_map else 0)+'\nprofileConfigurationBytes = '+str(configuration_map['size'] if configuration_map else 0)+'\n)\n')
        stream.write('const (\nprofileFabricControlBase = '+str(control_map['cpu_base'] if control_map else 0)+'\nprofileFabricControlBytes = '+str(control_map['span'] if control_map else 0)+'\n)\n')
        stream.write('const (\nprofileReaderMemoryBase = '+str(reader_memory_map['arena_base'] if reader_memory_map else 0)+'\nprofileReaderMemoryArenaBytes = '+str(reader_memory_map['arena_bytes'] if reader_memory_map else 0)+'\n)\n')
    network_constants = {"profileNetworkEnabled": net is not None,
                         "profileNetworkBase": 0x10810000 + (net["mac_index"] * 0x10000) if net else 0,
                         "profileNetworkPHY": net["phy_address"] if net else 0,
                         "profileNetworkPHYID": net["phy_id"] if net else 0,
                         "profileNetworkClock": net["csr_clock_hz"] if net else 0,
                         "profileNetworkRXShift": net["rx_buffer_shift"] if net else 0,
                         "profileNetworkMAC": net["mac_address"] if net else "",
                         "profileNetworkAddress": net["address"] if net else "",
                         "profileNetworkGateway": net["gateway"] if net else "",
                         "profileNetworkDMAStart": net["dma_start"] if net else 0,
                         "profileNetworkDMABytes": net["dma_size"] if net else 0}
    with generated.open("a") as stream:
        stream.write("const (\n" + "".join(key + " = " + json.dumps(value) + "\n" for key, value in network_constants.items()) + ")\n")
    display_constants = {"profileDisplayEnabled": display is not None,
                         "profileDisplayBase": display["base"] if display else 0,
                         "profileDisplayBytes": display["size"] if display else 0,
                         "profileDisplayWidth": display["width"] if display else 0,
                         "profileDisplayHeight": display["height"] if display else 0,
                         "profileDisplayStride": display["stride"] if display else 0,
                         "profileDisplayArtifact": display["firmware_artifact_sha256"] if display else ""}
    with generated.open("a") as stream:
        stream.write("const (\n" + "".join(key + " = " + json.dumps(value) + "\n" for key, value in display_constants.items()) + ")\n")
    serial_generated = output / "serial-profile.go"
    serial_generated.write_text("//go:build tamago && arm64 && de25nano\n\npackage serial\n" +
                                f"const profileConsoleUARTBase = {uart_base}\n")
    overlay = output / "overlay.json"
    replacements={
        str(APP / "board/de25nano/profile.go"): str(generated),
        str(APP / "serial/profile-de25.go"): str(serial_generated),
    }
    if package_archive is not None:
        (output/'package.fcore').write_bytes(package_archive)
    overlay.write_text(json.dumps({'Replace':replacements}))
    env = dict(os.environ, GOOS="tamago", GOARCH="arm64", CGO_ENABLED="0", GOOSPKG="github.com/usbarmory/tamago",
               GOTOOLCHAIN="local", GOENV="off", GOWORK="off", GOPROXY="off", GOSUMDB="off",
               GOMODCACHE=str(args.module_cache.resolve()), GOCACHE=str(ROOT / "out/embedded-base-tools/tamago/cache"))
    compiler = args.compiler.resolve()
    options = ["-mod=readonly", "-tags=de25nano", "-overlay=" + str(overlay)]
    command = [str(compiler), "list", "-deps", "-json", *options, "./cmd/" + args.program]
    deps = subprocess.check_output(command, cwd=APP, env=env, text=True)
    from run import objects, sha
    packages = list(objects(deps))
    forbidden = {"github.com/DeanoC/fes/platform/embedded/tamago" + name for name in ("/board/qemuvirt", "/display")}
    if any(p["ImportPath"] in forbidden for p in packages):
        raise ValueError("Nano dependency graph includes a QEMU device backend")
    forbidden_files = {"virtio.go", "xhci_tamago.go", "xhci_tamago.s", "ramfb.go", "nic_tamago.go"}
    if any(forbidden_files.intersection(p.get("GoFiles", []) + p.get("SFiles", [])) for p in packages):
        raise ValueError("Nano selected a QEMU controller implementation")
    files = {APP / "go.mod", APP / "go.sum", APP / "build.py", APP / "run.py", APP / "toolchain.lock.toml", APP.parent/'nano/smmu-profile.json'}
    if context is not None:files.update(hps.inputs())
    for p in packages:
        directory = Path(p["Dir"])
        if directory.is_relative_to(ROOT):
            for field in ("GoFiles", "SFiles", "EmbedFiles"):
                files.update(directory / f for f in p.get(field, []))
    sources = {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)}
    elf = output / (args.program + ".elf")
    link_flags=f"-s -w -T {entry:#x} -R 0x1000"
    if expected_package_id:link_flags+=' -X main.expectedPackageID='+expected_package_id
    if context is not None:link_flags+=' -X main.expectedBootContext='+base64.b64encode(hps.encode(context)).decode()
    if io_map is not None:link_flags+=' -X main.expectedHPSIO='+base64.b64encode(hps.encode(io_map)).decode()
    if control_map is not None:link_flags+=' -X main.expectedControlMap='+base64.b64encode(hps.encode(control_map)).decode()
    if configuration_map is not None:link_flags+=' -X main.expectedConfigurationMap='+base64.b64encode(hps.encode(configuration_map)).decode()
    if reader_memory_map is not None:link_flags+=' -X main.expectedReaderMemoryMap='+base64.b64encode(hps.encode(reader_memory_map)).decode()
    if ddr_read_map is not None:link_flags+=' -X main.expectedDDRReadMap='+base64.b64encode(hps.encode(ddr_read_map)).decode()
    if window_memory:link_flags+=' -X main.expectedWindowMemory=1'
    if window_read_map is not None:link_flags+=' -X main.expectedWindowReadMap='+base64.b64encode(hps.encode(window_read_map)).decode()
    if window_present:link_flags+=' -X main.expectedWindowPresent=1'
    if computer_map is not None:link_flags+=' -X main.expectedComputerMap='+base64.b64encode(hps.encode(computer_map)).decode()
    if computer_reload:link_flags+=' -X main.expectedComputerReload=1'
    if computer_storage_return:link_flags+=' -X main.expectedComputerStorageReturn=1'
    if program_platform is not None:
        link_flags+=' -X main.expectedProgramPlatform='+base64.b64encode(hps.encode(program_platform)).decode()
        link_flags+=' -X main.expectedSelectedPrograms=1'
    command = [str(compiler), "build", *options, "-trimpath", "-ldflags", link_flags, "-o", str(elf), "./cmd/" + args.program]
    with (output / "build.log").open("w") as log:
        subprocess.run(command, cwd=APP, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
    data = elf.read_bytes()
    validate_native_elf(data, start, size, entry)
    if context is not None and (hps.derive(hps_project)!=context or base64.b64encode(hps.encode(context)) not in data):
        raise ValueError('HPS source context changed or was not linked into the native image')
    if io_map is not None and (hps.hps_io(hps_project)!=io_map or base64.b64encode(hps.encode(io_map)) not in data):
        raise ValueError('HPS I/O map changed or was not linked into the native image')
    if control_map is not None and (hps.control(hps_project)!=control_map or base64.b64encode(hps.encode(control_map)) not in data):
        raise ValueError('FPGA control map changed or was not linked into the native image')
    if configuration_map is not None and (hps.configuration(hps_project)!=configuration_map or
            base64.b64encode(hps.encode(configuration_map)) not in data or hps.package_archive(hps_project,configuration_map)!=package_archive):
        raise ValueError('sealed configuration map or selected package archive changed')
    if configuration_map is not None and (hps.reader_memory(hps_project)!=reader_memory_map or reader_memory_map is not None and
            base64.b64encode(hps.encode(reader_memory_map)) not in data):
        raise ValueError('sealed reader memory changed or was not linked into the native image')
    if ddr_read_map is not None and (hps.ddr_read(hps_project)!=ddr_read_map or
            base64.b64encode(hps.encode(ddr_read_map)) not in data):
        raise ValueError('sealed DDR read contract changed or was not linked into the native image')
    if window_read_map is not None and (hps.window_read(hps_project)!=window_read_map or
            base64.b64encode(hps.encode(window_read_map)) not in data):
        raise ValueError('sealed full-window access changed or was not linked into the native image')
    if computer_map is not None and (hps.computer(hps_project)!=computer_map or
            base64.b64encode(hps.encode(computer_map)) not in data):
        raise ValueError('sealed computer control changed or was not linked into the native image')
    if program_platform is not None and (hps.program_platform(hps_project)!=program_platform or
            hps.selected_program(hps_project)!=program_metadata or base64.b64encode(hps.encode(program_platform)) not in data):
        raise ValueError('compiled program platform or example selected metadata changed')
    if sources != {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)}:
        raise ValueError("sources changed while building")
    report = {"status": "cross-built", "board": "de25-nano", "program": args.program,
              "expected_package_id":expected_package_id,
              "hps_context":context,"hps_context_sha256":hps.digest(context) if context else None,
              "hps_io":io_map,"hps_io_sha256":hps.digest(io_map) if io_map else None,
              "cpu_mapping":cpu_map,"cpu_mapping_sha256":hps.digest(cpu_map) if cpu_map else None,
              "hps_control":control_map,"hps_control_sha256":hps.digest(control_map) if control_map else None,
              "hps_configuration":configuration_map,"hps_configuration_sha256":hps.digest(configuration_map) if configuration_map else None,
              "hps_reader_memory":reader_memory_map,"hps_reader_memory_sha256":hps.digest(reader_memory_map) if reader_memory_map else None,
              "hps_ddr_read":ddr_read_map,"hps_ddr_read_sha256":hps.digest(ddr_read_map) if ddr_read_map else None,
              "window_memory":window_memory,
              "window_present":window_present,
              "window_read":window_read,"hps_window_read":window_read_map,"hps_window_read_sha256":hps.digest(window_read_map) if window_read_map else None,
              "hps_computer":computer_map,"hps_computer_sha256":hps.digest(computer_map) if computer_map else None,
              "computer_reload":computer_reload,
              "computer_storage_return":computer_storage_return,
              "selected_programs":selected_programs,
              "hps_program_platform":program_platform,"hps_program_platform_sha256":hps.digest(program_platform) if program_platform else None,
              "configuration_core_sha256":configuration_map["core_sha256"] if configuration_map else None,
              "configuration_package_sha256":hashlib.sha256(package_archive).hexdigest() if package_archive is not None else None,
              "configuration_package_size":len(package_archive) if package_archive is not None else None,
              "hps_project":str(hps_project) if context else None,
              "classification": "host-only cross-build; not boot or hardware acceptance",
              "firmware_profile_sha256": hashlib.sha256(raw).hexdigest(), "source_sha256": sources,
              "generated_profile_sha256": sha(generated), "generated_serial_profile_sha256": sha(serial_generated), "environment": {k: env[k] for k in ("GOOS", "GOARCH", "GOOSPKG", "GOTOOLCHAIN", "GOMODCACHE", "GOCACHE")},
              "compiler_sha256": sha(compiler), "elf_sha256": sha(elf), "command": command,
              "imports": [p["ImportPath"] for p in packages],
              "limitations": ["firmware must supply SD/USB PHY and input clocks plus verified resource reservations",
                              "SD cold card initialization, read-only PIO; SoC PHY setup remains firmware-owned", "USB direct low/full-speed boot keyboard only; Ethernet XGMAC 2.10 static IPv4/1000-full prototype; display is boot-firmware scanout only, no drain or FPGA replacement; physical activation unavailable", "SDM probe is read-only TF-A mailbox v1; no programming, live package identity or containment; SMC bounded return is a firmware obligation"]}
    (output / "result.json").write_text(json.dumps(report, indent=2) + "\n")
    print(f"Cross-built {elf}; hardware not executed")
