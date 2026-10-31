#!/usr/bin/env python3
"""Replay the source/generated-HDL audit without invoking Quartus or hardware."""
import argparse
from decimal import Decimal
from fractions import Fraction
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tomllib
import xml.etree.ElementTree as ET
import zipfile


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def parameters(path):
    ns = {"i": "http://www.accellera.org/XMLSchema/IPXACT/1685-2014",
          "a": "http://www.altera.com/XMLSchema/IPXact2014/extensions"}
    tree = ET.parse(path)
    result = {n.get("parameterId"): n.findtext("i:value", namespaces=ns) or ""
              for n in tree.findall(".//a:altera_module_parameters/i:parameters/i:parameter", ns)}
    for module in tree.findall(".//a:sub_module", ns):
        for node in module.findall("i:parameters/i:parameter", ns):
            name = "submodule/" + module.get("{" + ns["a"] + "}name") + "/" + node.get("parameterId")
            result[name] = node.findtext("i:value", namespaces=ns) or ""
    return result


def pll(folder, rate):
    xml = folder / (folder.name + ".xml")
    tree = ET.parse(xml)
    require([n.get("value") for n in tree.findall(
        "./perimeter/interface[@name='outclk0']/property[@name='clockRate']")] == [str(rate)], "PLL interface rate")
    for name, value in (("gui_actual_phase_shift0", 0), ("gui_actual_duty_cycle0", 50)):
        nodes = tree.findall("./entity/parameter[@name='" + name + "']")
        require(len(nodes) == 1 and Decimal(nodes[0].get("value")) == value, "PLL phase/duty")
    hdl = list(folder.glob("altera_iopll_*/synth/*.v"))
    require(len(hdl) == 1, "PLL atom source count")
    source = hdl[0].read_text()
    counters = {}
    for name in ("fb_clk_m_div", "ref_clk_n_div", "out_clk_0_c_div", "fb_clk_fractional_div_num",
                 "fb_clk_fractional_div_den", "fb_clk_fractional_div_value"):
        values = re.findall(r"\." + name + r"\((\d+)\)", source)
        require(len(values) == 1 and int(values[0]) > 0, "PLL counter " + name)
        counters[name] = int(values[0])
    require(all(v == 1 for n, v in counters.items() if "fractional" in n), "PLL fractional feedback")
    require(Fraction(50000000 * counters["fb_clk_m_div"], counters["ref_clk_n_div"] *
                     counters["out_clk_0_c_div"]) == rate, "PLL exact counter ratio")
    require(re.findall(r"\.out_clk_0_freq\(36'd(\d+)\)", source) == [str(rate)], "PLL atom rate")
    return {"hz": rate, "counters": counters, "xml_sha256": sha(xml.read_bytes()),
            "hdl_sha256": sha(hdl[0].read_bytes())}


def mif(path):
    text = path.read_text()
    depth = int(re.search(r"DEPTH\s*=\s*(\d+)", text)[1])
    require(re.search(r"WIDTH\s*=\s*8\s*;", text) is not None, "ROM width")
    result = {}
    for row in re.finditer(r"^\s*([0-9A-Fa-f]+)\s*:\s*([^;]+);", text, re.M):
        start = int(row[1], 16)
        for offset, token in enumerate(row[2].split()):
            require(start + offset not in result, "duplicate ROM address")
            result[start + offset] = int(token, 16)
    require(set(result) == set(range(depth)), "MIF contiguous defined bytes")
    return bytes(result[n] for n in range(depth))


def audit(directory):
    directory = directory.resolve()
    project, snapshot = directory / "project", directory.parent / "functional-source"
    repo = Path(__file__).resolve().parents[6]
    inputs = json.loads((directory / "inputs.json").read_text())
    vendor = json.loads((directory / "vendor-inputs.json").read_text())
    revision = inputs["fes_revision"]
    require(inputs["source_classification"] == "committed" and re.fullmatch(r"[0-9a-f]{40}", revision),
            "committed producer identity")
    selected = inputs["functional_snapshot_sha256"]
    for name, digest in selected.items():
        require(sha((snapshot / name).read_bytes()) == digest, "snapshot hash: " + name)
        committed = subprocess.check_output(["git", "show", revision + ":" + name], cwd=repo)
        require(sha(committed) == digest, "selected Git blob: " + name)
    for name, digest in inputs["computer"]["source_sha256"].items():
        require(sha((project / "fes_mailbox" / name).read_bytes()) == digest, "staged hash: " + name)
    archive = directory.parent / "vendor.zip"
    require(sha(archive.read_bytes()) == inputs["vendor_archive_sha256"], "retained vendor archive")
    lock = tomllib.loads((snapshot / "sources/misteross/boards/de25nano/hps/vendor.lock.toml").read_text())
    require(lock["archive"]["sha256"] == inputs["vendor_archive_sha256"], "selected vendor lock")
    with zipfile.ZipFile(archive) as source:
        original_top = source.read(lock["archive"]["prefix"] + "golden_top.v").decode()
    top = (project / "golden_top.v").read_text()
    top_connections = lambda s: dict(re.findall(
        r"^\s*\.(hps_io_\w+|emif_hps_\w+)\s*\(\s*([^)]*?)\s*\)", s, re.M))
    original_connections = top_connections(original_top)
    require(len(original_connections) == 53 and top_connections(top) == original_connections,
            "original HPS/EMIF top connections")
    parameter_counts = {}
    for name, expected in inputs["vendor_hps_emif_parameters"].items():
        require(parameters(project / name) == expected, "original HPS/EMIF parameter set: " + name)
        parameter_counts[name] = len(expected)
    boot = {n: digest for n, digest in vendor["source_sha256"].items() if n.startswith("software/")}
    require(len(boot) == 4, "vendor boot/software source count")
    for name, digest in boot.items():
        require(sha((project / name).read_bytes()) == digest, "retained boot data: " + name)

    fingerprint = sha(json.dumps({"source_sha256": selected,
        "vendor_sha256": inputs["vendor_archive_sha256"], "system_hz": 100000000,
        "pixel_hz": 74250000}, sort_keys=True).encode())[:32]
    require(fingerprint == inputs["computer"]["build_id"], "recomputed BUILD_ID")
    qsf = (project / "golden_top.qsf").read_text()
    require(qsf.count("FES_DE25_COMPUTER_BUILD_ID") == 1 and
            "FES_DE25_COMPUTER_BUILD_ID=128'h" + fingerprint in qsf, "BUILD_ID assignment")
    leafdir = project / "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_computer"
    leaf = leafdir / "fes_de25_computer_10/synth/fes_de25_computer.v"
    leaf_text = leaf.read_text()
    require("parameter [127:0] BUILD_ID = `FES_DE25_COMPUTER_BUILD_ID" in leaf_text and
            ".build_id(BUILD_ID)" in leaf_text, "leaf BUILD_ID propagation")
    wrapper = leafdir / "synth/peripheral_subsys_fes_computer.v"
    require("fes_de25_computer fes_computer (" in wrapper.read_text(), "no generated BUILD_ID override")
    report = project / "output_files/golden_top.syn.ae.rpt"
    report_text = report.read_text()
    require(re.search(r"Refer\s*;\s*7\s*;\s*QSF\s*;\s*128'h" + fingerprint, report_text) is not None,
            "synthesis report QSF macro reference")
    prefix = re.findall(r";\s*BUILD_ID\s*;\s*([01]+)\s*<truncated>", report_text)
    require(len(prefix) == 1 and len(prefix[0]) >= 64 and
            f"{int(fingerprint, 16):0128b}".startswith(prefix[0]), "elaborated BUILD_ID prefix")

    handoff = project / "qsys_top/qsys_top.sopcinfo"
    tree = ET.parse(handoff)
    connections = {(n.get("kind"), n.get("start"), n.get("end")): n for n in tree.findall("connection")}
    clocks = []
    for start, end, rate in (("clk_100.out_clk", "subsys_periph_periph_clk.in_clk", 100000000),
            ("subsys_periph_periph_clk.out_clk", "subsys_periph_fes_computer.clk", 100000000),
            ("subsys_periph_fes_computer_pixel_clk.out_clk", "subsys_periph_fes_computer.pixel_clk", 74250000)):
        node = connections[("clock", start, end)]
        require(node.findtext("parameter[@name='clockRateSysInfo']/value") == str(rate), "handoff clock rate")
        clocks.append({"start": start, "end": end, "hz": rate})
    require(("reset", "subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_computer.reset") in
            connections, "handoff computer reset")
    avs = connections[("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_computer.avs")]
    require(int(avs.findtext("parameter[@name='baseAddress']/value"), 0) == 0x10300, "Avalon base")
    periph = project / "peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v"
    periph_text = periph.read_text()
    for pattern in (r"fes_computer\s*\(\s*\.clk\s*\(periph_clk_out_clk_clk\)",
                    r"\.pixel_clk\s*\(fes_computer_pixel_clk_out_clk_clk\)",
                    r"\.reset_n\s*\(~rst_controller_reset_out_reset\)",
                    r'\.OUTPUT_RESET_SYNC_EDGES\s*\("deassert"\)', r"\.SYNC_DEPTH\s*\(2\)"):
        require(re.search(pattern, periph_text) is not None, "generated peripheral clock/reset HDL")
    require(".CLOCK_NUMERATOR(13), .CLOCK_DENOMINATOR(200)" in leaf_text and
            leaf_text.count(".clk_sys(clk)") == 2, "machine/renderer write-domain clock")
    require(re.search(r"system_clk_50\s*=\s*CLOCK2_50", top) is not None and
            re.search(r"\.clk_100_clk\s*\(system_clk_100_internal\)", top) is not None,
            "retained system-clock top route")

    machine = leaf.parent / "zx81_machine.sv"
    require("ZX81_MODEL_" not in qsf and "FES_ZX81_ROM_LINK" not in qsf,
            "default ZX81 ROM model")
    require(".address_a({1'b0, rom_a[12], rom_a[11:0]})" in machine.read_text(),
            "default ROM address bit13 tied low")
    rom = mif(project / "zx8x.mif")
    hex_bytes = bytes.fromhex((project / "zx8x.hex").read_text())
    require(len(rom) == 12288 and len(hex_bytes) == 16384 and hex_bytes[:len(rom)] == rom,
            "MIF/HEX defined-byte identity")
    require(mif(project / "fes_mailbox/zx8x.mif") == rom and mif(leaf.parent / "zx8x.mif") == rom,
            "staged/generated ROM identity")
    require(sha(rom[:8192]) == "14ad84f4243efcd41587ff46ab932d11087043e8d455a1ed2a227b9657828dfa",
            "selected reachable low-8KiB ROM identity")
    evidence = [directory / n for n in ("inputs.json", "vendor-inputs.json", "generation.json",
                "hps-emif-parameter-review.json")]
    evidence += [project / "golden_top.v", project / "golden_top.qsf", handoff, periph,
                 project / "qsys_top/synth/qsys_top.v", leaf, wrapper, machine, report,
                 project / "zx8x.mif", project / "zx8x.hex", leaf.parent / "zx8x.mif"]
    return {"format": 1, "producer_revision": revision, "build_directory": str(directory),
        "evidence_class": "source and generated-HDL diagnostic; no hardware acceptance",
        "build_id": fingerprint, "build_id_elaborated_prefix_bits": len(prefix[0]),
        "selected_git_blob_count": len(selected), "staged_file_count": len(inputs["computer"]["source_sha256"]),
        "unchanged_hps_emif_top_connections": len(original_connections),
        "unchanged_parameter_counts": parameter_counts, "retained_boot_software_sha256": boot,
        "clock_routes": clocks, "reset": "100MHz async assertion, deassert synchronization depth2",
        "machine_ula_ratio": [13, 200], "pll_reviews": {"system": pll(project / "ip/pll", 100000000),
            "pixel": pll(project / "sys_pll", 74250000)},
        "rom": {"mif_defined_bytes": len(rom), "hex_bytes": len(hex_bytes),
            "default_address_max": 8191, "reachable_bytes": 8192,
            "reachable_sha256": sha(rom[:8192]), "defined_mif_matches_hex": True,
            "depth_warning": "design16384/MIF12288; undefined tail outside default13-bit ROM address"},
        "evidence_sha256": {str(n.relative_to(directory)): sha(n.read_bytes()) for n in evidence}}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build-dir", type=Path, required=True, help="producer hps directory")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    result = json.dumps(audit(args.build_dir), indent=2, sort_keys=True) + "\n"
    if args.output:
        args.output.write_text(result)
    else:
        print(result, end="")


if __name__ == "__main__":
    main()
