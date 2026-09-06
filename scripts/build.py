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
import tempfile

from inputs import git, validate
import bundle as core_bundle
from environment import build_environment

ROOT = Path(__file__).resolve().parents[1]

def run(args, **kwargs):
    print("+ " + " ".join(map(str, args)), flush=True)
    return subprocess.run(list(map(str, args)), check=True, **kwargs)

def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()

def publish_file(source, destination):
    # Child selection records are read-only. Replace the old inode instead of
    # trying to open it for writing on the next integration build.
    destination = Path(destination)
    temporary = destination.with_name(destination.name + ".tmp")
    try:
        temporary.unlink(missing_ok=True)
        shutil.copy2(source, temporary)
        temporary.replace(destination)
    finally:
        temporary.unlink(missing_ok=True)

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

def output_volume(root, profile):
    identity = hashlib.sha256((str(root) + "\0" + profile).encode()).hexdigest()[:16]
    return "fes-native-" + identity


def source_checkout(name, revision, suffix="", restore=()):
    # Isolate child Git identity from the parent and keep .git inside container mounts.
    path = ROOT / "out/work" / (name + "-" + revision + suffix)
    if not path.exists():
        path.parent.mkdir(parents=True, exist_ok=True)
        run(["git", "clone", "--no-hardlinks", "--no-checkout",
             ROOT / "sources" / name, path])
        run(["git", "-C", path, "checkout", "--detach", revision])
    status = git(path, "status", "--porcelain", "--untracked-files=all")
    changed = {line.split(maxsplit=1)[-1] for line in status.splitlines() if line}
    if changed and changed == set(restore):
        for relative in restore:
            (path / relative).write_bytes(subprocess.check_output(
                ["git", "-C", str(path), "show", "HEAD:" + relative]
            ))
        status = git(path, "status", "--porcelain", "--untracked-files=all")
    if git(path, "rev-parse", "HEAD") != revision or status:
        raise ValueError(f"staged source is changed; inspect and remove {path} before rebuilding")
    return path


def selected_cores(profile):
    cores = profile.get("fpga_cores", [profile.get("fpga_core", "megadrive")])
    if cores not in (["megadrive"], ["megadrive", "pong", "snes"]):
        raise ValueError("profile must select megadrive or megadrive, pong, snes")
    return tuple(cores)


def bundle_arguments(cores, bundles):
    if set(cores) != set(bundles):
        raise ValueError("bundle set differs from selected cores")
    return ["NATIVE_RUNTIME_SYSTEMS=" + " ".join(cores), "MEGADRIVE_RBF_SOURCE=source-built"] + [
        core.upper() + "_RBF_BUNDLE=" + str(bundles[core]) for core in cores]


def validate_bundle(directory, source, revision, system):
    recipe = "scripts/build_pong.py" if system == "pong" else "scripts/rebuild_core.py"
    return core_bundle.load(directory, digest(source / recipe), system=system,
                            expected_revision=revision if system == "pong" else None)


def build_bundle(revisions, env, force=False, *, system="megadrive"):
    if system not in ("megadrive", "pong", "snes"):
        raise ValueError("unsupported FPGA core")
    source = source_checkout("misteross", revisions["misteross"])
    directory = source / "build/bundles" / system
    bundles = list(directory.glob(f"*/{system}-rbf.toml"))
    if bundles and not force:
        if len(bundles) != 1:
            raise ValueError(f"misteross has more than one cached {system} bundle")
        bundle_dir = bundles[0].parent
        validate_bundle(bundle_dir, source, revisions["misteross"], system)
        print(f"Reusing validated revision-scoped FPGA bundle: {bundle_dir}", flush=True)
        return bundle_dir
    if not env.get("QUARTUS_ROOTDIR"):
        raise ValueError("source-built cores require QUARTUS_ROOTDIR")
    if system == "pong":
        run(["make", "-C", source, "build-pong"], env=env)
    else:
        run(["make", "-C", source, "fetch-core", "CORE=" + system], env=env)
        run(["make", "-C", source, "rebuild-core", "CORE=" + system], env=env)
    run(["make", "-C", source, "export-core-bundle", "CORE=" + system], env=env)
    bundles = list(directory.glob(f"*/{system}-rbf.toml"))
    if len(bundles) != 1:
        raise ValueError(f"misteross did not produce exactly one {system} bundle")
    validate_bundle(bundles[0].parent, source, revisions["misteross"], system)
    return bundles[0].parent


def build_bundles(revisions, env, cores, force=False):
    return {core: build_bundle(revisions, env, force, system=core) for core in cores}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["doctor", "build", "host", "image", "verify", "rebuild", "dev"])
    parser.add_argument("--profile", default="native-integration-dev",
                        choices=["native-dev", "native-source-dev", "native-integration-dev"])
    args = parser.parse_args()
    profile = tomllib.loads((ROOT / "profiles" / (args.profile + ".toml")).read_text())
    revisions = validate(ROOT, profile)
    cores = selected_cores(profile)
    if platform.system() != "Linux" or platform.machine() not in ("x86_64", "amd64"):
        raise ValueError("this initial image builder requires Linux amd64")
    container = os.environ.get("CONTAINER_RUNTIME", "docker")
    for tool in ("git", "make", "go"):
        if not shutil.which(tool):
            raise ValueError(f"required executable is missing: {tool}")
    env = build_environment()
    # Historical profiles may select a different go.mod from the current gitlink.
    with tempfile.TemporaryDirectory(prefix="fes-go-version-") as temporary:
        (Path(temporary) / "go.mod").write_text(git(ROOT / "sources/FogCast", "show",
            revisions["FogCast"] + ":go.mod") + "\n")
        toolchain = subprocess.check_output(["go", "version"], cwd=temporary,
                                            env=env, text=True).strip()
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
        restore = ("build/native-runtime.inputs.lock.toml",) if args.profile == "native-source-dev" else ()
        fogcast = source_checkout("FogCast", revisions["FogCast"], "-" + args.profile, restore)
        if profile.get("check_packages"):
            from consistency import check
            selected = {name: source_checkout(name, revision)
                        for name, revision in revisions.items() if name != "FogCast"}
            check(ROOT, dict(selected, FogCast=fogcast))
        lock_path = fogcast / "build/native-runtime.inputs.lock.toml"
        lock_path.write_bytes(subprocess.check_output([
            "git", "-C", str(fogcast), "show", "HEAD:build/native-runtime.inputs.lock.toml"
        ]))
        fp, info = fingerprint(revisions, profile, toolchain)
        env["TARGET_IMAGE_CONTAINER_RUNTIME"] = container
        env["TARGET_IMAGE_OUTPUT_VOLUME"] = output_volume(ROOT, args.profile)
        # The host has a small /tmp tmpfs; Go temporary files belong in out/.
        temp = ROOT / "out/tmp"
        temp.mkdir(exist_ok=True)
        env["GOTMPDIR"] = str(temp)
        child_make = ["make", "-C", fogcast, "CONTAINER_RUNTIME=" + container, "VERSION=" + profile["version"]]
        if args.action == "dev":
            if profile.get("bundle_interface") != "selection":
                raise ValueError("make dev requires native-integration-dev; historical profiles stay cold")
            from native_dev import build_development
            runtime = source_checkout("libmister-runtime", revisions["libmister-runtime"])
            bundles = build_bundles(revisions, env, cores)
            build_development(ROOT, fogcast, runtime, args.profile, profile, info,
                              fp, env, child_make, bundles)
            return
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
                if profile.get("fpga_source") == "misteross":
                    recipe_source = source_checkout("misteross", revisions["misteross"])
                    for core in cores:
                        validate_bundle(output, recipe_source, revisions["misteross"], core)
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
                if profile.get("fpga_source") == "misteross":
                    bundles = build_bundles(revisions, env, cores, args.action == "rebuild")
                    bundle_dir = bundles["megadrive"]
                    recipe_source = source_checkout("misteross", revisions["misteross"])
                    recipe_sha = digest(recipe_source / "scripts/rebuild_core.py")
                    manifest = core_bundle.load(bundle_dir, recipe_sha)
                    if profile.get("bundle_interface") == "selection":
                        run(child_make + ["target-image-native",
                            "LIBMISTER_RUNTIME_DIR=" + str(runtime),
                            *bundle_arguments(cores, bundles)], env=env)
                    else:
                        run([fogcast / "scripts/target-image-container.sh", "fetch",
                             "/work/scripts/fetch-native-runtime-inputs.sh"], env=env)
                        core_bundle.prepare(fogcast, bundle_dir, recipe_sha)
                        run(child_make + ["build-agent"], env=env)
                        run([fogcast / "scripts/build-target-image.sh", "--fetch", "native-dev"],
                            env=dict(env, LIBMISTER_RUNTIME_DIR=str(runtime)))
                        run([fogcast / "scripts/build-target-image.sh", "native-dev"],
                            env=dict(env, LIBMISTER_RUNTIME_DIR=str(runtime)))
                else:
                    manifest = None
                    run(child_make + ["target-image-native", "LIBMISTER_RUNTIME_DIR=" + str(runtime)], env=env)
                # Verification reads the runtime commit from the image/lock; no source mount required.
                run(child_make + ["target-image-native-verify"],
                    env=dict(env, NATIVE_RUNTIME_SYSTEMS=" ".join(cores)))
                built = fogcast / "build/output/target-image/native-dev"
                names = ["linux.img", "reproducibility.txt", "manifest.tsv", "library-report.tsv"]
                if profile.get("bundle_interface") == "selection":
                    names.extend(core + ".selection.toml" for core in cores)
                for name in names:
                    publish_file(built / name, output / name)
                if manifest is not None:
                    for core in cores:
                        for name in (core + "-rbf.toml", core + ".rbf"):
                            publish_file(bundles[core] / name, output / name)
                            names.append(name)
                write_receipt(output, "image", fp, names)
        if args.action == "verify":
            if not reusable(output, "host", fp) or not reusable(output, "image", fp):
                raise ValueError("build outputs are missing, changed, or stale; run make build")
            if profile.get("fpga_source") == "misteross":
                recipe_source = source_checkout("misteross", revisions["misteross"])
                recipe_sha = digest(recipe_source / "scripts/rebuild_core.py")
                for core in cores:
                    validate_bundle(output, recipe_source, revisions["misteross"], core)
                # A new invocation starts from the pinned FogCast commit. Recreate the
                # generated lock/cache overlay from the published bundle before asking
                # FogCast to verify the already-built image.
                if profile.get("bundle_interface") != "selection":
                    core_bundle.prepare(fogcast, output, recipe_sha)
            built = fogcast / "build/output/target-image/native-dev/linux.img"
            if not built.is_file() or digest(built) != digest(output / "linux.img"):
                raise ValueError("child image differs from published image; run make rebuild")
            run(child_make + ["target-image-native-verify"],
                    env=dict(env, NATIVE_RUNTIME_SYSTEMS=" ".join(cores)))
            run(child_make + ["target-image-native-qemu-smoke"],
                env=dict(env, NATIVE_RUNTIME_SYSTEMS=" ".join(cores)))
            actual = digest(output / "linux.img")
            evidence = dict(line.split("=", 1) for line in (output / "reproducibility.txt").read_text().splitlines())
            if evidence.get("run_1_sha256") != actual or evidence.get("run_2_sha256") != actual:
                raise ValueError("image does not match both recorded build passes")
            baseline = profile.get("baseline_image_sha256")
            matches = None if baseline is None else actual == baseline
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
