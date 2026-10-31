#!/usr/bin/env python3
"""Execute offline composition on native and emulated AArch64 CPUs; no FPGA access."""
from __future__ import annotations

import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tarfile

BOARD = Path(__file__).resolve().parent


def canonical(value: object) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode("ascii")


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def run(command: list[str], log: Path, *, cwd: Path | None = None,
        env: dict | None = None, success: bool = True) -> subprocess.CompletedProcess:
    result = subprocess.run(command, cwd=cwd, env=env, capture_output=True, timeout=180)
    log.write_bytes(result.stdout + result.stderr)
    if (result.returncode == 0) != success:
        raise ValueError(f"unexpected exit {result.returncode}; retained {log}")
    return result


def check_arm_elf(path: Path) -> None:
    data = path.read_bytes()
    if data[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", data, 18)[0] != 183:
        raise ValueError("target must be a little-endian ELF64 AArch64 executable")
    offset = struct.unpack_from("<Q", data, 32)[0]
    size, count = struct.unpack_from("<HH", data, 54)
    if size < 56 or offset + size * count > len(data):
        raise ValueError("invalid AArch64 program headers")
    if any(struct.unpack_from("<I", data, offset + n * size)[0] == 3 for n in range(count)):
        raise ValueError("target must be static, without an ELF interpreter")


def inspect_bundle(path: Path, manifest_path: Path, manifest: dict, persona: dict) -> dict:
    """Independent inspection: exact bytes, canonical plan and ordered identities."""
    names = ["deployment.json", "artifacts.json", "base.rbf", "slot0/persona.rbf"]
    with tarfile.open(path, "r:") as archive:
        members = archive.getmembers()
        if [m.name for m in members] != names:
            raise ValueError("bundle has unexpected, duplicate or reordered members")
        if any(not m.isfile() or m.mode != 0o644 or m.mtime != 0 or m.uid or m.gid
               for m in members):
            raise ValueError("bundle members are not deterministic ordinary files")
        contents = {m.name: archive.extractfile(m).read() for m in members}
    descriptor = manifest_path.read_bytes()
    if contents["artifacts.json"] != descriptor:
        raise ValueError("bundle changed the producer descriptor")
    base = manifest["shell"]["base"]
    for name, blob in (("base.rbf", base), ("slot0/persona.rbf", persona)):
        original = (manifest_path.parent / blob["path"]).read_bytes()
        if contents[name] != original or len(original) != blob["size"] or digest(original) != blob["sha256"]:
            raise ValueError("bundle changed a vendor payload")
    steps = [
        {"action": "load-base", "artifact": "base.rbf", "sha256": base["sha256"], "size": base["size"]},
        {"action": "load-persona", "artifact": "slot0/persona.rbf", "sha256": persona["sha256"],
         "size": persona["size"], "region_id": persona["region_id"], "persona": persona["id"],
         "hardware_id": persona["hardware_id"]},
    ]
    identity = {"shell_id": manifest["shell"]["id"], "control_build_id": manifest["shell"]["control_build_id"],
                "descriptor_sha256": digest(descriptor), "steps": steps}
    plan = {"format": 2, "kind": "fes.de25-pr-proof-deployment", "diagnostic_only": True,
            "physical_acceptance": "not-run", "controller": "runtime-protocol-only", "monolithic_rbf": False,
            "device": manifest["device"], **identity,
            "composition_id": digest(b"fes-de25-pr-deployment-v2\0" + canonical(identity))}
    if contents["deployment.json"] != canonical(plan) + b"\n":
        raise ValueError("bundle plan differs from independently computed load order/identity")
    return {"sha256": digest(path.read_bytes()), "size": path.stat().st_size,
            "composition_id": plan["composition_id"], "base_sha256": base["sha256"],
            "persona_sha256": persona["sha256"]}


def execute(artifacts: Path, output: Path, qemu: Path, go: str) -> dict:
    qemu_command = shutil.which(str(qemu)) if qemu.parent == Path(".") else str(qemu)
    if not qemu_command:
        raise ValueError("QEMU executable was not found")
    artifacts, output, qemu = artifacts.resolve(), output.resolve(), Path(qemu_command).resolve()
    driver_sha256 = digest(Path(__file__).read_bytes())
    descriptor_bytes = artifacts.read_bytes()
    manifest = json.loads(descriptor_bytes)
    output.mkdir(parents=True, mode=0o700, exist_ok=False)
    logs = output / "logs"
    logs.mkdir()
    # Build both architectures from one captured source tree, and execute the
    # captured QEMU bytes. Hashing a live tree after execution misidentifies it.
    source = output / "target-source"
    source.mkdir()
    source_sha256 = {}
    for path in sorted((BOARD / "target").glob("*")):
        if path.is_file():
            data = path.read_bytes()
            (source / path.name).write_bytes(data)
            source_sha256["target/" + path.name] = digest(data)
    qemu_bytes = qemu.read_bytes()
    qemu = output / "qemu-aarch64"
    qemu.write_bytes(qemu_bytes)
    qemu.chmod(0o700)
    # Resolve the module-selected Go executable, then forbid further automatic
    # toolchain selection for both builds. Record/check that executable's bytes.
    resolved = run([go, "env", "GOROOT"], logs / "go-root.txt", cwd=source)
    go_binary = Path(resolved.stdout.decode().strip()) / "bin/go"
    go_sha256 = digest(go_binary.read_bytes())
    go = str(go_binary)
    environment = dict(os.environ, CGO_ENABLED="0", GOTOOLCHAIN="local")
    environment.pop("GOOS", None)
    environment.pop("GOARCH", None)
    run([go, "version"], logs / "go-version.txt", cwd=source, env=environment)
    run([str(qemu), "--version"], logs / "qemu-version.txt")
    native, arm = output / "compose-native", output / "compose-aarch64"
    for name, binary, extra in (("native", native, {}), ("aarch64", arm, {"GOOS": "linux", "GOARCH": "arm64", "GOARM64": "v8.0"})):
        run([go, "build", "-trimpath", "-buildvcs=false", "-o", str(binary), "."],
            logs / f"build-{name}.log", cwd=source, env=environment | extra)
    check_arm_elf(arm)
    commands = {"native": [str(native)], "aarch64-cortex-a76": [str(qemu), "-cpu", "cortex-a76", str(arm)]}
    expected_shell, expected_base = manifest["shell"]["id"], manifest["shell"]["base"]["sha256"]
    expected_descriptor = digest(descriptor_bytes)

    def compose(arch: str, descriptor: Path, selections: list[str], destination: Path,
                label: str, *, shell: str = expected_shell, base: str = expected_base,
                descriptor_pin: str = expected_descriptor, success: bool = True) -> None:
        command = commands[arch] + ["compose", "--artifacts", str(descriptor),
                                   "--expected-shell-id", shell, "--expected-base-sha256", base,
                                   "--expected-descriptor-sha256", descriptor_pin]
        for selection in selections:
            command += ["--persona", selection]
        command += ["--output", str(destination)]
        run(command, logs / f"{arch}-{label}.log", success=success)
        if not success and destination.exists():
            raise ValueError("rejected composition published a partial output")

    bundles = {}
    for persona in manifest["personas"]:
        per_arch = {}
        for arch in commands:
            for repeat in range(2):
                name = f"{arch}-{persona['id']}-{repeat}"
                path = output / (name + ".tar")
                compose(arch, artifacts, [persona["id"]], path, name)
                per_arch[name] = inspect_bundle(path, artifacts, manifest, persona)
        if len({item["sha256"] for item in per_arch.values()}) != 1:
            raise ValueError("native/AArch64 or repeat composition bytes differ")
        bundles[persona["id"]] = per_arch
    if len(bundles) != 2 or len({next(iter(v.values()))["composition_id"] for v in bundles.values()}) != 2:
        raise ValueError("proof requires two distinct alternative persona compositions")

    # Exercise real artifact bytes on both CPU architectures. Only the damaged
    # case uses private copies; the producer directory is never modified.
    negatives = output / "negative-inputs"
    negatives.mkdir(mode=0o700)

    def inventory(name: str) -> Path:
        directory = negatives / name
        directory.mkdir()
        shutil.copyfile(artifacts, directory / "artifacts.json")
        for blob in [manifest["shell"]["base"], *manifest["personas"]]:
            destination = directory / blob["path"]
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(artifacts.parent / blob["path"], destination)
        return directory

    foreign = inventory("foreign-root")
    changed = copy.deepcopy(manifest)
    changed["personas"][0]["root_qdb_sha256"] = "f" * 64
    (foreign / "artifacts.json").write_bytes(canonical(changed) + b"\n")
    replaced = inventory("replaced-descriptor")
    replacement = copy.deepcopy(manifest)
    # This edit remains internally consistent and leaves every real payload
    # available; only the original external receipt pin must reject it.
    replacement["personas"][0]["revision"] += "_replacement"
    (replaced / "artifacts.json").write_bytes(canonical(replacement) + b"\n")
    corrupt = inventory("corrupt")
    payload = corrupt / manifest["personas"][0]["path"]
    with payload.open("r+b") as handle:
        first = handle.read(1)
        if not first:
            raise ValueError("empty persona artifact")
        handle.seek(0)
        handle.write(bytes([first[0] ^ 1]))
    selected = manifest["personas"][0]["id"]
    controls = []
    for arch in commands:
        cases = [
            ("wrong-shell", artifacts, [selected], {"shell": "0" * 64}),
            ("wrong-base", artifacts, [selected], {"base": "0" * 64}),
            ("foreign-root", foreign / "artifacts.json", [selected],
             {"descriptor_pin": digest((foreign / "artifacts.json").read_bytes())}),
            ("replaced-descriptor", replaced / "artifacts.json", [selected], {}),
            ("corrupt-persona", corrupt / "artifacts.json", [selected], {}),
            ("overlapping-personas", artifacts, [p["id"] for p in manifest["personas"]], {}),
        ]
        for name, descriptor, selections, extra in cases:
            compose(arch, descriptor, selections, output / f"rejected-{arch}-{name}.tar",
                    "negative-" + name, success=False, **extra)
            controls.append({"architecture": arch, "case": name, "result": "rejected-no-output"})
    if (digest(go_binary.read_bytes()) != go_sha256 or digest(Path(__file__).read_bytes()) != driver_sha256
            or artifacts.read_bytes() != descriptor_bytes):
        raise ValueError("Go executable, proof driver or producer descriptor changed during the proof")
    report = {"format": 1, "kind": "fes.de25-aarch64-composition-proof", "diagnostic_only": True,
              "execution": "native-and-qemu-user-aarch64-cortex-a76", "physical_acceptance": "not-run",
              "producer_descriptor_sha256": expected_descriptor, "shell_id": expected_shell,
              "source_sha256": source_sha256, "driver_sha256": driver_sha256,
              "go_executable_sha256": go_sha256,
              "go_version": (logs / "go-version.txt").read_text().strip(),
              "qemu": {"sha256": digest(qemu.read_bytes()), "version": (logs / "qemu-version.txt").read_text().strip()},
              "executable_sha256": {"native": digest(native.read_bytes()), "aarch64": digest(arm.read_bytes())},
              "bundles": bundles, "negative_controls": controls}
    (output / "proof.json").write_bytes(canonical(report) + b"\n")
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifacts", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--qemu", required=True, type=Path)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    try:
        report = execute(args.artifacts, args.output, args.qemu, args.go)
    except (ValueError, OSError, KeyError, subprocess.SubprocessError, tarfile.TarError) as error:
        parser.exit(1, f"composition proof failed: {error}\n")
    print(json.dumps({"output": str(args.output), "shell_id": report["shell_id"],
                      "personas": sorted(report["bundles"]), "negative_controls": len(report["negative_controls"])}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
