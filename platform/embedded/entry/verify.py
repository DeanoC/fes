#!/usr/bin/env python3
"""Exercise the QEMU-only U-Boot entry image and its rejection paths."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import selectors
import struct
import subprocess
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
ADDRESS = 0x58080000
GOOD = b"FES_NATIVE_GO_PROBE PASS os=tamago board=qemu"


def digest(data):
    return hashlib.sha256(data).hexdigest()


def run_guest(qemu, uboot, image, virtualization, expected, log_path, timeout):
    command = [str(qemu), "-machine", "virt,virtualization=" + virtualization,
               "-cpu", "cortex-a53", "-m", "512M", "-smp", "1", "-nographic",
               "-monitor", "none", "-serial", "stdio", "-bios", str(uboot),
               "-device", f"loader,file={image},addr={ADDRESS:#x},force-raw=on", "-net", "none"]
    captured = bytearray()
    process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT, bufsize=0)
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ)
    phase = 0
    deadline = time.monotonic() + timeout
    try:
        while time.monotonic() < deadline and process.poll() is None:
            for key, _ in selector.select(min(0.25, max(0, deadline - time.monotonic()))):
                block = os.read(key.fd, 65536)
                if not block:
                    break
                captured.extend(block)
                if len(captured) > 1024 * 1024:
                    raise ValueError("QEMU console exceeded the diagnostic limit")
                if phase == 0 and b"Hit any key to stop autoboot" in captured:
                    process.stdin.write(b" ")
                    process.stdin.flush()
                    phase = 1
                if phase == 1 and b"=> " in captured:
                    process.stdin.write(f"booti {ADDRESS:#x} - $fdtcontroladdr\r\n".encode())
                    process.stdin.flush()
                    phase = 2
                if expected in captured:
                    return command, bytes(captured)
        raise ValueError("QEMU did not reach the expected marker before its deadline")
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        selector.close()
        process.stdin.close()
        process.stdout.close()
        log_path.write_bytes(captured)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image-build", required=True, type=Path)
    parser.add_argument("--uboot-build", required=True, type=Path,
                        help="output of build_qemu_uboot.py")
    parser.add_argument("--qemu", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--timeout", type=float, default=30)
    args = parser.parse_args()
    output = args.output.absolute()
    if (".." in output.parts or any(p.is_symlink() for p in (output, *output.parents)) or
            not output.is_relative_to(ROOT / "out") or output.exists() or "," in str(output)):
        parser.error("output must be a fresh non-symlink directory beneath this worktree's out/, without commas")
    if not 0 < args.timeout <= 120:
        parser.error("timeout must be between 0 and 120 seconds")
    qemu = args.qemu.resolve(strict=True)
    uboot_build = args.uboot_build.resolve(strict=True)
    uboot, config = uboot_build / "build/u-boot.bin", uboot_build / "build/.config"
    if not qemu.is_file() or not os.access(qemu, os.X_OK) or not uboot.is_file() or not config.is_file():
        parser.error("QEMU, U-Boot binary and U-Boot configuration must be regular files")
    configuration = config.read_text()
    uboot_receipt_bytes = (uboot_build / "result.json").read_bytes()
    uboot_receipt = json.loads(uboot_receipt_bytes)
    if (uboot_receipt["status"] != "built" or
            uboot_receipt["source_revision"] != "d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413" or
            uboot_receipt["binary_sha256"] != digest(uboot.read_bytes()) or
            uboot_receipt["config_sha256"] != digest(config.read_bytes())):
        raise ValueError("U-Boot binary or configuration differs from its pinned-source build receipt")
    if ("CONFIG_CMD_BOOTI=y\n" not in configuration or "CONFIG_BOOTM_LINUX=y\n" not in configuration or
            "CONFIG_ARMV8_SWITCH_TO_EL1=y\n" in configuration):
        parser.error("U-Boot configuration does not match the tested booti-to-EL2 contract")
    image_build = args.image_build.resolve(strict=True)
    record = json.loads((image_build / "result.json").read_text())
    original = (image_build / "entry.img").read_bytes()
    payload = (image_build / "payload.elf").read_bytes()
    offset = record["payload_offset"]
    if (record["status"] != "built" or record["target"] != "qemu" or record["entry_image_address"] != ADDRESS or
            record["entry_image_size"] != len(original) or record["payload_size"] != len(payload) or
            record["entry_image_sha256"] != digest(original) or record["payload_sha256"] != digest(payload) or
            type(offset) is not int or offset < 64 or offset + len(payload) > len(original) or
            original[offset:offset + len(payload)] != payload):
        raise ValueError("entry image or embedded payload differs from its build receipt")

    # Alter only private test copies after host validation, so the target itself
    # must reject an invalid ELF and an otherwise aligned overlap with its image.
    invalid = bytearray(original)
    invalid[offset] = 0
    overlap = bytearray(original)
    phoff = struct.unpack_from("<Q", payload, 32)[0]
    phsize, count = struct.unpack_from("<HH", payload, 54)
    loads = [phoff + i * phsize for i in range(count)
             if struct.unpack_from("<I", payload, phoff + i * phsize)[0] == 1]
    if len(loads) < 2:
        raise ValueError("negative test requires two load segments")
    struct.pack_into("<QQ", overlap, offset + loads[1] + 16, ADDRESS, ADDRESS)
    variants = (("native-probe", original, "on", GOOD),
                ("bad-magic", invalid, "on", b"FES_ENTRY REJECT ELF"),
                ("staging-overlap", overlap, "on", b"FES_ENTRY REJECT ELF"),
                ("wrong-exception-level", original, "off", b"FES_ENTRY REJECT EL"))
    output.mkdir(parents=True)
    result = {"status": "running", "classification": "QEMU-only U-Boot/native handoff, not DE25 hardware",
              "producer_sha256": digest((HERE / "verify.py").read_bytes()),
              "entry_build_sha256": digest((image_build / "result.json").read_bytes()),
              "entry_image_sha256": digest(original), "payload_sha256": digest(payload),
              "qemu_sha256": digest(qemu.read_bytes()), "uboot_sha256": digest(uboot.read_bytes()),
              "uboot_config_sha256": digest(config.read_bytes()),
              "uboot_build_sha256": digest(uboot_receipt_bytes), "cases": [],
              "limitations": ["QEMU firmware and CPU are not the Nano firmware or CPU",
                              "no physical DDR, DMA, device, FPGA or recovery proof",
                              "build receipts are diagnostic hashes, not signatures"]}
    try:
        for name, image, virtualization, expected in variants:
            path = output / (name + ".img")
            path.write_bytes(image)
            command, console = run_guest(qemu, uboot, path, virtualization, expected,
                                         output / (name + ".log"), args.timeout)
            if name == "native-probe":
                markers = (b"FES_ENTRY EL2 CLEAN", b"FES_ENTRY EL1 HANDOFF",
                           b"PASS timer goroutine allocation GC", b"PASS volatile memory filesystem", GOOD)
                positions = [console.find(marker) for marker in markers]
                if -1 in positions or positions != sorted(positions):
                    raise ValueError(name + " lacks the ordered native handoff/probe markers")
            elif b"FES_ENTRY EL1 HANDOFF" in console or GOOD in console:
                raise ValueError(name + " reached native handoff after rejection")
            elif name != "wrong-exception-level" and b"FES_ENTRY EL2 CLEAN" not in console:
                raise ValueError(name + " did not reach target-side ELF validation")
            if b"FES_ENTRY EXCEPTION" in console or b"panic:" in console:
                raise ValueError(name + " raised an exception")
            result["cases"].append({"name": name, "status": "passed", "image_sha256": digest(image),
                                    "console_sha256": digest(console), "command": command})
        if ((image_build / "entry.img").read_bytes() != original or
                digest(qemu.read_bytes()) != result["qemu_sha256"] or
                digest(uboot.read_bytes()) != result["uboot_sha256"] or
                digest(config.read_bytes()) != result["uboot_config_sha256"] or
                digest((HERE / "verify.py").read_bytes()) != result["producer_sha256"] or
                (uboot_build / "result.json").read_bytes() != uboot_receipt_bytes):
            raise ValueError("test inputs changed during the run")
        result["status"] = "passed"
    finally:
        if result["status"] == "running":
            result["status"] = "failed"
        (output / "result.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print("FES_ENTRY_QEMU PASSED:", output / "result.json")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError, struct.error, KeyError) as error:
        raise SystemExit("native entry verification: " + str(error))
