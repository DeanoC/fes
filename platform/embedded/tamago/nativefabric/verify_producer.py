#!/usr/bin/env python3
"""Build the DE25 simulation producer and verify its native identity binding."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

HERE = Path(__file__).resolve().parent
TAMAGO = HERE.parent
ROOT = TAMAGO.parents[2]
PRODUCER = ROOT / "sources/misteross/prototypes/embedded_control/producer.py"
spec = importlib.util.spec_from_file_location("de25_identity_producer", PRODUCER)
producer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(producer)


def sha(path): return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def check_tool_search_order(out):
    # A later simulator directory may also contain g++; the selected earlier
    # compiler must still win in the producer's controlled build environment.
    early, late = out / "search-order" / "early", out / "search-order" / "late"
    early.mkdir(parents=True)
    late.mkdir()
    for path in (early / "g++", late / "g++", late / "verilator", late / "verilator_bin", late / "make"):
        path.touch()
        path.chmod(0o755)
    paths = {"g++": early / "g++", "verilator": late / "verilator",
             "verilator_bin": late / "verilator_bin", "make": late / "make"}
    previous = os.environ.get("PATH")
    try:
        os.environ["PATH"] = os.pathsep.join((str(early), str(late)))
        controlled = producer.build_environment(paths)["PATH"]
    finally:
        if previous is None:
            os.environ.pop("PATH", None)
        else:
            os.environ["PATH"] = previous
    if shutil.which("g++", path=controlled) != str(early / "g++"):
        raise RuntimeError("controlled PATH changed the selected compiler")


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--output", type=Path, default=ROOT / "out" / ("de25-producer-check-" + uuid.uuid4().hex))
    p.add_argument("--allow-dirty", action="store_true", help="label uncommitted prototype inputs as a working-tree diagnostic")
    args = p.parse_args()
    out = args.output.absolute()
    if ".." in out.parts or any(x.is_symlink() for x in (out, *out.parents)) or not out.is_relative_to(ROOT / "out") or out.exists():
        p.error("output must be a fresh private directory beneath out/")
    out.mkdir(parents=True)
    check_tool_search_order(out)
    native_env = dict(os.environ)
    for name in ("GOOS", "GOARCH", "GOARM", "GOAMD64", "GOARM64", "GOOSPKG", "GOFLAGS", "GOTOOLCHAIN",
                 "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "GNUMAKEFLAGS", "PYTHON"):
        native_env.pop(name, None)
    files = {HERE / "verify_producer.py"}
    for directory in (HERE, TAMAGO / "embeddedcontrol", TAMAGO / "fabricmodel"):
        files.update(x for x in directory.rglob("*.go") if x.is_file())
    sources = {str(x.relative_to(ROOT)): sha(x) for x in sorted(files)}
    commands = []
    def run(label, argv, *, cwd=ROOT, extra_env=None, expect=0):
        argv = [str(x) for x in argv]
        commands.append({"label": label, "cwd": str(cwd), "argv": argv})
        env = dict(native_env, **(extra_env or {}))
        result = subprocess.run(argv, cwd=cwd, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180)
        (out / (label + ".log")).write_bytes(result.stdout)
        if (result.returncode == 0) != (expect == 0):
            raise RuntimeError(f"{label} returned {result.returncode}; see {out / (label + '.log')}")
        return result.stdout.decode()
    def reject(label, argv, marker):
        if marker not in run(label, argv, expect=1):
            raise RuntimeError(f"{label} failed for an unexpected reason; see {out / (label + '.log')}")
    extra = ["--allow-dirty"] if args.allow_dirty else []
    a, b = out / "first", out / "second"
    run("first-build", [sys.executable, PRODUCER, "build", "--output", a, "--control-abi", "0x10203040", *extra])
    run("second-build", [sys.executable, PRODUCER, "build", "--output", b, "--control-abi", "0x10203041", *extra])
    first, second = (json.loads((d / "result.json").read_text()) for d in (a, b))
    if first["build_id"] == second["build_id"] or first["observed"]["control_abi"] == second["observed"]["control_abi"]:
        raise RuntimeError("changing the control ABI did not change the build ID and live observation")
    first_record, second_record = (json.loads((d / "build-inputs.json").read_text()) for d in (a, b))
    del first_record["parameters"]["control_abi"]
    del second_record["parameters"]["control_abi"]
    if first_record != second_record or first["source_sha256"] != second["source_sha256"] or first["tools"] != second["tools"]:
        raise RuntimeError("non-ABI producer inputs changed between the two builds")
    if not args.allow_dirty and (first["source_state"] != "committed" or second["source_state"] != "committed"):
        raise RuntimeError("verified producer inputs were not committed")
    if any(out.rglob("*.rbf")):
        raise RuntimeError("simulation producer unexpectedly emitted an FPGA bitstream")
    run("first-verify", [sys.executable, PRODUCER, "verify", "--output", a])
    bad_id = ("0" if first["build_id"][0] != "0" else "1") + first["build_id"][1:]
    reject("wrong-live-id", [a / producer.ARTIFACT, bad_id, str(first["observed"]["control_abi"])], "live build identity mismatch")
    reject("wrong-live-abi", [a / producer.ARTIFACT, first["build_id"], str(first["observed"]["control_abi"] + 1)], "control ABI mismatch")

    # Use a private copy for corruption checks; never modify the retained output.
    scratch = out / "corruption"
    scratch.mkdir()
    for name in ("descriptor.json", "build-inputs.json", producer.ARTIFACT):
        shutil.copy2(a / name, scratch / name)
    original_descriptor = (scratch / "descriptor.json").read_bytes()
    descriptor = json.loads(original_descriptor)
    descriptor["build_id"] = bad_id
    (scratch / "descriptor.json").write_bytes(producer.canonical(descriptor))
    reject("wrong-descriptor", [sys.executable, PRODUCER, "verify", "--output", scratch], "descriptor and build-input identity differ")
    (scratch / "descriptor.json").write_bytes(original_descriptor)
    original_record = (scratch / "build-inputs.json").read_bytes()
    (scratch / "build-inputs.json").write_bytes(original_record + b" ")
    reject("wrong-record", [sys.executable, PRODUCER, "verify", "--output", scratch], "build-input record is not canonical")
    (scratch / "build-inputs.json").write_bytes(original_record)
    # Even a self-consistent altered record/descriptor must fail against the
    # identity actually compiled into the retained model.
    changed = json.loads(original_record)
    changed["parameters"]["control_abi"] += 1
    changed_record = producer.encode_build_record(changed)
    changed_descriptor = json.loads(original_descriptor)
    changed_descriptor["control_abi"] += 1
    changed_descriptor["build_id"] = producer.build_identity(changed_record)
    changed_descriptor["build_inputs_sha256"] = hashlib.sha256(changed_record).hexdigest()
    (scratch / "build-inputs.json").write_bytes(changed_record)
    (scratch / "descriptor.json").write_bytes(producer.canonical(changed_descriptor))
    reject("wrong-live-binding", [sys.executable, PRODUCER, "verify", "--output", scratch], "live simulation rejected descriptor identity")
    (scratch / "build-inputs.json").write_bytes(original_record)
    (scratch / "descriptor.json").write_bytes(original_descriptor)
    artifact = scratch / producer.ARTIFACT
    with artifact.open("r+b") as stream:
        stream.seek(-1, os.SEEK_END); value = stream.read(1); stream.seek(-1, os.SEEK_END); stream.write(bytes([value[0] ^ 1]))
    reject("wrong-artifact", [sys.executable, PRODUCER, "verify", "--output", scratch], "simulation artifact differs from descriptor")

    # The format-2 ID must also respond to source inputs, not just ABI options.
    fields = json.loads((a / "build-inputs.json").read_text())
    fields["source_inputs"][producer.RTL] = "f" * 64
    if producer.build_identity(producer.encode_build_record(fields)) == first["build_id"]:
        raise RuntimeError("source digest did not enter the functional build ID")

    root = subprocess.check_output(["go", "env", "GOROOT"], cwd=TAMAGO, env=dict(native_env, GOWORK="off"), text=True).strip()
    go = Path(root) / "bin/go"
    run("native-binding", [go, "test", "-race", "-count=1", "-timeout", "45s", "-v", "-run", "^TestProducedIdentityBindsNativeControl$", "./nativefabric"], cwd=TAMAGO,
        extra_env={"FES_DE25_PRODUCER_DIR": str(a), "GOWORK": "off", "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off"})
    if sources != {str(x.relative_to(ROOT)): sha(x) for x in sorted(files)}:
        raise RuntimeError("native integration sources changed during verification")
    report = {"status": "passed", "classification": "host-only producer simulation and native Go control; no RBF or hardware acceptance",
              "tool_search_order": "passed", "commands": commands, "first": first, "second": second, "go_sha256": sha(go.resolve()),
              "native_source_sha256": sources,
              "logs_sha256": {name: sha(out / (name + ".log")) for name in ("first-build", "second-build", "first-verify", "wrong-live-id", "wrong-live-abi", "wrong-descriptor", "wrong-record", "wrong-live-binding", "wrong-artifact", "native-binding")},
              "limitations": ["host executables are not RBFs or FES packages", "no Nano source pinout, actual producer, physical timing or hardware qualification", "record/tool hashes are diagnostic correlations, not authenticated production seals"]}
    (out / "result.json").write_text(json.dumps(report, indent=2) + "\n")
    print("PASS:", out)


if __name__ == "__main__": main()
