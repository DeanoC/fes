#!/usr/bin/env python3
"""Verify the native lifecycle with patched TF-A functions and real FPGA RTL."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid

HERE = Path(__file__).resolve().parent
TAMAGO = HERE.parent
ROOT = TAMAGO.parents[2]
FIRMWARE = ROOT / "platform/embedded/firmware"
FPGA = ROOT / "sources/misteross"
sys.path.insert(0, str(TAMAGO))
from admission_fixtures import prepare as prepare_admission


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--reference", type=Path, help="pinned TF-A source; omit to fetch the exact public Git blob")
    parser.add_argument("--reset-reference", type=Path, help="Terasic bridge source, when separate from its service source")
    parser.add_argument("--nano-bridge-service", action="store_true", help="opt-in unbound C bridge service with pinned vendor driver and injected registers")
    parser.add_argument("--output", type=Path, default=ROOT / "out" / ("native-fabric-hooks-" + uuid.uuid4().hex))
    args = parser.parse_args()
    output = args.output.absolute()
    if ".." in output.parts or any(p.is_symlink() for p in (output, *output.parents)) or not output.is_relative_to(ROOT / "out") or output.exists():
        parser.error("output must be a fresh private directory beneath out/")
    output.mkdir(parents=True)
    commands = []
    go_root = subprocess.check_output(["go", "env", "GOROOT"], cwd=TAMAGO,
                                      env=dict(os.environ, GOWORK="off"), text=True).strip()
    go = Path(go_root) / "bin/go"
    commands.append({"label": "host-go-root", "cwd": str(TAMAGO), "argv": ["go", "env", "GOROOT"]})
    env = dict(os.environ, GOWORK="off", GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off")

    def run(name, argv, cwd=ROOT, extra_env=None):
        argv = [str(a) for a in argv]
        commands.append({"label": name, "cwd": str(cwd), "argv": argv})
        merged = dict(env, **(extra_env or {}))
        result = subprocess.run(argv, cwd=cwd, env=merged, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False)
        (output / (name + ".log")).write_bytes(result.stdout)
        if result.returncode:
            raise RuntimeError(f"{name} exited {result.returncode}; see {output / (name + '.log')}")
        return result.stdout.decode()

    # Freeze the source set across compilation and execution. The generated
    # fixtures are independently built from the existing production oracle.
    files = set()
    for directory in (HERE, TAMAGO / "embeddedcontrol", TAMAGO / "fabricmodel", TAMAGO / "nativefabriccheck", TAMAGO / "modelbackend", TAMAGO / "runtimeadmission", FIRMWARE):
        files.update(p for p in directory.rglob("*") if p.is_file() and p.suffix in (".go", ".py", ".cpp", ".hpp", ".h", ".c", ".inc"))
    files.update((TAMAGO / "go.mod", TAMAGO / "go.sum", FPGA / "cores/fes-common/rtl/fes_embedded_control.sv",
                  FPGA / "cores/fes-common/generated/fes_embedded_control.vh",
                  ROOT / "sources/mister-packages/packages/fabric/fes_fabric_embedded_control.yaml"))
    sources = {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)}

    firmware_out = output / "firmware"
    command = [sys.executable, FIRMWARE / "verify.py", "--output", firmware_out]
    if args.reference:
        command += ["--reference", args.reference.absolute()]
    if args.reset_reference:
        command += ["--reset-reference", args.reset_reference.absolute()]
    run("firmware", command)
    firmware = json.loads((firmware_out / "result.json").read_text())
    vendor = firmware.get("reference_profile") == "terasic-de25-v2.12"

    nano = None
    objects = [firmware_out / "tfa_subset.o"]
    if args.nano_bridge_service:
        if not vendor or not args.reference:
            parser.error("Nano bridge service requires the explicit pinned Terasic source")
        sys.path.insert(0, str(FIRMWARE))
        import verify_nano_bridges
        reference = args.reset_reference or args.reference.parent / "soc/socfpga_reset_manager.c"
        nano = verify_nano_bridges.build(reference.absolute(), output / "nano-driver", run)
        objects += [output / "nano-driver/driver.o", output / "nano-driver/service.o"]

    rpc_dir = output / "rpc"
    run("rpc-build", ["verilator", "--cc", "--exe", "--build", "--top-module", "fes_embedded_control", "-Wall",
        "--Mdir", rpc_dir, "-I" + str(FPGA / "cores/fes-common/generated"),
        "-GBUILD_ID=128'h0123456789abcdef0123456789abcdef",
        "-CFLAGS", "-std=c++14 -O2 -fsanitize=undefined -fno-sanitize-recover=all " + ("-DFES_TEST_TERASIC_TFA " if vendor else "") + ("-DFES_TEST_NANO_BRIDGES " if nano else "") + "-I" + str(FIRMWARE) + " -I" + str(FIRMWARE / "tests"),
        "-LDFLAGS", " ".join(str(p) for p in objects) + " -fsanitize=undefined",
        FPGA / "cores/fes-common/rtl/fes_embedded_control.sv", FIRMWARE / "control.cpp", FIRMWARE / "tests/native_rpc.cpp"])

    fixture_dir, _, fixture_sources = prepare_admission(ROOT, output, lambda name, argv: run(name, argv))
    fixture_snapshot = {str(p.relative_to(ROOT)): sha(p) for p in fixture_sources if p.is_file() and p.is_relative_to(ROOT)}
    helper = output / "session-fixtures"
    run("session-fixtures-build", [go, "build", "-mod=readonly", "-o", helper, "./cmd/session-fixtures"], cwd=TAMAGO)
    run("session-fixtures", [helper, fixture_dir, output / "admission-oracle"])

    test_env = {"FES_EMBEDDED_RPC": str(rpc_dir / "Vfes_embedded_control"), "FES_MODEL_FIXTURES": str(fixture_dir)}
    native_cases = ["./nativefabric", "./nativefabriccheck", "./embeddedcontrol/...", "./fabricmodel", "./runtimeadmission"]
    tenfoot_cases = ["./modelbackend"]
    if nano:
        test_env["FES_NANO_BRIDGE_RPC"] = test_env["FES_EMBEDDED_RPC"]
        native_cases = ["-run", "^TestNanoFirmwareBridge", "./nativefabric"]
        tenfoot_cases = ["-run", "^(TestTenfootInjectedEndpointLaunchReturnAndIdentityFailure|TestCPUBridgeFailureStopsActorBeforeProgramming)$", "./modelbackend"]
    run("native-tests", [go, "test", "-race", "-count=1", "-timeout", "90s", "-v", *native_cases], cwd=TAMAGO, extra_env=test_env)
    run("tenfoot-tests", [go, "test", "-race", "-count=1", "-timeout", "90s", "-v", *tenfoot_cases], cwd=TAMAGO, extra_env=test_env)
    if sources != {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)} or fixture_snapshot != {str(p.relative_to(ROOT)): sha(p) for p in fixture_sources if p.is_file() and p.is_relative_to(ROOT)}:
        raise RuntimeError("source changed during verification")
    report = {
        "status": "passed",
        "classification": "host native Go lifecycle/tenfoot with actual patched TF-A C and Verilated FPGA RTL; simulated bridges, memory and SDM; no hardware acceptance",
        "commands": commands,
        "source_sha256": sources,
        "fixture_source_sha256": fixture_snapshot,
        "firmware_reference_git_blob": firmware["reference_git_blob"],
        "firmware_reference_profile": firmware["reference_profile"],
        "firmware_patched_sha256": firmware["patched_tfa_sha256"],
        "firmware_reset_patched_sha256": firmware["reset_patched_sha256"],
        "firmware_checks": firmware["checks"],
        "rpc_sha256": sha(rpc_dir / "Vfes_embedded_control"),
        "host_go_sha256": sha(go.resolve()),
        "nano_bridge_service": nano,
        "fixture_sha256": {p.name: sha(p) for p in sorted(fixture_dir.iterdir()) if p.is_file()},
        "test_logs_sha256": {name: sha(output / (name + ".log")) for name in ("native-tests", "tenfoot-tests", "rpc-build", "firmware")},
    }
    if nano:
        report["classification"] = "host native lifecycle/tenfoot with C Nano bridge service, actual pinned vendor bridge functions, prepared TF-A functions and Verilated FPGA RTL; injected registers, ownership, memory and SDM; no physical acceptance"
    (output / "result.json").write_text(json.dumps(report, indent=2) + "\n")
    print("PASS:", output)


if __name__ == "__main__":
    main()
