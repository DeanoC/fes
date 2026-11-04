#!/usr/bin/env python3
"""Exercise the QEMU-only U-Boot entry image and its rejection paths."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import selectors
import shutil
import struct
import subprocess
import time
from startup import decode as decode_startup

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
_codec_spec=importlib.util.spec_from_file_location('fes_verify_entry_payload',HERE/'payload.py')
entry_payload=importlib.util.module_from_spec(_codec_spec);_codec_spec.loader.exec_module(entry_payload)
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


def fault_image(record, original_build, packed, directory):
    """Relink the same checked entry objects around private negative input.

    Production build validation is never bypassed. These images exist only in
    the emulator verifier and must stop before the native handoff.
    """
    directory.mkdir()
    objects=record['objects']
    names=['part-'+str(i)+'.o' for i in range(6)]
    if set(objects)!=set(names):raise ValueError('entry fault fixture requires the complete object inventory')
    for name in names:
        source=original_build/name
        if digest(source.read_bytes())!=objects[name]:raise ValueError('entry object changed: '+name)
        shutil.copyfile(source,directory/name)
    tools=record['tools']
    for name in ('gcc','objcopy','cc1','as','ld'):
        if digest(Path(tools[name]['path']).read_bytes())!=tools[name]['sha256']:
            raise ValueError('entry compiler changed: '+name)
    for name,expected in record['sources'].items():
        if digest((ROOT/name).read_bytes())!=expected:raise ValueError('entry source changed: '+name)
    (directory/'payload.gz').write_bytes(packed)
    (directory/'payload.S').write_text('.section .payload,"a"\n.global __payload_start, __payload_end\n__payload_start:\n.incbin "payload.gz"\n__payload_end:\n')
    elf=directory/'entry.elf';image=directory/'entry.img'
    commands=[
        [tools['gcc']['path'],'-c','payload.S','-o','part-5.o'],
        [tools['gcc']['path'],'-nostdlib','-static','-no-pie','-Wl,--build-id=none',
         '-Wl,-T,'+str(HERE/'entry.ld'),'-Wl,--defsym=FES_ENTRY_IMAGE_ADDRESS='+hex(ADDRESS),*names,'-o',str(elf)],
        [tools['objcopy']['path'],'-O','binary',str(elf),str(image)]]
    for i,command in enumerate(commands):
        with (directory/('relink-'+str(i)+'.log')).open('wb') as log:
            subprocess.run(command,cwd=directory,env=record['environment'],stdout=log,stderr=subprocess.STDOUT,check=True,timeout=30)
    return image.read_bytes()


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
            type(offset) is not int or offset < 64):
        raise ValueError("entry image or embedded payload differs from its build receipt")

    entry_payload.checked(record,original,payload)
    # Rebuild only private encoded-payload objects so target ELF checks still
    # run on invalid decoded images, independently of the host's validation.
    output.mkdir(parents=True)
    invalid=bytearray(payload);invalid[0]=0
    overlap=bytearray(payload)
    phoff=struct.unpack_from('<Q',payload,32)[0]
    phsize,count=struct.unpack_from('<HH',payload,54)
    loads=[phoff+i*phsize for i in range(count) if struct.unpack_from('<I',payload,phoff+i*phsize)[0]==1]
    if len(loads)<2:raise ValueError('negative test requires two load segments')
    struct.pack_into('<QQ',overlap,loads[1]+16,ADDRESS,ADDRESS)
    scratch=bytearray(payload)
    struct.pack_into('<QQ',scratch,loads[1]+16,record['decode_base'],record['decode_base'])
    packed=original[offset:offset+record['payload_stored_size']]
    crc=bytearray(packed);crc[-8]^=1
    length=bytearray(packed);struct.pack_into('<I',length,len(length)-4,len(payload)+1)
    inputs=[('bad-magic',entry_payload.encode(invalid),b'FES_ENTRY REJECT ELF'),
            ('staging-overlap',entry_payload.encode(overlap),b'FES_ENTRY REJECT ELF'),
            ('decode-overlap',entry_payload.encode(scratch),b'FES_ENTRY REJECT ELF'),
            ('bad-payload-crc',bytes(crc),b'FES_ENTRY REJECT PAYLOAD'),
            ('truncated-deflate',packed[:10]+packed[10:-9]+packed[-8:],b'FES_ENTRY REJECT PAYLOAD'),
            ('wrong-decoded-size',bytes(length),b'FES_ENTRY REJECT PAYLOAD'),
            ('extra-member',packed+packed,b'FES_ENTRY REJECT PAYLOAD')]
    variants=[('native-probe',original,'on',GOOD)]
    for name,encoded,marker in inputs:
        variants.append((name,fault_image(record,image_build,encoded,output/(name+'-build')),'on',marker))
    variants.append(('wrong-exception-level',original,'off',b'FES_ENTRY REJECT EL'))
    result = {"status": "running", "classification": "QEMU-only U-Boot/native handoff, not DE25 hardware",
              "producer_sha256": digest((HERE / "verify.py").read_bytes()),
              "entry_build_sha256": digest((image_build / "result.json").read_bytes()),
              "entry_image_sha256": digest(original), "payload_sha256": digest(payload), "payload_encoding": entry_payload.ENCODING,
              "decode_base": record["decode_base"], "decode_capacity": record["decode_capacity"],
              "qemu_sha256": digest(qemu.read_bytes()), "uboot_sha256": digest(uboot.read_bytes()),
              "uboot_config_sha256": digest(config.read_bytes()),
              "uboot_build_sha256": digest(uboot_receipt_bytes), "cases": [],
              "startup_decoder_sha256": digest((HERE / "startup.py").read_bytes()),
              "limitations": ["QEMU firmware and CPU are not the Nano firmware or CPU",
                              "no physical DDR, DMA, device, FPGA or recovery proof",
                              "build receipts are diagnostic hashes, not signatures"]}
    try:
        for name, image, virtualization, expected in variants:
            path = output / (name + ".img")
            path.write_bytes(image)
            command, console = run_guest(qemu, uboot, path, virtualization, expected,
                                         output / (name + ".log"), args.timeout)
            startup = decode_startup(console, through='handoff')
            if startup['errors']:
                raise ValueError(name + ' has invalid startup CPU/counter checkpoints: ' + str(startup['errors']))
            if name == "native-probe":
                if not startup['complete']:
                    raise ValueError(name + ' lacks complete cache-on/decode/copy/cache-off checkpoints')
                markers = (b"FES_ENTRY EL2 CLEAN", b"FES_ENTRY EL1 HANDOFF",
                           b"PASS timer goroutine allocation GC", b"PASS volatile memory filesystem", GOOD)
                positions = [console.find(marker) for marker in markers]
                if -1 in positions or positions != sorted(positions):
                    raise ValueError(name + " lacks the ordered native handoff/probe markers")
            elif b"FES_ENTRY EL1 HANDOFF" in console or GOOD in console:
                raise ValueError(name + " reached native handoff after rejection")
            elif name != "wrong-exception-level" and b"FES_ENTRY EL2 CLEAN" not in console:
                raise ValueError(name + " did not reach target-side ELF validation")
            elif name != 'wrong-exception-level' and not startup['rejected']:
                raise ValueError(name + ' did not restore the clean cache state before rejection')
            if b"FES_ENTRY EXCEPTION" in console or b"panic:" in console:
                raise ValueError(name + " raised an exception")
            result["cases"].append({"name": name, "status": "passed", "image_sha256": digest(image),
                                    "console_sha256": digest(console), "command": command, "startup": startup})
        if ((image_build / "entry.img").read_bytes() != original or
                digest(qemu.read_bytes()) != result["qemu_sha256"] or
                digest(uboot.read_bytes()) != result["uboot_sha256"] or
                digest(config.read_bytes()) != result["uboot_config_sha256"] or
                digest((HERE / "verify.py").read_bytes()) != result["producer_sha256"] or
                digest((HERE / "startup.py").read_bytes()) != result["startup_decoder_sha256"] or
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
