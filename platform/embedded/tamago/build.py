#!/usr/bin/env python3
"""Build an explicit board image without executing it or opening devices."""
import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess

APP = Path(__file__).resolve().parent
ROOT = APP.parents[2]


def profile_constants(profile):
    """Validate a firmware-owned memory/card handoff, not a board default."""
    expected = {"schema", "firmware", "ram_start", "ram_size", "entry", "reserved", "sd_sectors", "sd_rca", "handoff"}
    if not isinstance(profile, dict) or set(profile) != expected or type(profile["schema"]) is not int or profile["schema"] != 1:
        raise ValueError("expected exact schema-1 handoff fields")
    if not isinstance(profile["firmware"], str) or not profile["firmware"].strip():
        raise ValueError("firmware identity is required")
    required = {"el1", "single_cpu", "counter_access", "exclusive_devices", "uart0_configured", "sdhc_selected", "mmu_off_caches_clean"}
    if not isinstance(profile["handoff"], dict) or set(profile["handoff"]) != required or any(v is not True for v in profile["handoff"].values()):
        raise ValueError("all firmware handoff requirements must be explicit and true")
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
    sectors, rca = number("sd_sectors"), number("sd_rca")
    if not 0 < sectors <= 1 << 32 or not 0 < rca <= 65535:
        raise ValueError("invalid firmware SDHC capacity/RCA")
    return start, size, entry, sectors, rca


def build(args):
    if args.interactive or args.disk is not None or args.qemu is not None or args.display is not None:
        raise ValueError("Nano selection cross-builds only; QEMU/disk/interactive/display options are not accepted")
    if args.program == "play":
        raise ValueError("play is a QEMU exercise; use tenfoot for a board cross-build")
    if args.profile is None:
        raise ValueError("--board de25-nano requires --profile from the firmware handoff")
    raw = args.profile.resolve().read_bytes()
    profile = json.loads(raw)
    start, size, entry, sectors, rca = profile_constants(profile)
    output = args.output.resolve()
    if output.exists() or not output.is_relative_to(ROOT / "out"):
        raise ValueError("output must be a fresh directory beneath this worktree's out/")
    output.mkdir(parents=True)
    (output / "handoff.json").write_bytes(raw)
    generated = output / "profile.go"
    generated.write_text("//go:build tamago && arm64 && de25nano\n\npackage de25nano\nconst (\n" +
                         f"profileRAMStart = {start}\nprofileRAMSize = {size}\nprofileSDSectors = {sectors}\nprofileSDRCA = {rca}\n)\n")
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
    forbidden = ("/board/qemuvirt", "/network", "/display")
    if any(p["ImportPath"].startswith("github.com/DeanoC/fes/platform/embedded/tamago") and p["ImportPath"].endswith(forbidden) for p in packages):
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
              "limitations": ["firmware must supply selected SDHC card and verified resource reservations",
                              "SD read-only PIO; no cold card/PHY initialization", "USB/Ethernet/display/physical FPGA adapters unavailable"]}
    (output / "result.json").write_text(json.dumps(report, indent=2) + "\n")
    print(f"Cross-built {elf}; hardware not executed")
