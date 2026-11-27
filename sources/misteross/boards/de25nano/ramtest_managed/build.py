#!/usr/bin/env python3
"""Integrate a bounded SDRAM test with the managed DE25 machine shell."""
from pathlib import Path
import importlib.util
import sys

HERE = Path(__file__).resolve().parent
BOARD = "sources/misteross/boards/de25nano/"
ROOT = "sources/misteross/"
NAME = "RAM Tester"
DIRECTORY = "ramtest_managed"
CORE_ID = "fes.ramtest"
COMPONENT = "fes_de25_ramtest_standard"
SDC = BOARD + "ramtest/managed/ramtest-clocks.sdc"
MODELS = {"sdram": None}
# Same clock, ports, bridges and reset graph as the Spectrum component.
PARENT_SHA256 = "a08edf0b098e0ddb583a98963efa6bcce1735fc41a0a28c4a9019e2eb27f1ca8"
ASSIGNMENTS = ()
LOCAL = ("fes_de25_ramtest_standard.v", "fes_de25_ramtest_standard_hw.tcl",
         "scan_control.v", "hold_guard.v", "status_video.v", "ramtest-clocks.sdc")
SOURCES = ({BOARD + "ramtest/managed/" + name for name in LOCAL} |
           {BOARD + "ramtest/" + name for name in ("scan.v", "sdram.v")} |
           {BOARD + "ramtest_managed/build.py", BOARD + "spectrum/derive.py",
            ROOT + "cores/fes-ramtest/rtl/ram_font.v", ROOT + "cores/fes-common/rtl/fes_video_720p.v"})


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


board = load(HERE.parent / "machine/board.py", "de25_ramtest_machine_board")


def component_files(sources):
    derive = board.module(sources, BOARD + "spectrum/derive.py", "ramtest_selected_transport")
    local = BOARD + "ramtest/managed/"
    files = {name: sources[local + name] for name in
             ("fes_de25_ramtest_standard.v", "fes_de25_ramtest_standard_hw.tcl")}
    for dest, source in {
        "fes_de25_managed_ram_scan.v": local + "scan_control.v",
        "fes_de25_ramtest_hold_guard.v": local + "hold_guard.v",
        "fes_de25_ramtest_video.v": local + "status_video.v",
        "fes_de25_ram_scan.v": BOARD + "ramtest/scan.v",
        "fes_de25_sdram.v": BOARD + "ramtest/sdram.v",
        "ram_font.v": ROOT + "cores/fes-ramtest/rtl/ram_font.v",
        "fes_video_720p.v": ROOT + "cores/fes-common/rtl/fes_video_720p.v",
    }.items():
        files[dest] = sources[source]
    files["fes_de25_computer_transport.v"], record = derive.transport(
        sources[BOARD + "zx81-hps/fes_de25_computer.v"])
    return files, {"fes_de25_computer_transport.v": record}


def project_files(sources):
    return {}


def record_parameters(root):
    return {"sdram_test_base": 0,
            "sdram_test_bytes": 128 * 1024 * 1024, "untested_banks": "hps-lpddr4,fpga-lpddr4"}


if __name__ == "__main__":
    board.main(sys.modules[__name__])
