#!/usr/bin/env python3
"""Exercise the CRTC frame tracker embedded in profile-hook.patch."""

from pathlib import Path
import re
import shutil
import subprocess
import tempfile


HERE = Path(__file__).resolve().parent
PATCH = HERE / "profile-hook.patch"
HEADER = "sources/misteross/cores/fes-pc386/sim/cpu_profile_io.h"


def added_file(patch_text: str, path: str) -> str:
    lines = patch_text.splitlines()
    marker = f"+++ b/{path}"
    for index, line in enumerate(lines):
        if line.startswith(marker):
            match = re.fullmatch(r"@@ -0,0 \+1,(\d+) @@", lines[index + 1])
            if not match:
                raise AssertionError(f"unexpected new-file hunk for {path}")
            count = int(match.group(1))
            body = lines[index + 2:index + 2 + count]
            if len(body) != count or any(not line.startswith("+") for line in body):
                raise AssertionError(f"malformed new-file body for {path}")
            return "\n".join(line[1:] for line in body) + "\n"
    raise AssertionError(f"{path} is missing from profile-hook.patch")


def main() -> None:
    patch_text = PATCH.read_text()
    assert "if (crtc_frame.observe(write, port, length, wdata)) ++frames;" in patch_text
    assert "if (frame_tracker.observe(write, port, length, value)) ++flips;" in patch_text
    assert "prof->io(io_write, io_port, io_length, io_wdata)" in patch_text

    compiler = shutil.which("c++") or shutil.which("g++") or shutil.which("clang++")
    if not compiler:
        raise SystemExit("need a C++ compiler (c++, g++ or clang++)")

    with tempfile.TemporaryDirectory(prefix="profile-hook-io-", dir=HERE) as temp:
        temp_path = Path(temp)
        (temp_path / "cpu_profile_io.h").write_text(added_file(patch_text, HEADER))
        source = temp_path / "test.cpp"
        source.write_text(r"""#include "cpu_profile_io.h"
#include <cassert>

int main() {
    CrtcFrameTracker tracker;
    unsigned frames = 0;
    auto io = [&](bool write, uint16_t port, unsigned length, uint32_t value) {
        if (tracker.observe(write, port, length, value)) ++frames;
    };

    io(true, 0x3d4, 1, 0x0c);  // selector only: must not count a frame
    assert(frames == 0);
    io(true, 0x3d5, 1, 0x80);  // selected register 0Ch data write
    assert(frames == 1);

    io(true, 0x3d4, 1, 0x0d);  // low start-address register is not the marker
    io(true, 0x3d5, 1, 0x20);
    assert(frames == 1);
    io(false, 0x3d4, 1, 0x0c); // reads cannot change the selected register
    io(true, 0x3d5, 1, 0x81);
    assert(frames == 1);

    io(true, 0x3d4, 2, 0x800c); // word OUT: select 0Ch, then write data at 3D5h
    assert(frames == 2);
    io(true, 0x3d5, 1, 0x82);
    assert(frames == 3);
    io(true, 0x3d4, 1, 0x0c);
    io(true, 0x3d5, 2, 0x0083); // its low byte is a write to 3D5h
    assert(frames == 4);
    io(true, 0x3d5, 1, 0x84);
    assert(frames == 5);
}
""")
        binary = temp_path / "test"
        subprocess.run(
            [compiler, "-std=c++17", "-Wall", "-Wextra", "-Werror", "-I", str(temp_path),
             str(source), "-o", str(binary)],
            check=True,
        )
        subprocess.run([str(binary)], check=True)

    print("profile hook CRTC I/O checks passed")


if __name__ == "__main__":
    main()
