#!/usr/bin/env python3
"""Run tenfoot's complete model launch/return suite through QEMU U-Boot."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shutil
import sys
from startup import decode as decode_startup

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
_codec_spec=importlib.util.spec_from_file_location('fes_play_entry_payload',HERE/'payload.py')
entry_payload=importlib.util.module_from_spec(_codec_spec);_codec_spec.loader.exec_module(entry_payload)
sys.path.insert(0, str(HERE.parent / "tamago"))
from host_support import Guest  # noqa: E402
from play_support import exercise  # noqa: E402

ADDRESS = 0x58080000


def sha(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--play-build", required=True, type=Path,
                        help="passed direct-QEMU play run and its synthetic FAT32 image")
    parser.add_argument("--image-build", required=True, type=Path,
                        help="entry image built from that run's play.elf")
    parser.add_argument("--uboot-build", required=True, type=Path)
    parser.add_argument("--qemu", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--timeout", type=float, default=90)
    args = parser.parse_args()
    output = args.output.absolute()
    if (".." in output.parts or any(p.is_symlink() for p in (output, *output.parents)) or
            not output.is_relative_to(ROOT / "out") or output.exists() or "," in str(output)):
        parser.error("output must be a fresh non-symlink directory under this worktree's out/, without commas")
    if not 0 < args.timeout <= 300:
        parser.error("timeout must be between 0 and 300 seconds")
    play, image_build, uboot_build, qemu = (p.resolve(strict=True) for p in
                                           (args.play_build, args.image_build, args.uboot_build, args.qemu))
    play_receipt = json.loads((play / "result.json").read_text())
    image_receipt = json.loads((image_build / "result.json").read_text())
    uboot_receipt = json.loads((uboot_build / "result.json").read_text())
    native, original_disk = play / "play.elf", play / "fat32.img"
    image, embedded = image_build / "entry.img", image_build / "payload.elf"
    uboot, config = uboot_build / "build/u-boot.bin", uboot_build / "build/.config"
    native_hash, disk_hash = sha(native), sha(original_disk)
    image_bytes, payload_bytes = image.read_bytes(), embedded.read_bytes()
    offset = image_receipt["payload_offset"]
    if (play_receipt["status"] != "passed" or play_receipt["program"] != "play" or
            play_receipt["elf_sha256"] != native_hash or play_receipt["disk"]["before_sha256"] != disk_hash or
            play_receipt["disk"]["after_sha256"] != disk_hash or
            image_receipt["status"] != "built" or image_receipt["target"] != "qemu" or
            image_receipt["entry_image_address"] != ADDRESS or image_receipt["entry_image_sha256"] != sha(image) or
            image_receipt["entry_image_size"] != len(image_bytes) or image_receipt["payload_size"] != len(payload_bytes) or
            image_receipt["payload_sha256"] != native_hash or sha(embedded) != native_hash or
            uboot_receipt["status"] != "built" or
            uboot_receipt["source_revision"] != "d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413" or
            uboot_receipt["binary_sha256"] != sha(uboot) or uboot_receipt["config_sha256"] != sha(config)):
        raise ValueError("play, entry or U-Boot artifacts differ from their build receipts")
    entry_payload.checked(image_receipt,image_bytes,payload_bytes)
    output.mkdir(parents=True)
    disk = output / "fat32.img"
    shutil.copyfile(original_disk, disk)
    if sha(disk) != disk_hash:
        raise ValueError("private FAT32 image differs from the recorded play input")
    commands = []
    sources = [HERE / "verify_tenfoot.py", HERE / "payload.py", HERE / "startup.py", HERE.parent / "tamago/host_support.py", HERE.parent / "tamago/play_support.py"]
    source_hashes = {str(path.relative_to(ROOT)): sha(path) for path in sources}
    result = {"status": "running", "classification": "QEMU U-Boot and tenfoot software model; no physical FPGA",
              "native_elf_sha256": native_hash, "input_disk_sha256": disk_hash,
              "image_sha256": sha(image), "uboot_sha256": sha(uboot), "qemu_sha256": sha(qemu),
              "play_receipt_sha256": sha(play / "result.json"),
              "entry_receipt_sha256": sha(image_build / "result.json"),
              "uboot_receipt_sha256": sha(uboot_build / "result.json"), "commands": commands, "sources": source_hashes,
              "limitations": ["single QEMU CPU and emulated USB, storage and display",
                              "software model sessions, no emulated gameplay or physical FPGA programming",
                              "not the Nano U-Boot binary or a physical Nano handoff"]}
    try:
        with Guest(qemu, image, output / "boot-1", commands.append, disk=disk, usb=True,
                   boot_firmware=uboot, image_address=ADDRESS) as guest:
            guest.wait(lambda text: "Hit any key to stop autoboot" in text, args.timeout, "U-Boot autoboot")
            guest.send(b" ")
            guest.wait(lambda text: "=> " in text, args.timeout, "U-Boot prompt")
            def uboot_command(command):
                start, prompts = len(guest.text), guest.text.count("=> ")
                guest.send((command + "\r").encode())
                guest.wait(lambda text: text.count("=> ") > prompts, args.timeout, "U-Boot: " + command)
                return guest.text[start:]
            # This is specific to the emulated qemu-xhci at 0:1.0. U-Boot has
            # enumerated the keyboard and leaves PCI bus mastering enabled,
            # while the native driver refuses a controller it does not own.
            identity = uboot_command("pci display.l 0.1.0 0 1")
            if not re.search(r"(?im)^00000000:\s+000d1b36\s*$", identity):
                raise ValueError("unexpected QEMU xHCI PCI identity")
            stopped = uboot_command("usb stop f")
            if "stopping USB.." not in stopped:
                raise ValueError("U-Boot did not stop the emulated USB controller")
            uboot_command("pci write.w 0.1.0 4 0")
            command = uboot_command("pci display.w 0.1.0 4 1")
            if not re.search(r"(?im)^00000004:\s+0000\s*$", command):
                raise ValueError("emulated xHCI PCI decode/bus mastering remained enabled")
            guest.send(f"booti {ADDRESS:#x} - $fdtcontroladdr\r".encode())
            guest.wait(lambda text: "FES_ENTRY EL2 CLEAN" in text, args.timeout, "clean EL2 entry")
            guest.wait(lambda text: "FES_ENTRY EL1 HANDOFF" in text, args.timeout, "EL1 handoff")
            result["play_results"], result["screenshots"] = exercise(
                guest, play_receipt["model_config"]["entries"], args.timeout)
            if "FES_ENTRY EXCEPTION" in guest.text or "FES_ENTRY REJECT" in guest.text:
                raise ValueError("entry rejected or faulted during tenfoot boot")
            result['startup'] = decode_startup(guest.text, through='handoff')
            if not result['startup']['complete']:
                raise ValueError('model Play lacks the complete clean loader CPU/cache/counter sequence')
        if (sha(disk) != disk_hash or sha(original_disk) != disk_hash or sha(image) != result["image_sha256"] or
                sha(native) != native_hash or sha(uboot) != result["uboot_sha256"] or sha(qemu) != result["qemu_sha256"] or
                sha(play / "result.json") != result["play_receipt_sha256"] or
                sha(image_build / "result.json") != result["entry_receipt_sha256"] or
                sha(uboot_build / "result.json") != result["uboot_receipt_sha256"] or
                source_hashes != {str(path.relative_to(ROOT)): sha(path) for path in sources}):
            raise ValueError("test image or inputs changed during the run")
        result["status"] = "passed"
    finally:
        if result["status"] == "running":
            result["status"] = "failed"
        (output / "result.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print("FES_ENTRY_TENFOOT PASSED:", output / "result.json")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, RuntimeError) as error:
        raise SystemExit("U-Boot tenfoot verification: " + str(error))
