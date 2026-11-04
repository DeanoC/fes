#!/usr/bin/env python3
"""Build native Go and verify QEMU boot, real catalog, input and FAT32 persistence.

No dependencies are downloaded. Tenfoot uses a private local FogCast host and
synthetic .sfc catalog files, a regular-file FAT32 disk, virtio networking,
ramfb display and an xHCI USB keyboard (or explicit UART diagnostic input). No physical device is opened.
"""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys
import time
from urllib.parse import urlparse
from urllib.request import urlopen

from host_support import Guest, frames, network_catalog

APP = Path(__file__).resolve().parent
ROOT = APP.parents[2]
LAYOUT_NEXT = {"grid": "shelf", "shelf": "list", "list": "grid"}


def sha(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def objects(stream):
    decoder = json.JSONDecoder()
    position = 0
    while position < len(stream):
        while position < len(stream) and stream[position].isspace():
            position += 1
        if position == len(stream):
            break
        value, position = decoder.raw_decode(stream, position)
        yield value


def stop(process):
    if process is not None and process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("program", choices=("probe", "mapcheck", "fabric", "packages", "admission", "play", "tenfoot", "storage", "keyboard", "network", "display", "fpga", "sdpackages", "bootcontext", "hpsio", "fpgacontext", "fpgastatus", "fpgaload"))
    parser.add_argument("--compiler", required=True, type=Path)
    parser.add_argument("--qemu", type=Path)
    parser.add_argument("--board", choices=("qemu", "de25-nano"), default="qemu")
    parser.add_argument("--profile", type=Path, help="required firmware handoff JSON for Nano cross-build")
    parser.add_argument("--package-id", help="independently expected package ID for the Nano sdpackages diagnostic")
    parser.add_argument("--hps-first-project", type=Path, help="sealed HPS-first project for the Nano context diagnostics")
    parser.add_argument("--ddr-read", action="store_true", help="opt in to the fixed read-only FPGA DDR diagnostic (Nano fpgaload only)")
    parser.add_argument("--window-memory", action="store_true", help="reserve the sealed 4 MiB window and publish one tenfoot frame while FPGA data access remains held (Nano fpgaload only)")
    parser.add_argument("--window-read", action="store_true", help="read and drain one complete frame through the owned firmware and native driver; requires --window-memory")
    parser.add_argument("--window-present", action="store_true", help="present twelve changing tenfoot frames with local drain and final firmware return; requires --window-read")
    parser.add_argument("--window-storage-return", action="store_true", help="resume SD and freshly retain the selected package after a verified window return; requires --window-present")
    parser.add_argument("--window-reload", action="store_true", help="load a second verified window after fresh SD retention; requires --window-storage-return")
    parser.add_argument("--computer-control", action="store_true", help="opt in to the owned control-only ZX81 command diagnostic (Nano fpgaload only)")
    parser.add_argument("--computer-reload", action="store_true", help="run two fixed-package load/Run/return cycles; requires --computer-control")
    parser.add_argument("--computer-storage-return", action="store_true", help="enable owned SD return and fresh retention between computer sessions")
    parser.add_argument("--selected-programs", action="store_true", help="serve repeated selected-program requests over the bounded Nano serial interface (requires computer control/rearm/storage return)")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--module-cache", type=Path,
                        default=ROOT / "out/embedded-base-tools/tamago/mod")
    parser.add_argument("--host", type=Path, default=ROOT / "out/embedded-base-tools/fogcast-host",
                        help="FogCast embedded host executable (tenfoot only)")
    parser.add_argument("--disk", type=Path,
                        help="persistent regular FAT32 image beneath out/; created if absent")
    parser.add_argument("--input", choices=("usb", "uart"), default="usb",
                        help="explicit keyboard backend (tenfoot only)")
    parser.add_argument("--interactive", action="store_true",
                        help="keep tenfoot running; USB keys use the QEMU window, Ctrl-] in terminal exits")
    parser.add_argument("--display", default=None,
                        help="QEMU display backend: defaults to sdl interactively, none for checks")
    parser.add_argument("--timeout", type=float, default=90,
                        help="startup or per-action deadline, seconds (not interactive runtime)")
    args = parser.parse_args()
    if args.board == "de25-nano":
        from build import build
        try:
            build(args)
        except (ValueError, OSError, subprocess.CalledProcessError) as error:
            parser.error(str(error))
        return
    if args.profile is not None or args.package_id is not None or args.hps_first_project is not None or args.ddr_read or args.window_memory or args.window_read or args.window_present or args.window_storage_return or args.window_reload or args.computer_control or args.computer_reload or args.computer_storage_return or args.selected_programs:
        parser.error("--profile, --package-id and --hps-first-project require --board de25-nano")
    if args.program in ("fpga", "sdpackages", "bootcontext", "hpsio", "fpgacontext", "fpgastatus", "fpgaload"):
        parser.error("this diagnostic requires the Nano firmware service; QEMU does not emulate it")
    if args.qemu is None:
        parser.error("--board qemu requires --qemu")
    output = args.output.resolve()
    if output.exists():
        parser.error("output exists; choose a new directory to retain earlier evidence")
    if not output.is_relative_to(ROOT / "out"):
        parser.error("output must be beneath this worktree's out/ directory")
    if args.timeout <= 0 or args.timeout > 300:
        parser.error("timeout must be in (0, 300] seconds")
    if args.interactive and args.program != "tenfoot":
        parser.error("--interactive requires tenfoot")
    disk = (args.disk or output / "fat32.img").resolve()
    if args.program in ("packages", "admission", "play") and disk.exists():
        parser.error("package diagnostics require a fresh disk image")
    if not disk.is_relative_to(ROOT / "out") or (disk.exists() and not disk.is_file()):
        parser.error("disk must be a regular file beneath this worktree's out/")
    if args.program == "storage" and not disk.is_file():
        parser.error("storage requires --disk with an existing FAT32 regular-file image")
    if "," in str(output) or "," in str(disk):
        parser.error("output/disk paths cannot contain a comma (QEMU option separator)")
    output.mkdir(parents=True)
    compiler, qemu = args.compiler.resolve(), args.qemu.resolve()
    env = dict(os.environ, GOOS="tamago", GOARCH="arm64", CGO_ENABLED="0",
               GOOSPKG="github.com/usbarmory/tamago", GOTOOLCHAIN="local",
               GOENV="off", GOWORK="off", GOPROXY="off", GOSUMDB="off",
               GOMODCACHE=str(args.module_cache.resolve()),
               GOCACHE=str(ROOT / "out/embedded-base-tools/tamago/cache"))
    record = {"status": "incomplete", "program": args.program,
              "classification": "QEMU-only native Go bare metal; not DE25 hardware",
              "commands": [], "environment": {k: env[k] for k in (
                  "GOOS", "GOARCH", "CGO_ENABLED", "GOOSPKG", "GOTOOLCHAIN",
                  "GOENV", "GOWORK", "GOPROXY", "GOSUMDB", "GOMODCACHE", "GOCACHE")},
              "input": args.input if args.program in ("tenfoot", "play") else None,
              "limitations": ["QEMU USB is not a DE25 DWC2 controller",
                              "virtio block is not a physical SD controller",
                              "synthetic catalog files are not playable ROMs",
                              "clean-reboot persistence is not power-loss atomicity"]}
    host_process, host_log, disk_lock = None, None, None
    if args.program in ("packages", "admission", "play"):
        record["limitations"] = ["package inspection is not hardware admission",
                                "synthetic shared corpus, not deployable RBFs",
                                "no FPGA programming or ROM linking",
                                "largest production-package latency is unqualified"]

    def save():
        (output / "result.json").write_text(json.dumps(record, indent=2) + "\n")

    def command_record(command):
        record["commands"].append([str(x) for x in command])
        save()

    def checked(name, command, *, native=False):
        command_record(command)
        result = subprocess.run([str(x) for x in command], cwd=APP,
                                env=env if native else os.environ,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180)
        (output / (name + ".log")).write_bytes(result.stdout)
        if result.returncode:
            raise RuntimeError(f"{name} exited {result.returncode}; inspect {output / (name + '.log')}")
        return result.stdout.decode(errors="replace")

    def preferences(name):
        path = output / (name + ".json")
        checked(name, ["mcopy", "-i", disk, "::FES/PREFS.JSON", path])
        return json.loads(path.read_bytes().rstrip(b" \r\n\t\0"))

    def prepare_host():
        nonlocal host_process, host_log
        host = args.host.resolve()
        record["host_sha256"] = sha(host)
        directory = output / "host"
        library = directory / "library"
        library.mkdir(parents=True)
        # Real directory scanning and HTTP serving, with deliberately synthetic
        # files. Nothing attempts to execute or qualify these as game software.
        for number, name in enumerate(("Aster", "Borealis", "Comet"), 1):
            (library / (name + ".sfc")).write_bytes(
                (f"FES synthetic catalog sample {number}: {name}\n".encode() * 1024)[:32768])
        record["catalog_files_sha256"] = {p.name: sha(p) for p in sorted(library.iterdir())}
        config = directory / "config.toml"
        config.write_text('selected_target = "unavailable"\nrequest_timeout_seconds = 3\nupload_timeout_seconds = 5\n' +
                          f"state = {json.dumps(str(directory / 'state'))}\n" +
                          f"staging = {json.dumps(str(directory / 'staging'))}\n" +
                          '[[targets]]\nname = "unavailable"\nenabled = false\n' +
                          '[[libraries]]\nid = "native-snes"\nsystem = "snes"\n' +
                          f"root = {json.dumps(str(library))}\n")
        ready = directory / "ready.json"
        command = [host, "--config", config, "--ready-file", ready]
        command_record(command)
        host_log = (directory / "console.log").open("wb")
        host_process = subprocess.Popen([str(x) for x in command], stdout=host_log, stderr=host_log)
        deadline = time.monotonic() + args.timeout
        while not ready.exists():
            if host_process.poll() is not None or time.monotonic() >= deadline:
                raise RuntimeError(f"private FogCast host did not become ready; inspect {directory / 'console.log'}")
            time.sleep(0.05)
        base = json.loads(ready.read_text())["base"]
        address = urlparse(base)
        if address.scheme != "http" or address.hostname != "127.0.0.1" or not address.port:
            raise RuntimeError("private host returned an unexpected listen URL")
        with urlopen(base + "/api/v1/games", timeout=args.timeout) as response:
            catalog = json.load(response)
        (directory / "catalog.json").write_text(json.dumps(catalog, indent=2) + "\n")
        games = catalog.get("games", [])
        if len(games) != 3:
            raise RuntimeError(f"expected 3 scanned sample files, host returned {len(games)} games")
        record["host"] = {"base": base, "guest_base": f"http://10.0.2.2:{address.port}",
                          "catalog_count": len(games), "catalog_ids": sorted(g["id"] for g in games),
                          "config_sha256": sha(config)}
        return record["host"]["guest_base"]

    def lock_disk():
        nonlocal disk_lock
        disk.parent.mkdir(parents=True, exist_ok=True)
        disk_lock = Path(str(disk) + ".lock").open("a+")
        try:
            fcntl.flock(disk_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            disk_lock.close()
            disk_lock = None
            raise RuntimeError(f"disk is busy in another runner: {disk}") from None
        disk_lock.seek(0)
        disk_lock.truncate()
        disk_lock.write(f"pid={os.getpid()} disk={disk}\n")
        disk_lock.flush()

    def format_disk():
        fresh = not disk.exists()
        disk.parent.mkdir(parents=True, exist_ok=True)
        if fresh:
            with disk.open("xb") as stream:
                stream.truncate(128 << 20)
            if formatter := shutil.which("mkfs.fat"):
                checked("format", [formatter, "-F", "32", "-n", "FESNATIVE", disk])
            else:
                formatter = shutil.which("mformat")
                if not formatter:
                    raise RuntimeError("install dosfstools (mkfs.fat) or mtools (mformat) to create FAT32")
                checked("format", [formatter, "-i", disk, "-F", "-v", "FESNATIVE", "::"])
            record["fat_formatter"] = {"path": formatter, "sha256": sha(Path(formatter))}
            checked("mkdir-fat", ["mmd", "-i", disk, "::FES"])
        return fresh

    def prepare_disk(api_url):
        fresh = format_disk()
        if fresh:
            initial = output / "preferences-initial.json"
            data = json.dumps({"safe_area_pct": 0.05, "layout": "grid",
                               "attract_enabled": False, "home": "library"}).encode() + b"\n"
            initial.write_bytes(data.ljust(4096, b" "))
            checked("write-prefs", ["mcopy", "-i", disk, initial, "::FES/PREFS.JSON"])
        boot = output / "boot.json"
        boot.write_text(json.dumps({"api_url": api_url, "api_host": urlparse(record["host"]["base"]).netloc, "input": args.input}) + "\n")
        checked("write-boot", ["mcopy", "-o", "-i", disk, boot, "::FES/BOOT.JSON"])
        record["disk"] = {"path": str(disk), "created": fresh, "bytes": disk.stat().st_size,
                          "before_sha256": sha(disk), "boot_config_sha256": sha(boot)}
        record["initial_preferences"] = preferences("preferences-before")

    def ready(guest, expected):
        guest.wait(lambda text: "FES_TENFOOT_READY" in text, args.timeout, "tenfoot ready")
        text = guest.text
        for marker in ("FES_BOOT_CONFIG", "FES_STORAGE_READY", "FES_NETWORK_READY",
                       f"FES_PREFERENCES_LOADED layout={expected}"):
            if marker not in text:
                raise RuntimeError(f"missing startup evidence: {marker}")
        if args.input == "usb" and "FES_USB_READY" not in text:
            raise RuntimeError("no USB enumeration evidence")
        catalog_lines = re.findall(r"FES_CATALOG ids=(\[[^\r\n]*\])", text)
        if not catalog_lines or sorted(json.loads(catalog_lines[-1])) != record["host"]["catalog_ids"]:
            raise RuntimeError("guest catalog IDs differ from the real host scanner response")
        available = [f for f in frames(text) if f["titles"] >= 3 and f["layout"] == expected]
        if not available:
            raise RuntimeError("ready marker has no matching rendered catalog frame")
        return available[-1]

    package_sources = set()
    package_directory = None
    package_cases = []
    started = time.monotonic()
    try:
        record["compiler_sha256"] = sha(compiler)
        record["compiler_version"] = checked("compiler", [compiler, "version"], native=True).strip()
        if "go1.26.5 " not in record["compiler_version"]:
            raise RuntimeError("this experiment pins the TamaGo Go 1.26.5 compiler")
        record["qemu_sha256"] = sha(qemu)
        record["qemu_version"] = checked("qemu", [qemu, "--version"]).strip()
        if args.program == "packages":
            from package_fixtures import prepare
            package_directory, package_cases, package_sources = prepare(ROOT, output)
            record["package_fixtures"] = {
                "cases": package_cases,
                "files_sha256": {p.name: sha(p) for p in sorted(package_directory.iterdir())}}
        if args.program in ("admission", "play"):
            from admission_fixtures import prepare
            record["oracle_compiler_sha256"] = sha(Path(shutil.which("c++")).resolve())
            record["oracle_compiler"] = checked("oracle-compiler", ["c++", "--version"])
            package_directory, package_cases, package_sources = prepare(ROOT, output, checked)
            record["admission_policy"] = json.loads((package_directory / "RUNTIME.JSON").read_text())
            record["admission_cases"] = package_cases
            record["linked_cases"] = json.loads((package_directory / "LINKS.JSON").read_text())
            record["oracle_sha256"] = sha(output / "admission-oracle")
            record["oracle_system_headers_sha256"] = sha(output / "oracle-system-headers.json")
            record["limitations"] = ["trusted runner-owned model policy; not authenticated admission",
                                     "C++ compatibility check is not full physical runtime admission",
                                     "DE10 fixture target, not DE25 support", "synthetic payloads, software completions",
                                     "ROM linking uses Cyclone V synthetic vectors; no FPGA programming, live GP identity or tenfoot Play"]
        target = "tenfoot" if args.program == "play" else args.program
        if args.program == "play":
            if args.input != "usb":
                raise RuntimeError("Play diagnostic requires USB input")
            record["host_go"] = checked("host-go", ["go", "version"])
            host_go = Path(checked("host-go-root", ["go", "env", "GOROOT"]).strip()) / "bin/go"
            record["host_go_sha256"] = sha(host_go)
            helper = output / "session-fixtures"
            checked("session-fixtures-build", ["env", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", host_go, "build", "-mod=readonly", "-o", helper, "./cmd/session-fixtures"])
            checked("session-fixtures", [helper, package_directory, output / "admission-oracle"])
            record["session_fixtures_sha256"] = sha(helper)
            record["model_config"] = json.loads((package_directory / "PLAY.JSON").read_text())
            package_sources.update((APP / "cmd/session-fixtures").glob("*.go"))
            record["admission_policy"] = json.loads((package_directory / "RUNTIME.JSON").read_text())
        deps = checked("packages", [compiler, "list", "-deps", "-json", "-mod=readonly",
                                    "./cmd/" + target], native=True)
        files = {APP / name for name in ("go.mod", "go.sum", "run.py", "host_support.py", "package_fixtures.py", "admission_fixtures.py", "play_support.py", "toolchain.lock.toml")}
        files.update(package_sources)
        modules = {}
        for package in objects(deps):
            if module := package.get("Module"):
                modules[module["Path"]] = module
            directory = Path(package["Dir"])
            if directory.is_relative_to(ROOT):
                for field in ("GoFiles", "CgoFiles", "CFiles", "HFiles", "SFiles", "EmbedFiles"):
                    files.update(directory / f for f in package.get(field, []))
        record["modules"] = modules
        record["source_sha256"] = {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)}
        elf = output / (args.program + ".elf")
        checked("build", [compiler, "build", "-mod=readonly", "-trimpath", "-ldflags",
                           "-T 0x40020000 -R 0x1000", "-o", elf, "./cmd/" + target], native=True)
        record["elf_sha256"], record["elf_bytes"] = sha(elf), elf.stat().st_size
        data = elf.read_bytes()
        if data[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", data, 18)[0] != 183:
            raise RuntimeError("expected a little-endian AArch64 ELF")
        phoff = struct.unpack_from("<Q", data, 32)[0]
        phsize, phcount = struct.unpack_from("<HH", data, 54)
        if any(struct.unpack_from("<I", data, phoff + n * phsize)[0] == 3 for n in range(phcount)):
            raise RuntimeError("bare-metal image unexpectedly has an ELF interpreter")
        record.update(elf_machine="AArch64", elf_interpreter=None)
        checked("image-go-version", [compiler, "version", "-m", elf], native=True)
        display = args.display or ("sdl" if args.interactive else "none")
        if args.program in ("probe", "mapcheck", "fabric", "storage"):
            if args.program == "storage":
                lock_disk()
                record["disk"] = {"path": str(disk), "before_sha256": sha(disk)}
            with Guest(qemu, elf, output / "boot-1", command_record, disk=disk if args.program == "storage" else None, display=display) as guest:
                marker = {"probe": "FES_NATIVE_GO_PROBE PASS", "mapcheck": "FES_NATIVE_CPU_MAP PASS", "fabric": "FES_FABRIC_MODEL PASS", "storage": "FES_STORAGE_PROBE PASS"}[args.program]
                guest.wait(lambda text: marker in text, args.timeout, "diagnostic completion")
                if args.program == 'mapcheck':
                    pattern=(r'FES_NATIVE_CPU_MAP PASS el=1 sctlr=([0-9a-f]{1,16}) tcr=200003519 root=40004000 mair=44ff00 '
                             r'mmfr0=([0-9a-f]{1,16}) mpidr=([0-9a-f]{1,16}) reads=([0-9]{1,5}) translations=([0-9]{1,5}) '
                             r'bytes=402653188 descriptor_sha256=([0-9a-f]{64}) mapping_only=1\r?\n')
                    guest.wait(lambda text: re.search(pattern,text) is not None,args.timeout,'complete native CPU mapping observation')
                    rows=re.findall(pattern,guest.text)
                    if len(rows)!=1 or any(int(rows[0][i])<=0 for i in (3,4)) or int(rows[0][5],16)==0:
                        raise RuntimeError('native CPU mapping observation is incomplete or differs from the QEMU contract')
                    record['cpu_mapping_observation']={'sctlr':int(rows[0][0],16),'mmfr0':int(rows[0][1],16),
                        'mpidr':int(rows[0][2],16),'table_reads':int(rows[0][3]),'translations':int(rows[0][4]),
                        'descriptor_sha256':rows[0][5],'mapping_only':True,'physical_admission':False}
            if args.program == "storage":
                record["disk"]["after_sha256"] = sha(disk)
                if record["disk"]["before_sha256"] != record["disk"]["after_sha256"]:
                    raise RuntimeError("read-only storage probe changed its disk")
            record["termination"] = "runner stopped its QEMU process after probe marker"
        elif args.program == "display":
            captures = []
            with Guest(qemu, elf, output / "boot-1", command_record, display=display, video=True) as guest:
                guest.wait(lambda text: "FES_DISPLAY_PROBE_READY frame=1" in text, args.timeout, "first scanout frame")
                captures.append(guest.screenshot("bars"))
                guest.send(b"n")
                guest.wait(lambda text: "FES_DISPLAY_PROBE PASS frame=2" in text, args.timeout, "second scanout frame")
                captures.append(guest.screenshot("inverted"))
            colors = ((255, 0, 0), (0, 255, 0), (0, 0, 255), (255, 255, 255))
            for number, capture in enumerate(captures):
                raw = (output / capture["ppm_path"]).read_bytes().split(b"\n", 3)
                if len(raw) != 4 or raw[:3] != [b"P6", b"1280 720", b"255"]:
                    raise RuntimeError("display probe capture geometry differs")
                row = b"".join(bytes(255 - c if number else c for c in rgb) * 320 for rgb in colors)
                if raw[3] != row * 720:
                    raise RuntimeError("display probe pixel order/content differs")
            record["screenshots"] = captures
            record["display_checks"] = "two actual QEMU scanout frames match every expected RGB pixel"
            record["termination"] = "runner stopped QEMU after copies; no display drain or guest shutdown proof"
        elif args.program == "network":
            lock_disk()
            prepare_disk(prepare_host())
            before = sha(disk)
            with Guest(qemu, elf, output / "boot-1", command_record, disk=disk, display=display) as guest:
                guest.wait(lambda text: network_catalog(text) is not None, args.timeout, "HTTP catalog probe")
                ids = network_catalog(guest.text)
                if sorted(ids) != record["host"]["catalog_ids"]:
                    raise RuntimeError("network probe catalog differs from private host")
            record["disk"] = {"path": str(disk), "before_sha256": before, "after_sha256": sha(disk)}
            if before != sha(disk):
                raise RuntimeError("read-only network probe changed its disk")
            record["termination"] = "runner stopped QEMU after actual IPv4/TCP/HTTP catalog; not guest shutdown"
        elif args.program == "keyboard":
            with Guest(qemu, elf, output / "boot-1", command_record, usb=True, display=display) as guest:
                guest.wait(lambda text: "FES_KEYBOARD_READY" in text, args.timeout, "keyboard initialization")
                guest.press("a")
                guest.wait(lambda text: "FES_KEYBOARD_EVENT key=a" in text, args.timeout, "USB keyboard input")
            record["termination"] = "runner stopped QEMU after actual USB/HID input; not guest shutdown"
        elif args.program == "play":
            from play_support import exercise
            lock_disk()
            if not format_disk():
                raise RuntimeError("Play diagnostic refuses an existing disk")
            for path in sorted(package_directory.iterdir()):
                checked("copy-" + path.name, ["mcopy", "-i", disk, path, "::FES/" + path.name])
            initial = output / "preferences-initial.json"
            initial.write_bytes(json.dumps({"home": "library", "attract_enabled": False, "layout": "grid"}).encode().ljust(4096, b" "))
            checked("write-prefs", ["mcopy", "-i", disk, initial, "::FES/PREFS.JSON"])
            boot = output / "boot.json"
            boot.write_text(json.dumps({"model": True, "input": "usb"})+"\n")
            checked("write-boot", ["mcopy", "-i", disk, boot, "::FES/BOOT.JSON"])
            before = sha(disk)
            with Guest(qemu, elf, output / "boot-1", command_record, disk=disk, usb=True, display=display) as guest:
                # Case labels are diagnostic expectations, not product API.
                entries = record["model_config"]["entries"]
                record["play_results"], record["screenshots"] = exercise(guest, entries, args.timeout)
            record["disk"] = {"path": str(disk), "before_sha256": before, "after_sha256": sha(disk)}
            if record["disk"]["before_sha256"] != record["disk"]["after_sha256"]:
                raise RuntimeError("Play diagnostic changed the input FAT32 image")
            record["termination"] = "tenfoot quit and model closed before runner reaped QEMU"
            record["limitations"] = ["software model sessions, no emulated game or physical programming", "Cyclone V synthetic vectors; no DE25 acceptance", "in-process model catalog, not production host/target orchestration", "no power-loss durability or hardware DMA proof"]
        elif args.program in ("packages", "admission"):
            lock_disk()
            if not format_disk():
                raise RuntimeError("package diagnostics refuse an existing disk")
            for path in sorted(package_directory.iterdir()):
                checked("copy-" + path.name, ["mcopy", "-i", disk, path, "::FES/" + path.name])
            before = sha(disk)
            with Guest(qemu, elf, output / "boot-1", command_record, disk=disk, ui=False) as guest:
                if args.program == "admission":
                    guest.wait(lambda text: "FES_NATIVE_ADMISSION PASS" in text, args.timeout, "model runtime admission")
                    observed = [json.loads(line) for line in re.findall(r"FES_ADMISSION_CASE ([^\r\n]+)", guest.text)]
                    expected = [{"name": c["name"], "package_id": c["package_id"], "code": c["expected"],
                                 "owner_unchanged": c["expected"] != "none",
                                 "native_compatibility_code": c["runtime_code"]} for c in package_cases]
                    if observed != expected:
                        raise RuntimeError("guest admission decisions differ from fixture expectations")
                    record["admission_results"] = observed
                    marker = f"FES_NATIVE_CONTRACT_COMPATIBILITY PASS cases={len(package_cases)} linked={len(record['linked_cases'])} physical_activation=unavailable"
                    if marker not in guest.text:
                        raise RuntimeError("guest did not complete native contract compatibility checks")
                    record["native_compatibility_checked"] = True
                    linked = [json.loads(line) for line in re.findall(r"FES_ROM_LINK_CASE ([^\r\n]+)", guest.text)]
                    projection = [{k: c[k] for k in ("name", "package_id", "programmed_sha256", "programmed_size")} for c in linked]
                    expected_linked = [{k: c[k] for k in ("name", "package_id", "programmed_sha256", "programmed_size")} for c in record["linked_cases"]]
                    if projection != expected_linked or any(not re.fullmatch("[0-9a-f]{64}", c["receipt_sha256"]) for c in linked) or "FES_RETAINED_ROM_LINKS PASS" not in guest.text:
                        raise RuntimeError("guest retained linking differs from Mistral oracle")
                    record["linked_results"] = linked
                else:
                    guest.wait(lambda text: "FES_NATIVE_PACKAGES PASS" in text, args.timeout, "package inspection")
                    observed = [json.loads(line) for line in re.findall(r"FES_PACKAGE_CASE ([^\r\n]+)", guest.text)]
                    expected = [{"name": c["name"], "accepted": c["valid"], "package_id": c["package_id"] if c["valid"] else ""}
                                for c in package_cases]
                    projection = [{k: c[k] for k in ("name", "accepted", "package_id")} for c in observed]
                    if projection != expected or "FES_PACKAGE_SOURCE_MUTATION PASS" not in guest.text:
                        raise RuntimeError("guest package results differ from the shared corpus")
                    record["package_results"] = observed
            after = sha(disk)
            if before != after:
                raise RuntimeError("package diagnostic changed the final FAT32 disk")
            record["disk"] = {"path": str(disk), "before_sha256": before, "after_sha256": after}
            record["termination"] = "runner quit QEMU after package diagnostic; no FPGA programming"
        else:
            lock_disk()
            prepare_disk(prepare_host())
            initial = record["initial_preferences"].get("layout", "grid")
            if initial not in LAYOUT_NEXT:
                raise RuntimeError("unsupported initial layout in existing disk")
            screenshots = record["screenshots"] = []
            with Guest(qemu, elf, output / "boot-1", command_record, disk=disk, display=display, usb=args.input == "usb") as guest:
                first = ready(guest, initial)
                screenshots.append(guest.screenshot("initial"))
                if args.interactive:
                    print(f"Screenshot: {output / screenshots[-1]['path']}", flush=True)
                    guest.interactive()
                    screenshots.append(guest.screenshot("final"))
                else:
                    guest.press("right")
                    record["input_events"] = [{"source": args.input, "key": "right"}]
                    guest.wait(lambda text: any(f["seq"] > first["seq"] and f["focus"] != first["focus"]
                                                for f in frames(text)), args.timeout, "keyboard right navigation")
                    screenshots.append(guest.screenshot("right"))
                    count = guest.text.count("FES_PREFERENCES_SAVED")
                    expected = LAYOUT_NEXT[initial]
                    guest.press("l")
                    record["input_events"].append({"source": args.input, "key": "l"})
                    guest.wait(lambda text: any(f["layout"] == expected for f in frames(text)) and
                               text.count("FES_PREFERENCES_SAVED") > count,
                               args.timeout, "changed layout and saved preferences")
                    screenshots.append(guest.screenshot("layout"))
                record["first_boot_frames"] = frames(guest.text)
            saved = preferences("preferences-after-first-boot")
            record["saved_preferences"] = saved
            record["disk"]["after_first_boot_sha256"] = sha(disk)
            if not args.interactive:
                if saved.get("layout") != expected:
                    raise RuntimeError("FAT32 preference file does not contain selected layout")
                if len({s["sha256"] for s in screenshots}) != 3:
                    raise RuntimeError("QMP screenshots did not change with navigation and layout")
                with Guest(qemu, elf, output / "boot-2", command_record, disk=disk, display=display, usb=args.input == "usb") as guest:
                    ready(guest, expected)
                    screenshots.append(guest.screenshot("restored"))
                    record["second_boot_frames"] = frames(guest.text)
                record["reboot_preferences"] = preferences("preferences-after-reboot")
                if record["reboot_preferences"].get("layout") != expected:
                    raise RuntimeError("saved layout was not retained across reboot")
                record["persistence"] = "verified FAT32 write, fresh QEMU boot, loaded preference and displayed layout"
            record["disk"]["final_sha256"] = sha(disk)
            record["termination"] = "runner quit its QEMU process(es) through QMP; not guest shutdown evidence"
        if any(sha(ROOT / path) != digest for path, digest in record["source_sha256"].items()):
            raise RuntimeError("source changed during build/run; repeat when edits finish")
        record["status"] = "interactive-completed" if args.interactive else "passed"
        print(f"FES_NATIVE_{args.program.upper()} {record['status'].upper()}")
        print(output / "result.json")
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError, KeyboardInterrupt) as exc:
        record.update(status="failed", error=str(exc) or type(exc).__name__)
        print(record["error"], file=sys.stderr)
        return 1
    finally:
        stop(host_process)
        if host_log is not None:
            host_log.close()
        if disk_lock is not None:
            disk_lock.close()
        record["elapsed_seconds"] = time.monotonic() - started
        save()
    return 0


if __name__ == "__main__":
    sys.exit(main())
