#!/usr/bin/env python3
"""Build and inspect DE25-Nano headless or managed Linux artifacts; never write a device."""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import socket
import stat
import subprocess
import tarfile
import tempfile
import time
import tomllib
import urllib.request

LANE = Path(__file__).resolve().parent
ROOT = LANE.parents[1]
BOARD = "de25-nano"
ARCH = "aarch64"
BUILDER_FILES = (
    "image/build/target-image.sources.lock.toml",
    "image/build/target-image-container-packages.sha256",
    "image/containers/target-image/Dockerfile",
    "image/containers/target-image/create-builder-user.sh",
    "image/scripts/target-image-container.sh",
)


def run(argv, **kwargs):
    print("+ " + " ".join(map(str, argv)), flush=True)
    return subprocess.run(list(map(str, argv)), check=True, **kwargs)


def digest(path):
    value = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for chunk in iter(lambda: stream.read(8 << 20), b""):
            value.update(chunk)
    return value.hexdigest()


def lock():
    data = tomllib.loads((LANE / "buildroot.lock.toml").read_text())
    if (data.get("format") != 1 or data.get("board") != BOARD or
            data.get("architecture") != ARCH or
            not re.fullmatch(r"[a-f0-9]{64}", data["buildroot"]["sha256"])):
        raise ValueError("invalid DE25 Buildroot source lock")
    return data


def recipe_files(root=ROOT):
    lane = Path(root) / "image/de25-nano"
    return sorted([*(path for path in lane.rglob("*") if path.is_file() and
                     "__pycache__" not in path.parts and not path.name.startswith("test_")
                     and path.suffix != ".md"),
                   *(Path(root) / name for name in BUILDER_FILES)])


def recipe_key(root=ROOT):
    value = hashlib.sha256(b"FES-DE25-HEADLESS-1\n")
    for path in recipe_files(root):
        value.update(path.relative_to(root).as_posix().encode() + b"\0")
        value.update(path.read_bytes())
    return value.hexdigest()


def rootfs_key(root=ROOT):
    paths = [Path(root) / "image/de25-nano/buildroot.lock.toml",
             Path(root) / "image/de25-nano/kernel-modules.lock.json",
             Path(root) / "image/de25-nano/kernel_modules.py",
             *(Path(root) / name for name in BUILDER_FILES)]
    paths += [p for p in (Path(root) / "image/de25-nano/rootfs").rglob("*")
              if p.is_file() and "__pycache__" not in p.parts and not p.name.startswith("test_")]
    value = hashlib.sha256(b"FES-DE25-AARCH64-SDK-1\n")
    for path in sorted(paths):
        value.update(path.relative_to(root).as_posix().encode() + b"\0" + path.read_bytes())
    return value.hexdigest()


def download_buildroot(cache, selected=None):
    source = (lock() if selected is None else selected)["buildroot"]
    archive = cache / source["file"]
    if archive.exists() or archive.is_symlink():
        if not stat.S_ISREG(archive.lstat().st_mode):
            raise ValueError("Buildroot cache input must be a regular file")
    if archive.is_file() and digest(archive) == source["sha256"]:
        return archive
    descriptor, name = tempfile.mkstemp(prefix=archive.name + ".", dir=cache)
    temporary = Path(name)
    try:
        with os.fdopen(descriptor, "wb") as output, urllib.request.urlopen(source["url"], timeout=120) as response:
            shutil.copyfileobj(response, output, 1 << 20)
        if digest(temporary) != source["sha256"]:
            raise ValueError("Buildroot source archive digest differs from lock")
        temporary.replace(archive)
    finally:
        temporary.unlink(missing_ok=True)
    return archive


@contextmanager
def build_lock(cache):
    cache.mkdir(parents=True, exist_ok=True)
    with (cache / "build.lock").open("a") as stream:
        fcntl.flock(stream, fcntl.LOCK_EX)
        yield


@contextmanager
def output_lock(output):
    descriptor = os.open(output, os.O_RDONLY | os.O_DIRECTORY)
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX)
        yield
    finally:
        os.close(descriptor)


def validate_output(output):
    output.mkdir(parents=True, exist_ok=True)
    for path in output.rglob("*"):
        mode = path.lstat().st_mode
        if not (stat.S_ISREG(mode) or stat.S_ISDIR(mode)):
            raise ValueError("DE25 output must contain only regular files and directories: " + str(path))


def snapshot():
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    changes = subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=normal"],
                                      cwd=ROOT, text=True)
    if changes:
        raise ValueError("DE25 image builds require committed source; preserve and commit the working diff first")
    path = ROOT / "out/work" / ("de25-linux-" + revision)
    if not path.exists():
        path.parent.mkdir(parents=True, exist_ok=True)
        run(["git", "worktree", "add", "--detach", path, revision], cwd=ROOT)
    actual = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=path, text=True).strip()
    dirty = subprocess.check_output(["git", "status", "--porcelain"], cwd=path, text=True)
    if actual != revision or dirty:
        raise ValueError("retained DE25 source snapshot differs from selected committed source")
    return revision, path


def builder_tag(root):
    image = Path(root) / "image"
    source = tomllib.loads((image / "build/target-image.sources.lock.toml").read_text())["container"]
    ordered = [image / "containers/target-image/Dockerfile",
               image / "containers/target-image/create-builder-user.sh",
               image / "build/target-image-container-packages.sha256",
               image / "build/target-image.sources.lock.toml"]
    context = hashlib.sha256("".join(digest(p) + "\n" for p in ordered).encode()).hexdigest()[:12]
    return f"fogcast-target-image-build:{source['digest'][7:19]}-{context}-{os.getuid()}-{os.getgid()}"


def ensure_builder(source, runtime, volume):
    # Reuse the established builder and its base/package/UID provenance checks.
    env = dict(os.environ)
    for key in list(env):
        if key.startswith("FES_") or key.startswith("TARGET_IMAGE_") or key.startswith("LIBMISTER_"):
            env.pop(key)
    env.update(TARGET_IMAGE_CONTAINER_RUNTIME=runtime, TARGET_IMAGE_OUTPUT_VOLUME=volume,
               FOGCAST_DIR="", NATIVE_RUNTIME_MODE="package-only")
    container = tomllib.loads((source / "image/build/target-image.sources.lock.toml").read_text())["container"]
    base = container["image"] + "@" + container["digest"]
    if subprocess.run([runtime, "image", "inspect", base], stdout=subprocess.DEVNULL,
                      stderr=subprocess.DEVNULL).returncode:
        run([runtime, "pull", "--platform", container["platform"], base])
    run([source / "image/scripts/target-image-container.sh", "run", "true"], env=env)
    return subprocess.check_output([runtime, "image", "inspect", builder_tag(source),
                                    "--format", "{{.Id}}"], text=True).strip()


def container(source, runtime, image, volume, cache, output, action, jobs, inputs=None):
    mounts = [] if inputs is None else ["--volume", f"{inputs}:/inputs:ro"]
    return run([runtime, "run", "--rm", "--platform", "linux/amd64",
                "--network", "bridge" if action in ("fetch", "base-fetch", "factory-fetch") else "none",
                "--user", f"{os.getuid()}:{os.getgid()}", "--ulimit", "core=0:0",
                # Buildroot's fakeroot closes every possible descriptor at
                # startup; container hosts may otherwise expose a billion.
                "--ulimit", "nofile=65536:65536",
                "--volume", f"{source}:/src:ro", "--volume", f"{volume}:/build",
                "--volume", f"{cache}:/cache", "--volume", f"{output}:/export",
                *mounts,
                "--env", "LC_ALL=C", "--env", "TZ=UTC", image,
                "python3", "/src/image/de25-nano/build.py", "_inside", action, str(jobs)])


def prepare_buildroot(modules_overlay=None, *, managed=False, factory_key=None):
    data = lock()
    archive = Path("/cache") / data["buildroot"]["file"]
    if digest(archive) != data["buildroot"]["sha256"]:
        raise ValueError("cached Buildroot archive differs from lock")
    source = Path("/build/source-" + data["buildroot"]["sha256"])
    # Re-materialize the small, pinned source tree; cached compiler outputs
    # remain separate. Modified or extra cached source files cannot enter a build.
    if source.exists():
        shutil.rmtree(source)
    source.mkdir(parents=True)
    run(["tar", "-xf", archive, "--strip-components=1", "-C", source])
    if managed:
        import managed_base
    if factory_key is not None and (not managed or not re.fullmatch('factory-[0-9a-f]{64}', factory_key)):
        raise ValueError("invalid managed factory build key")
    output = Path("/build") / (factory_key or (managed_base.recipe_key() if managed else rootfs_key()))
    output.mkdir(parents=True, exist_ok=True)
    config = output / "de25_defconfig"
    if managed:
        config.write_text(managed_base.configuration(factory_key=factory_key))
    elif modules_overlay is None:
        raise ValueError("DE25 rootfs requires the verified kernel-module overlay")
    else:
        config.write_text((LANE / "rootfs/defconfig").read_text()
                          .replace("@ROOTFS_DIR@", str(LANE / "rootfs"))
                          .replace("@KERNEL_MODULES_DIR@", str(modules_overlay)))
    environment = dict(os.environ, SOURCE_DATE_EPOCH=str(data["source_date_epoch"]),
                       BR2_DL_DIR="/cache/dl")
    command = ["make", "-C", source, f"O={output}", f"BR2_DEFCONFIG={config}"]
    if managed:
        command.append(f"BR2_EXTERNAL={ROOT / 'image/buildroot'}")
        environment.update(FOGCAST_DIR=str(ROOT / "sources/FogCast"),
                           FES_TARGET_BOARD=BOARD)
    run([*command, "defconfig"], env=environment)
    configured = (output / ".config").read_text()
    toolchain = (("BR2_TOOLCHAIN_EXTERNAL_CUSTOM_GLIBC=y", "BR2_TOOLCHAIN_EXTERNAL_CXX=y")
                 if managed else ("BR2_TOOLCHAIN_BUILDROOT_GLIBC=y", "BR2_TOOLCHAIN_BUILDROOT_CXX=y"))
    for option in ("BR2_aarch64=y", "BR2_cortex_a53=y", *toolchain,
                   "BR2_PACKAGE_DROPBEAR=y", 'BR2_TARGET_GENERIC_GETTY_PORT="ttyS0"'):
        if option not in configured.splitlines():
            raise ValueError("Buildroot configuration lost required selection " + option)
    return source, output, command, environment


def elf_aarch64(data):
    if len(data) < 20 or data[:6] != b"\x7fELF\x02\x01" or int.from_bytes(data[18:20], "little") != 183:
        raise ValueError("target executable is not little-endian ELF64 AArch64")


def debugfs(image, command):
    result = subprocess.run(["debugfs", "-R", command, str(image)], check=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if b"File not found" in result.stderr or b"not found" in result.stderr:
        raise ValueError("missing rootfs path: " + command)
    return result.stdout


def validate_rootfs_configuration(files, password):
    for path, expected in {
        "/etc/fes-board": b"de25-nano\n",
        "/etc/hostname": b"fes-de25-nano\n",
    }.items():
        if files[path] != expected:
            raise ValueError("rootfs configuration differs: " + path)
    if not re.search(rb"(?m)^iface eth0 inet dhcp\r?$", files["/etc/network/interfaces"]):
        raise ValueError("rootfs configuration differs: /etc/network/interfaces")
    if not re.search(rb"(?m)^ttyS0::respawn:.*getty\b.*\bttyS0\s+115200\b",
                     files["/etc/inittab"]):
        raise ValueError("rootfs serial getty is not ttyS0 at 115200 baud")
    if not re.search(rb"(?m)^[ \t]*(?!#)[^\r\n]*\bdropbear\b", files["/etc/init.d/S50dropbear"]):
        raise ValueError("rootfs configuration differs: /etc/init.d/S50dropbear")
    root_entries = [line.split(b":") for line in files["/etc/shadow"].splitlines()
                    if line.split(b":", 1)[0] == b"root"]
    if len(root_entries) != 1 or len(root_entries[0]) < 2 or root_entries[0][1] != password:
        raise ValueError("rootfs development login differs from the selected configuration")


def verify_rootfs(directory):
    import kernel_modules
    image = directory / "rootfs.ext4"
    run(["e2fsck", "-fn", image], stdout=subprocess.DEVNULL)
    busybox = debugfs(image, "cat /bin/busybox")
    dropbear = debugfs(image, "cat /usr/sbin/dropbear")
    elf_aarch64(busybox)
    elf_aarch64(dropbear)
    password = re.search(r'^BR2_TARGET_GENERIC_ROOT_PASSWD="(.+)"$',
                         (LANE / "rootfs/defconfig").read_text(), re.M).group(1).replace("$$", "$")
    configuration = {path: debugfs(image, "cat " + path) for path in (
        "/etc/fes-board", "/etc/hostname", "/etc/network/interfaces", "/etc/inittab",
        "/etc/init.d/S50dropbear", "/etc/shadow")}
    validate_rootfs_configuration(configuration, password.encode())
    with tarfile.open(directory / "rootfs.tar") as archive:
        members = {item.name.removeprefix("./").lstrip("/"): item for item in archive.getmembers()}
        for path, data in (("bin/busybox", busybox), ("usr/sbin/dropbear", dropbear)):
            item = members[path]
            stream = archive.extractfile(item)
            if stream is None or hashlib.sha256(stream.read()).digest() != hashlib.sha256(data).digest():
                raise ValueError("rootfs tar and ext4 disagree on " + path)
        forbidden = [name for name in members if "dropbear_" in name and name.endswith("_host_key")]
        forbidden += [name for name in members if name.endswith(("mister-runtime", "fes-kit-software"))
                      or name.endswith(".rbf")]
        if forbidden:
            raise ValueError("unexpected hardware service or generated SSH key in rootfs: " + str(forbidden))
        modules = kernel_modules.verify_rootfs(image, archive, members, debugfs)
    return {"architecture": ARCH, "console": "ttyS0:115200", "ethernet": "eth0:dhcp",
            "ssh": "dropbear", "kernel_modules": modules, "hardware_acceptance": "not-run"}


def read_ssh_banner(connection):
    # Dropbear can send its binary key exchange in the same packet. Read only
    # the bounded identification line, including when TCP splits that line.
    with connection.makefile("rb") as stream:
        banner = stream.readline(256)
    if not banner.endswith(b"\n") or not banner.startswith(b"SSH-2.0-dropbear_"):
        raise ValueError("QEMU Dropbear banner unavailable: " + repr(banner))
    return banner.decode("ascii").rstrip("\r\n")


def smoke_rootfs(directory):
    checks = verify_rootfs(directory)
    with tempfile.TemporaryDirectory(prefix="fes-de25-smoke-") as temporary:
        root = Path(temporary) / "root"
        root.mkdir()
        # The tar is bound to image.json before entering the container. Extract
        # only into this disposable tree; never run the target init or network scripts.
        run(["tar", "-xf", directory / "rootfs.tar", "-C", root, "--no-same-owner",
             "--exclude=./dev/*", "--exclude=dev/*"])
        qemu = ["qemu-aarch64-static", "-L", str(root)]
        arch = subprocess.check_output([*qemu, str(root / "bin/busybox"), "uname", "-m"], text=True).strip()
        if arch != ARCH:
            raise ValueError("QEMU userspace architecture differs: " + arch)
        key = Path(temporary) / "host_key"
        run([*qemu, root / "usr/bin/dropbearkey", "-t", "ed25519", "-f", key],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        server = subprocess.Popen([*qemu, str(root / "usr/sbin/dropbear"), "-F", "-E",
                                   "-P", str(Path(temporary) / "dropbear.pid"),
                                   "-p", "127.0.0.1:2222", "-r", str(key)],
                                  stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic() + 10
            banner = ""
            while time.monotonic() < deadline and server.poll() is None:
                try:
                    with socket.create_connection(("127.0.0.1", 2222), timeout=1) as connection:
                        banner = read_ssh_banner(connection)
                    break
                except OSError:
                    time.sleep(0.1)
            if not banner:
                raise ValueError("QEMU Dropbear banner unavailable: " + repr(banner))
        finally:
            server.terminate()
            try:
                server.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.communicate()
        checks.update(classification="userspace-only", qemu_architecture=arch,
                      ssh_banner=banner, ssh_authentication="not-tested",
                      board_boot="not-run")
    return checks


def inside(action, jobs):
    if action.startswith("base-"):
        import managed_base
        return managed_base.inside(action, jobs)
    output = Path("/export")
    if action in ("verify", "smoke"):
        checks = smoke_rootfs(output) if action == "smoke" else verify_rootfs(output)
        checks.update(board=BOARD, recipe_sha256=recipe_key(),
                      rootfs_sha256=digest(output / "rootfs.ext4"),
                      image_sha256=digest(output / "fes-de25-nano.img"))
        (output / (action + ".json")).write_text(json.dumps(checks, indent=2) + "\n")
        return
    if action == "fetch":
        # Fetch needs no BSP extraction; build materializes and verifies this path.
        overlay = Path('/build/kernel-modules') / rootfs_key()
    else:
        import kernel_modules
        overlay = Path('/build/kernel-modules') / rootfs_key()
        if overlay.exists():
            shutil.rmtree(overlay)
        import boot
        vendor = boot.load_lock()
        # The host prepares/authenticates vendor boot data before this container.
        # It also copies the locked BSP ZIP into the shared cache for extraction.
        kernel_modules.prepare(Path('/cache') / vendor['bsp']['filename'], overlay)
    source, work, command, env = prepare_buildroot(overlay)
    if action == "fetch":
        run([*command, "source", f"-j{jobs}"], env=env)
        return
    run([*command, f"-j{jobs}"], env=env)
    run([*command, "sdk", f"-j{jobs}"], env=env)
    for name in ("rootfs.ext4", "rootfs.tar"):
        shutil.copyfile(work / "images" / name, output / name)
    sdk = list((work / "images").glob("*_sdk-buildroot.tar.gz"))
    if len(sdk) != 1:
        raise ValueError("Buildroot did not publish one AArch64 SDK")
    shutil.copyfile(sdk[0], output / "aarch64-sdk.tar.gz")
    shutil.copyfile(work / ".config", output / "buildroot.config")
    verify_rootfs(output)


def verify_receipt(output, source=ROOT):
    record = json.loads((output / "image.json").read_text())
    if (record.get("format") != 1 or record.get("board") != BOARD or
            record.get("architecture") != ARCH or record.get("recipe_sha256") != recipe_key(source) or
            record.get("rootfs_key") != rootfs_key(source) or
            not re.fullmatch(r"sha256:[a-f0-9]{64}", record.get("builder_image", "")) or
            not re.fullmatch(r"[a-f0-9]{40}", record.get("fes_revision", ""))):
        raise ValueError("DE25 receipt differs from target or recipe")
    if set(record.get("files", {})) != set(artifact_names(source)):
        raise ValueError("DE25 receipt has missing or unexpected artifacts")
    for name, expected in record["files"].items():
        path = output / name
        if path.is_symlink() or not path.is_file() or digest(path) != expected:
            raise ValueError("DE25 artifact hash or path differs: " + name)
    return record


def artifact_names(source=ROOT):
    import boot
    vendor = boot.load_lock(Path(source) / "image/de25-nano/vendor.lock.toml")
    return ["fes-de25-nano.img", "rootfs.ext4", "rootfs.tar", "aarch64-sdk.tar.gz", "buildroot.config",
            "vendor-boot/vendor-boot-prefix.bin",
            *("vendor-boot/" + item["name"] for item in vendor["boot_files"]),
            *("vendor-boot/factory-boot/" + item["name"] for item in vendor["factory_boot"])]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("fetch", "build", "verify", "smoke", "managed-base", "verify-base",
                                          "managed-factory", "verify-factory", "boot-smoke", "_inside"))
    parser.add_argument("inside_action", nargs="?", help=argparse.SUPPRESS)
    parser.add_argument("inside_jobs", nargs="?", type=int, help=argparse.SUPPRESS)
    parser.add_argument("--output", type=Path, default=ROOT / "out/de25-nano-linux")
    parser.add_argument("--cache", type=Path, default=ROOT / "out/de25-linux-cache")
    parser.add_argument("--jobs", type=int, default=min(8, os.cpu_count() or 1))
    parser.add_argument("--container-runtime", default="docker")
    parser.add_argument("--kernel", type=Path, help="verified matching owner kernel for managed base or factory")
    parser.add_argument("--sdk", type=Path, help="AArch64 SDK archive pinned by the owner-kernel source lock")
    parser.add_argument("--artifacts", type=Path, help="selected DE25 image-artifact set for managed-factory")
    parser.add_argument("--image-profile", choices=("managed-base", "managed-factory"),
                        help="explicit managed image selection for boot-smoke")
    parser.add_argument("--boot-output", type=Path, help="new evidence directory for boot-smoke")
    args = parser.parse_args(argv)
    if args.action == "_inside":
        if str(ROOT) == "/src" and args.inside_action in ("factory-fetch", "factory-build", "factory-verify"):
            import managed_factory
            managed_factory.inside(args.inside_action, args.inside_jobs)
            return 0
        if str(ROOT) != "/src" or args.inside_action not in ("fetch", "build", "verify", "smoke", "base-fetch", "base-build", "base-verify"):
            parser.error("internal action requires the isolated DE25 builder")
        inside(args.inside_action, args.inside_jobs)
        return 0
    if not 1 <= args.jobs <= 64:
        parser.error("--jobs must be between 1 and 64")
    if args.inside_action is not None or args.inside_jobs is not None:
        parser.error("unexpected positional arguments")
    if args.action == "boot-smoke":
        import managed_boot
        try:
            return managed_boot.publish(args)
        except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
            parser.exit(1, f"de25-managed-boot: {error}\n")
    if args.image_profile is not None or args.boot_output is not None:
        parser.error("--image-profile and --boot-output apply only to boot-smoke")
    if args.action in ("managed-factory", "verify-factory"):
        import managed_factory
        try:
            return managed_factory.publish(args)
        except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
            parser.exit(1, f"de25-managed-factory: {error}\n")
    if args.artifacts is not None:
        parser.error("--artifacts applies only to managed-factory")
    if args.action in ("managed-base", "verify-base"):
        import managed_base
        try:
            return managed_base.publish(args)
        except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
            parser.exit(1, f"de25-managed-base: {error}\n")
    if args.kernel is not None or args.sdk is not None:
        parser.error("--kernel and --sdk apply only to managed-base")
    cache, output = args.cache.resolve(), args.output.resolve()
    cache.mkdir(parents=True, exist_ok=True)
    try:
        output.mkdir(parents=True, exist_ok=True)
        import boot
        # Capture the selected commit before waiting behind another build.
        if args.action == "build":
            revision, source = snapshot()
        with build_lock(cache), output_lock(output):
            validate_output(output)
            if args.action in ("verify", "smoke"):
                (output / (args.action + ".json")).unlink(missing_ok=True)
                record = verify_receipt(output)
                source = ROOT
            elif args.action == "fetch":
                source, record = ROOT, None
            else:
                record = None
            volume = "fes-de25-linux-" + hashlib.sha256((str(ROOT) + "\0" + str(cache)).encode()).hexdigest()[:16]
            image = ensure_builder(source, args.container_runtime, volume)
            if record and image != record.get("builder_image"):
                raise ValueError("verification builder differs from the producing builder")
            if args.action in ("fetch", "build"):
                if args.action == "build":
                    for name in ("image.json", "verify.json", "smoke.json"):
                        (output / name).unlink(missing_ok=True)
                selected = tomllib.loads((source / "image/de25-nano/buildroot.lock.toml").read_text())
                download_buildroot(cache, selected)
                if args.action == "build":
                    spec = importlib.util.spec_from_file_location("de25_selected_boot", source / "image/de25-nano/boot.py")
                    boot = importlib.util.module_from_spec(spec)
                    spec.loader.exec_module(boot)
                # Vendor downloads and boot extraction are independently bound by vendor.lock.toml.
                boot.prepare_boot(cache, output / "vendor-boot")
                container(source, args.container_runtime, image, volume, cache, output, "fetch", args.jobs)
                if args.action == "fetch":
                    return 0
                container(source, args.container_runtime, image, volume, cache, output, "build", args.jobs)
                image_path = output / "fes-de25-nano.img"
                temporary = output / "fes-de25-nano.img.new"
                temporary.unlink(missing_ok=True)
                boot.assemble(output / "vendor-boot/vendor-boot-prefix.bin", output / "rootfs.ext4", temporary)
                temporary.replace(image_path)
                container(source, args.container_runtime, image, volume, cache, output, "verify", args.jobs)
                files = {name: digest(output / name) for name in artifact_names(source)}
                record = {"format": 1, "board": BOARD, "architecture": ARCH,
                          "classification": "headless-bringup", "fes_revision": revision,
                          "recipe_sha256": recipe_key(source), "rootfs_key": rootfs_key(source),
                          "builder_image": image, "files": files,
                          "hardware_acceptance": "not-run"}
                (output / "image.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
                verify_receipt(output, source)
            if args.action != "fetch":
                boot.verify_image(output / "fes-de25-nano.img", output / "rootfs.ext4")
                if args.action != "build":
                    container(source, args.container_runtime, image, volume, cache, output,
                              "smoke" if args.action == "smoke" else "verify", args.jobs)
                print(json.dumps({"board": BOARD, "output": str(output), "status": "passed",
                                  "hardware_acceptance": "not-run"}, indent=2))
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"de25-linux: {error}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
