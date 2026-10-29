#!/usr/bin/env python3
"""Exercise the board-local complete-frame owner with independent digital clocks."""
from pathlib import Path
import argparse
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent


def run(output=None):
    if output is None:
        output = Path(tempfile.mkdtemp(prefix="fes-de25-video-"))
    else:
        output = Path(output).resolve()
        output.mkdir(parents=True, exist_ok=False)
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "2", "-O3",
                    "--public-flat-rw", "--x-initial", "unique", "--top-module", "fes_de25_zx81_video",
                    "-Wno-WIDTHEXPAND", "-Wno-WIDTHTRUNC", "--Mdir", str(output),
                    str(HERE / "video.v"), str(HERE / "video_tb.cpp")], check=True)
    subprocess.run([str(output / "Vfes_de25_zx81_video"), "+verilator+rand+reset+2", "+verilator+seed+37"], check=True)
    print(f"DE25 complete-frame video simulation: {output}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path)
    run(parser.parse_args().output)
