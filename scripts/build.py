#!/usr/bin/env python3
"""Build the pinned native system using the FES image recipe."""
import argparse
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
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
from recipes import FORMAT2_RECIPES, recipe_for

ROOT = Path(__file__).resolve().parents[1]
IMAGE = ROOT / "image"
DIAGNOSTIC_RECIPE_NAMES = frozenset({"scripts/build_diagnostics.py"})
FPGA_BUNDLE_CACHE = ROOT / "out/cache/fpga-bundles"


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
    core_ids = []
    for entry in packages:
        if not isinstance(entry, dict) or type(entry.get("core_id")) is not str:
            raise ValueError("fpga_packages entries must be tables with a core_id")
        core_id = entry["core_id"]
        if core_id not in FORMAT2_RECIPES:
            supported = ", ".join(FORMAT2_RECIPES)
            raise ValueError(f"unknown format-2 recipe {core_id!r}; supported: {supported}")
        core_ids.append(core_id)
    if len(core_ids) != len(set(core_ids)):
        raise ValueError("duplicate format-2 package selections are not supported")
    if profile_name != "native-integration-dev":
        raise ValueError("format-2 packages are only selected by native-integration-dev")
    return tuple(core_ids)


def _package_tuple(packages):
    """Normalize compatibility inputs to the ordered package tuple."""
    if packages is None:
        return ()
    if isinstance(packages, dict):
        return (packages,)
    if not isinstance(packages, (tuple, list)):
        raise ValueError("selected format-2 packages must be an ordered tuple")
    return tuple(packages)


def _package_details(packages):
    normalized = _package_tuple(packages)
    details = []
    core_ids = set()
    package_ids = set()
    for package in normalized:
        if not isinstance(package, dict):
            raise ValueError("selected format-2 package must be a record")
        try:
            selection = package["inputs"]["selection"]
            core_id = selection["core_id"]
            identity = selection["package_id"]
            recipe = recipe_for(core_id)
        except (KeyError, TypeError):
            raise ValueError("selected format-2 package is malformed") from None
        if not isinstance(identity, str) or not re.fullmatch(r"[0-9a-f]{64}", identity):
            raise ValueError("selected package has an invalid package ID")
        if core_id in core_ids:
            raise ValueError("duplicate format-2 package selections are not supported")
        if identity in package_ids:
            raise ValueError("duplicate format-2 package IDs are not supported")
        core_ids.add(core_id)
        package_ids.add(identity)
        details.append((package, recipe, identity))
    return normalized, tuple(details)


def package_arguments(packages):
    normalized, details = _package_details(packages)
    if not normalized:
        return []
    arguments = ["FES_PACKAGE_IDS=" + ",".join(recipe.core_id for _, recipe, _ in details)]
    for package, recipe, _ in details:
        arguments.extend((recipe.package_dir_env + "=" + str(package["directory"]),
                          recipe.package_selection_env + "=" + str(package["selection_path"])))
    return arguments


def image_fingerprint(base_fingerprint, info, packages):
    normalized = _package_tuple(packages)
    if not normalized:
        return base_fingerprint, dict(info)
    package_inputs = [package["inputs"] for package in normalized]
    _validate_package_input_records(package_inputs)
    data = {"base_fingerprint": base_fingerprint, "fpga_packages": package_inputs}
    fingerprint = hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest()
    enriched = dict(info, fpga_packages=package_inputs,
                    image_base_fingerprint=base_fingerprint, image_fingerprint=fingerprint)
    return fingerprint, enriched


def recorded_image_fingerprint(info):
    """Recover the receipt key from a persisted base or package image record."""
    packages = info.get("fpga_packages")
    if packages is None:
        return hashlib.sha256(json.dumps(info, sort_keys=True).encode()).hexdigest()
    base = info.get("image_base_fingerprint")
    if type(base) is not str or type(packages) is not list or not packages:
        raise ValueError("invalid persisted package image inputs")
    _validate_package_input_records(packages)
    fingerprint = hashlib.sha256(json.dumps({
        "base_fingerprint": base, "fpga_packages": packages,
    }, sort_keys=True).encode()).hexdigest()
    if info.get("image_fingerprint") != fingerprint:
        raise ValueError("persisted package image fingerprint differs from its inputs")
    return fingerprint


def _validate_package_input_records(packages):
    identities = set()
    for package in packages:
        try:
            identity = package["selection"]["package_id"]
        except (KeyError, TypeError):
            raise ValueError("invalid persisted package image inputs") from None
        if type(identity) is not str or not re.fullmatch(r"[0-9a-f]{64}", identity):
            raise ValueError("invalid persisted package image inputs")
        if identity in identities:
            raise ValueError("duplicate format-2 package IDs in persisted image inputs")
        identities.add(identity)


def package_output_names(package):
    normalized, details = _package_details(package)
    if len(normalized) != 1:
        raise ValueError("package output names require exactly one selected package")
    _, recipe, identity = details[0]
    prefix = "core-packages/" + identity + "/"
    return [recipe.selection_filename, prefix + "manifest.toml", prefix + "core.rbf"]


def package_selection_names(packages):
    """Return selected child record names in package order."""
    _, details = _package_details(packages)
    return tuple(recipe.selection_filename for _, recipe, _ in details)


FORMAT1_PARENT_OUTPUT_NAMES = (
    'megadrive.rbf', 'megadrive-rbf.toml', 'megadrive.selection.toml',
    'pong.rbf', 'pong-rbf.toml', 'pong.selection.toml',
    'snes.rbf', 'snes-rbf.toml', 'snes.selection.toml',
    'nes.rbf', 'nes-rbf.toml', 'nes.selection.toml',
)

_PACKAGE_GENERATION_MARKER = '.package-generation.complete'
_PACKAGE_RESTORE_STAGING = '.package-generation.restore'
_PACKAGE_SELECTION_SUFFIX = '.package-selection.toml'
_PACKAGE_BACKUP_CLEANUP_PREFIX = '.package-generation.previous.cleanup-'


def remove_format1_parent_outputs(output):
    """Remove only known legacy top-level artifacts from a package output."""
    output = Path(output)
    for name in FORMAT1_PARENT_OUTPUT_NAMES:
        path = output / name
        try:
            metadata = path.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
            raise ValueError('legacy format-1 output destination is not a regular file')
        path.unlink()


def verify_package_only_outputs(output, packages):
    """Verify a parent package-only output has no legacy core artifacts."""
    verify_package_outputs(output, packages)
    output = Path(output)
    for name in FORMAT1_PARENT_OUTPUT_NAMES:
        try:
            output.joinpath(name).lstat()
        except FileNotFoundError:
            continue
        raise ValueError('package-only output contains a legacy format-1 artifact')


def _format2_selection_paths(output):
    output = Path(output)
    return tuple(path for path in output.iterdir()
                 if path.name.endswith('.package-selection.toml'))


def verify_package_outputs(output, packages):
    """Require a closed parent copy of the exact selected package bytes."""
    output = Path(output)
    normalized, details = _package_details(packages)
    if not normalized:
        try:
            for path in list(_format2_selection_paths(output)) + [output / "core-packages"]:
                try:
                    path.lstat()
                except FileNotFoundError:
                    continue
                raise ValueError
        except (OSError, ValueError):
            raise ValueError("package-free output contains stale FES package files") from None
        return []
    names = []
    expected_selection_names = {recipe.selection_filename for _, recipe, _ in details}
    for path in _format2_selection_paths(output):
        try:
            metadata = path.lstat()
            if (path.name not in expected_selection_names or
                    stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode)):
                raise ValueError
        except (OSError, ValueError):
            raise ValueError("published FES package selections are not a closed set") from None
    expected_identities = {identity for _, _, identity in details}
    root = output / "core-packages"
    try:
        metadata = root.lstat()
        if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
            raise ValueError
        if {entry.name for entry in root.iterdir()} != expected_identities:
            raise ValueError
        for package, recipe, identity in details:
            directory = root / identity
            for path in (directory,):
                metadata = path.lstat()
                if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
                    raise ValueError
            if sorted(entry.name for entry in directory.iterdir()) != ["core.rbf", "manifest.toml"]:
                raise ValueError
            package_names = [recipe.selection_filename,
                             f"core-packages/{identity}/manifest.toml",
                             f"core-packages/{identity}/core.rbf"]
            expected = {
                output / package_names[0]: package["inputs"]["selection_sha256"],
                output / package_names[1]: package["inputs"]["manifest_sha256"],
                output / package_names[2]: package["inputs"]["core_rbf_sha256"],
            }
            for path, expected_sha256 in expected.items():
                metadata = path.lstat()
                if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
                    raise ValueError
                if digest(path) != expected_sha256:
                    raise ValueError
            names.extend(package_names)
    except (OSError, ValueError):
        raise ValueError("published FES package changed or differs from its selection") from None
    return names


def _remove_sealed_tree(path):
    """Remove a package-owned sealed tree without following links."""
    path = Path(path)
    try:
        metadata = path.lstat()
    except FileNotFoundError:
        return
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        raise ValueError("package staging path must be a non-symlink directory")
    for current, directories, files in os.walk(path, topdown=False, followlinks=False):
        current_path = Path(current)
        current_path.chmod(0o755)
        for name in files + directories:
            child = current_path / name
            metadata = child.lstat()
            if stat.S_ISLNK(metadata.st_mode):
                raise ValueError("package staging path must not contain symlinks")
            if stat.S_ISDIR(metadata.st_mode):
                child.chmod(0o755)
            elif stat.S_ISREG(metadata.st_mode):
                child.chmod(0o644)
            else:
                raise ValueError("package staging path must contain only regular files and directories")
    path.chmod(0o755)
    shutil.rmtree(path)


def _package_tree_inventory(root, prefix='core-packages', *, closed=False):
    """Return a safe inventory, optionally requiring a closed package tree."""
    root = Path(root)
    try:
        metadata = root.lstat()
    except FileNotFoundError:
        return (), ()
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        raise ValueError("package output tree must be a non-symlink directory")
    directories = [prefix]
    files = []

    if not closed:
        def visit(directory, relative):
            for child in sorted(directory.iterdir(), key=lambda path: path.name):
                metadata = child.lstat()
                child_relative = relative + '/' + child.name
                if stat.S_ISLNK(metadata.st_mode):
                    raise ValueError("package output tree must not contain symlinks")
                if stat.S_ISDIR(metadata.st_mode):
                    directories.append(child_relative)
                    visit(child, child_relative)
                elif stat.S_ISREG(metadata.st_mode):
                    files.append(child_relative)
                else:
                    raise ValueError(
                        "package output tree must contain only regular files and directories")

        visit(root, prefix)
        return tuple(sorted(directories)), tuple(sorted(files))

    for child in sorted(root.iterdir(), key=lambda path: path.name):
        metadata = child.lstat()
        if stat.S_ISLNK(metadata.st_mode):
            raise ValueError("package output tree must not contain symlinks")
        if not stat.S_ISDIR(metadata.st_mode):
            raise ValueError("package output tree must contain only package directories")
        if not re.fullmatch(r"[0-9a-f]{64}", child.name):
            raise ValueError("package output tree contains an invalid package identity")
        child_relative = prefix + '/' + child.name
        directories.append(child_relative)
        entries = sorted(child.iterdir(), key=lambda path: path.name)
        for entry in entries:
            metadata = entry.lstat()
            if stat.S_ISLNK(metadata.st_mode):
                raise ValueError("package output tree must not contain symlinks")
            if not stat.S_ISREG(metadata.st_mode):
                raise ValueError(
                    "package generation contains nested or special entries")
        if [entry.name for entry in entries] != ["core.rbf", "manifest.toml"]:
            raise ValueError("package directories must contain the closed two-file set")
        files.extend(child_relative + '/' + name for name in ("manifest.toml", "core.rbf"))
    return tuple(sorted(directories)), tuple(sorted(files))


def _package_output_inventory(output, *, closed=False):
    """Inspect only the package-owned destinations in a parent output."""
    output = Path(output)
    files = []
    for selection in sorted(_format2_selection_paths(output), key=lambda path: path.name):
        metadata = selection.lstat()
        if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
            raise ValueError("package selection destination must be a non-symlink regular file")
        files.append(selection.name)
    directories, package_files = _package_tree_inventory(
        output / 'core-packages', closed=closed)
    files.extend(package_files)
    return directories, tuple(sorted(files))


def _package_generation_inventory(generation):
    """Inspect a staged or backup generation, rejecting every unsafe entry."""
    generation = Path(generation)
    try:
        metadata = generation.lstat()
    except FileNotFoundError:
        raise ValueError("package generation is missing") from None
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        raise ValueError("package generation must be a non-symlink directory")
    directories = []
    files = []
    for child in sorted(generation.iterdir(), key=lambda path: path.name):
        metadata = child.lstat()
        if child.name == _PACKAGE_GENERATION_MARKER:
            if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
                raise ValueError("package generation marker must be a regular file")
            continue
        if child.name == 'core-packages':
            package_directories, package_files = _package_tree_inventory(
                child, closed=True)
            directories.extend(package_directories)
            files.extend(package_files)
        elif child.name.endswith(_PACKAGE_SELECTION_SUFFIX):
            if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
                raise ValueError("package generation selection must be a regular file")
            files.append(child.name)
        else:
            raise ValueError("package generation contains an unexpected entry")
    return tuple(sorted(directories)), tuple(sorted(files))


def _validate_package_generation_shape(generation, directories, files):
    """Require an empty generation or a complete generic package set."""
    if not directories and not files:
        return
    package_root_present = 'core-packages' in directories
    package_directories = [relative for relative in directories
                           if relative.startswith('core-packages/')
                           and relative.count('/') == 1]
    selection_files = [relative for relative in files
                       if '/' not in relative and
                       relative.endswith(_PACKAGE_SELECTION_SUFFIX)]
    if (not package_root_present or not package_directories or
            len(package_directories) != len(selection_files)):
        raise ValueError(
            f"{generation} is not an empty or complete package generation")


def _manifest_path(value, field):
    if type(value) is not str or not value or '\\' in value:
        raise ValueError(f"package generation manifest has an invalid {field} path")
    path = PurePosixPath(value)
    if (path.is_absolute() or path.as_posix() != value or
            any(part in ('', '.', '..') for part in path.parts)):
        raise ValueError(f"package generation manifest has an invalid {field} path")
    return value


def _read_package_generation_manifest(backup):
    """Read and normalize the durable marker for a package backup."""
    marker = Path(backup) / _PACKAGE_GENERATION_MARKER
    try:
        metadata = marker.lstat()
    except FileNotFoundError:
        raise ValueError("package backup is not marked complete") from None
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
        raise ValueError("package backup marker must be a non-symlink regular file")
    try:
        data = json.loads(marker.read_text())
    except (OSError, TypeError, ValueError):
        raise ValueError("package backup marker is malformed") from None
    return _normalize_package_generation_manifest(data)


def _normalize_package_generation_manifest(data):
    """Validate and normalize a package generation manifest."""
    if not isinstance(data, dict) or data.get('format') != 1:
        raise ValueError("package backup marker format differs")
    raw_directories = data.get('directories')
    raw_files = data.get('files')
    if (not isinstance(raw_directories, (list, tuple)) or
            not isinstance(raw_files, (list, tuple))):
        raise ValueError("package backup marker entries are malformed")
    directories = []
    for value in raw_directories:
        value = _manifest_path(value, 'directory')
        if value != 'core-packages' and not value.startswith('core-packages/'):
            raise ValueError("package backup marker contains an unexpected directory")
        if value in directories:
            raise ValueError("package backup marker contains duplicate directories")
        directories.append(value)
    files = []
    paths = set()
    for entry in raw_files:
        if isinstance(entry, dict):
            value = _manifest_path(entry.get('path'), 'file')
            checksum = entry.get('sha256')
        elif isinstance(entry, (list, tuple)) and len(entry) == 2:
            value = _manifest_path(entry[0], 'file')
            checksum = entry[1]
        else:
            raise ValueError("package backup marker files are malformed")
        if type(checksum) is not str or not re.fullmatch(r'[0-9a-f]{64}', checksum):
            raise ValueError("package backup marker has an invalid file digest")
        if value in paths:
            raise ValueError("package backup marker contains duplicate files")
        paths.add(value)
        files.append((value, checksum))
    return {
        'format': 1,
        'directories': tuple(sorted(directories)),
        'files': tuple(sorted(files)),
    }


def _validate_package_generation(generation, manifest, *, require_marker=False, sealed=False):
    """Validate a complete generation against its marker manifest."""
    generation = Path(generation)
    if not (isinstance(manifest, dict) and
            isinstance(manifest.get('directories'), tuple) and
            isinstance(manifest.get('files'), tuple)):
        manifest = _normalize_package_generation_manifest(manifest)
    marker = generation / _PACKAGE_GENERATION_MARKER
    try:
        metadata = marker.lstat()
    except FileNotFoundError:
        if require_marker:
            raise ValueError("package generation is not marked complete") from None
        metadata = None
    if metadata is not None and (stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode)):
        raise ValueError("package generation marker must be a non-symlink regular file")
    directories, files = _package_generation_inventory(generation)
    _validate_package_generation_shape(generation, directories, files)
    expected_directories = tuple(manifest['directories'])
    expected_files = tuple(path for path, _ in manifest['files'])
    if directories != expected_directories or files != expected_files:
        raise ValueError("package backup entries are not a complete closed generation")
    for relative, expected in manifest['files']:
        path = generation / relative
        if digest(path) != expected:
            raise ValueError("package backup file differs from its manifest")
    if sealed:
        paths = [generation / relative for relative in directories + files]
        if metadata is not None:
            paths.append(marker)
        for path in paths:
            if stat.S_IMODE(path.lstat().st_mode) & 0o222:
                raise ValueError("package backup generation is not sealed")
    return manifest


def _package_generation_manifest(generation):
    generation = Path(generation)
    directories, files = _package_generation_inventory(generation)
    _validate_package_generation_shape(generation, directories, files)
    return _normalize_package_generation_manifest({
        'format': 1,
        'directories': list(directories),
        'files': [
            {'path': relative, 'sha256': digest(generation / relative)}
            for relative in files
        ],
    })


def _fsync_regular_file(path):
    """Fsync one regular file without following a replacement symlink."""
    path = Path(path)
    metadata = path.lstat()
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
        raise ValueError("package durability path must be a regular file")
    flags = os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0)
    descriptor = os.open(str(path), flags)
    try:
        opened = os.fstat(descriptor)
        if stat.S_ISLNK(opened.st_mode) or not stat.S_ISREG(opened.st_mode):
            raise ValueError("package durability path must be a regular file")
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _fsync_directory(path):
    """Fsync one directory without following a replacement symlink."""
    path = Path(path)
    metadata = path.lstat()
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        raise ValueError("package durability path must be a directory")
    flags = (os.O_RDONLY | getattr(os, 'O_DIRECTORY', 0) |
             getattr(os, 'O_NOFOLLOW', 0))
    descriptor = os.open(str(path), flags)
    try:
        opened = os.fstat(descriptor)
        if stat.S_ISLNK(opened.st_mode) or not stat.S_ISDIR(opened.st_mode):
            raise ValueError("package durability path must be a directory")
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _fsync_relative_tree(root, directories, files, include_marker=False):
    """Fsync every file and containing directory in a package-owned tree."""
    root = Path(root)
    for relative in files:
        _fsync_regular_file(root / relative)
    if include_marker:
        marker = root / _PACKAGE_GENERATION_MARKER
        try:
            marker.lstat()
        except FileNotFoundError:
            pass
        else:
            _fsync_regular_file(marker)
    for relative in sorted(directories,
                          key=lambda value: (value.count('/'), value),
                          reverse=True):
        _fsync_directory(root / relative)
    _fsync_directory(root)


def _fsync_package_generation(generation):
    """Fsync a staged or marked package generation and its payload."""
    generation = Path(generation)
    directories, files = _package_generation_inventory(generation)
    _fsync_relative_tree(generation, directories, files, include_marker=True)


def _fsync_package_output(output):
    """Fsync the live package payload and every containing directory."""
    output = Path(output)
    directories, files = _package_output_inventory(output, closed=True)
    _fsync_relative_tree(output, directories, files)


def _cleanup_package_backup(output, backup):
    """Atomically move a backup aside before best-effort deletion."""
    output = Path(output)
    backup = Path(backup)
    try:
        metadata = backup.lstat()
    except FileNotFoundError:
        return
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        raise ValueError("package backup path must be a non-symlink directory")

    disposable = None
    handed_off = False
    try:
        disposable = Path(tempfile.mkdtemp(
            prefix=_PACKAGE_BACKUP_CLEANUP_PREFIX, dir=output))
        disposable.rmdir()
        backup.replace(disposable)
        handed_off = True
        _fsync_directory(output)
    except BaseException:
        if not handed_off and disposable is not None:
            try:
                disposable.rmdir()
            except BaseException:
                pass
        return

    try:
        _remove_sealed_tree(disposable)
    except BaseException:
        return
    try:
        _fsync_directory(output)
    except BaseException:
        pass


def _write_package_generation_manifest(backup, manifest):
    """Publish the complete marker only after the copied generation is valid."""
    backup = Path(backup)
    temporary = backup / (_PACKAGE_GENERATION_MARKER + '.tmp')
    data = {
        'format': manifest['format'],
        'directories': list(manifest['directories']),
        'files': [
            {'path': relative, 'sha256': checksum}
            for relative, checksum in manifest['files']
        ],
    }
    temporary.write_text(json.dumps(data, sort_keys=True) + '\n')
    temporary.chmod(0o444)
    _fsync_regular_file(temporary)
    temporary.replace(backup / _PACKAGE_GENERATION_MARKER)
    _fsync_regular_file(backup / _PACKAGE_GENERATION_MARKER)
    _fsync_directory(backup)


def _copy_package_tree(source, destination):
    """Copy a validated package tree without following links."""
    source = Path(source)
    destination = Path(destination)
    metadata = source.lstat()
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        raise ValueError("package output tree must be a non-symlink directory")
    destination.mkdir()
    for child in sorted(source.iterdir(), key=lambda path: path.name):
        child_destination = destination / child.name
        metadata = child.lstat()
        if stat.S_ISLNK(metadata.st_mode):
            raise ValueError("package output tree must not contain symlinks")
        if stat.S_ISDIR(metadata.st_mode):
            _copy_package_tree(child, child_destination)
        elif stat.S_ISREG(metadata.st_mode):
            shutil.copy2(child, child_destination, follow_symlinks=False)
        else:
            raise ValueError("package output tree must contain only regular files and directories")


def _copy_package_outputs(output, destination):
    """Copy current package destinations into a backup generation."""
    output = Path(output)
    destination = Path(destination)
    _package_output_inventory(output)
    try:
        _validate_complete_package_output(output)
    except ValueError:
        return
    metadata = destination.lstat()
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        raise ValueError("package backup path must be a non-symlink directory")
    root = output / 'core-packages'
    try:
        root.lstat()
    except FileNotFoundError:
        pass
    else:
        _copy_package_tree(root, destination / 'core-packages')
    for selection in sorted(_format2_selection_paths(output), key=lambda path: path.name):
        shutil.copy2(selection, destination / selection.name, follow_symlinks=False)


def _copy_package_generation(source, destination):
    """Copy a validated backup generation into restore staging."""
    source = Path(source)
    destination = Path(destination)
    destination.mkdir()
    root = source / 'core-packages'
    try:
        root.lstat()
    except FileNotFoundError:
        pass
    else:
        _copy_package_tree(root, destination / 'core-packages')
    for selection in sorted(_format2_selection_paths(source), key=lambda path: path.name):
        shutil.copy2(selection, destination / selection.name, follow_symlinks=False)
    shutil.copy2(source / _PACKAGE_GENERATION_MARKER,
                 destination / _PACKAGE_GENERATION_MARKER, follow_symlinks=False)


def _validate_package_output_manifest(output, manifest):
    """Validate live output against a previously sealed generation."""
    output = Path(output)
    directories, files = _package_output_inventory(output, closed=True)
    expected_directories = tuple(manifest['directories'])
    expected_files = tuple(path for path, _ in manifest['files'])
    if directories != expected_directories or files != expected_files:
        raise ValueError("restored package output is not the complete prior generation")
    for relative, expected in manifest['files']:
        if digest(output / relative) != expected:
            raise ValueError("restored package output differs from the prior generation")


def _set_package_tree_modes(path, directory_mode, file_mode):
    """Set package-tree modes without traversing symlinks or special files."""
    path = Path(path)
    try:
        metadata = path.lstat()
    except FileNotFoundError:
        return
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        raise ValueError("package output tree must be a non-symlink directory")
    for current, directories, files in os.walk(path, topdown=False, followlinks=False):
        current_path = Path(current)
        current_path.chmod(directory_mode)
        for name in files + directories:
            child = current_path / name
            metadata = child.lstat()
            if stat.S_ISLNK(metadata.st_mode):
                raise ValueError("package output tree must not contain symlinks")
            if stat.S_ISDIR(metadata.st_mode):
                child.chmod(directory_mode)
            elif stat.S_ISREG(metadata.st_mode):
                child.chmod(file_mode)
            else:
                raise ValueError("package output tree must contain only regular files and directories")
    path.chmod(directory_mode)


def _make_package_tree_writable(path):
    _set_package_tree_modes(path, 0o755, 0o644)


def _seal_package_tree(path):
    _set_package_tree_modes(path, 0o555, 0o444)


def _validate_package_output_destinations(output):
    """Validate existing package destinations before a transactional publish."""
    _package_output_inventory(output)


def _validate_complete_package_output(output):
    """Validate the live output as a generic complete package generation."""
    output = Path(output)
    directories, files = _package_output_inventory(output, closed=True)
    _validate_package_generation_shape(output, directories, files)
    return directories, files


def _clean_package_staging(output):
    """Clean sealed staging left by an interrupted current or prior publisher."""
    output = Path(output)
    for name in (".package-generation.new", _PACKAGE_RESTORE_STAGING,
                 ".core-packages.new", ".package-selections.new"):
        path = output / name
        try:
            metadata = path.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
            raise ValueError("package staging destination must be a non-symlink directory")
        _remove_sealed_tree(path)


def _restore_package_backup(output, backup, clear_current=True):
    """Restore the prior package generation from a publish backup directory."""
    output = Path(output)
    backup = Path(backup)
    manifest = _read_package_generation_manifest(backup)
    _validate_package_generation(backup, manifest, require_marker=True, sealed=True)
    restore = output / _PACKAGE_RESTORE_STAGING
    try:
        try:
            restore.lstat()
        except FileNotFoundError:
            pass
        else:
            _remove_sealed_tree(restore)
        _copy_package_generation(backup, restore)
        _fsync_package_generation(restore)
        _fsync_directory(output)
        _validate_package_generation(restore, manifest, require_marker=True)
        if clear_current:
            _remove_package_outputs(output)
        elif _package_output_inventory(output) != ((), ()):
            raise ValueError("current package output must be empty before restore")
        restore_root = restore / 'core-packages'
        try:
            restore_root.lstat()
        except FileNotFoundError:
            pass
        else:
            restore_root.replace(output / 'core-packages')
            _seal_package_tree(output / 'core-packages')
        for selection in sorted(_format2_selection_paths(restore), key=lambda path: path.name):
            selection.replace(output / selection.name)
        _validate_package_output_manifest(output, manifest)
        _fsync_package_output(output)
    finally:
        try:
            _remove_sealed_tree(restore)
        except BaseException:
            pass


def _recover_package_backup(output, packages):
    """Recover an interrupted package replacement before starting a publish."""
    output = Path(output)
    backup = output / ".package-generation.previous"
    try:
        metadata = backup.lstat()
    except FileNotFoundError:
        return
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        raise ValueError("package backup path must be a non-symlink directory")
    marker = backup / _PACKAGE_GENERATION_MARKER
    try:
        marker.lstat()
    except FileNotFoundError:
        try:
            _validate_complete_package_output(output)
        except (OSError, KeyError, TypeError, ValueError):
            raise ValueError("unmarked package backup cannot be used for recovery") from None
        _fsync_package_output(output)
        _cleanup_package_backup(output, backup)
        _fsync_directory(output)
        return
    manifest = _read_package_generation_manifest(backup)
    _validate_package_generation(backup, manifest, require_marker=True, sealed=True)
    try:
        verify_package_outputs(output, packages)
    except (OSError, KeyError, TypeError, ValueError):
        _restore_package_backup(output, backup)
    else:
        _fsync_package_output(output)
    _cleanup_package_backup(output, backup)
    _fsync_directory(output)


def _remove_package_outputs(output):
    """Remove a previous package pair without following output symlinks."""
    output = Path(output)
    _package_output_inventory(output)
    selections = list(_format2_selection_paths(output))
    root = output / "core-packages"
    present = []
    for path, expected in [(path, stat.S_ISREG) for path in selections] + [(root, stat.S_ISDIR)]:
        try:
            metadata = path.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISLNK(metadata.st_mode) or not expected(metadata.st_mode):
            raise ValueError("package output destination must be a non-symlink regular file or directory")
        present.append(path)
    for path in selections:
        if path in present:
            path.unlink()
    if root in present:
        _remove_sealed_tree(root)


def publish_package_state(packages, built_selections, output):
    """Publish the selected pair, or close a successful package-free output."""
    if not _package_tuple(packages):
        _remove_package_outputs(output)
        return verify_package_outputs(output, None)
    return publish_package_outputs(packages, built_selections, output)


def publish_action_inputs(output, action, info):
    """Publish image inputs only for actions that own image output."""
    if action not in ("build", "image", "rebuild"):
        return False
    output = Path(output)
    temporary = output / "inputs.json.tmp"
    temporary.write_text(json.dumps(info, indent=2, sort_keys=True) + "\n")
    temporary.replace(output / "inputs.json")
    return True


def publish_package_outputs(packages, built_selections, output):
    """Publish the child-emitted record and an exact closed package directory."""
    output = Path(output)
    normalized, details = _package_details(packages)
    expected_selection_names = [recipe.selection_filename for _, recipe, _ in details]
    if isinstance(built_selections, dict) and len(normalized) > 1:
        selections = built_selections
    elif len(normalized) == 1 and not isinstance(built_selections, dict):
        selections = {expected_selection_names[0]: built_selections}
    else:
        selections = built_selections
    if not isinstance(selections, dict) or set(selections) != set(expected_selection_names):
        raise ValueError("child FES package selections are not a complete set")
    try:
        for package, recipe, _ in details:
            built_selection = Path(selections[recipe.selection_filename])
            selected_path = Path(package["selection_path"])
            metadata = built_selection.lstat()
            if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
                raise ValueError
            if (built_selection.read_bytes() != selected_path.read_bytes() or
                    digest(selected_path) != package["inputs"]["selection_sha256"]):
                raise ValueError
            for name, field in (("manifest.toml", "manifest_sha256"),
                                ("core.rbf", "core_rbf_sha256")):
                if digest(Path(package["directory"]) / name) != package["inputs"][field]:
                    raise ValueError
    except (OSError, KeyError, TypeError, ValueError):
        raise ValueError("child FES package selection differs from selected inputs") from None
    _recover_package_backup(output, normalized)
    _clean_package_staging(output)
    staged_generation = output / ".package-generation.new"
    staged_root = staged_generation / "core-packages"
    staged_selections = staged_generation
    staged_generation.mkdir()
    staged_root.mkdir()
    backup_created = False
    backup_complete = False
    backup = output / ".package-generation.previous"
    try:
        for package, recipe, identity in details:
            staged = staged_root / identity
            staged.mkdir()
            for name in ("manifest.toml", "core.rbf"):
                shutil.copy2(Path(package["directory"]) / name, staged / name)
                (staged / name).chmod(0o444)
            staged.chmod(0o555)
            selection_stage = staged_selections / recipe.selection_filename
            shutil.copy2(selections[recipe.selection_filename], selection_stage)
            selection_stage.chmod(0o444)
        staged_root.chmod(0o555)
        staged_generation.chmod(0o755)
        verify_package_outputs(staged_generation, normalized)
        _fsync_package_generation(staged_generation)
        _validate_package_output_destinations(output)
        backup.mkdir()
        backup_created = True
        backup.chmod(0o755)
        _copy_package_outputs(output, backup)
        if (backup / 'core-packages').exists():
            _seal_package_tree(backup / 'core-packages')
        for selection in _format2_selection_paths(backup):
            selection.chmod(0o444)
        manifest = _package_generation_manifest(backup)
        _validate_package_generation(backup, manifest, sealed=True)
        _fsync_package_generation(backup)
        _write_package_generation_manifest(backup, manifest)
        backup.chmod(0o555)
        _validate_package_generation(
            backup, _read_package_generation_manifest(backup),
            require_marker=True, sealed=True)
        _fsync_directory(output)
        _fsync_directory(backup)
        backup_complete = True
        staged_root.chmod(0o755)
        old_root = output / 'core-packages'
        _remove_package_outputs(output)
        staged_root.replace(old_root)
        old_root.chmod(0o555)
        for selection_name in expected_selection_names:
            (staged_selections / selection_name).replace(output / selection_name)
        names = []
        for package, recipe, identity in details:
            names.extend([recipe.selection_filename,
                          f"core-packages/{identity}/manifest.toml",
                          f"core-packages/{identity}/core.rbf"])
        verify_package_outputs(output, normalized)
        _fsync_package_output(output)
        backup_complete = False
        _cleanup_package_backup(output, backup)
        _fsync_directory(output)
        return names
    except BaseException:
        if backup_created:
            if backup_complete:
                try:
                    _restore_package_backup(output, backup)
                except BaseException:
                    pass
                else:
                    _cleanup_package_backup(output, backup)
            else:
                _cleanup_package_backup(output, backup)
        raise
    finally:
        try:
            _remove_sealed_tree(staged_generation)
        except BaseException:
            if sys.exc_info()[0] is None:
                raise


def bundle_arguments(cores, bundles):
    if set(cores) != set(bundles):
        raise ValueError("bundle set differs from selected cores")
    return ["NATIVE_RUNTIME_SYSTEMS=" + " ".join(cores), "MEGADRIVE_RBF_SOURCE=source-built"] + [
        core.upper() + "_RBF_BUNDLE=" + str(bundles[core]) for core in cores]


def _bundle_write_bits(mode):
    return bool(stat.S_IMODE(mode) & (stat.S_IWUSR | stat.S_IWGRP | stat.S_IWOTH))


def _closed_bundle_digest(directory, system):
    hasher = hashlib.sha256()
    for name in (f"{system}.rbf", f"{system}-rbf.toml"):
        encoded = name.encode("utf-8")
        data = (Path(directory) / name).read_bytes()
        hasher.update(len(encoded).to_bytes(8, "big"))
        hasher.update(encoded)
        hasher.update(len(data).to_bytes(8, "big"))
        hasher.update(data)
    return hasher.hexdigest()


def _bundle_directories(root, system):
    root = Path(root)
    try:
        metadata = root.lstat()
    except FileNotFoundError:
        return ()
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        return ()
    base = root / system
    try:
        metadata = base.lstat()
    except FileNotFoundError:
        return ()
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        return ()
    candidates = []
    for child in sorted(base.iterdir(), key=lambda path: path.name):
        try:
            metadata = child.lstat()
        except FileNotFoundError:
            continue
        if (child.name.startswith(".") or stat.S_ISLNK(metadata.st_mode)
                or not stat.S_ISDIR(metadata.st_mode)):
            continue
        candidates.append(child)
    return tuple(candidates)


def _require_closed_bundle(directory, system, *, sealed=False):
    directory = Path(directory)
    try:
        metadata = directory.lstat()
    except FileNotFoundError as exc:
        raise ValueError("bundle directory must exist") from exc
    if stat.S_ISLNK(metadata.st_mode):
        raise ValueError("bundle directory must not be a symlink")
    if not stat.S_ISDIR(metadata.st_mode):
        raise ValueError("bundle path must be a directory")
    if sealed and _bundle_write_bits(metadata.st_mode):
        raise ValueError("sealed bundle directory must not be writable")
    expected = {f"{system}.rbf", f"{system}-rbf.toml"}
    names = []
    for child in sorted(directory.iterdir(), key=lambda path: path.name):
        try:
            metadata = child.lstat()
        except FileNotFoundError as exc:
            raise ValueError("sealed bundle files must exist") from exc
        if child.name.startswith("."):
            raise ValueError("sealed bundle has unexpected files")
        names.append(child.name)
        if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode):
            raise ValueError("bundle files must be non-symlink regular files")
        if sealed and _bundle_write_bits(metadata.st_mode):
            raise ValueError("sealed bundle files must not be writable")
    if set(names) != expected:
        raise ValueError("sealed bundle must contain the closed two-file set")


def _require_sealed_bundle(directory, system):
    _require_closed_bundle(directory, system, sealed=True)


def _remove_tree(path):
    path = Path(path)
    if path.is_symlink():
        path.unlink()
        return
    if not path.exists():
        return
    if not path.is_dir():
        path.unlink()
        return
    for current, directories, _ in os.walk(path, topdown=False, followlinks=False):
        for name in directories:
            child = Path(current) / name
            metadata = child.lstat()
            if not stat.S_ISLNK(metadata.st_mode):
                child.chmod(0o755)
    path.chmod(0o755)
    shutil.rmtree(path)


def publish_bundle_cache(directory, system):
    directory = Path(directory)
    _require_closed_bundle(directory, system, sealed=False)
    names = (f"{system}.rbf", f"{system}-rbf.toml")
    dest_root = Path(FPGA_BUNDLE_CACHE)
    if dest_root.is_symlink():
        raise ValueError("FPGA bundle cache must not be a symlink")
    if dest_root.exists() and not dest_root.is_dir():
        raise ValueError("FPGA bundle cache must be a directory")
    system_root = dest_root / system
    destination = None
    staged = None
    try:
        dest_root.mkdir(parents=True, exist_ok=True)
        if system_root.is_symlink():
            raise ValueError("FPGA bundle cache system directory must not be a symlink")
        if system_root.exists() and not system_root.is_dir():
            raise ValueError("FPGA bundle cache system directory must be a directory")
        system_root.mkdir(exist_ok=True)
        destination = system_root / _closed_bundle_digest(directory, system)
        if destination.is_symlink():
            raise ValueError(f"FPGA bundle cache destination must not be a symlink: {destination}")
        if destination.exists():
            try:
                _require_sealed_bundle(destination, system)
            except ValueError as exc:
                raise ValueError(f"FPGA bundle cache destination is not sealed: {destination}: {exc}") from exc
            for name in names:
                if (destination / name).read_bytes() != (directory / name).read_bytes():
                    raise ValueError(f"existing FPGA bundle cache entry differs: {destination}")
            return destination
        staged = Path(tempfile.mkdtemp(prefix=".new-", dir=system_root))
        for name in names:
            shutil.copy2(directory / name, staged / name)
            (staged / name).chmod(0o444)
        staged.chmod(0o555)
        staged.replace(destination)
        return destination
    except OSError as exc:
        target = destination if destination is not None else system_root
        raise OSError(f"FPGA bundle cache publication failed: {target}: {exc}") from exc
    finally:
        if staged is not None and (staged.exists() or staged.is_symlink()):
            try:
                _remove_tree(staged)
            except OSError:
                pass


def validate_bundle(directory, source, revision, system):
    recipe = "scripts/build_pong.py" if system == "pong" else "scripts/rebuild_core.py"
    return core_bundle.load(directory, digest(source / recipe), system=system,
                            expected_revision=revision if system == "pong" else None)


def _validated_bundle_candidates(source, revision, system, diagnostics=None):
    source = Path(source)
    selected_root = source / "build/bundles" / system
    selected = []
    if selected_root.exists() or selected_root.is_symlink():
        try:
            metadata = selected_root.lstat()
        except FileNotFoundError:
            metadata = None
        else:
            if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
                raise ValueError(f"misteross {system} bundle directory must be a non-symlink directory")
            bundles = list(selected_root.glob(f"*/{system}-rbf.toml"))
            if bundles:
                if len(bundles) != 1:
                    raise ValueError(f"misteross has more than one cached {system} bundle")
                bundle_dir = bundles[0].parent
                selected.append((bundle_dir, validate_bundle(bundle_dir, source, revision, system)))
    stable = []
    try:
        stable_dirs = _bundle_directories(FPGA_BUNDLE_CACHE, system)
    except OSError as exc:
        reason = f"stable cache skipped: {FPGA_BUNDLE_CACHE / system}: {exc}"
        if diagnostics is not None:
            diagnostics.cache("fpga:" + system, "miss", reason)
        print(reason, flush=True)
        stable_dirs = ()
    for candidate in stable_dirs:
        try:
            _require_sealed_bundle(candidate, system)
            manifest = validate_bundle(candidate, source, revision, system)
        except ValueError:
            continue
        except OSError as exc:
            reason = f"stable candidate skipped: {candidate}: {exc}"
            if diagnostics is not None:
                diagnostics.cache("fpga:" + system, "miss", reason)
            print(reason, flush=True)
            continue
        stable.append((candidate, manifest))
    pairs = selected + stable
    if not pairs:
        return ()
    if len(pairs) == 1:
        return (pairs[0][0],)
    by_digest = {}
    for path, manifest in pairs:
        digest_value = manifest.get("sha256") if isinstance(manifest, dict) else None
        if not isinstance(digest_value, str) or not digest_value:
            raise ValueError(f"validated {system} bundle is missing sha256")
        if digest_value not in by_digest:
            by_digest[digest_value] = path
    if len(by_digest) > 1:
        listed = ", ".join(str(path) for path, _ in pairs)
        raise ValueError(f"ambiguous validated {system} FPGA bundles: {listed}")
    return tuple(by_digest.values())


def build_bundle(revisions, env, force=False, *, system="megadrive", diagnostics=None):
    if system not in ("megadrive", "pong", "snes", "nes"):
        raise ValueError("unsupported FPGA core")
    source = source_checkout("misteross", revisions["misteross"])
    if not force:
        candidates = _validated_bundle_candidates(
            source, revisions["misteross"], system, diagnostics)
        if candidates:
            bundle_dir = candidates[0]
            try:
                Path(bundle_dir).relative_to(FPGA_BUNDLE_CACHE)
                reason = "validated stable-cache FPGA bundle"
            except ValueError:
                reason = "validated selected-checkout FPGA bundle"
            if diagnostics is not None:
                diagnostics.cache("fpga:" + system, "hit", reason)
            print(f"Reusing {reason}: {bundle_dir}", flush=True)
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
    directory = source / "build/bundles" / system
    bundles = list(directory.glob(f"*/{system}-rbf.toml"))
    if len(bundles) != 1:
        raise ValueError(f"misteross did not produce exactly one {system} bundle")
    validate_bundle(bundles[0].parent, source, revisions["misteross"], system)
    bundle_dir = bundles[0].parent
    try:
        publish_bundle_cache(bundle_dir, system)
    except (ValueError, OSError) as exc:
        reason = f"stable publish skipped: {exc}"
        if diagnostics is not None:
            diagnostics.cache("fpga:" + system, "miss", reason)
        print(reason, flush=True)
    return bundle_dir


def build_bundles(revisions, env, cores, force=False, diagnostics=None):
    return {core: build_bundle(revisions, env, force, system=core, diagnostics=diagnostics)
            for core in cores}


def native_image_mode(profile):
    """Return the explicit native image lane selected by a profile."""
    mode = profile.get('native_image_mode', 'format1')
    if mode not in ('format1', 'package-only'):
        raise ValueError('profile native_image_mode must be format1 or package-only')
    return mode


def build_bundles_for_profile(profile, revisions, env, cores, force=False, diagnostics=None):
    """Dispatch legacy FPGA bundles only for an explicit historical lane."""
    if native_image_mode(profile) == 'package-only':
        return {}
    return build_bundles(revisions, env, cores, force, diagnostics=diagnostics)


def legacy_source_image_environment(env, runtime, mode):
    """Bind the selected native lane when invoking the legacy image scripts."""
    return dict(env, LIBMISTER_RUNTIME_DIR=str(runtime), NATIVE_RUNTIME_MODE=mode)


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


def resolve_selected_package(revisions, selection_path, env, force=False, recipe=None):
    recipe_source = source_checkout("misteross", revisions["misteross"])
    return core_bundle.resolve_core_package(
        recipe_source, revisions["mister-packages"], selection_path,
        force=force, env=env, recipe=recipe)


def resolve_package_for_action(revisions, output, env, action, recipe):
    """Resolve the selected package, rebuilding it for an explicit rebuild."""
    return resolve_selected_package(
        revisions, Path(output) / recipe.selection_filename, env,
        force=action == 'rebuild', recipe=recipe)


def resolve_packages_for_action(revisions, output, env, action, package_ids):
    """Resolve every selected format-2 recipe in profile order."""
    return tuple(resolve_package_for_action(
        revisions, output, env, action, recipe_for(package_id))
        for package_id in package_ids)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["doctor", "build", "host", "image", "verify", "rebuild", "dev"])
    parser.add_argument("--profile", default="native-integration-dev",
                        choices=["native-dev", "native-source-dev", "native-integration-dev"])
    args = parser.parse_args()
    profile = tomllib.loads((ROOT / "profiles" / (args.profile + ".toml")).read_text())
    revisions = validate(ROOT, profile)
    mode = native_image_mode(profile)
    cores = selected_cores(profile) if mode == 'format1' else ()
    package_ids = selected_packages(profile, args.profile)
    if mode == 'package-only' and not package_ids:
        raise ValueError('package-only native image requires one selected format-2 package')
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
        packages = ()
        image_fp, image_info = fp, info
        if package_ids and args.action in ("build", "image", "verify", "rebuild", "dev"):
            packages = resolve_packages_for_action(
                revisions, output, env, args.action, package_ids)
            image_fp, image_info = image_fingerprint(fp, info, packages)
        env["TARGET_IMAGE_CONTAINER_RUNTIME"] = container
        env["TARGET_IMAGE_OUTPUT_VOLUME"] = output_volume(ROOT, args.profile)
        env["FOGCAST_DIR"] = str(fogcast)
        # The host has a small /tmp tmpfs; Go temporary files belong in out/.
        temp = ROOT / "out/tmp"
        temp.mkdir(exist_ok=True)
        env["GOTMPDIR"] = str(temp)
        fogcast_make = ["make", "-C", fogcast, "CONTAINER_RUNTIME=" + container, "VERSION=" + profile["version"]]
        image_make = ["make", "-C", IMAGE, "FOGCAST_DIR=" + str(fogcast),
                      "CONTAINER_RUNTIME=" + container,
                      "NATIVE_RUNTIME_MODE=" + mode]
        if args.action == "dev":
            if mode == 'format1' and profile.get("bundle_interface") != "selection":
                raise ValueError("make dev requires native-integration-dev; historical profiles stay cold")
            from native_dev import build_development
            runtime = source_checkout("libmister-runtime", revisions["libmister-runtime"])
            bundles = build_bundles_for_profile(
                profile, revisions, env, cores, diagnostics=diagnostics)
            build_development(ROOT, IMAGE, fogcast, runtime, args.profile, profile, image_info,
                              image_fp, env, fogcast_make, image_make, bundles, packages,
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
                if mode == 'package-only':
                    verify_package_only_outputs(output, packages)
                else:
                    verify_package_outputs(output, packages)
                if mode == 'format1' and profile.get("fpga_source") == "misteross":
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
                if mode == 'package-only':
                    bundles = {}
                    run_stage(diagnostics, "image subprocess", image_make + ["target-image-native",
                        "LIBMISTER_RUNTIME_DIR=" + str(runtime), *package_arguments(packages)], env=env)
                    manifest = None
                elif profile.get("fpga_source") == "misteross":
                    bundles = build_bundles(revisions, env, cores, args.action == "rebuild",
                                            diagnostics=diagnostics)
                    bundle_dir = bundles["megadrive"]
                    recipe_source = source_checkout("misteross", revisions["misteross"])
                    recipe_sha = digest(recipe_source / "scripts/rebuild_core.py")
                    manifest = core_bundle.load(bundle_dir, recipe_sha)
                    if profile.get("bundle_interface") == "selection":
                        run_stage(diagnostics, "image subprocess", image_make + ["target-image-native",
                            "LIBMISTER_RUNTIME_DIR=" + str(runtime),
                            *bundle_arguments(cores, bundles), *package_arguments(packages)], env=env)
                    else:
                        run_stage(diagnostics, "image subprocess", [IMAGE / "scripts/target-image-container.sh", "fetch",
                             "/work/scripts/fetch-native-runtime-inputs.sh"], env=env)
                        core_bundle.prepare(fogcast, bundle_dir, recipe_sha, cache_root=IMAGE)
                        run_stage(diagnostics, "image subprocess", [IMAGE / "scripts/build-target-image.sh", "--fetch", "native-dev"],
                            env=legacy_source_image_environment(env, runtime, mode))
                        run_stage(diagnostics, "image subprocess", [IMAGE / "scripts/build-target-image.sh", "native-dev"],
                            env=legacy_source_image_environment(env, runtime, mode))
                else:
                    manifest = None
                    run_stage(diagnostics, "image subprocess",
                              image_make + ["target-image-native", "LIBMISTER_RUNTIME_DIR=" + str(runtime)], env=env)
                # Verification reads the runtime commit from the image/lock; no source mount required.
                verify_env = dict(env, NATIVE_RUNTIME_MODE=mode,
                                  **dict(argument.split("=", 1)
                                        for argument in package_arguments(packages)))
                if mode == 'format1':
                    verify_env['NATIVE_RUNTIME_SYSTEMS'] = " ".join(cores)
                run_stage(diagnostics, "image subprocess", image_make + ["target-image-native-verify"],
                    env=verify_env)
                built = IMAGE / "build/output/target-image/native-dev"
                names = ["linux.img", "reproducibility.txt", "manifest.tsv", "library-report.tsv"]
                if mode == 'format1' and profile.get("bundle_interface") == "selection":
                    names.extend(core + ".selection.toml" for core in cores)
                for name in names:
                    publish_file(built / name, output / name)
                if mode == 'format1' and manifest is not None:
                    for core in cores:
                        for name in (core + "-rbf.toml", core + ".rbf"):
                            publish_file(bundles[core] / name, output / name)
                            names.append(name)
                built_selections = {
                    recipe_for(package["inputs"]["selection"]["core_id"]).selection_filename:
                    built / recipe_for(package["inputs"]["selection"]["core_id"]).selection_filename
                    for package in packages}
                names.extend(publish_package_state(
                    packages, built_selections if packages else None, output))
                if mode == 'package-only':
                    remove_format1_parent_outputs(output)
                    verify_package_only_outputs(output, packages)
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
            if mode == 'package-only':
                verify_package_only_outputs(output, packages)
            else:
                verify_package_outputs(output, packages)
            if mode == 'format1' and profile.get("fpga_source") == "misteross":
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
                    env=dict(env, NATIVE_RUNTIME_MODE=mode,
                             **dict(argument.split("=", 1)
                                   for argument in package_arguments(packages)),
                             **({'NATIVE_RUNTIME_SYSTEMS': " ".join(cores)}
                                if mode == 'format1' else {})))
            run_stage(diagnostics, "verification subprocess", image_make + ["target-image-native-qemu-smoke"],
                env=dict(env, NATIVE_RUNTIME_MODE=mode,
                         **dict(argument.split("=", 1)
                               for argument in package_arguments(packages)),
                         **({'NATIVE_RUNTIME_SYSTEMS': " ".join(cores)}
                            if mode == 'format1' else {})))
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
