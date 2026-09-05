#!/usr/bin/env python3
"""Build the pinned native system using the existing component recipes."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tomllib

from inputs import git, validate

ROOT = Path(__file__).resolve().parents[1]

def run(args, **kwargs):
    print("+ " + " ".join(map(str, args)), flush=True)
    return subprocess.run(list(map(str, args)), check=True, **kwargs)

def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()

def write_receipt(output, kind, fingerprint, names):
    data = {"inputs": fingerprint, "files": {name: digest(output / name) for name in names}}
    temporary = output / (kind + ".json.tmp")
    temporary.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n")
    temporary.replace(output / (kind + ".json"))

def reusable(output, kind, fingerprint):
    try:
        receipt = json.loads((output / (kind + ".json")).read_text())
        return (receipt["inputs"] == fingerprint and bool(receipt["files"])
                and all(digest(output / name) == sha for name, sha in receipt["files"].items()))
    except (OSError, ValueError, KeyError, TypeError):
        return False

def fingerprint(revisions, profile, toolchain):
    data = {"sources": revisions, "profile": profile, "go": toolchain,
            "recipe": {str(p.relative_to(ROOT)): digest(p)
                       for p in sorted((ROOT / "scripts").glob("*.py"))}}
    return hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest(), data

def source_checkout(name, revision):
    # Isolate child Git identity from the parent and keep .git inside container mounts.
    path = ROOT / "out/work" / (name + "-" + revision)
    if not path.exists():
        path.parent.mkdir(parents=True, exist_ok=True)
        run(["git", "clone", "--no-hardlinks", "--no-checkout",
             ROOT / "sources" / name, path])
        run(["git", "-C", path, "checkout", "--detach", revision])
    if git(path, "rev-parse", "HEAD") != revision or git(path, "status", "--porcelain", "--untracked-files=all"):
        raise ValueError(f"staged source is changed; inspect and remove {path} before rebuilding")
    return path

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["doctor", "build", "host", "image", "verify", "rebuild"])
    parser.add_argument("--profile", default="native-dev", choices=["native-dev"])
    args = parser.parse_args()
    revisions = validate(ROOT)
    profile = tomllib.loads((ROOT / "profiles" / (args.profile + ".toml")).read_text())
    if platform.system() != "Linux" or platform.machine() not in ("x86_64", "amd64"):
        raise ValueError("this initial image builder requires Linux amd64")
    container = os.environ.get("CONTAINER_RUNTIME", "docker")
    for tool in ("git", "make", "go"):
        if not shutil.which(tool):
            raise ValueError(f"required executable is missing: {tool}")
    fogcast = ROOT / "sources/FogCast"
    env = os.environ.copy()
    for name in tuple(env):
        if (name.startswith(("TARGET_IMAGE_", "NATIVE_RUNTIME_"))
                or name in ("LIBMISTER_RUNTIME_DIR", "MAKEFLAGS", "MAKEOVERRIDES", "MFLAGS",
                            "GOFLAGS", "GOEXPERIMENT", "GOOS", "GOARCH", "GOARM", "GOAMD64",
                            "GOWORK", "GOTOOLCHAIN", "GOENV", "GOFIPS140")):
            del env[name]
    env.update(GOENV="off", GOWORK="off", GOFLAGS="", GOEXPERIMENT="",
               GOAMD64="v1", GOTOOLCHAIN="auto", GOFIPS140="off")
    toolchain = subprocess.check_output(["go", "version"], cwd=fogcast, env=env, text=True).strip()
    if args.action != "host":
        if not shutil.which(container):
            raise ValueError(f"required container runtime is missing: {container}")
        run([container, "info", "--format", "{{.ServerVersion}}"])
    print(json.dumps({"sources": revisions, "go": toolchain, "profile": args.profile}, indent=2), flush=True)
    if args.action == "doctor":
        return
    output = ROOT / "out" / args.profile
    output.mkdir(parents=True, exist_ok=True)
    # Serialize this workspace only; do not share the child's default output volume.
    with (ROOT / "out/build.lock").open("w") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError("another parent build is running in this workspace")
        fogcast = source_checkout("FogCast", revisions["FogCast"])
        fp, info = fingerprint(revisions, profile, toolchain)
        env["TARGET_IMAGE_CONTAINER_RUNTIME"] = container
        env["TARGET_IMAGE_OUTPUT_VOLUME"] = "fes-native-" + hashlib.sha256(str(ROOT).encode()).hexdigest()[:16]
        # The host has a small /tmp tmpfs; Go temporary files belong in out/.
        temp = ROOT / "out/tmp"
        temp.mkdir(exist_ok=True)
        env["GOTMPDIR"] = str(temp)
        child_make = ["make", "-C", fogcast, "CONTAINER_RUNTIME=" + container, "VERSION=" + profile["version"]]
        if args.action in ("build", "host", "rebuild"):
            if args.action != "rebuild" and reusable(output, "host", fp):
                print("Host: reusing verified output", flush=True)
            else:
                run(child_make + ["build-fogcast", "FOGCAST_GOOS=" + profile["host_os"],
                    "FOGCAST_GOARCH=" + profile["host_arch"],
                    "FOGCAST_OUTPUT=" + str(output / "fogcast"),
                    "REVISION=" + revisions["FogCast"]], env=env)
                # The pinned child's API target hard-codes Darwin. Use its Go recipe
                # with Linux settings here until that target becomes parameterized.
                api_env = dict(env, CGO_ENABLED="0", GOOS=profile["host_os"], GOARCH=profile["host_arch"])
                ldflags = ("-s -w -X github.com/DeanoC/FogCast/internal/version.Version=" + profile["version"]
                           + " -X github.com/DeanoC/FogCast/internal/version.Revision=" + revisions["FogCast"])
                run(["go", "build", "-buildvcs=false", "-trimpath", "-ldflags", ldflags,
                     "-o", output / "fogcast-api", "./cmd/fogcast-api"], cwd=fogcast, env=api_env)
                write_receipt(output, "host", fp, ["fogcast", "fogcast-api"])
        if args.action in ("build", "image", "rebuild"):
            if args.action != "rebuild" and reusable(output, "image", fp):
                print("Image: reusing verified output", flush=True)
            else:
                runtime = source_checkout("libmister-runtime", revisions["libmister-runtime"])
                source_lock = tomllib.loads((fogcast / "build/target-image.sources.lock.toml").read_text())
                base = source_lock["container"]
                ref = base["image"] + "@" + base["digest"]
                present = subprocess.run([container, "image", "inspect", ref], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                if present.returncode:
                    run([container, "pull", "--platform", base["platform"], ref])
                run(child_make + ["build-target-image-lock-container"], env=env)
                run([fogcast / "scripts/target-image-container.sh", "fetch",
                     "/work/scripts/fetch-target-image-sources.sh"], env=env)
                run(child_make + ["target-image-native", "LIBMISTER_RUNTIME_DIR=" + str(runtime)], env=env)
                # Verification reads the runtime commit from the image/lock; no source mount required.
                run(child_make + ["target-image-native-verify"], env=env)
                built = fogcast / "build/output/target-image/native-dev"
                names = ["linux.img", "reproducibility.txt", "manifest.tsv", "library-report.tsv"]
                for name in names:
                    shutil.copy2(built / name, output / name)
                write_receipt(output, "image", fp, names)
        if args.action == "verify":
            if not reusable(output, "host", fp) or not reusable(output, "image", fp):
                raise ValueError("build outputs are missing, changed, or stale; run make build")
            built = fogcast / "build/output/target-image/native-dev/linux.img"
            if not built.is_file() or digest(built) != digest(output / "linux.img"):
                raise ValueError("child image differs from published image; run make rebuild")
            run(child_make + ["target-image-native-verify"], env=env)
            run(child_make + ["target-image-native-qemu-smoke"], env=env)
            actual = digest(output / "linux.img")
            evidence = dict(line.split("=", 1) for line in (output / "reproducibility.txt").read_text().splitlines())
            if evidence.get("run_1_sha256") != actual or evidence.get("run_2_sha256") != actual:
                raise ValueError("image does not match both recorded build passes")
            matches = actual == profile["baseline_image_sha256"]
            result = {"image_sha256": actual, "historical_baseline_match": matches,
                      "two_pass_reproducibility": "pass", "structural": "pass", "qemu_packaging": "pass"}
            (output / "verification.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
            shutil.copy2(fogcast / "build/output/target-image/native-dev/qemu-smoke.log", output / "qemu-smoke.log")
            print("Two-pass reproducibility, structural and QEMU packaging checks passed.", flush=True)
            print(f"Historical image hash match: {matches} (see README provenance note)", flush=True)
        (output / "inputs.json").write_text(json.dumps(info, indent=2, sort_keys=True) + "\n")

if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(f"fes: {error}", file=sys.stderr)
        sys.exit(1)
