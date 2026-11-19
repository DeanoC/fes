#!/usr/bin/env python3
"""Drive a DE25 standard machine's RTL with the kit runtime's own driver.

The Spectrum and Atari ST standard benches issue fes.computer requests from
the bench. This proof replaces that hand-written host with libmister-runtime:
the real FesGp DE25 transport and FesGpCoreDriver, linked into the Verilated
model, read and write the machine's computer slave. It shows that the package
descriptor the runtime admits identifies this RTL, that neither side releases
a machine without firmware, and that the driver's upload makes the RTL ready
before the one release. No hardware is involved.
"""
from pathlib import Path
import argparse
import importlib.util
import json
import shutil
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
BOARD = HERE.parent
ROOT = BOARD.parents[1]
RUNTIME = ROOT.parent / "libmister-runtime"
RUNTIME_SOURCES = ("src/native/fes_gp.cpp", "src/native/artifacts.cpp", "src/native/diagnostic.cpp")
MACHINES = {
    "spectrum-48": ("spectrum", "48", "mister_test::De25SpectrumDescriptor(bytes)"),
    "spectrum-128": ("spectrum", "128", "mister_test::De25SpectrumDescriptor(bytes)"),
    "atari-st": ("atari-st", None, "mister_test::De25AtariStDescriptor()"),
}


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run(name, keep):
    directory, model, descriptor = MACHINES[name]
    bench = BOARD / directory
    sim = load(bench / "sim.py", "de25_driver_proof_" + directory.replace("-", "_"))
    spectrum = directory == "spectrum"
    if spectrum:
        diagnostic = ROOT / "build/diagnostics/fes-spectrum"
        subprocess.run([sys.executable, str(ROOT / "cores/fes-spectrum/diagnostic/firmware.py"),
                        "--output-dir", str(diagnostic)], cwd=ROOT, check=True)
        firmware = diagnostic / ("firmware.rom" if model == "48" else "firmware-32k.rom")
        top, waivers, defines = "fes_de25_spectrum_standard", sim.WAIVERS, sim.MODELS[model]
        parameters = ["-GSDRAM_POWERUP_CYCLES=16"]
        cflags = f"-DSPECTRUM_BANKS={3 if model == '48' else 8}"
    else:
        subprocess.run([sys.executable, str(ROOT / "cores/fes-atari-st/sim/check_sources.py")], cwd=ROOT, check=True)
        diagnostic = ROOT / "build/diagnostics/fes-atari-st"
        subprocess.run([sys.executable, str(ROOT / "cores/fes-atari-st/diagnostic/firmware.py"),
                        "--out", str(diagnostic)], cwd=ROOT, check=True)
        firmware = diagnostic / "firmware.rom"
        top, waivers, defines, parameters, cflags = "fes_de25_atari_st_standard", sim.WAIVERS, [], [], ""
    parent = ROOT / "build/sim/de25-driver-proof"
    parent.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix=name + "-", dir=parent))
    print(f"DE25 {name} driver proof directory: {output}", flush=True)
    if not spectrum:
        for rom in ("microrom.mem", "nanorom.mem"):
            shutil.copy(sim.CPU / rom, output / rom)
    # The machine's own bench supplies Board. Its main is renamed so that the
    # proof's main is the program; the bench source itself is not edited.
    main = output / "driver_proof_main.cpp"
    main.write_text(
        "#define main fes_de25_standard_bench_main\n"
        f'#include "{bench / "standard_tb.cpp"}"\n'
        "#undef main\n"
        f"#define FES_PROOF_DESCRIPTOR(bytes) {descriptor}\n"
        f'#define FES_PROOF_MACHINE "{name}"\n'
        f'#include "{HERE / "driver_proof_tb.cpp"}"\n')
    include = " ".join("-I" + str(path) for path in
                       (RUNTIME / "src", RUNTIME / "include", RUNTIME / "tests/support"))
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "4", "--public-flat-rw", "-O3",
                    *(["-Wall"] if spectrum else []), *["-Wno-" + waiver for waiver in waivers], *defines,
                    "--top-module", top, "--Mdir", str(output), *parameters,
                    "-GBUILD_ID=128'h00112233445566778899aabbccddeeff",
                    *["-I" + str(path) for path in sim.includes()],
                    "-CFLAGS", f"-O2 -std=c++17 -pthread {cflags} {include}", "-LDFLAGS", "-pthread",
                    *map(str, sim.sources(output)), str(main),
                    *(str(RUNTIME / source) for source in RUNTIME_SOURCES)], cwd=ROOT, check=True)
    result = subprocess.run([str(output / ("V" + top)), str(firmware)], cwd=output, check=True,
                            capture_output=True, text=True)
    report = json.loads(result.stdout.strip().splitlines()[-1])
    if report["result"] != "passed" or report["data_words"] * 2 != firmware.stat().st_size:
        raise SystemExit(f"{name}: incomplete driver proof {report}")
    if not keep:
        shutil.rmtree(output)
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--machine", action="append", choices=tuple(MACHINES))
    parser.add_argument("--keep", action="store_true", help="keep the Verilator build directory")
    args = parser.parse_args()
    reports = [run(name, args.keep) for name in (args.machine or MACHINES)]
    print(json.dumps({"status": "passed", "driver": "libmister-runtime FesGpCoreDriver",
                      "transport": "de25_computer", "hardware": "not-run", "machines": reports}, indent=2))


if __name__ == "__main__":
    main()
