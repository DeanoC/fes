#!/usr/bin/env python3
"""Add the MAC register observation at the pre-storage CBL console checkpoint."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import sys

HERE = Path(__file__).resolve().parent
TRACE_COMMIT = "d3ec4a3928eac87477b67d7c92d6e63c4a0d1bc7"


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--trace-checkout", type=Path, required=True,
                        help="private checkout of the qualified pre-storage trace revision")
    args, build_args = parser.parse_known_args()
    root = args.trace_checkout.resolve(strict=True)
    if subprocess.check_output(["git", "-C", root, "rev-parse", "HEAD"], text=True).strip() != TRACE_COMMIT:
        raise ValueError("wrong qualified trace revision")
    if subprocess.check_output(["git", "-C", root, "status", "--porcelain", "--untracked-files=no"]):
        raise ValueError("trace producer differs from its checked revision")
    if "--console-control" in build_args:
        raise ValueError("MAC observation requires the complete qualified initial trace")
    archived = root / "platform/embedded/nano/cpu_diagnostic.py"
    sys.path.insert(0, str(archived.parent))
    spec = importlib.util.spec_from_file_location("qualified_cpu_spl", archived)
    trace = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(trace)
    base = trace.contain_source

    def contain_source(source, console_control):
        hashes = base(source, console_control)
        directory = source / "arch/arm/mach-socfpga"
        report = directory / "fes_nano_spl.c"
        anchor = "__attribute__((__noreturn__,__noinline__)) static void fes_nano_cpu_diag_park(void)\n{\n"
        text = report.read_text()
        if text.count(anchor) != 1:
            raise ValueError("qualified pre-storage park checkpoint differs")
        for name, dest in (("snapshot.h", "mac_spl_snapshot.h"), ("observe.inc", "mac_spl_observe.inc")):
            f = directory / dest
            shutil.copyfile(HERE / name, f)
            hashes[str(f.relative_to(source))] = sha(f)
        report.write_text(text.replace(anchor, '#include "mac_spl_observe.inc"\n' + anchor + '    fes_nano_mac_observe();\n'))
        hashes[str(report.relative_to(source))] = sha(report)
        return hashes

    pfg = Path(build_args[build_args.index("--quartus-pfg") + 1]).resolve(strict=True)
    trace.contain_source = contain_source
    sys.argv = [str(archived), *build_args]
    trace.main()
    output = Path(build_args[build_args.index("--output") + 1]).resolve(strict=True)
    receipt = json.loads((output / "result.json").read_bytes())
    if receipt.get("status") != "prepared" or not receipt.get("initial_proof_attempted"):
        raise ValueError("qualified SPL producer did not complete")
    previous = next(Path(path) for path, value in receipt["inputs"].items()
                    if value == trace.REFERENCE_SOF)
    sys.path.insert(0, str(HERE.parent))
    from sof_hps import inspect
    embedded = inspect((output / "cpu-state.sof").read_bytes(),
                       (output / "build/spl/u-boot-spl-dtb.hex").read_bytes(), previous.read_bytes())
    result = dict(scope="de25-cbl-mac-ocramspl-v1", status="prepared", historical_trace=TRACE_COMMIT,
                  quartus_pfg_sha256=sha(pfg),
                  diagnostic_id=receipt["diagnostic_id"], sof_sha256=receipt["sof_sha256"],
                  spl_sha256=receipt["spl_sha256"], trace_build_receipt_sha256=sha(output / "result.json"),
                  embedded_spl=embedded, sources={p.name: sha(p) for p in (Path(__file__), HERE/"observe.inc",
                  HERE/"snapshot.h", HERE/"verify.py", HERE.parent/"sof_hps.py")})
    (output / "mac-observation.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")


if __name__ == "__main__":
    main()
