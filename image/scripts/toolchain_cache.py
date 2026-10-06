#!/usr/bin/env python3
"""Content-addressed Buildroot SDK cache and generated Buildroot configurations."""
import hashlib
import json
import os
from pathlib import Path
import posixpath
import shutil
import sys
import tarfile
import tempfile
import tomllib

IMAGE = Path(__file__).resolve().parents[1]
EPOCH = 1751459412
FRAGMENT = "buildroot/configs/fogcast_toolchain.fragment"
PREFIX = "arm-buildroot-linux-gnueabihf"
EXTERNAL_PATH = "/target-image-output/external-toolchain/host"
SHARED_CONTAINER_PATH = "/target-image-shared-cache"
CCACHE_OPTIONS = ("BR2_CCACHE=y", f'BR2_CCACHE_DIR="{SHARED_CONTAINER_PATH}/ccache"',
                  "BR2_CCACHE_USE_BASEDIR=y", 'BR2_CCACHE_INITIAL_SETUP="--max-size=20G"')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def file_sha(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def key(image=IMAGE, epoch=EPOCH):
    image = Path(image)
    lock = tomllib.loads((image / "build/target-image.sources.lock.toml").read_text())
    inputs = (lock["buildroot"]["commit"].encode(), (image / FRAGMENT).read_bytes(),
              sha((image / "build/target-image-container-packages.sha256").read_bytes()).encode(),
              str(epoch).encode())
    return sha(b"".join(len(value).to_bytes(8, "big") + value for value in inputs))


def cache_dir(image=IMAGE):
    image = Path(image)
    shared = shared_root(image)
    return (shared / "toolchains" if shared else image / "build/cache/target-image/toolchains") / key(image)


def shared_root(image=IMAGE):
    value = os.environ.get("FES_TARGET_IMAGE_SHARED_CACHE", "")
    if not value:
        return None
    path = Path(value)
    if not path.is_absolute():
        raise ValueError("FES_TARGET_IMAGE_SHARED_CACHE must be an absolute path")
    path = path.resolve()
    worktree = Path(image).resolve().parent
    container_mount = Path(image).resolve() == Path("/work") and path == Path(SHARED_CONTAINER_PATH)
    if not container_mount and (path == worktree or worktree in path.parents):
        raise ValueError("FES_TARGET_IMAGE_SHARED_CACHE must be outside the worktree")
    return path


def validated_sha(directory, expected_key):
    try:
        record = json.loads((directory / "receipt.json").read_text())
        archive = directory / "host.tar"
        if record["key"] == expected_key and len(record["sha256"]) == 64 and \
                file_sha(archive) == record["sha256"]:
            return record["sha256"]
    except (OSError, ValueError, KeyError, TypeError):
        pass
    return None


def package(host, directory, expected_key, epoch=EPOCH):
    host, directory = Path(host), Path(directory)
    if not (host / "bin" / (PREFIX + "-gcc")).exists():
        raise ValueError("SDK cross compiler is missing")
    directory.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=directory, delete=False) as stream:
        temporary = Path(stream.name)
        try:
            with tarfile.open(fileobj=stream, mode="w") as archive:
                for path in (host, *sorted(host.rglob("*"))):
                    info = archive.gettarinfo(str(path), "host" if path == host else
                                              "host/" + path.relative_to(host).as_posix())
                    info.uid = info.gid = 0
                    info.uname = info.gname = ""
                    info.mtime = epoch
                    if info.isfile():
                        with path.open("rb") as source:
                            archive.addfile(info, source)
                    else:
                        archive.addfile(info)
            stream.flush()
            os.fsync(stream.fileno())
            archive_sha = file_sha(temporary)
            temporary.replace(directory / "host.tar")
            receipt = directory / "receipt.json.tmp"
            receipt.write_text(json.dumps({"key": expected_key, "sha256": archive_sha},
                                          sort_keys=True) + "\n")
            receipt.replace(directory / "receipt.json")
            return archive_sha
        finally:
            temporary.unlink(missing_ok=True)


def extract(directory, expected_key, destination):
    directory, destination = Path(directory), Path(destination)
    archive_sha = validated_sha(directory, expected_key)
    if archive_sha is None:
        raise ValueError("cached SDK key or sha256 mismatch")
    if destination.exists():
        shutil.rmtree(destination)
    destination.mkdir(parents=True)
    with tarfile.open(directory / "host.tar") as archive:
        for member in archive.getmembers():
            if member.name != "host" and not member.name.startswith("host/"):
                raise ValueError("unsafe SDK archive member")
            if ".." in Path(member.name).parts or member.name.startswith("/"):
                raise ValueError("unsafe SDK archive member")
            if member.issym() or member.islnk():
                target = posixpath.normpath(posixpath.join(posixpath.dirname(member.name), member.linkname))
                if member.linkname.startswith("/") or (target != "host" and not target.startswith("host/")):
                    raise ValueError("unsafe SDK archive link")
        archive.extractall(destination)
    return archive_sha


NO_INET_RPC = "# BR2_TOOLCHAIN_EXTERNAL_INET_RPC is not set"


def config(image, kind, destination):
    image = Path(image)
    shared = (image / FRAGMENT).read_text()
    required = ("BR2_arm=y", "BR2_cortex_a9=y", "BR2_ARM_ENABLE_VFP=y",
                "BR2_ARM_EABIHF=y", "BR2_TOOLCHAIN_BUILDROOT_GLIBC=y",
                "BR2_TOOLCHAIN_BUILDROOT_CXX=y", "BR2_GCC_VERSION_9_X=y",
                "BR2_KERNEL_HEADERS_5_10=y", "BR2_REPRODUCIBLE=y")
    if set(shared.splitlines()) != set(required):
        raise ValueError("shared toolchain fragment drifted from its expected options")
    base = image / "buildroot/configs" / ("fogcast_toolchain_only_defconfig" if kind == "toolchain"
                                         else "fogcast_target_native_dev_defconfig")
    base_text = base.read_text()
    if any(line.startswith(("BR2_arm=", "BR2_cortex_", "BR2_ARM_", "BR2_TOOLCHAIN_",
                            "BR2_GCC_VERSION_", "BR2_KERNEL_HEADERS_", "BR2_REPRODUCIBLE=")) for line in base_text.splitlines()):
        raise ValueError("toolchain option duplicated outside shared fragment")
    if kind == "external":
        shared = "\n".join(line for line in shared.splitlines()
                           if not line.startswith(("BR2_TOOLCHAIN_BUILDROOT_", "BR2_GCC_VERSION_",
                                                   "BR2_KERNEL_HEADERS_"))) + "\n"
        shared += ("BR2_TOOLCHAIN_EXTERNAL=y\nBR2_TOOLCHAIN_EXTERNAL_CUSTOM=y\n"
                   f'BR2_TOOLCHAIN_EXTERNAL_PATH="{EXTERNAL_PATH}"\n'
                   f'BR2_TOOLCHAIN_EXTERNAL_CUSTOM_PREFIX="{PREFIX}"\n'
                   "BR2_TOOLCHAIN_EXTERNAL_GCC_9=y\n"
                   "BR2_TOOLCHAIN_EXTERNAL_HEADERS_5_10=y\n"
                   "BR2_TOOLCHAIN_EXTERNAL_CUSTOM_GLIBC=y\n"
                   "BR2_TOOLCHAIN_EXTERNAL_CXX=y\n"
                   # Buildroot 2021.02 builds glibc 2.32, which no longer ships SunRPC;
                   # the custom-glibc default (y) fails Buildroot's external check.
                   f"{NO_INET_RPC}\n")
    elif kind not in ("toolchain", "internal"):
        raise ValueError("unknown configuration kind")
    options = "" if kind != "external" or shared_root(image) is None else "".join(
        option + "\n" for option in CCACHE_OPTIONS)
    Path(destination).write_text(base_text + "\n" + shared + options)


def validate_config(kind, path):
    selected = set(Path(path).read_text().splitlines())
    common = {"BR2_arm=y", "BR2_cortex_a9=y", "BR2_ARM_ENABLE_VFP=y",
              "BR2_ARM_EABIHF=y", "BR2_REPRODUCIBLE=y"}
    internal = {"BR2_TOOLCHAIN_BUILDROOT_GLIBC=y", "BR2_TOOLCHAIN_BUILDROOT_CXX=y",
                "BR2_GCC_VERSION_9_X=y", "BR2_KERNEL_HEADERS_5_10=y"}
    external = {"BR2_TOOLCHAIN_EXTERNAL=y", "BR2_TOOLCHAIN_EXTERNAL_CUSTOM=y",
                f'BR2_TOOLCHAIN_EXTERNAL_PATH="{EXTERNAL_PATH}"',
                f'BR2_TOOLCHAIN_EXTERNAL_CUSTOM_PREFIX="{PREFIX}"',
                "BR2_TOOLCHAIN_EXTERNAL_GCC_9=y", "BR2_TOOLCHAIN_EXTERNAL_HEADERS_5_10=y",
                "BR2_TOOLCHAIN_EXTERNAL_CUSTOM_GLIBC=y", "BR2_TOOLCHAIN_EXTERNAL_CXX=y", NO_INET_RPC}
    required = common | (external if kind == "external" else internal)
    missing = required - selected
    if kind == "external" and shared_root() is not None:
        missing |= set(CCACHE_OPTIONS) - selected
    if kind in ("toolchain", "internal") and "BR2_CCACHE=y" in selected:
        raise ValueError("source and toolchain configurations must not enable ccache")
    if missing:
        raise ValueError("Buildroot rejected toolchain configuration: " + ", ".join(sorted(missing)))


def main(argv):
    command = argv[1]
    if command == "key":
        print(key())
    elif command == "epoch":
        print(EPOCH)
    elif command == "path":
        print(EXTERNAL_PATH)
    elif command == "shared-root":
        value = shared_root()
        if value is not None:
            print(value)
    elif command == "status":
        value = validated_sha(cache_dir(), key())
        if value is None:
            return 1
        print(value)
    elif command == "package":
        print(package(argv[2], cache_dir(), key()))
    elif command == "extract":
        print(extract(cache_dir(), key(), argv[2]))
    elif command == "config":
        config(IMAGE, argv[2], argv[3])
    elif command == "validate-config":
        validate_config(argv[2], argv[3])
    else:
        raise ValueError("unknown command")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv))
    except (OSError, ValueError) as error:
        print(f"toolchain-cache: {error}", file=sys.stderr)
        sys.exit(1)
