#!/usr/bin/env python3
"""Build an explicit board image without executing it or opening devices."""
import hashlib
import ipaddress
import re
import json
import os
from pathlib import Path
import struct
import subprocess

APP = Path(__file__).resolve().parent
ROOT = APP.parents[2]


def profile_constants(profile):
    """Validate firmware memory and platform readiness, not a board default."""
    expected = {"schema", "firmware", "ram_start", "ram_size", "entry", "reserved", "sd_input_clock_hz", "usb_phy_clock_hz", "network", "display", "fpga_probe", "handoff"}
    if not isinstance(profile, dict) or set(profile) != expected or type(profile["schema"]) is not int or profile["schema"] != 5:
        raise ValueError("expected exact schema-5 handoff fields")
    if not isinstance(profile["firmware"], str) or not profile["firmware"].strip():
        raise ValueError("firmware identity is required")
    required = {"el1", "single_cpu", "counter_access", "exclusive_devices", "uart0_configured", "sd_platform_ready", "mmu_off_caches_clean"}
    required.update(("usb_platform_ready", "ethernet_platform_ready", "display_platform_ready"))
    if not isinstance(profile["handoff"], dict) or set(profile["handoff"]) != required or any(v is not True for k,v in profile["handoff"].items() if k not in ("usb_platform_ready", "ethernet_platform_ready", "display_platform_ready")) or type(profile["handoff"].get("usb_platform_ready")) is not bool or type(profile["handoff"].get("ethernet_platform_ready", "display_platform_ready")) is not bool:
        raise ValueError("firmware readiness must be explicit; USB, Ethernet and display may be disabled")
    def number(key):
        value = profile[key]
        if type(value) is not int:
            raise ValueError(key + " must be an integer")
        return value
    start, size, entry = (number(k) for k in ("ram_start", "ram_size", "entry"))
    # Board has 1 GiB HPS DDR. TamaGo uses 2 MiB memory block boundaries.
    if start < 0x80000000 or size < 64 << 20 or start + size > 0xc0000000 or start % (2 << 20) or size % (2 << 20):
        raise ValueError("RAM must be an aligned region inside the 1 GiB HPS DDR window")
    if entry != start + 0x10000:
        raise ValueError("entry must leave 64 KiB for runtime tables/vectors")
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
    if not 400000 <= frequency <= 200000000:
        raise ValueError("invalid firmware SD input clock")
    usb_frequency = number("usb_phy_clock_hz")
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
    return start, size, entry, frequency, usb_frequency, network, display, fpga


def build(args):
    if args.interactive or args.disk is not None or args.qemu is not None or args.display is not None:
        raise ValueError("Nano selection cross-builds only; QEMU/disk/interactive/display options are not accepted")
    if args.program == "play":
        raise ValueError("play is a QEMU exercise; use tenfoot for a board cross-build")
    if args.profile is None:
        raise ValueError("--board de25-nano requires --profile from the firmware handoff")
    raw = args.profile.resolve().read_bytes()
    profile = json.loads(raw)
    start, size, entry, frequency, usb_frequency, net, display, fpga = profile_constants(profile)
    if args.program == "fpga" and fpga is None:
        raise ValueError("fpga probe requires an explicit read-only service profile")
    output = args.output.resolve()
    if output.exists() or not output.is_relative_to(ROOT / "out"):
        raise ValueError("output must be a fresh directory beneath this worktree's out/")
    output.mkdir(parents=True)
    (output / "handoff.json").write_bytes(raw)
    generated = output / "profile.go"
    generated.write_text("//go:build tamago && arm64 && de25nano\n\npackage de25nano\nconst (\n" +
                         f"profileRAMStart = {start}\nprofileRAMSize = {size}\nprofileSDInputClockHz = {frequency}\nprofileUSBPHYClockHz = {usb_frequency}\n" +
                         f"profileFPGAProbeEnabled = {str(fpga is not None).lower()}\nprofileFPGAProbeBase = {fpga['base'] if fpga else 0}\nprofileFPGAProbeBytes = {fpga['size'] if fpga else 0}\n)\n")
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
    overlay = output / "overlay.json"
    overlay.write_text(json.dumps({"Replace": {str(APP / "board/de25nano/profile.go"): str(generated)}}))
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
    files = {APP / "go.mod", APP / "go.sum", APP / "build.py", APP / "run.py", APP / "toolchain.lock.toml"}
    for p in packages:
        directory = Path(p["Dir"])
        if directory.is_relative_to(ROOT):
            for field in ("GoFiles", "SFiles", "EmbedFiles"):
                files.update(directory / f for f in p.get(field, []))
    sources = {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)}
    elf = output / (args.program + ".elf")
    command = [str(compiler), "build", *options, "-trimpath", "-ldflags", f"-T {entry:#x} -R 0x1000", "-o", str(elf), "./cmd/" + args.program]
    with (output / "build.log").open("w") as log:
        subprocess.run(command, cwd=APP, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
    data = elf.read_bytes()
    if data[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", data, 18)[0] != 183:
        raise ValueError("expected AArch64 ELF")
    phoff = struct.unpack_from("<Q", data, 32)[0]
    phsize, count = struct.unpack_from("<HH", data, 54)
    executable_entry = False
    elf_entry = struct.unpack_from("<Q", data, 24)[0]
    for i in range(count):
        typ, flags, offset, va, pa, filesz, memsz, _ = struct.unpack_from("<IIQQQQQQ", data, phoff + i * phsize)
        if typ == 1 and flags & 1 and va <= elf_entry < va + memsz:
            executable_entry = True
        if typ == 3 or (typ == 1 and (pa != va or va < start or va + memsz > start + size or filesz > memsz or offset + filesz > len(data))):
            raise ValueError("ELF interpreter or segment outside owned RAM")
    if not executable_entry or elf_entry < entry:
        raise ValueError("ELF entry overlaps early runtime reservation")
    if sources != {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)}:
        raise ValueError("sources changed while building")
    report = {"status": "cross-built", "board": "de25-nano", "program": args.program,
              "classification": "host-only cross-build; not boot or hardware acceptance",
              "firmware_profile_sha256": hashlib.sha256(raw).hexdigest(), "source_sha256": sources,
              "generated_profile_sha256": sha(generated), "environment": {k: env[k] for k in ("GOOS", "GOARCH", "GOOSPKG", "GOTOOLCHAIN", "GOMODCACHE", "GOCACHE")},
              "compiler_sha256": sha(compiler), "elf_sha256": sha(elf), "command": command,
              "imports": [p["ImportPath"] for p in packages],
              "limitations": ["firmware must supply SD/USB PHY and input clocks plus verified resource reservations",
                              "SD cold card initialization, read-only PIO; SoC PHY setup remains firmware-owned", "USB direct low/full-speed boot keyboard only; Ethernet XGMAC 2.10 static IPv4/1000-full prototype; display is boot-firmware scanout only, no drain or FPGA replacement; physical activation unavailable", "SDM probe is read-only TF-A mailbox v1; no programming, live package identity or containment; SMC bounded return is a firmware obligation"]}
    (output / "result.json").write_text(json.dumps(report, indent=2) + "\n")
    print(f"Cross-built {elf}; hardware not executed")
