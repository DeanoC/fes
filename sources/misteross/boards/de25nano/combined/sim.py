#!/usr/bin/env python3
"""Exercise the exact derived video child with real generic RTL and board-IP models."""
from pathlib import Path
import subprocess
from build import capture, derive_video, load, DISPLAY, HPS, ROOT

HERE = Path(__file__).resolve().parent
display = load(HERE.parent / "display/build.py", "combined_sim_display")
sources = capture()
video, _ = derive_video(sources[DISPLAY + "top.v"])
bench = (HERE.parent / "display/display_tb.cpp").read_text().replace("de25nano_display", "fes_de25_combined_video")
for state in ("0", "1"):
    before = "t.rootp->fes_de25_combined_video__DOT__configuration_release__DOT__ninit_done = " + state + ";"
    if bench.count(before) != 1:
        raise ValueError("display bench configuration boundary changed")
    bench = bench.replace(before, "t.ninit_done = " + state + ";")
if bench.count("    t.HDMI_TX_INT = 1;") != 1:
    raise ValueError("display bench initialization changed")
bench = bench.replace("    t.HDMI_TX_INT = 1;", "    t.HDMI_TX_INT = 1;\n    t.ninit_done = 1;")
bench = bench.replace("PASS DE25 %s:", "PASS combined DE25 %s:")
for mode in ("idle", "menu-pattern"):
    output = ROOT / "sources/misteross/build/sim" / ("de25-combined-" + mode)
    output.mkdir(parents=True, exist_ok=True)
    (output / "video.v").write_bytes(video)
    (output / "video_tb.cpp").write_text(bench)
    (output / "fes_de25_top_reset.v").write_bytes(sources[HPS + "fes_de25_top_reset.v"])
    (output / "sim_models.v").write_bytes((HERE.parent / "display/sim_models.v").read_bytes())
    rtl = [output / name for name in ("video.v", "sim_models.v", "fes_de25_top_reset.v")]
    names = ("fes_splash_core",) if mode == "idle" else ("fes_menu_reader", "fes_menu_video", "fes_menu_pattern_memory")
    for name in names:
        kind = "fes-splash" if mode == "idle" else "fes-menu"
        data = sources[f"sources/misteross/cores/{kind}/rtl/{name}.v"]
        target = name + ".v"
        if name == "fes_menu_reader":
            data, _ = display.retarget_menu_ram(data)
            target = "fes_menu_reader_de25.v"
        (output / target).write_bytes(data)
        rtl.append(output / target)
    subprocess.run(["verilator", "--cc", "--exe", "--build", "--public-flat-rw", "-O3",
        "--top-module", "fes_de25_combined_video", "-GMENU_PATTERN=1'b" + str(int(mode == "menu-pattern")),
        "-CFLAGS", "-O2 -DMENU_PATTERN=" + str(int(mode == "menu-pattern")),
        "--Mdir", str(output), *map(str, rtl), str(output / "video_tb.cpp")], check=True)
    subprocess.run([str(output / "Vfes_de25_combined_video")], check=True)
