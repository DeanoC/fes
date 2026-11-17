#!/usr/bin/env python3
"""Run a sealed diagnostic Nano package through simulated native CPU/firmware."""
import argparse
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import uuid

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
TAMAGO = ROOT / "platform/embedded/tamago"
FIRMWARE = ROOT / "platform/embedded/firmware"
FPGA = ROOT / "sources/misteross"
spec = importlib.util.spec_from_file_location("board_control_verifier", HERE / "verify.py")
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", required=True, type=Path)
    parser.add_argument("--reference", type=Path)
    parser.add_argument("--reset-reference", type=Path)
    parser.add_argument("--output", type=Path, default=ROOT / "out" / ("de25-sealed-native-" + uuid.uuid4().hex))
    args = parser.parse_args()
    project, output = args.project.absolute(), args.output.absolute()
    for path in (project, output):
        if ".." in path.parts or any(p.is_symlink() for p in (path, *path.parents)) or not path.is_relative_to(ROOT / "out"):
            parser.error("paths must be private locations beneath this worktree's out/")
    if not project.is_dir() or output.exists():
        parser.error("project must exist and output must be fresh")
    inputs, _, _ = verifier.inputs_for(project)
    import seal
    from scripts.export_core_package import _archive_bytes
    if not inputs["embedded_control"].get("build_inputs_sha256"):
        parser.error("project is not a sealed diagnostic")
    result = json.loads((project / "board/hps/seal-result.json").read_text())
    package = project / "board/hps/packages" / (result["package_id"] + ".fcore")
    package_dir = package.with_suffix("")
    expected_package = seal.read_package(package_dir)
    output.mkdir(parents=True)
    env = dict(os.environ)
    for name in ("GOOS", "GOARCH", "GOARM", "GOAMD64", "GOARM64", "GOOSPKG", "GOFLAGS", "GOTOOLCHAIN", "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "GNUMAKEFLAGS", "PYTHON"):
        env.pop(name, None)
    env["GOWORK"] = "off"
    go_root = subprocess.check_output(["go", "env", "GOROOT"], cwd=TAMAGO, env=env, text=True).strip()
    go = Path(go_root) / "bin/go"
    env.update(GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off")
    commands = []

    def run(label, command, cwd=ROOT, extra=None):
        command = [str(v) for v in command]
        commands.append({"label": label, "cwd": str(cwd), "argv": command})
        completed = subprocess.run(command, cwd=cwd, env=dict(env, **(extra or {})), stdout=subprocess.PIPE,
                                   stderr=subprocess.STDOUT, timeout=240)
        (output / (label + ".log")).write_bytes(completed.stdout)
        if completed.returncode:
            raise ValueError(f"{label} failed; see {output / (label + '.log')}")
        return completed.stdout.decode()

    files = set()
    for directory in (TAMAGO / "nativefabric", TAMAGO / "fabricmodel", TAMAGO / "embeddedcontrol",
                      TAMAGO / "runtimeadmission", TAMAGO / "modelbackend", FIRMWARE, HERE,
                      ROOT / "sources/FogCast/corepackage", ROOT / "sources/FogCast/hostclient",
                      ROOT / "sources/FogCast/ui/tenfoot", ROOT / "sources/FogCast/ui/gfx"):
        files.update(p for p in directory.rglob("*") if p.is_file() and p.suffix in (".go", ".py", ".cpp", ".hpp", ".h", ".sv", ".tcl"))
    files.update((TAMAGO / "go.mod", TAMAGO / "go.sum", FPGA / "cores/fes-common/rtl/fes_embedded_control.sv",
                  FPGA / "cores/fes-common/generated/fes_embedded_control.vh"))
    sources = {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)}

    # A valid but different archive under the expected filename must not be
    # reported as the original sealed package.
    altered = bytearray(expected_package.payload_bytes)
    altered[0] ^= 1
    fields = copy.deepcopy(expected_package.fields)
    fields["payload"]["sha256"] = hashlib.sha256(altered).hexdigest()
    different = output / "different-valid.fcore"
    different.write_bytes(_archive_bytes(seal.encode_manifest(fields), bytes(altered)))
    try:
        verifier.check_archive(different, expected_package)
    except ValueError:
        pass
    else:
        raise ValueError("a different valid archive was accepted as the selected package")

    runtime = ROOT / "sources/libmister-runtime"
    units = [ROOT / "platform/embedded/runtime/admission_oracle.cpp"] + [runtime / "src" / name for name in
             ("native/core_package.cpp", "native/artifacts.cpp", "native/sha256.cpp", "native/diagnostic.cpp", "runtime.cpp")]
    objects, dependencies = [], set()
    for index, source in enumerate(units):
        obj, dep = output / f"oracle-{index}.o", output / f"oracle-{index}.d"
        run(f"oracle-compile-{index}", ["c++", "-O2", "-std=c++14", "-D_FILE_OFFSET_BITS=64", "-ffunction-sections", "-fdata-sections", "-pthread",
            "-I" + str(runtime / "include"), "-I" + str(runtime / "src"), "-isystem", runtime / "third_party/toml11/include",
            "-MD", "-MF", dep, "-c", source, "-o", obj])
        dependencies.update(Path(p) for p in shlex.split(dep.read_text().replace("\\\n", " ").split(":", 1)[1]))
        objects.append(obj)
    oracle = output / "admission-oracle"
    run("oracle-link", ["c++", "-pthread", *objects, "-Wl,--gc-sections", "-o", oracle])
    dep_hashes = {str(p): sha(p) for p in sorted(dependencies) if p.is_file()}
    decision = json.loads(run("oracle-nano", [oracle, package_dir, result["package_id"]]))
    if decision["package_id"] != result["package_id"] or decision["code"] != "unsupported_target":
        raise ValueError("existing physical runtime did not reject the Nano target")
    firmware_out = output / "firmware"
    command = [sys.executable, FIRMWARE / "verify.py", "--output", firmware_out]
    if args.reference:
        command += ["--reference", args.reference.absolute()]
    if args.reset_reference:
        command += ["--reset-reference", args.reset_reference.absolute()]
    run("firmware", command)
    firmware = json.loads((firmware_out / "result.json").read_text())
    vendor = firmware.get("reference_profile") == "terasic-de25-v2.12"
    model = output / "rpc"
    run("rpc-build", ["verilator", "--cc", "--exe", "--build", "--top-module", "fes_embedded_control", "-Wall",
                      "--Mdir", model, "-I" + str(FPGA / "cores/fes-common/generated"),
                      "-GBUILD_ID=128'h" + result["build_id"], "-GABI_ID=" + str(inputs["embedded_control"]["control_abi"]),
                      "-CFLAGS", "-std=c++14 -O2 -fsanitize=undefined -fno-sanitize-recover=all " + ("-DFES_TEST_TERASIC_TFA " if vendor else "") + "-I" + str(FIRMWARE),
                      "-LDFLAGS", str(firmware_out / "tfa_subset.o") + " -fsanitize=undefined",
                      FPGA / "cores/fes-common/rtl/fes_embedded_control.sv", FIRMWARE / "control.cpp", FIRMWARE / "tests/native_rpc.cpp"])
    test_env = {"FES_DE25_SEALED_PACKAGE": str(package), "FES_SEALED_TFA_RPC": str(model / "Vfes_embedded_control")}
    hps_first = seal.boot_order(inputs) == "HPS FIRST"
    if hps_first:
        test_env.update({"FES_DE25_HPS_PAIR": str(project / "board/hps/hps-first-pair.json"),
                         "FES_DE25_HPS_PHASE1": str(project / "board/hps/hps-first.hps.rbf")})
    run("native", [go, "test", "-race", "-count=1", "-timeout", "180s", "-v", "-run", "^TestSealedNanoPackageNativeFirmwareCycle$", "./nativefabric"], TAMAGO, test_env)
    run("tenfoot", [go, "test", "-race", "-count=1", "-timeout", "180s", "-v", "-run", "^TestSealedNanoTenfootModel$", "./modelbackend"], TAMAGO, test_env)
    # The nested verifier discovers Go again. Prefer the already selected
    # toolchain binary so its lookup works with our offline module settings.
    run("board-rtl", [sys.executable, HERE / "verify.py", "--project", project, "--output", output / "board-rtl"],
        extra={"PATH": str(go.parent) + os.pathsep + env.get("PATH", os.defpath)})
    if sources != {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)} or dep_hashes != {str(p): sha(p) for p in sorted(dependencies) if p.is_file()}:
        raise ValueError("verification source changed during the run")
    verifier.inputs_for(project)
    firmware = json.loads((firmware_out / "result.json").read_text())
    report = {"status": "passed", "classification": "sealed " + ("HPS-first core" if hps_first else "full-SoC") + " diagnostic package through simulated CPU, TF-A functions and RTL; no physical configuration",
              "package_id": result["package_id"], "build_id": result["build_id"], "rbf_sha256": result["rbf_sha256"],
              "archive_sha256": sha(package), "record_sha256": result["build_inputs_sha256"],
              "firmware_reference_git_blob": firmware["reference_git_blob"], "firmware_patched_sha256": firmware["patched_tfa_sha256"],
              "firmware_reset_patched_sha256": firmware["reset_patched_sha256"],
              "rpc_sha256": sha(model / "Vfes_embedded_control"), "go_sha256": sha(go.resolve()), "source_sha256": sources,
              "oracle_sha256": sha(oracle), "oracle_dependencies_sha256": dep_hashes, "existing_runtime_decision": decision,
              "commands": commands, "log_sha256": {name: sha(output / (name + ".log")) for name in ("firmware", "rpc-build", "native", "tenfoot", "board-rtl", "oracle-nano")},
              "limitations": ["transport, memory, bridges, boot handoff and SDM simulated",
                              "HPS-first pair not booted or qualified on this board" if hps_first else "full SoC RBF includes vendor SPL; not qualified for running-HPS reconfiguration",
                              "no physical CPU register mapping, DDR path or global containment proof"]}
    if hps_first:
        report["hps_first_pair_sha256"] = result["hps_first_pair_sha256"]
    (output / "result.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print("PASS:", output)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        print("sealed Nano verification:", error, file=sys.stderr)
        sys.exit(1)
