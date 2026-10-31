#!/usr/bin/env python3
"""Compose the locked DE25 GHRD and local idle/menu-pattern display; never program."""
from __future__ import annotations
import argparse
import ast
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import tomllib
import zipfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[4]
BOARD = "sources/misteross/boards/de25nano/"
HPS = BOARD + "hps/"
DISPLAY = BOARD + "display/"
COMBINED = BOARD + "combined/"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def identity():
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    dirty = subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=normal"], cwd=ROOT, text=True)
    return {"base_revision" if dirty else "fes_revision": revision,
            "source_classification": "working-copy-diagnostic" if dirty else "committed"}


def capture(revision=None):
    def read(path):
        return (ROOT / path).read_bytes() if revision is None else subprocess.check_output(
            ["git", "show", revision + ":" + path], cwd=ROOT)
    # The captured HPS producer declares its own closure. Do not use an earlier
    # live import to decide which revision's functional inputs are selected.
    hps_builder = read(HPS + "build.py")
    declarations = [node.value for node in ast.parse(hps_builder).body
                    if isinstance(node, ast.Assign) and any(isinstance(t, ast.Name) and t.id == "SOURCES" for t in node.targets)]
    if len(declarations) != 1:
        raise ValueError("HPS builder source closure declaration changed")
    names = ast.literal_eval(declarations[0])
    if not isinstance(names, tuple) or any(not isinstance(name, str) or Path(name).is_absolute() or ".." in Path(name).parts for name in names):
        raise ValueError("unsafe HPS source closure")
    paths = {HPS + name for name in names} | {BOARD + "build.py", COMBINED + "build.py"}
    paths |= {DISPLAY + name for name in ("build.py", "vendor.lock.toml", "top.v", "clocks.sdc")}
    paths |= {"sources/misteross/cores/fes-splash/rtl/fes_splash_core.v"}
    paths |= {"sources/misteross/cores/fes-menu/rtl/" + name + ".v"
              for name in ("fes_menu_reader", "fes_menu_video", "fes_menu_pattern_memory")}
    return {path: hps_builder if path == HPS + "build.py" else read(path) for path in sorted(paths)}


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def derive_video(source):
    changes = (
        (b"module de25nano_display #(", b"module fes_de25_combined_video #("),
        (b"    input wire CLOCK0_50,", b"    input wire CLOCK0_50,\n    input wire ninit_done,"),
        (b"    wire ninit_done, pixel_clk,", b"    wire pixel_clk,"),
        (b"    agilex_reset_release configuration_release (.ninit_done(ninit_done));\n", b""),
        (b"// FPGA-only DE25 display diagnostics. No HPS, DDR, GP or runtime menu ABI.",
         b"// Local display child of the GHRD; no DDR scanout, GP or runtime menu ABI."))
    original = source
    for before, after in changes:
        if source.count(before) != 1:
            raise ValueError("standalone display source patch target changed: " + before.decode())
        source = source.replace(before, after)
    return source, {"original_sha256": sha(original), "derived_sha256": sha(source),
                    "patches": [{"before": a.decode(), "after": b.decode()} for a, b in changes]}


def attach_video(source, mode):
    if mode not in ("idle", "menu-pattern"):
        raise ValueError("unsupported combined display mode")
    anchor = ".ninit_done_ninit_done                     (ninit_done),"
    if source.count(anchor) != 1 or "h2f_warm_reset_handshake" in source:
        raise ValueError("GHRD configuration/warm-reset top connection changed")
    source = source.replace(anchor, anchor + "\n"
        ".h2f_warm_reset_handshake_reset_req         (),\n"
        "// Active LOW: never acknowledge an unproven global soft-logic reset.\n"
        ".h2f_warm_reset_handshake_reset_ack         (1'b1),")
    if source.count("endmodule") != 1:
        raise ValueError("GHRD top module boundary changed")
    instance = """
// Same physical HDMI endpoints as the pinned SCS board; no HPS/DDR scanout.
fes_de25_combined_video #(.MENU_PATTERN(1'b@MODE@)) fes_video_inst (
  .CLOCK0_50(CLOCK0_50), .ninit_done(ninit_done), .HDMI_TX_INT(HDMI_TX_INT),
  .HDMI_TX_CLK(HDMI_TX_CLK), .HDMI_TX_DE(HDMI_TX_DE),
  .HDMI_TX_HS(HDMI_TX_HS), .HDMI_TX_VS(HDMI_TX_VS), .HDMI_TX_D(HDMI_TX_D),
  .HDMI_I2C_SCL(HDMI_I2C_SCL), .HDMI_I2C_SDA(HDMI_I2C_SDA),
  .HDMI_MCLK(HDMI_MCLK), .HDMI_SCLK(HDMI_SCLK),
  .HDMI_LRCLK(HDMI_LRCLK), .HDMI_I2S(HDMI_I2S)
);
""".replace("@MODE@", str(int(mode == "menu-pattern")))
    return source.replace("endmodule", instance + "\nendmodule")


def derive_constraints(source):
    changes = (("{hdmi_config|mI2C_CTRL_CLK}", "{fes_video_inst|hdmi_config|mI2C_CTRL_CLK}"),
               ("{config_reset|sync_regs*|aclr pixel_reset|sync_regs*|aclr}",
                "{fes_video_inst|config_reset|sync_regs*|aclr fes_video_inst|pixel_reset|sync_regs*|aclr}"),
               ("{config_reset|sync_regs*|clrn pixel_reset|sync_regs*|clrn}",
                "{fes_video_inst|config_reset|sync_regs*|clrn fes_video_inst|pixel_reset|sync_regs*|clrn}"))
    for before, after in changes:
        if source.count(before) != 1:
            raise ValueError("display constraint target changed: " + before)
        source = source.replace(before, after)
    return source


def verify_i2c_clock(source):
    expected = (r"parameter\s+CLK_Freq\s*=\s*50000000\s*;", r"parameter\s+I2C_Freq\s*=\s*20000\s*;",
                r"if\(\s*mI2C_CLK_DIV\s*<\s*\(CLK_Freq/I2C_Freq\)\s*\)",
                r"mI2C_CLK_DIV\s*<=\s*0\s*;\s*mI2C_CTRL_CLK\s*<=\s*~mI2C_CTRL_CLK")
    if any(not re.search(pattern, source.decode()) for pattern in expected):
        raise ValueError("vendor I2C clock divider contract changed")


def prepare(archive, mode, output=None, revision=None):
    sources = capture(revision)
    selected = {"fes_revision": revision, "source_classification": "committed"} if revision else identity()
    if revision is None:
        selected = {"base_revision": selected.get("fes_revision", selected.get("base_revision")),
                    "source_classification": "working-copy-diagnostic"}
    archive = Path(archive)
    if not archive.is_file() or archive.is_symlink():
        raise ValueError("vendor ZIP must be a regular file")
    archive_bytes = archive.read_bytes()
    locks = [tomllib.loads(sources[p + "vendor.lock.toml"].decode()) for p in (HPS, DISPLAY)]
    if any(sha(archive_bytes) != data["archive"]["sha256"] for data in locks):
        raise ValueError("vendor ZIP differs from a selected HPS/display lock")
    if output is None:
        directory = ROOT / "sources/misteross/build/diagnostic/de25nano-combined"
        directory.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix=mode + "-", dir=directory))
    else:
        output = Path(output).resolve()
        output.mkdir(parents=True, exist_ok=False)
    snapshot = output / "functional-source"
    for path, data in sources.items():
        target = snapshot / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
    hps = load(snapshot / HPS / "build.py", "de25_combined_hps")
    display = load(snapshot / DISPLAY / "build.py", "de25_combined_display")
    # The retained snapshot is not a Git checkout. This callback supplies only
    # metadata; all preparation bytes come from its captured, retained files.
    hps.source_identity = lambda: selected.copy()
    retained_archive = output / "vendor.zip"
    retained_archive.write_bytes(archive_bytes)
    hps_output = hps.prepare_project(retained_archive, output / "hps")
    project = hps_output / "project"
    vendor_inputs = json.loads((hps_output / "vendor-inputs.json").read_text())
    with zipfile.ZipFile(io.BytesIO(archive_bytes)) as package:
        # Bind every extracted GHRD input back to the exact captured ZIP buffer,
        # in addition to the existing producer's normal archive validation.
        prefix = locks[0]["archive"]["prefix"]
        for path, expected in vendor_inputs["source_sha256"].items():
            if sha(package.read(prefix + path)) != expected:
                raise ValueError("GHRD extraction differs from captured archive: " + path)
        prefix = locks[1]["archive"]["prefix"]
        names = ("golden_top.qsf", "sys_pll.ip", "HDMI_I2C/I2C_HDMI_Config.v", "HDMI_I2C/I2C_Controller.v")
        if any(package.namelist().count(prefix + name) != 1 for name in names):
            raise ValueError("missing/duplicate vendor display input")
        vendor = {name: package.read(prefix + name) for name in names}
    if display.pin_assignments((project / "golden_top.qsf").read_text()) != display.pin_assignments(vendor["golden_top.qsf"].decode()):
        raise ValueError("GHRD and selected display pin/IO assignments differ")
    verify_i2c_clock(vendor["HDMI_I2C/I2C_HDMI_Config.v"])
    pll, pll_repair = display.retarget_pll(vendor["sys_pll.ip"])
    video, video_repair = derive_video(sources[DISPLAY + "top.v"])
    local = project / "fes_combined"
    local.mkdir()
    files = {"video.v": video, "sys_pll.ip": pll,
             "clocks.sdc": derive_constraints(sources[DISPLAY + "clocks.sdc"].decode()).encode(),
             **{Path(name).name: vendor[name] for name in names[2:]}}
    rtl = ["video.v", "I2C_HDMI_Config.v", "I2C_Controller.v"]
    menu_repair = None
    core_names = ("fes_splash_core",) if mode == "idle" else ("fes_menu_reader", "fes_menu_video", "fes_menu_pattern_memory")
    for name in core_names:
        kind = "fes-splash" if mode == "idle" else "fes-menu"
        data = sources[f"sources/misteross/cores/{kind}/rtl/{name}.v"]
        target = name + ".v"
        if name == "fes_menu_reader":
            data, menu_repair = display.retarget_menu_ram(data)
            target = "fes_menu_reader_de25.v"
        files[target] = data
        rtl.append(target)
    for name, data in files.items():
        (local / name).write_bytes(data)
    top = project / "golden_top.v"
    original_top = top.read_bytes()
    top.write_text(attach_video(original_top.decode(), mode))
    with (project / "golden_top.qsf").open("a") as qsf:
        qsf.write("\n# FES local video; vendor HPS/EMIF/debug configuration retained.\n"
                  "set_global_assignment -name IP_FILE fes_combined/sys_pll.ip\n"
                  "set_global_assignment -name SDC_FILE fes_combined/clocks.sdc\n")
        for name in rtl:
            qsf.write("set_global_assignment -name VERILOG_FILE fes_combined/" + name + "\n")
    inputs = json.loads((hps_output / "inputs.json").read_text())
    inputs.pop("base_revision", None)
    inputs.update(selected)
    inputs["combined_display"] = {"mode": mode, "display_source_sha256": {name: sha(data) for name, data in files.items()},
        "vendor_source_sha256": {name: sha(data) for name, data in vendor.items()},
        "reviewed_video_derivation": video_repair, "reviewed_pll_device_repair": pll_repair,
        "reviewed_menu_ram_hint_repair": menu_repair,
        "pixel_pll_parameters": display.pll_parameters(pll), "pixel_clock_hz": 74250000,
        "i2c_internal_clock": {"source_hz": 50000000, "divide_by": 5002,
                               "derivation": "2 * (CLK_Freq / I2C_Freq + 1)"},
        "configuration_release": "existing GHRD ninit_done; no second Reset Release IP",
        "prepared_top_before_sha256": sha(original_top), "prepared_top_after_sha256": sha(top.read_bytes()),
        "warm_reset_ack_n": "explicit constant1 (inactive); global quiesce/ownership barrier unsupported",
        "debug_paths": "all existing JTAG/OCM/F2H/F2SDRAM paths preserved",
        "ddr_scanout": False, "runtime_menu": False}
    inputs["functional_snapshot_sha256"] = {path: sha(data) for path, data in sources.items()}
    inputs["prepared_combined_qsf_sha256"] = sha((project / "golden_top.qsf").read_bytes())
    (hps_output / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    (output / "snapshot.json").write_text(json.dumps({**selected, "source_sha256": inputs["functional_snapshot_sha256"],
        "vendor_archive_sha256": sha(archive_bytes), "hps_output": "hps"}, indent=2, sort_keys=True) + "\n")
    return output, hps, display


def generate(output, hps, display, tool, version):
    directory = output / "hps"
    hps.generate_project(directory, tool, version)
    expected = json.loads((directory / "inputs.json").read_text())["combined_display"]["pixel_pll_parameters"]
    actual = display.pll_parameters((directory / "project/fes_combined/sys_pll.ip").read_bytes())
    if actual != expected:
        raise ValueError("generation changed selected pixel PLL parameters")
    generation = json.loads((directory / "generation.json").read_text())
    generation["combined_display"] = {"pixel_pll_parameters_unchanged": True,
        "warm_reset_ack_n": "constant1; unsupported", "ddr_scanout": False}
    (directory / "generation.json").write_text(json.dumps(generation, indent=2, sort_keys=True) + "\n")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "generate", "build"))
    parser.add_argument("--mode", choices=("idle", "menu-pattern"), required=True)
    parser.add_argument("--vendor-archive", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    try:
        selected = identity()
        revision = selected.get("fes_revision") if args.action != "project" else None
        if args.action != "project" and not revision:
            raise ValueError("combined generation/build requires clean committed source")
        output, hps, display = prepare(args.vendor_archive, args.mode, args.output, revision)
        print("Combined diagnostic: " + str(output), flush=True)
        if args.action != "project":
            if identity() != selected:
                raise ValueError("source identity changed before IP generation")
            tool, version = hps.BOARD_BUILD.locate_quartus(args.quartus_root)
            generate(output, hps, display, tool, version)
            if args.action == "build":
                artifact = hps.compile_project(output / "hps", tool)
                if display.pll_parameters((output / "hps/project/fes_combined/sys_pll.ip").read_bytes()) != json.loads(
                        (output / "hps/inputs.json").read_text())["combined_display"]["pixel_pll_parameters"]:
                    raise ValueError("compile changed selected pixel PLL parameters")
                print("Diagnostic SOF: " + str(artifact), flush=True)
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.CalledProcessError) as error:
        parser.exit(1, "de25-combined: " + str(error) + "\n")


if __name__ == "__main__":
    main()
