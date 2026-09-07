"""Build and validate the pinned unprivileged boot-media tool container."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

try:
    from .media_inputs import MediaLock
except ImportError:
    from media_inputs import MediaLock

BASE = "docker.io/library/debian:12.11-slim@sha256:b1a741487078b369e78119849663d7f1a5341ef2768798f7b7406c4240f86aef"
CONTEXT_FILES = ("Dockerfile", "create-builder-user.sh", "packages.sha256")
PACKAGE_QUERY = "import hashlib,subprocess; data=subprocess.check_output(['dpkg-query','-W','-f=${Package}\\t${Version}\\t${Architecture}\\n']); print(hashlib.sha256(b''.join(sorted(data.splitlines(keepends=True)))).hexdigest())"


def ensure_media_container(root: Path, runtime: str, lock: MediaLock) -> str:
    if lock.layout != "de10-nano-mister-v1":
        raise ValueError("unsupported media layout")
    uid, gid = os.getuid(), os.getgid()
    if not uid or not gid:
        raise ValueError("boot-media container requires a non-root caller")
    context = Path(root) / "containers/boot-media"
    hasher = hashlib.sha256()
    for name in CONTEXT_FILES:
        data = (context / name).read_bytes()
        hasher.update(name.encode() + b"\0" + len(data).to_bytes(8, "big") + data)
    context_sha = hasher.hexdigest()
    packages_sha = (context / "packages.sha256").read_text().strip()
    if not re.fullmatch(r"[0-9a-f]{64}", packages_sha):
        raise ValueError("invalid media package-set hash")
    expected = {"org.fes.media.base": BASE, "org.fes.media.context-sha256": context_sha,
                "org.fes.media.packages-sha256": packages_sha,
                "org.fes.media.uid": str(uid), "org.fes.media.gid": str(gid)}
    tag = f"fes-boot-media:{context_sha}-{uid}-{gid}"
    inspected = subprocess.run([runtime, "image", "inspect", tag], capture_output=True, text=True)
    if inspected.returncode:
        subprocess.run([runtime, "build", "--build-arg", f"BUILDER_UID={uid}",
                        "--build-arg", f"BUILDER_GID={gid}", "--build-arg", f"CONTEXT_SHA256={context_sha}",
                        "--build-arg", f"PACKAGES_SHA256={packages_sha}", "-t", tag, str(context)], check=True)
        inspected = subprocess.run([runtime, "image", "inspect", tag], capture_output=True, text=True, check=True)
    try:
        record, = json.loads(inspected.stdout)
        image_id = record["Id"]
        config = record["Config"]
        if any(config.get("Labels", {}).get(key) != value for key, value in expected.items()):
            raise ValueError("boot-media container labels differ from locked build")
        if config.get("User") != "builder" or not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id):
            raise ValueError("boot-media container identity is invalid")
    except (KeyError, TypeError, json.JSONDecodeError) as error:
        raise ValueError("invalid boot-media container inspection") from error
    actual = subprocess.check_output([runtime, "run", "--rm", "--network=none", "--cap-drop=ALL",
                                      "--security-opt=no-new-privileges", "--entrypoint", "python3",
                                      image_id, "-c", PACKAGE_QUERY], text=True).strip()
    if actual != packages_sha:
        raise ValueError("boot-media container package set differs from lock")
    identity = subprocess.check_output([runtime, "run", "--rm", "--network=none", "--cap-drop=ALL",
                                       "--entrypoint", "python3", image_id, "-c",
                                       "import os; print(f'{os.getuid()}:{os.getgid()}')"], text=True).strip()
    if identity != f"{uid}:{gid}":
        raise ValueError("boot-media container user differs from caller")
    return image_id
