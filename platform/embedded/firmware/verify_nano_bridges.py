#!/usr/bin/env python3
"""Exercise the unbound Nano bridge service with pinned vendor C and fake registers."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import uuid

import prepare_tfa
from verify import function

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
HEADERS = {
    "socfpga_reset_manager.h": "4b0b42939ad59536cafbdeb4bdfeff16beb6ae6daf0093d0b1a84b6aa3773733",
    "socfpga_system_manager.h": "aea7680828b3368a093aad72d093dc98e4d40d1653dbc12aa55067f799652c3f",
}


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def build(reference, output, run):
    """Build host-only injection objects; never mutate the reference checkout."""
    raw = reference.read_bytes()
    patched = prepare_tfa.nano_bridge_driver_source(raw).decode()
    prefix = (HERE / "tests/terasic_reset_test.h").read_text().split("static unsigned fail_mask, poll_count;", 1)[0]
    headers = {}
    for name, expected in HEADERS.items():
        path = reference.parent.parent / "include" / name
        if sha(path) != expected:
            raise ValueError("Nano driver register header is not the pinned vendor source: " + name)
        headers[name] = sha(path)
        constants = dict(re.findall(r"(?m)^#define\s+((?:RSTMGR|SYSMGR)_\w+)\s+(0x[0-9a-fA-F]+)\b", path.read_text()))
        for macro in re.findall(r"(?m)^#define ((?:RSTMGR|SYSMGR)_\w+) ", prefix):
            if macro in constants:
                prefix += f"\n#undef {macro}\n#define {macro} {constants[macro]}U\n"
    prefix += "\n#undef FES_REG_HDSKEN\n#define FES_REG_HDSKEN 0x10U\n#undef FES_REG_HDSKACK\n#define FES_REG_HDSKACK 0x18U\n"
    prefix += '\n#define FES_NANO_TFA_DIAG\n#include "nano_bridge_diag.h"\n'
    output.mkdir()
    body = patched[:patched.index("*/") + 2] + "\n" + prefix
    body += (HERE / "tests/nano_bridge_fixture.inc").read_text() + "\n"
    body += prepare_tfa.DIAG_HELPERS.read_text() + "\n"
    for direction in ("enable", "disable"):
        name = "socfpga_bridges_" + direction
        body += function(patched, name).replace("int " + name, "static int fes_nano_raw_" + direction, 1)
    source = output / "driver.c"
    source.write_text(body)
    flags = ["-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I" + str(HERE), "-I" + str(HERE / "tests")]
    run("nano-driver-compile", ["cc", *flags, "-c", source, "-o", output / "driver.o"])
    run("nano-service-compile", ["cc", *flags, "-c", HERE / "nano_bridges.c", "-o", output / "service.o"])
    run("nano-driver-link", ["cc", *flags, output / "driver.o", output / "service.o", HERE / "tests/nano_bridge_driver_test.c", "-o", output / "driver-test"])
    run("nano-driver-cases", [output / "driver-test"])
    return {"source_sha256": hashlib.sha256(raw).hexdigest(), "pinned_headers_sha256": headers,
            "driver_transformation_sha256": hashlib.sha256(patched.encode()).hexdigest(),
            "generated_driver_sha256": sha(source), "driver_object_sha256": sha(output / "driver.o"),
            "service_object_sha256": sha(output / "service.o"), "cases": 18}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--reference", type=Path, required=True, help="exact pinned Terasic reset-manager C")
    parser.add_argument("--output", type=Path, default=ROOT / "out" / ("nano-bridge-service-" + uuid.uuid4().hex))
    args = parser.parse_args()
    output = args.output.absolute()
    if ".." in output.parts or any(p.is_symlink() for p in (output, *output.parents)) or not output.is_relative_to(ROOT / "out") or output.exists():
        parser.error("output must be a fresh private directory beneath out/")
    output.mkdir()
    files = [HERE / name for name in ("nano_bridges.c", "nano_bridges.h", "prepare_tfa.py", "verify.py", "verify_nano_bridges.py", "tests/terasic_reset_test.h", "nano_bridge_diag.inc", "nano_bridge_diag.h", "tests/nano_bridge_fixture.h", "tests/nano_bridge_fixture.inc", "tests/nano_bridge_driver_test.c")]
    sources = {str(p.relative_to(ROOT)): sha(p) for p in files}
    commands = []
    def run(label, command):
        command = [str(a) for a in command]
        commands.append({"label": label, "argv": command})
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
        (output / (label + ".log")).write_bytes(result.stdout)
        if result.returncode:
            raise RuntimeError(f"{label} failed; see {output / (label + '.log')}")
    result = build(args.reference.absolute(), output / "driver", run)
    if sources != {str(p.relative_to(ROOT)): sha(p) for p in files}:
        raise RuntimeError("source changed during verification")
    result.update(status="passed", classification="unbound C firmware service with actual pinned bridge function bodies and injected registers/polls; no physical drain proof", sources=sources, commands=commands)
    (output / "result.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print("PASS:", output)


if __name__ == "__main__":
    main()
