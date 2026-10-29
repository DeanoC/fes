#!/usr/bin/env python3
"""Inspect the locked DE25 boot pair and optional HPS build without device access."""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import struct
import sys
import zlib

ROOT = Path(__file__).resolve().parents[1]


def load_builder():
    lane = ROOT / "image/de25-nano"
    spec = importlib.util.spec_from_file_location("de25_boot_contract_builder", lane / "build.py")
    builder = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(builder)
    boot_spec = importlib.util.spec_from_file_location("de25_boot_contract_vendor", lane / "boot.py")
    boot = importlib.util.module_from_spec(boot_spec)
    boot_spec.loader.exec_module(boot)
    return builder, boot


def sha(data):
    return hashlib.sha256(data).hexdigest()


def parse_fdt(data):
    """Decode bounded version-17 DTB/FIT properties, including external FIT tails."""
    if len(data) < 40:
        raise ValueError("truncated FDT header")
    magic, total, tree, names, reserve, version, compatible, _, names_size, tree_size = struct.unpack_from(">10I", data)
    if (magic != 0xd00dfeed or version != 17 or compatible > 17 or total > len(data)
            or total < 40 or tree % 4 or reserve % 8 or tree_size % 4
            or not 40 <= reserve < tree <= tree + tree_size <= names <= names + names_size <= total):
        raise ValueError("unsupported FDT header or overlapping sections")
    if reserve + 16 > tree or data[reserve:reserve + 16] != bytes(16):
        raise ValueError("unsupported FDT reservation map; review the pinned boot contract")
    string_table = data[names:names + names_size]
    end = tree + tree_size
    nodes, stack = {}, []

    def string_at(block, offset):
        if not 0 <= offset < len(block):
            raise ValueError("FDT string offset outside section")
        stop = block.find(b"\0", offset)
        if stop < 0:
            raise ValueError("unterminated FDT string")
        return block[offset:stop].decode("ascii"), stop + 1

    while tree + 4 <= end:
        token = struct.unpack_from(">I", data, tree)[0]
        tree += 4
        if token == 1:
            name, used = string_at(data[tree:end], 0)
            tree += (used + 3) & ~3
            if "/" in name or len(stack) >= 128 or (not stack and (name or nodes)):
                raise ValueError("invalid FDT node name or root")
            path = ((stack[-1].rstrip("/") if stack else "") + "/" + name) or "/"
            if path in nodes:
                raise ValueError("duplicate FDT node")
            nodes[path] = {}
            stack.append(path)
        elif token == 2:
            if not stack:
                raise ValueError("unbalanced FDT node")
            stack.pop()
        elif token == 3:
            if not stack or tree + 8 > end:
                raise ValueError("FDT property outside node or truncated")
            size, offset = struct.unpack_from(">II", data, tree)
            tree += 8
            if tree + ((size + 3) & ~3) > end:
                raise ValueError("FDT property outside structure block")
            name, _ = string_at(string_table, offset)
            if not name or name in nodes[stack[-1]]:
                raise ValueError("duplicate or empty FDT property")
            nodes[stack[-1]][name] = data[tree:tree + size]
            tree += (size + 3) & ~3
        elif token == 4:
            continue
        elif token == 9:
            if stack or "/" not in nodes or tree != end:
                raise ValueError("incomplete FDT structure")
            return nodes, total
        else:
            raise ValueError("unknown FDT token")
    raise ValueError("missing FDT end token")


def strings(data):
    if not data or not data.endswith(b"\0"):
        raise ValueError("invalid FDT string property")
    return data[:-1].decode("ascii").split("\0")


def cells(data):
    if len(data) % 4:
        raise ValueError("invalid FDT cell property")
    return list(struct.unpack(">" + "I" * (len(data) // 4), data))


def fit_images(data):
    nodes, total = parse_fdt(data)
    images = {}
    for path, props in nodes.items():
        if not path.startswith("/images/") or path.count("/") != 2:
            continue
        if "data" in props:
            if any(name in props for name in ("data-offset", "data-position", "data-size")):
                raise ValueError("ambiguous FIT payload location")
            payload, start = props["data"], None
        else:
            offsets = [name for name in ("data-offset", "data-position") if name in props]
            if len(offsets) != 1 or len(props.get(offsets[0], b"")) != 4 or len(props.get("data-size", b"")) != 4:
                raise ValueError("unsupported FIT external payload location")
            start = cells(props[offsets[0]])[0]
            if offsets[0] == "data-offset":
                start += (total + 3) & ~3
            size = cells(props["data-size"])[0]
            if start < total or size <= 0 or start + size > len(data):
                raise ValueError("FIT external payload outside file")
            payload = data[start:start + size]
        hashes = [value for name, value in nodes.items()
                  if name.startswith(path + "/") and name.count("/") == 3 and name.rsplit("/", 1)[1].startswith("hash")]
        if not hashes:
            raise ValueError("FIT payload has no hash")
        for item in hashes:
            algorithm = strings(item["algo"])
            if algorithm == ["crc32"]:
                actual = struct.pack(">I", zlib.crc32(payload) & 0xffffffff)
            elif algorithm == ["sha256"]:
                actual = hashlib.sha256(payload).digest()
            else:
                raise ValueError("unsupported FIT payload hash")
            if actual != item.get("value"):
                raise ValueError("FIT payload hash differs")
        images[path.rsplit("/", 1)[1]] = {
            "data": payload, "size": len(payload), "sha256": sha(payload), "file_offset": start,
            "type": strings(props["type"])[0],
            "load": cells(props["load"]) if "load" in props else [],
            "entry": cells(props["entry"]) if "entry" in props else [],
        }
    if not images:
        raise ValueError("FIT has no images")
    return nodes, images


def tree_summary(data):
    nodes, _ = parse_fdt(data)
    aliases = {name: strings(value)[0] for name, value in nodes.get("/aliases", {}).items()}
    serial = aliases.get("serial0")
    ethernet = aliases.get("ethernet0")
    if serial not in nodes or ethernet not in nodes:
        raise ValueError("DE25 serial0/ethernet0 aliases are missing")
    regions, bridges, firmware = [], [], []
    for path, props in nodes.items():
        compatible = strings(props["compatible"]) if "compatible" in props else []
        if "fpga-region" in compatible:
            regions.append({"path": path, "ranges": cells(props["ranges"]) if "ranges" in props else None,
                            "children": sorted(name for name in nodes if name.startswith(path + "/"))})
        if any("fpga-bridge" in name for name in compatible):
            bridges.append({"path": path, "compatible": compatible})
        if any(name in compatible for name in ("intel,agilex5-svc", "intel,agilex5-soc-fpga-mgr", "arm,psci-1.0")):
            firmware.append({"path": path, "compatible": compatible,
                             "method": strings(props["method"])[0] if "method" in props else None})
    memory = [{"path": path, "reg_cells": cells(props["reg"])} for path, props in nodes.items()
              if props.get("device_type") == b"memory\0" and "reg" in props]
    return {"model": strings(nodes["/"]["model"])[0], "compatible": strings(nodes["/"]["compatible"]),
            "serial0": {"path": serial, "status": strings(nodes[serial].get("status", b"okay\0"))[0],
                        "reg_cells": cells(nodes[serial]["reg"])},
            "ethernet0": {"path": ethernet, "status": strings(nodes[ethernet].get("status", b"okay\0"))[0],
                          "reg_cells": cells(nodes[ethernet]["reg"])},
            "chosen": {name: strings(value) for name, value in nodes.get("/chosen", {}).items()
                       if name in ("stdout-path", "bootargs")},
            "memory": memory, "fpga_regions": regions, "fpga_bridge_nodes": bridges,
            "firmware_interfaces": firmware}


def inspect_boot(directory, lock, boot):
    boot._verify_prepared_boot(directory, lock)

    def pinned_bytes(name):
        data = boot.regular_file(directory / name).read_bytes()
        expected = next(item for item in lock["boot_files"] if item["name"] == name)
        if len(data) != expected["size"] or sha(data) != expected["sha256"]:
            raise ValueError("decoded boot bytes differ from lock: " + name)
        return data

    linux = tree_summary(pinned_bytes("socfpga_agilex5_de25_nano.dtb"))
    if (linux["model"] != "SoCFPGA Agilex5 Terasic DE25-Nano"
            or "intel,socfpga-agilex5" not in linux["compatible"]
            or linux["serial0"]["path"] != "/soc@0/serial@10c02100"
            or linux["ethernet0"]["path"] != "/soc@0/ethernet@10810000"
            or linux["serial0"]["status"] != "okay" or linux["ethernet0"]["status"] != "okay"):
        raise ValueError("pinned DE25 device-tree boot contract differs")
    script_nodes, script_images = fit_images(pinned_bytes("boot.scr.uimg"))
    script_name = strings(script_nodes["/images"]["default"])[0]
    script = script_images[script_name]["data"].decode("ascii")
    commands = []
    for line in script.splitlines():
        line = line.split("#", 1)[0]
        commands.extend(command.strip() for command in line.split(";") if command.strip())
    bridge_commands = [command for command in commands if re.match(r"^bridge(?:\s|$)", command)]
    bootargs_commands = [command for command in commands
                         if re.match(r"^setenv\s+bootargs(?:\s|$)", command)]
    expected_bootargs = 'setenv bootargs "console=ttyS0,115200 root=${mmcroot} rw rootwait"'
    if (script_images[script_name]["type"] != "script" or bridge_commands != ["bridge enable"]
            or bootargs_commands != [expected_bootargs]
            or not any("socfpga_agilex5_de25_nano.dtb" in command.split() for command in commands)
            or "booti ${kernel_addr_r} - ${fdt_addr_r}" not in commands):
        raise ValueError("pinned boot script contract differs")
    fit_nodes, images = fit_images(pinned_bytes("u-boot.itb"))
    selected = strings(fit_nodes["/configurations"]["default"])[0]
    config = fit_nodes["/configurations/" + selected]
    roles = {name: strings(config[name]) for name in ("firmware", "loadables", "fdt")}
    if roles != {"firmware": ["atf"], "loadables": ["uboot"], "fdt": ["fdt-0"]}:
        raise ValueError("pinned U-Boot FIT configuration differs")
    for references in roles.values():
        if any(name not in images for name in references):
            raise ValueError("FIT configuration references missing image")
    uboot_tree = tree_summary(images["fdt-0"]["data"])
    env = images["uboot"]["data"]
    root_entries = [entry for entry in env.split(b"\0")
                    if entry.partition(b"=")[0] == b"mmcroot"]
    if root_entries != [b"mmcroot=/dev/mmcblk0p2"]:
        raise ValueError("U-Boot root-device environment differs")
    return {"linux_device_tree": linux, "uboot_device_tree": uboot_tree,
            "fit_configuration": {"name": selected, **roles},
            "fit_images": {name: {key: value for key, value in image.items() if key != "data"}
                           for name, image in images.items()},
            "boot_script": {"sha256": script_images[script_name]["sha256"], "text": script,
                            "enables_fpga_bridges": True, "overrides_chosen_bootargs": True},
            "root_device": "/dev/mmcblk0p2", "cpu_mailbox_address": "not-established",
            "memory_size": "raw DT declarations; runtime memory banks require board observation"}


def inspect_hps(directory, lock, boot):
    inputs_path, build_path = directory / "inputs.json", directory / "build.json"
    inputs_bytes = boot.regular_file(inputs_path).read_bytes()
    inputs = json.loads(inputs_bytes)
    build = json.loads(boot.regular_file(build_path).read_bytes())
    if (inputs.get("vendor_archive_sha256") != lock["resources"]["sha256"]
            or inputs.get("source_classification") != "committed"
            or inputs.get("fes_revision") != build.get("fes_revision")
            or not re.fullmatch(r"[a-f0-9]{40}", build.get("fes_revision", ""))
            or build.get("board") != "de25_nano" or build.get("device") != "A5EB013BB23BE4SCS"
            or build.get("diagnostic_only") is not True or build.get("inputs_sha256") != sha(inputs_bytes)
            or build.get("artifact") != "project/output_files/golden_top.sof"):
        raise ValueError("HPS build receipt or vendor identity differs")
    sof = boot.regular_file(directory / build["artifact"])
    handoff_name = "output_files/hps_bootloader_handoff.bin"
    handoff = boot.regular_file(directory / "project" / handoff_name)
    sof_bytes, handoff_bytes = sof.read_bytes(), handoff.read_bytes()
    if (sha(sof_bytes) != build["sha256"] or sha(handoff_bytes) !=
            build["generated_handoff_and_sdc_sha256"].get(handoff_name)):
        raise ValueError("HPS SOF or handoff differs from build receipt")
    if handoff_bytes[:4] != b"BOOT":
        raise ValueError("unsupported HPS handoff header")
    return {"fes_revision": build["fes_revision"], "sof_sha256": build["sha256"],
            "handoff_sha256": sha(handoff_bytes), "handoff_size": len(handoff_bytes),
            "vendor_archive_sha256": inputs["vendor_archive_sha256"],
            "mailbox_relative_offset": inputs["mailbox_peripheral_offset"],
            "cpu_mailbox_address": "not-established", "factory_boot_pairing": "not-qualified",
            "reason": "The resource GHRD build has no established provenance to the BSP factory QSPI image."}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image-output", type=Path, default=ROOT / "out/de25-nano-linux")
    parser.add_argument("--hps-output", type=Path)
    parser.add_argument("--output", type=Path, help="new JSON report path; existing reports are refused")
    args = parser.parse_args(argv)
    try:
        builder, boot = load_builder()
        # verify_receipt imports the lane's boot module, not this inspector.
        sys.path.insert(0, str(ROOT / "image/de25-nano"))
        receipt = builder.verify_receipt(args.image_output)
        lock = boot.load_lock()
        result = {"format": 1, "board": "de25-nano", "classification": "host-only-boot-contract",
                  "inspector_sha256": sha(Path(__file__).read_bytes()),
                  "image_producer": receipt["fes_revision"], "image_sha256": receipt["files"]["fes-de25-nano.img"],
                  "factory_qspi_sha256": next(item["sha256"] for item in lock["factory_boot"]
                                              if item["role"] == "factory-qspi-boot-dependency"),
                  "boot": inspect_boot(args.image_output / "vendor-boot", lock, boot),
                  "hardware_boot": "not-run"}
        if args.hps_output:
            result["hps_build"] = inspect_hps(args.hps_output, lock, boot)
        serialized = json.dumps(result, indent=2, sort_keys=True) + "\n"
        if args.output:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            with args.output.open("x") as target:
                target.write(serialized)
        else:
            print(serialized, end="")
        return 0
    except (OSError, ValueError, KeyError, struct.error, UnicodeError) as error:
        parser.exit(1, "de25-boot-contract: " + str(error) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
