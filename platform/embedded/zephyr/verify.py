#!/usr/bin/env python3
"""Build and verify real emulated FAT32/NVMe and Ethernet/TCP base services."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import time

APP = Path(__file__).resolve().parent
ROOT = APP.parents[2]
ZEPHYR_COMMIT = "413b789deb391d3a37d06b463288a5fe765ee57e"
FATFS_COMMIT = "16245c7c41d2b79e74984f49b5202551786b8a9b"


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def execute(argv, log, *, env=None, timeout=300):
    with log.open("wb") as output:
        result = subprocess.run([str(x) for x in argv], stdout=output,
                                stderr=subprocess.STDOUT, env=env, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): see {log}")


def revision(directory):
    return subprocess.check_output(["git", "-C", str(directory), "rev-parse", "HEAD"],
                                   text=True).strip()


def exchange(port, payload):
    with socket.create_connection(("127.0.0.1", port), timeout=2) as client:
        client.sendall(payload)
        client.shutdown(socket.SHUT_WR)
        reply = bytearray()
        while not reply.endswith(b"\n") and len(reply) < 1024:
            part = client.recv(1024 - len(reply))
            if not part:
                break
            reply.extend(part)
    return json.loads(reply)


def scenario(qemu, elf, disk, output, name, expected_config, commands, storage_only=False):
    # QEMU owns the listener; no TAP, privilege escalation or exposed LAN port.
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    argv = [str(qemu), "-machine", "virt,gic-version=3", "-cpu", "cortex-a53",
            "-m", "128M", "-smp", "1", "-nographic", "-monitor", "none",
            "-kernel", str(elf), "-netdev",
            f"user,id=network,hostfwd=tcp:127.0.0.1:{port}-10.0.2.15:4242",
            "-device", "e1000,netdev=network,addr=1",
            "-drive", f"if=none,id=storage,file={disk},format=raw",
            "-device", "nvme,drive=storage,serial=FESBASE,addr=2"]
    commands.append(argv)
    outcomes = []
    before = digest(disk)
    with (output / f"{name}-console.log").open("wb") as console:
        process = subprocess.Popen(argv, stdout=console, stderr=subprocess.STDOUT)
        try:
            if storage_only:
                deadline = time.monotonic() + 10
                expected = "FES_BASE READY tcp=4242 storage=ready boot_config=" + (
                    "verified" if expected_config else "unavailable")
                while time.monotonic() < deadline:
                    if process.poll() is not None:
                        raise RuntimeError(f"{name}: QEMU exited before storage readiness")
                    text = (output / f"{name}-console.log").read_text(errors="replace")
                    if expected in text and "FES_BASE STORAGE_READY filesystem=fat32 driver=nvme" in text:
                        outcomes.append({"case": "real-fat32-config-read", "boot_config": expected_config})
                        break
                    time.sleep(0.1)
                else:
                    raise RuntimeError(f"{name}: storage readiness deadline")
            else:
                outcomes.extend(network_checks(process, port, name, expected_config))
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
    assert digest(disk) == before, f"{name}: read-only test changed the disk image"
    console_text = (output / f"{name}-console.log").read_text(errors="replace")
    if "FATAL" in console_text or "ASSERTION FAIL" in console_text:
        raise RuntimeError(f"{name}: kernel failure in console")
    return {"name": name, "status": "passed", "disk_sha256": before, "checks": outcomes}


def network_checks(process, port, name, expected_config):
    outcomes = []
    deadline = time.monotonic() + 25
    status = None
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"{name}: QEMU exited before service readiness")
        try:
            status = exchange(port, b"STATUS\n")
            break
        except (ConnectionError, TimeoutError, OSError, ValueError):
            time.sleep(0.1)
    if status is None:
        raise RuntimeError(f"{name}: TCP readiness deadline; inspect console")
    assert status["storage"]["ready"] is True, status
    assert status["storage"]["filesystem"] == "fat32", status
    assert status["storage"]["boot_config"] is expected_config, status
    assert status["network"] == "e1000-tcp", status
    assert status["sdcard"] == status["usb"] == status["fpga"] == "unavailable", status
    outcomes.append({"case": "status", "response": status})
    if expected_config:
        cases = [
            (b"INPUT 1 17\n", {"ok": True, "route": "diagnostic-only"}),
            (b"INPUT 1 18\n", {"error": "stale-sequence"}),
            (b"INPUT -1 0\n", {"error": "invalid-input"}),
            (b"INPUT 2 65536\n", {"error": "invalid-input"}),
            (b"INPUT 2 1 trailing\n", {"error": "invalid-input"}),
            (b"USB\n", {"error": "unavailable"}),
            (b"SDCARD\n", {"error": "unavailable"}),
            (b"FPGA\n", {"error": "unavailable"}),
            (b"UNKNOWN\n", {"error": "unknown-command"}),
            (b"STA", {"error": "invalid-request"}),
            (b"STATUS\0\n", {"error": "invalid-request"}),
            (b"X" * 128 + b"\n", {"error": "invalid-request"}),
        ]
        for payload, expected in cases:
            actual = exchange(port, payload)
            assert actual == expected, (payload, actual, expected)
            outcomes.append({"case": payload.decode(errors="backslashreplace").rstrip("\n"),
                             "response": actual})
        status = exchange(port, b"STATUS\n")
        assert status["input"] == {"route": "diagnostic-only", "sequence": 1, "buttons": 17}, status
        outcomes.append({"case": "input-state-after-rejections", "response": status["input"]})
    else:
        assert status["storage"]["error"] != 0, status
    return outcomes

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    if not __debug__:
        parser.error("verification requires assertions; run without -O")
    parser.add_argument("--zephyr-base", type=Path, required=True)
    parser.add_argument("--sdk", type=Path, required=True)
    parser.add_argument("--fatfs", type=Path, required=True)
    parser.add_argument("--python", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--storage-only", action="store_true",
                        help="verify only real NVMe/FAT32; do not claim network success")
    args = parser.parse_args()
    output = args.output.resolve()
    if not output.is_relative_to(ROOT / "out") or output.exists():
        parser.error("--output must be a fresh directory beneath this worktree's out/")
    zephyr, sdk, fatfs = args.zephyr_base.resolve(), args.sdk.resolve(), args.fatfs.resolve()
    if revision(zephyr) != ZEPHYR_COMMIT or revision(fatfs) != FATFS_COMMIT:
        parser.error("Zephyr/FatFS checkout does not match the tested pinned revisions")
    if (sdk / "sdk_version").read_text().strip() != "0.17.4":
        parser.error("SDK 0.17.4 is required")
    output.mkdir(parents=True)
    sources = {str(path.relative_to(ROOT)): digest(path)
               for path in sorted(APP.rglob("*")) if path.is_file() and "__pycache__" not in str(path)}
    record = {"status": "incomplete", "classification": "emulated block-device software" if args.storage_only else "emulated device software",
              "zephyr": ZEPHYR_COMMIT, "fatfs": FATFS_COMMIT, "sdk": "0.17.4",
              "network": "not-tested" if args.storage_only else "pending",
              "sources": sources, "commands": [], "scenarios": []}
    record_path = output / "result.json"
    try:
        env = dict(os.environ, ZEPHYR_BASE=str(zephyr), ZEPHYR_SDK_INSTALL_DIR=str(sdk),
                   ZEPHYR_TOOLCHAIN_VARIANT="zephyr")
        configure = ["cmake", "-S", APP, "-B", output / "build", "-GNinja",
                     "-DBOARD=qemu_cortex_a53", "-DPython3_EXECUTABLE=" + str(args.python.absolute()),
                     "-DZEPHYR_MODULES=" + str(fatfs)]
        build = ["cmake", "--build", output / "build", "--parallel", "4"]
        for name, argv in (("configure", configure), ("build", build)):
            record["commands"].append([str(x) for x in argv])
            execute(argv, output / f"{name}.log", env=env)
        good = output / "good.img"
        make_disk = [sys.executable, APP / "make_disk.py", good]
        record["commands"].append([str(x) for x in make_disk])
        execute(make_disk, output / "disk.log")
        missing, bad = output / "missing.img", output / "bad.img"
        nul_suffix = output / "nul-suffix.img"
        shutil.copyfile(good, missing)
        shutil.copyfile(good, bad)
        shutil.copyfile(good, nul_suffix)
        wrong = output / "wrong-config.txt"
        wrong.write_text("format=unrecognized\nlauncher=tenfoot\n")
        nul_config = output / "nul-suffix-config.bin"
        nul_config.write_bytes(b"format=fes-base-v1\nlauncher=tenfoot\n\0unexpected-junk")
        for name, argv in (
                ("missing-config", ["mdel", "-i", missing, "::FESBOOT.CFG"]),
                ("bad-config", ["mcopy", "-o", "-i", bad, wrong, "::FESBOOT.CFG"]),
                ("nul-suffix-config", ["mcopy", "-o", "-i", nul_suffix, nul_config, "::FESBOOT.CFG"])):
            record["commands"].append([str(x) for x in argv])
            execute(argv, output / f"{name}.log")
        elf = output / "build/zephyr/zephyr.elf"
        qemu = sdk / "sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64"
        record["elf_sha256"] = digest(elf)
        record["qemu_sha256"] = digest(qemu)
        record["nvme_original_sha256"] = digest(zephyr / "drivers/disk/nvme/nvme_controller.c")
        record["nvme_adapted_sha256"] = digest(output / "build/nvme_controller_aarch64.c")
        for name, disk, config_ready in (("good", good, True), ("missing", missing, False),
                                        ("bad", bad, False), ("nul-suffix", nul_suffix, False)):
            record["scenarios"].append(scenario(qemu, elf, disk, output, name, config_ready,
                                                record["commands"], args.storage_only))
        if any(digest(ROOT / name) != value for name, value in sources.items()):
            raise RuntimeError("source changed during verification")
        record["status"] = "passed"
        if not args.storage_only: record["network"] = "passed"
        label = "STORAGE_PASS network=not-tested" if args.storage_only else "FULL_PASS"
        print(f"FES_BASE {label} scenarios={len(record['scenarios'])}; {record_path}")
    except (AssertionError, OSError, RuntimeError, subprocess.SubprocessError) as error:
        record.update(status="failed", error=str(error))
        if not args.storage_only: record["network"] = "failed"
        print(error, file=sys.stderr)
        return 1
    finally:
        record_path.write_text(json.dumps(record, indent=2) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
