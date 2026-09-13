#!/usr/bin/env python3
"""Build the pinned native system using the FES image recipe."""
import argparse
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import stat
import subprocess
import sys
import tomllib
import tempfile

from inputs import git, validate
import bundle as core_bundle
from build_diagnostics import BuildDiagnostics
from environment import build_environment

ROOT = Path(__file__).resolve().parents[1]
IMAGE = ROOT / "image"
DIAGNOSTIC_RECIPE_NAMES = frozenset({"scripts/build_diagnostics.py"})


def is_diagnostic_recipe_file(path, root=ROOT):
    try:
        return path.relative_to(root).as_posix() in DIAGNOSTIC_RECIPE_NAMES
    except ValueError:
        return False


MEDIA_RECIPE_FILES = tuple(ROOT / name for name in (
    "scripts/media.py", "scripts/media_inputs.py", "scripts/media_container.py",
    "scripts/media_inside.py", "scripts/prepare_launcher.py", "boot-media.lock.toml",
    "scripts/appliance.py", "scripts/appliance_inside.py",
    "scripts/appliance_media.py", "scripts/appliance_media_inside.py",
    "containers/boot-media/Dockerfile", "containers/boot-media/create-builder-user.sh",
    "containers/boot-media/packages.sha256"))
BUILD_RECIPE_FILES = tuple(path for path in sorted((ROOT / "scripts").glob("*.py"))
                           if path not in MEDIA_RECIPE_FILES and not is_diagnostic_recipe_file(path))
IMAGE_RECIPE_NAMES = (
    "Makefile",
    "build/target-image.sources.lock.toml",
    "build/target-image-container-packages.sha256",
    "build/target-image-kernel-defconfig.sha256",
)
IMAGE_RECIPE_DIRS = ("buildroot", "containers/target-image", "scripts")
HOST_RECIPE_FILES = tuple(ROOT / name for name in (
    "scripts/build.py", "scripts/environment.py"))


def image_recipe_files(root=ROOT):
    image = root / "image"
    paths = [image / name for name in IMAGE_RECIPE_NAMES if (image / name).is_file()]
    for directory in IMAGE_RECIPE_DIRS:
        base = image / directory
        if not base.is_dir():
            continue
        paths.extend(path for path in sorted(base.rglob("*")) if path.is_file()
                     and "tests" not in path.parts)
    return tuple(path for path in paths if not is_diagnostic_recipe_file(path, root))

def run(args, **kwargs):
    print("+ " + " ".join(map(str, args)), flush=True)
    return subprocess.run(list(map(str, args)), check=True, **kwargs)


def run_stage(diagnostics, name, args, **kwargs):
    if diagnostics is None:
        return run(args, **kwargs)
    with diagnostics.measure(name):
        return run(args, **kwargs)


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

HOST_PLATFORMS = frozenset({('linux', 'amd64'), ('darwin', 'arm64'), ('darwin', 'amd64')})


def host_platform(os_name=None, arch=None):
    if os_name is None and arch is None:
        os_name, arch = 'linux', 'amd64'
    if (os_name, arch) not in HOST_PLATFORMS:
        raise ValueError('host receipt platform must be linux/amd64 or darwin/arm64 or darwin/amd64')
    return os_name, arch


def write_receipt(output, kind, fingerprint, names, os_name=None, arch=None):
    data = {"inputs": fingerprint, "files": {name: digest(output / name) for name in names}}
    if kind in ('host', 'image'):
        data['fes_revision'] = git(ROOT, 'rev-parse', 'HEAD')
        receipt_revision(data)
    if kind == 'host':
        os_name, arch = host_platform(os_name, arch)
        data['os'] = os_name
        data['arch'] = arch
    temporary = output / (kind + ".json.tmp")
    temporary.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n")
    temporary.replace(output / (kind + ".json"))

def receipt_revision(receipt):
    revision = receipt.get('fes_revision') if isinstance(receipt, dict) else None
    if type(revision) is not str or not re.fullmatch('[0-9a-f]{40}', revision):
        raise ValueError('cold artifact receipt requires a canonical FES revision')
    return revision


def reuse_status(output, kind, fingerprint):
    """Return whether a receipt is reusable and a safe explanation."""
    output = Path(output)
    try:
        raw = (output / (kind + '.json')).read_text()
    except FileNotFoundError:
        return False, 'receipt missing'
    except (OSError, UnicodeDecodeError):
        return False, 'receipt malformed'
    try:
        receipt = json.loads(raw)
    except (json.JSONDecodeError, UnicodeDecodeError):
        return False, 'receipt malformed'
    if not isinstance(receipt, dict):
        return False, 'receipt metadata invalid'
    if kind in ('host', 'image'):
        try:
            receipt_revision(receipt)
        except (ValueError, TypeError):
            return False, 'receipt metadata invalid'
    if kind == 'host':
        if set(receipt) != {'inputs', 'files', 'fes_revision', 'os', 'arch'}:
            return False, 'receipt metadata invalid'
        try:
            host_platform(receipt['os'], receipt['arch'])
        except (KeyError, TypeError, ValueError):
            return False, 'receipt metadata invalid'
    if receipt.get('inputs') != fingerprint:
        return False, 'selected inputs changed'
    files = receipt.get('files')
    if not isinstance(files, dict):
        return False, 'receipt metadata invalid'
    if not files:
        return False, 'receipt has no outputs'
    try:
        for name, expected in files.items():
            if not isinstance(name, str):
                return False, 'receipt metadata invalid'
            relative = Path(name)
            if (relative.is_absolute() or '..' in relative.parts or type(expected) is not str
                    or not re.fullmatch('[0-9a-f]{64}', expected)):
                return False, 'receipt metadata invalid'
            metadata = (output / relative).lstat()
            if not stat.S_ISREG(metadata.st_mode):
                return False, 'output missing or digest changed'
            if digest(output / name) != expected:
                return False, 'output missing or digest changed'
    except (OSError, TypeError, ValueError, AttributeError):
        return False, 'output missing or digest changed'
    return True, 'verified receipt and output digests match selected inputs'


def reusable(output, kind, fingerprint):
    """Retain the boolean receipt-reuse API for existing callers."""
    return reuse_status(output, kind, fingerprint)[0]


def _host_profile(profile):
    selected = {key: value for key, value in profile.items()
                if key == 'version' or key.startswith('host_')}
    os_name, arch = host_platform(selected.get('host_os'), selected.get('host_arch'))
    selected['host_os'] = os_name
    selected['host_arch'] = arch
    return selected


def host_fingerprint(revisions, profile, toolchain):
    """Fingerprint only inputs that can affect the host binaries."""
    data = {
        'sources': {'FogCast': revisions['FogCast']},
        'profile': _host_profile(profile),
        'go': toolchain,
        'recipe': recipe_fingerprint(HOST_RECIPE_FILES),
    }
    return hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest(), data


def recipe_fingerprint(paths):
    return {str(path.relative_to(ROOT)): digest(path) for path in paths}


def build_fingerprint(revisions, profile, toolchain):
    host_profile = dict(profile)
    host_profile.pop("fpga_packages", None)
    data = {"sources": revisions, "profile": host_profile, "go": toolchain,
            "recipe": recipe_fingerprint(BUILD_RECIPE_FILES),
            "image_recipe": recipe_fingerprint(image_recipe_files())}
    return hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest(), data


def fingerprint(revisions, profile, toolchain):
    """Compatibility wrapper for callers of the original receipt API."""
    return build_fingerprint(revisions, profile, toolchain)


def verification_record(output, image_sha256, baseline_match):
    return {"image_sha256": image_sha256, "historical_baseline_match": baseline_match,
            "two_pass_reproducibility": "pass", "structural": "pass", "qemu_packaging": "pass",
            "qemu_log_sha256": digest(output / "qemu-smoke.log")}


def load_verified_host(output, fingerprint, os_name='linux', arch='amd64'):
    """Require the exact current host receipt for one OS/arch product."""
    output = Path(output)
    os_name, arch = host_platform(os_name, arch)
    try:
        for name in ('host.json', 'fogcast', 'fogcast-api'):
            if not stat.S_ISREG((output / name).lstat().st_mode):
                raise ValueError('host output is not regular')
        def unique(pairs):
            result = {}
            for key, value in pairs:
                if key in result:
                    raise ValueError('duplicate host receipt field')
                result[key] = value
            return result
        receipt = json.loads((output / 'host.json').read_text(), object_pairs_hook=unique)
        hashes = {name: digest(output / name) for name in ('fogcast', 'fogcast-api')}
        revision = receipt_revision(receipt)
        if receipt != {'inputs': fingerprint, 'files': hashes, 'fes_revision': revision,
                       'os': os_name, 'arch': arch}:
            raise ValueError('host receipt differs from selected inputs or binaries')
        return {'fes_revision': revision, 'host_receipt_sha256': digest(output / 'host.json'),
                'fogcast_sha256': hashes['fogcast'], 'fogcast_api_sha256': hashes['fogcast-api'],
                'os': os_name, 'arch': arch}
    except (OSError, ValueError, TypeError):
        raise ValueError('cold host receipt is missing, changed, or stale; run make build and make verify') from None


def load_verified_image(output, fingerprint):
    selected_fingerprint = fingerprint
    if not reusable(output, "image", selected_fingerprint):
        try:
            inputs = json.loads((Path(output) / "inputs.json").read_text())
            derived = recorded_image_fingerprint(inputs)
            if (inputs.get("image_base_fingerprint") != fingerprint or
                    not reusable(output, "image", derived)):
                raise ValueError
            selected_fingerprint = derived
        except (OSError, ValueError, KeyError, TypeError, json.JSONDecodeError):
            raise ValueError("cold image receipt is missing or stale; run make build and make verify") from None
    try:
        receipt = json.loads((output / "image.json").read_text())
        verification = json.loads((output / "verification.json").read_text())
        actual = digest(output / "linux.img")
        qemu_log_sha256 = digest(output / "qemu-smoke.log")
        evidence = dict(line.split("=", 1) for line in
                        (output / "reproducibility.txt").read_text().splitlines())
        required = (receipt["files"]["linux.img"] == actual
                    and verification.get("image_sha256") == actual
                    and verification.get("structural") == "pass"
                    and verification.get("qemu_packaging") == "pass"
                    and verification.get("qemu_log_sha256") == qemu_log_sha256
                    and verification.get("two_pass_reproducibility") == "pass"
                    and evidence.get("run_1_sha256") == actual
                    and evidence.get("run_2_sha256") == actual)
        if required:
            return {"fes_revision": receipt_revision(receipt), "rootfs_sha256": actual,
                    "image_receipt_sha256": digest(output / "image.json"),
                    "verification_sha256": digest(output / "verification.json"),
                    "qemu_log_sha256": qemu_log_sha256}
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        pass
    raise ValueError("cold image verification is missing or stale; run make verify")

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
    if cores not in (["megadrive"], ["megadrive", "pong", "snes", "nes"]):
        raise ValueError("profile must select megadrive or megadrive, pong, snes, nes")
    return tuple(cores)


def selected_packages(profile, profile_name):
    packages = profile.get("fpga_packages", [])
    if not isinstance(packages, list):
        raise ValueError("fpga_packages must be an array of tables")
    if not packages:
        return ()
    if (profile_name != "native-integration-dev" or packages != [{"core_id": "fes.pong"}]):
        raise ValueError("only native-integration-dev may select the fes.pong package recipe")
    return ("fes.pong",)


def package_arguments(package):
    if package is None:
        return []
    return ["FES_PONG_PACKAGE_DIR=" + str(package["directory"]),
            "FES_PONG_PACKAGE_SELECTION=" + str(package["selection_path"])]


def image_fingerprint(base_fingerprint, info, package):
    if package is None:
        return base_fingerprint, dict(info)
    package_inputs = package["inputs"]
    data = {"base_fingerprint": base_fingerprint, "fpga_packages": [package_inputs]}
    fingerprint = hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest()
    enriched = dict(info, fpga_packages=[package_inputs],
                    image_base_fingerprint=base_fingerprint, image_fingerprint=fingerprint)
    return fingerprint, enriched


def recorded_image_fingerprint(info):
    """Recover the receipt key from a persisted base or package image record."""
    packages = info.get("fpga_packages")
    if packages is None:
        return hashlib.sha256(json.dumps(info, sort_keys=True).encode()).hexdigest()
    base = info.get("image_base_fingerprint")
    if type(base) is not str or type(packages) is not list or len(packages) != 1:
        raise ValueError("invalid persisted package image inputs")
    fingerprint = hashlib.sha256(json.dumps({
        "base_fingerprint": base, "fpga_packages": packages,
    }, sort_keys=True).encode()).hexdigest()
    if info.get("image_fingerprint") != fingerprint:
        raise ValueError("persisted package image fingerprint differs from its inputs")
    return fingerprint


def package_output_names(package):
    identity = package["inputs"]["selection"]["package_id"]
    if not re.fullmatch(r"[0-9a-f]{64}", identity):
        raise ValueError("selected package has an invalid package ID")
    prefix = "core-packages/" + identity + "/"
    return ["fes-pong.package-selection.toml", prefix + "manifest.toml", prefix + "core.rbf"]


def verify_package_outputs(output, package):
    """Require a closed parent copy of the exact selected package bytes."""
    output = Path(output)
    if package is None:
        try:
            for path in (output / "fes-pong.package-selection.toml",
                         output / "core-packages"):
                try:
                    path.lstat()
                except FileNotFoundError:
                    continue
                raise ValueError
        except (OSError, ValueError):
            raise ValueError("package-free output contains stale FES Pong package files") from None
        return []
    names = package_output_names(package)
    identity = package["inputs"]["selection"]["package_id"]
    root = output / "core-packages"
    directory = root / identity
    try:
        for path in (root, directory):
            metadata = path.lstat()
            if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
                raise ValueError
        if sorted(entry.name for entry in root.iterdir()) != [identity]:
            raise ValueError
        if sorted(entry.name for entry in directory.iterdir()) != ["core.rbf", "manifest.toml"]:
            raise ValueError
        expected = {
            output / names[0]: package["inputs"]["selection_sha256"],
            output / names[1]: package["inputs"]["manifest_sha256"],
            output / names[2]: package["inputs"]["core_rbf_sha256"],
        }
        for path, expected_sha256 in expected.items():
            metadata = path.lstat()
            if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
                raise ValueError
            if digest(path) != expected_sha256:
                raise ValueError
    except (OSError, ValueError):
        raise ValueError("published FES Pong package changed or differs from its selection") from None
    return names


def _remove_package_outputs(output):
    """Remove a previous package pair without following output symlinks."""
    output = Path(output)
    selection = output / "fes-pong.package-selection.toml"
    root = output / "core-packages"
    present = []
    for path, expected in ((selection, stat.S_ISREG), (root, stat.S_ISDIR)):
        try:
            metadata = path.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISLNK(metadata.st_mode) or not expected(metadata.st_mode):
            raise ValueError("package output destination must be a non-symlink regular file or directory")
        present.append(path)
    if selection in present:
        selection.unlink()
    if root in present:
        for current, directories, files in os.walk(root, topdown=False, followlinks=False):
            for name in files + directories:
                path = Path(current) / name
                metadata = path.lstat()
                if not stat.S_ISLNK(metadata.st_mode):
                    path.chmod(0o755 if stat.S_ISDIR(metadata.st_mode) else 0o644)
        root.chmod(0o755)
        shutil.rmtree(root)


def publish_package_state(package, built_selection, output):
    """Publish the selected pair, or close a successful package-free output."""
    if package is None:
        _remove_package_outputs(output)
        return verify_package_outputs(output, None)
    return publish_package_outputs(package, built_selection, output)


def publish_action_inputs(output, action, info):
    """Publish image inputs only for actions that own image output."""
    if action not in ("build", "image", "rebuild"):
        return False
    output = Path(output)
    temporary = output / "inputs.json.tmp"
    temporary.write_text(json.dumps(info, indent=2, sort_keys=True) + "\n")
    temporary.replace(output / "inputs.json")
    return True


def publish_package_outputs(package, built_selection, output):
    """Publish the child-emitted record and an exact closed package directory."""
    output = Path(output)
    built_selection = Path(built_selection)
    names = package_output_names(package)
    try:
        metadata = built_selection.lstat()
        if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
            raise ValueError
        if built_selection.read_bytes() != Path(package["selection_path"]).read_bytes():
            raise ValueError
    except (OSError, ValueError):
        raise ValueError("child FES Pong selection differs from selected inputs") from None
    identity = package["inputs"]["selection"]["package_id"]
    staged_root = output / ".core-packages.new"
    if staged_root.exists() or staged_root.is_symlink():
        if staged_root.is_symlink() or not staged_root.is_dir():
            raise ValueError("package staging destination must be a non-symlink directory")
        shutil.rmtree(staged_root)
    staged = staged_root / identity
    staged.mkdir(parents=True)
    try:
        for name in ("manifest.toml", "core.rbf"):
            shutil.copy2(Path(package["directory"]) / name, staged / name)
            (staged / name).chmod(0o444)
        staged.chmod(0o555)
        staged_root.chmod(0o555)
        old_root = output / "core-packages"
        if old_root.exists() or old_root.is_symlink():
            if old_root.is_symlink() or not old_root.is_dir():
                raise ValueError("package output destination must be a non-symlink directory")
            for path in old_root.rglob("*"):
                if not path.is_symlink():
                    path.chmod(0o755 if path.is_dir() else 0o644)
            old_root.chmod(0o755)
            shutil.rmtree(old_root)
        staged_root.replace(old_root)
        publish_file(built_selection, output / names[0])
        verify_package_outputs(output, package)
        return names
    finally:
        if staged_root.exists():
            for path in staged_root.rglob("*"):
                if not path.is_symlink():
                    path.chmod(0o755 if path.is_dir() else 0o644)
            staged_root.chmod(0o755)
            shutil.rmtree(staged_root)


def bundle_arguments(cores, bundles):
    if set(cores) != set(bundles):
        raise ValueError("bundle set differs from selected cores")
    return ["NATIVE_RUNTIME_SYSTEMS=" + " ".join(cores), "MEGADRIVE_RBF_SOURCE=source-built"] + [
        core.upper() + "_RBF_BUNDLE=" + str(bundles[core]) for core in cores]


def validate_bundle(directory, source, revision, system):
    recipe = "scripts/build_pong.py" if system == "pong" else "scripts/rebuild_core.py"
    return core_bundle.load(directory, digest(source / recipe), system=system,
                            expected_revision=revision if system == "pong" else None)


def build_bundle(revisions, env, force=False, *, system="megadrive", diagnostics=None):
    if system not in ("megadrive", "pong", "snes", "nes"):
        raise ValueError("unsupported FPGA core")
    source = source_checkout("misteross", revisions["misteross"])
    directory = source / "build/bundles" / system
    bundles = list(directory.glob(f"*/{system}-rbf.toml"))
    if bundles and not force:
        if len(bundles) != 1:
            raise ValueError(f"misteross has more than one cached {system} bundle")
        bundle_dir = bundles[0].parent
        validate_bundle(bundle_dir, source, revisions["misteross"], system)
        if diagnostics is not None:
            diagnostics.cache("fpga:" + system, "hit",
                              "validated revision-scoped FPGA bundle")
        print(f"Reusing validated revision-scoped FPGA bundle: {bundle_dir}", flush=True)
        return bundle_dir
    if diagnostics is not None:
        diagnostics.cache("fpga:" + system, "forced" if force else "miss",
                          "validated bundle missing" if not force else "forced rebuild")
    if not env.get("QUARTUS_ROOTDIR"):
        raise ValueError("source-built cores require QUARTUS_ROOTDIR")
    if system == "pong":
        run_stage(diagnostics, "fpga:" + system + " subprocess",
                  ["make", "-C", source, "build-pong"], env=env)
    else:
        run_stage(diagnostics, "fpga:" + system + " subprocess",
                  ["make", "-C", source, "fetch-core", "CORE=" + system], env=env)
        run_stage(diagnostics, "fpga:" + system + " subprocess",
                  ["make", "-C", source, "rebuild-core", "CORE=" + system], env=env)
    run_stage(diagnostics, "fpga:" + system + " subprocess",
              ["make", "-C", source, "export-core-bundle", "CORE=" + system], env=env)
    bundles = list(directory.glob(f"*/{system}-rbf.toml"))
    if len(bundles) != 1:
        raise ValueError(f"misteross did not produce exactly one {system} bundle")
    validate_bundle(bundles[0].parent, source, revisions["misteross"], system)
    return bundles[0].parent


def build_bundles(revisions, env, cores, force=False, diagnostics=None):
    return {core: build_bundle(revisions, env, force, system=core, diagnostics=diagnostics)
            for core in cores}


@contextmanager
def locked_diagnostics(root, output, action):
    """Acquire the parent lock before creating or updating diagnostics."""
    lock_path = Path(root) / "out/build.lock"
    with lock_path.open("w") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError("another parent build is running in this workspace") from None
        with BuildDiagnostics(output, action) as diagnostics:
            yield lock, diagnostics


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["doctor", "build", "host", "image", "verify", "rebuild", "dev"])
    parser.add_argument("--profile", default="native-integration-dev",
                        choices=["native-dev", "native-source-dev", "native-integration-dev"])
    args = parser.parse_args()
    profile = tomllib.loads((ROOT / "profiles" / (args.profile + ".toml")).read_text())
    revisions = validate(ROOT, profile)
    cores = selected_cores(profile)
    package_recipes = selected_packages(profile, args.profile)
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
    with locked_diagnostics(ROOT, output, args.action) as (lock, diagnostics):
        with diagnostics.measure("source staging"):
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
        fp, info = build_fingerprint(revisions, profile, toolchain)
        host_fp, _ = host_fingerprint(revisions, profile, toolchain)
        package = None
        image_fp, image_info = fp, info
        if package_recipes and args.action in ("build", "image", "verify", "rebuild", "dev"):
            recipe_source = source_checkout("misteross", revisions["misteross"])
            package = core_bundle.resolve_core_package(
                recipe_source, revisions["mister-packages"],
                output / "fes-pong.package-selection.toml")
            image_fp, image_info = image_fingerprint(fp, info, package)
        env["TARGET_IMAGE_CONTAINER_RUNTIME"] = container
        env["TARGET_IMAGE_OUTPUT_VOLUME"] = output_volume(ROOT, args.profile)
        env["FOGCAST_DIR"] = str(fogcast)
        # The host has a small /tmp tmpfs; Go temporary files belong in out/.
        temp = ROOT / "out/tmp"
        temp.mkdir(exist_ok=True)
        env["GOTMPDIR"] = str(temp)
        fogcast_make = ["make", "-C", fogcast, "CONTAINER_RUNTIME=" + container, "VERSION=" + profile["version"]]
        image_make = ["make", "-C", IMAGE, "FOGCAST_DIR=" + str(fogcast),
                      "CONTAINER_RUNTIME=" + container]
        if args.action == "dev":
            if profile.get("bundle_interface") != "selection":
                raise ValueError("make dev requires native-integration-dev; historical profiles stay cold")
            from native_dev import build_development
            runtime = source_checkout("libmister-runtime", revisions["libmister-runtime"])
            bundles = build_bundles(revisions, env, cores, diagnostics=diagnostics)
            build_development(ROOT, IMAGE, fogcast, runtime, args.profile, profile, image_info,
                              image_fp, env, fogcast_make, image_make, bundles, package,
                              diagnostics=diagnostics)
            return
        if args.action in ("build", "host", "rebuild"):
            host_hit, host_reason = (False, "forced rebuild") if args.action == "rebuild" else reuse_status(
                output, "host", host_fp)
            if host_hit:
                diagnostics.cache("host", "hit", host_reason)
                print("Host: reusing verified output", flush=True)
            else:
                diagnostics.cache("host", "forced" if args.action == "rebuild" else "miss", host_reason)
                run_stage(diagnostics, "host subprocess", fogcast_make + ["build-fogcast", "FOGCAST_GOOS=" + profile["host_os"],
                    "FOGCAST_GOARCH=" + profile["host_arch"],
                    "FOGCAST_OUTPUT=" + str(output / "fogcast"),
                    "REVISION=" + revisions["FogCast"]], env=env)
                run_stage(diagnostics, "host subprocess", fogcast_make + ["build-fogcast-api", "FOGCAST_GOOS=" + profile["host_os"],
                    "FOGCAST_GOARCH=" + profile["host_arch"],
                    "FOGCAST_API_OUTPUT=" + str(output / "fogcast-api"),
                    "REVISION=" + revisions["FogCast"]], env=env)
                write_receipt(output, "host", host_fp, ["fogcast", "fogcast-api"],
                              os_name=profile["host_os"], arch=profile["host_arch"])
        if args.action in ("build", "image", "rebuild"):
            image_hit, image_reason = (False, "forced rebuild") if args.action == "rebuild" else reuse_status(
                output, "image", image_fp)
            if image_hit:
                diagnostics.cache("image", "hit", image_reason)
                print("Image: reusing verified output", flush=True)
                verify_package_outputs(output, package)
                if profile.get("fpga_source") == "misteross":
                    recipe_source = source_checkout("misteross", revisions["misteross"])
                    for core in cores:
                        validate_bundle(output, recipe_source, revisions["misteross"], core)
            else:
                diagnostics.cache("image", "forced" if args.action == "rebuild" else "miss", image_reason)
                runtime = source_checkout("libmister-runtime", revisions["libmister-runtime"])
                source_lock = tomllib.loads((IMAGE / "build/target-image.sources.lock.toml").read_text())
                base = source_lock["container"]
                ref = base["image"] + "@" + base["digest"]
                present = subprocess.run([container, "image", "inspect", ref], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                if present.returncode:
                    run_stage(diagnostics, "image subprocess", [container, "pull", "--platform", base["platform"], ref], env=env)
                run_stage(diagnostics, "image subprocess", fogcast_make + ["build-agent", "build-fogcast-kit"], env=env)
                run_stage(diagnostics, "image subprocess", image_make + ["build-target-image-lock-container"], env=env)
                run_stage(diagnostics, "image subprocess", [IMAGE / "scripts/target-image-container.sh", "fetch",
                     "/work/scripts/fetch-target-image-sources.sh"], env=env)
                if profile.get("fpga_source") == "misteross":
                    bundles = build_bundles(revisions, env, cores, args.action == "rebuild",
                                            diagnostics=diagnostics)
                    bundle_dir = bundles["megadrive"]
                    recipe_source = source_checkout("misteross", revisions["misteross"])
                    recipe_sha = digest(recipe_source / "scripts/rebuild_core.py")
                    manifest = core_bundle.load(bundle_dir, recipe_sha)
                    if profile.get("bundle_interface") == "selection":
                        run_stage(diagnostics, "image subprocess", image_make + ["target-image-native",
                            "LIBMISTER_RUNTIME_DIR=" + str(runtime),
                            *bundle_arguments(cores, bundles), *package_arguments(package)], env=env)
                    else:
                        run_stage(diagnostics, "image subprocess", [IMAGE / "scripts/target-image-container.sh", "fetch",
                             "/work/scripts/fetch-native-runtime-inputs.sh"], env=env)
                        core_bundle.prepare(fogcast, bundle_dir, recipe_sha, cache_root=IMAGE)
                        run_stage(diagnostics, "image subprocess", [IMAGE / "scripts/build-target-image.sh", "--fetch", "native-dev"],
                            env=dict(env, LIBMISTER_RUNTIME_DIR=str(runtime)))
                        run_stage(diagnostics, "image subprocess", [IMAGE / "scripts/build-target-image.sh", "native-dev"],
                            env=dict(env, LIBMISTER_RUNTIME_DIR=str(runtime)))
                else:
                    manifest = None
                    run_stage(diagnostics, "image subprocess",
                              image_make + ["target-image-native", "LIBMISTER_RUNTIME_DIR=" + str(runtime)], env=env)
                # Verification reads the runtime commit from the image/lock; no source mount required.
                run_stage(diagnostics, "image subprocess", image_make + ["target-image-native-verify"],
                    env=dict(env, NATIVE_RUNTIME_SYSTEMS=" ".join(cores),
                             **dict(argument.split("=", 1) for argument in package_arguments(package))))
                built = IMAGE / "build/output/target-image/native-dev"
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
                names.extend(publish_package_state(
                    package,
                    built / "fes-pong.package-selection.toml" if package is not None else None,
                    output))
                publish_action_inputs(output, args.action, image_info)
                names.append("inputs.json")
                write_receipt(output, "image", image_fp, names)
        if args.action == "verify":
            host_hit, host_reason = reuse_status(output, "host", host_fp)
            image_hit, image_reason = reuse_status(output, "image", image_fp)
            diagnostics.cache("verify host", "hit" if host_hit else "miss", host_reason)
            diagnostics.cache("verify image", "hit" if image_hit else "miss", image_reason)
            if not host_hit or not image_hit:
                raise ValueError("build outputs are missing, changed, or stale; run make build")
            verify_package_outputs(output, package)
            if profile.get("fpga_source") == "misteross":
                recipe_source = source_checkout("misteross", revisions["misteross"])
                recipe_sha = digest(recipe_source / "scripts/rebuild_core.py")
                for core in cores:
                    validate_bundle(output, recipe_source, revisions["misteross"], core)
                # Recreate the generated lock/cache overlay from the published bundle
                # before verifying the already-built image.
                if profile.get("bundle_interface") != "selection":
                    core_bundle.prepare(fogcast, output, recipe_sha, cache_root=IMAGE)
            built = IMAGE / "build/output/target-image/native-dev/linux.img"
            if not built.is_file() or digest(built) != digest(output / "linux.img"):
                raise ValueError("child image differs from published image; run make rebuild")
            run_stage(diagnostics, "verification subprocess", image_make + ["target-image-native-verify"],
                    env=dict(env, NATIVE_RUNTIME_SYSTEMS=" ".join(cores),
                             **dict(argument.split("=", 1) for argument in package_arguments(package))))
            run_stage(diagnostics, "verification subprocess", image_make + ["target-image-native-qemu-smoke"],
                env=dict(env, NATIVE_RUNTIME_SYSTEMS=" ".join(cores),
                         **dict(argument.split("=", 1) for argument in package_arguments(package))))
            shutil.copy2(IMAGE / "build/output/target-image/native-dev/qemu-smoke.log", output / "qemu-smoke.log")
            actual = digest(output / "linux.img")
            evidence = dict(line.split("=", 1) for line in (output / "reproducibility.txt").read_text().splitlines())
            if evidence.get("run_1_sha256") != actual or evidence.get("run_2_sha256") != actual:
                raise ValueError("image does not match both recorded build passes")
            baseline = profile.get("baseline_image_sha256")
            matches = None if baseline is None else actual == baseline
            result = verification_record(output, actual, matches)
            (output / "verification.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
            print("Two-pass reproducibility, structural and QEMU packaging checks passed.", flush=True)
            print(f"Historical image hash match: {matches} (see README provenance note)", flush=True)
if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(f"fes: {error}", file=sys.stderr)
        sys.exit(1)
