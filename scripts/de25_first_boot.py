#!/usr/bin/env python3
"""Collect read-only DE25 first-boot evidence and compare it across a manual reboot."""
from __future__ import annotations

import argparse
import base64
import hashlib
import io
import ipaddress
import json
import os
from pathlib import Path
import re
import selectors
import struct
import subprocess
import sys
import tarfile
import tempfile
import time
from datetime import datetime, timedelta, timezone
import uuid

ROOT = Path(__file__).resolve().parents[1]
FILES = ("/etc/fes-board", "/etc/hostname", "/etc/inittab", "/etc/network/interfaces",
         "/etc/init.d/S50dropbear", "/bin/busybox", "/usr/sbin/dropbear")
FIELDS = ("boot_id_start", "architecture", "kernel_release", "board", "model", "compatible",
          "cmdline", "meminfo", "mountinfo", "root_dev", "root_uevent", "mac", "carrier",
          "addresses", "routes", "ssh_connection", "file_hashes", "boot_id_end")
HOST_ALGORITHMS = {"ssh-ed25519": "ssh-ed25519", "ecdsa-sha2-nistp256": "ecdsa-sha2-nistp256",
                   "ssh-rsa": "rsa-sha2-512,rsa-sha2-256"}
CAPTURE_LIMIT = 1024 * 1024  # Per stream, before protocol parsing; includes SSH diagnostic output.
SHELL = r'''set -eu
export LC_ALL=C
emit_text() { printf '%s\t' "$1"; printf '%s' "$2" | od -An -v -tx1 | tr -d ' \n'; printf '\n'; }
emit_file() { h=$(od -An -v -tx1 "$2"); printf '%s\t' "$1"; printf '%s' "$h" | tr -d ' \n'; printf '\n'; }
printf 'FES-DE25-FIRST-BOOT\t1\n'
emit_file boot_id_start /proc/sys/kernel/random/boot_id
emit_text architecture "$(uname -m)"
emit_text kernel_release "$(uname -r)"
emit_file board /etc/fes-board
emit_file model /proc/device-tree/model
emit_file compatible /proc/device-tree/compatible
emit_file cmdline /proc/cmdline
emit_file meminfo /proc/meminfo
emit_file mountinfo /proc/self/mountinfo
emit_file root_dev /sys/class/block/mmcblk0p2/dev
emit_file root_uevent /sys/class/block/mmcblk0p2/uevent
emit_file mac /sys/class/net/eth0/address
emit_file carrier /sys/class/net/eth0/carrier
emit_text addresses "$(ip -4 addr show dev eth0)"
emit_text routes "$(ip -4 route show)"
emit_text ssh_connection "${SSH_CONNECTION:?SSH_CONNECTION missing}"
emit_text file_hashes "$(sha256sum /etc/fes-board /etc/hostname /etc/inittab /etc/network/interfaces /etc/init.d/S50dropbear /bin/busybox /usr/sbin/dropbear)"
emit_file boot_id_end /proc/sys/kernel/random/boot_id
printf 'FES-DE25-FIRST-BOOT-END\t1\n'
'''.encode("ascii")


def sha(data):
    return hashlib.sha256(data).hexdigest()


def regular_bytes(path, limit=8 * 1024 * 1024):
    path = Path(path)
    if path.is_symlink() or not path.is_file() or path.stat().st_size > limit:
        raise ValueError("expected bounded regular file: " + str(path))
    with path.open("rb") as stream:
        data = stream.read(limit + 1)
    if len(data) > limit:
        raise ValueError("file grew beyond capture limit: " + str(path))
    return data


def save_json(path, value):
    with Path(path).open("x", encoding="utf-8") as target:
        json.dump(value, target, indent=2, sort_keys=True)
        target.write("\n")


def load_json(data):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate JSON key: " + key)
            result[key] = value
        return result
    return json.loads(data, object_pairs_hook=unique)


def prepare(image_output):
    """Use the existing inspector; retain its actual report, not a second FIT parser."""
    image_output = Path(image_output).resolve()
    receipt_bytes = regular_bytes(image_output / "image.json")
    record = load_json(receipt_bytes)
    try:
        result = subprocess.run([sys.executable, str(ROOT / "scripts/de25_boot_contract.py"),
                                 "--image-output", str(image_output)], check=True, capture_output=True)
    except subprocess.CalledProcessError as error:
        detail = (error.stderr or b"").decode("utf-8", errors="replace").strip()
        message = f"boot-contract inspector failed ({error.returncode})"
        if detail:
            message += ": " + detail
        raise ValueError(message) from error
    contract = load_json(result.stdout)
    archive = regular_bytes(image_output / "rootfs.tar", 512 * 1024 * 1024)
    kernel = regular_bytes(image_output / "vendor-boot/Image", 128 * 1024 * 1024)
    if (regular_bytes(image_output / "image.json") != receipt_bytes
            or sha(archive) != record["files"]["rootfs.tar"]
            or sha(kernel) != record["files"]["vendor-boot/Image"]
            or contract["image_producer"] != record["fes_revision"]
            or contract["image_sha256"] != record["files"]["fes-de25-nano.img"]):
        raise ValueError("image artifacts changed after boot-contract inspection")
    releases = set(re.findall(rb"Linux version ([!-~]+) ", kernel))
    if len(releases) != 1:
        raise ValueError("expected exactly one vendor kernel release banner")
    file_hashes = {}
    with tarfile.open(fileobj=io.BytesIO(archive)) as source:
        members = {}
        for item in source.getmembers():
            name = "/" + item.name.removeprefix("./").lstrip("/")
            if name in FILES:
                if name in members or not item.isfile():
                    raise ValueError("ambiguous/nonregular rootfs identity file: " + name)
                members[name] = item
        for name in FILES:
            file_hashes[name] = sha(source.extractfile(members[name]).read())
    expectation = {"format": 1, "kind": "de25-first-boot-expectation", "boot_contract": contract,
                   "kernel_release": releases.pop().decode("ascii"), "file_sha256": file_hashes,
                   "image_receipt_sha256": sha(receipt_bytes),
                   "rootfs_tar_sha256": sha(archive),
                   "whole_image_observation": "unavailable: no installed image digest marker"}
    validate_expectation(expectation)
    return expectation


def validate_expectation(expected):
    contract = expected["boot_contract"]
    boot = contract["boot"]
    if (expected.get("format") != 1 or expected.get("kind") != "de25-first-boot-expectation"
            or contract.get("format") != 1 or contract.get("board") != "de25-nano"
            or contract.get("classification") != "host-only-boot-contract"
            or boot["linux_device_tree"]["model"] != "SoCFPGA Agilex5 Terasic DE25-Nano"
            or boot["root_device"] != "/dev/mmcblk0p2"
            or not re.fullmatch(r"[0-9a-f]{40}", contract["image_producer"])
            or not re.fullmatch(r"[0-9a-f]{64}", contract["image_sha256"])
            or not re.fullmatch(r"[A-Za-z0-9._+-]+", expected["kernel_release"])
            or set(expected["file_sha256"]) != set(FILES)
            or any(not re.fullmatch(r"[0-9a-f]{64}", item) for item in expected["file_sha256"].values())):
        raise ValueError("unsupported first-boot expectation or board/image contract")


def host_key(data, host, port):
    lines = [line for line in data.decode("ascii").splitlines() if line and not line.startswith("#")]
    if len(lines) != 1:
        raise ValueError("known_hosts must contain exactly one pinned, unhashed host key")
    fields = lines[0].split()
    token = host if port == 22 else f"[{host}]:{port}"
    if len(fields) < 3 or fields[0] != token or fields[1] not in HOST_ALGORITHMS:
        raise ValueError("known_hosts host/port or host-key algorithm differs")
    key = base64.b64decode(fields[2], validate=True)
    if len(key) < 4:
        raise ValueError("truncated SSH host key")
    size = struct.unpack_from(">I", key)[0]
    if key[4:4 + size] != fields[1].encode("ascii") or len(key) <= 4 + size:
        raise ValueError("SSH host-key encoding differs from algorithm")
    fingerprint = "SHA256:" + base64.b64encode(hashlib.sha256(key).digest()).decode().rstrip("=")
    return {"algorithm": fields[1], "public_key": fields[2], "fingerprint": fingerprint}


def validate_ssh_endpoint(host, port, user):
    if (not isinstance(host, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9.:-]*", host)
            or type(port) is not int or not 1 <= port <= 65535
            or not isinstance(user, str) or not re.fullmatch(r"[a-z_][a-z0-9_-]*", user)):
        raise ValueError("invalid explicit SSH host, user or port")


def ssh_config_path(path):
    return '"' + str(path).replace('\\', '\\\\').replace('"', '\\"') + '"'


def ssh_argv(host, port, user, known_hosts, identity, key):
    # No ambient config, agent, proxy, multiplexed session, forwarding or host-key enrollment.
    validate_ssh_endpoint(host, port, user)
    # OpenSSH expands tokens/environment variables in these paths itself.
    # Refuse those forms; quote spaces for its config parser rather than a shell.
    for path in (known_hosts, identity):
        if path is not None and any(char in str(path) for char in '%$\r\n\0'):
            raise ValueError("SSH file paths must not contain expansion tokens/control characters")
    pinned_path = ssh_config_path(known_hosts)
    options = {"BatchMode": "yes" if identity else "no", "IdentitiesOnly": "yes", "IdentityAgent": "none",
               "PreferredAuthentications": "publickey" if identity else "password",
               "PubkeyAuthentication": "yes" if identity else "no",
               "PasswordAuthentication": "no" if identity else "yes",
               "KbdInteractiveAuthentication": "no", "NumberOfPasswordPrompts": "0" if identity else "1",
               "UserKnownHostsFile": pinned_path, "GlobalKnownHostsFile": "/dev/null",
               "StrictHostKeyChecking": "yes", "UpdateHostKeys": "no", "VerifyHostKeyDNS": "no",
               "HostKeyAlgorithms": HOST_ALGORITHMS[key["algorithm"]], "CheckHostIP": "no",
               "ClearAllForwardings": "yes", "ForwardAgent": "no", "ForwardX11": "no",
               "PermitLocalCommand": "no", "ProxyCommand": "none", "ProxyJump": "none",
               "ControlMaster": "no", "ControlPath": "none", "ControlPersist": "no",
               "CanonicalizeHostname": "no", "ConnectionAttempts": "1", "ConnectTimeout": "10",
               "ServerAliveInterval": "5", "ServerAliveCountMax": "2"}
    argv = ["ssh", "-F", "/dev/null", "-T", "-v", "-p", str(port), "-l", user]
    if identity:
        # Set the complete identity list through config. With IdentitiesOnly=yes this
        # excludes the default ~/.ssh keys and agent identities without a fake path.
        options["IdentityFile"] = ssh_config_path(identity)
    else:
        # OpenSSH's special value suppresses its built-in ~/.ssh identity list.
        options["IdentityFile"] = "none"
    for name, value in options.items():
        argv += ["-o", name + "=" + value]
    return argv + [host, "sh -s"]


def parse_wire(data):
    if len(data) > 1024 * 1024:
        raise ValueError("oversized observation protocol")
    lines = data.decode("ascii").splitlines()
    if (not lines or lines[0] != "FES-DE25-FIRST-BOOT\t1"
            or lines[-1] != "FES-DE25-FIRST-BOOT-END\t1" or len(lines) != len(FIELDS) + 2):
        raise ValueError("observation protocol framing differs")
    result = {}
    for expected, line in zip(FIELDS, lines[1:-1]):
        name, separator, value = line.partition("\t")
        if name != expected or not separator or not re.fullmatch(r"(?:[0-9a-f]{2})+", value):
            raise ValueError("missing, reordered or malformed observation: " + expected)
        result[name] = bytes.fromhex(value)
    return result


def observe(raw, expected):
    validate_expectation(expected)
    values = parse_wire(raw)
    text = {name: data.decode("ascii").strip() for name, data in values.items()
            if name not in ("model", "compatible")}
    boot_id = str(uuid.UUID(text["boot_id_start"]))
    if boot_id == str(uuid.UUID(int=0)) or boot_id != text["boot_id_start"] or boot_id != text["boot_id_end"]:
        raise ValueError("boot ID changed during collection or is malformed")
    tree = expected["boot_contract"]["boot"]["linux_device_tree"]
    if (text["architecture"] != "aarch64" or text["board"] != "de25-nano"
            or text["kernel_release"] != expected["kernel_release"]
            or values["model"] != tree["model"].encode() + b"\0"
            or values["compatible"] != b"\0".join(item.encode() for item in tree["compatible"]) + b"\0"):
        raise ValueError("observed architecture/board/kernel/device-tree identity differs")
    command = text["cmdline"].split()
    if ([item for item in command if item.startswith("root=")] != ["root=/dev/mmcblk0p2"]
            or [item for item in command if item.startswith("console=")] != ["console=ttyS0,115200"]):
        raise ValueError("booted root/console arguments differ")
    hashes = {}
    for line in text["file_hashes"].splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  (/[^ ]+)", line)
        if match is None or match[2] in hashes:
            raise ValueError("malformed/duplicate installed file hash")
        hashes[match[2]] = match[1]
    if hashes != expected["file_sha256"]:
        raise ValueError("observed diagnostic binary/configuration hashes differ")
    roots = [line.split() for line in text["mountinfo"].splitlines()
             if len(line.split()) > 6 and line.split()[4] == "/"]
    if len(roots) != 1:
        raise ValueError("expected exactly one mounted root filesystem")
    root = roots[0]
    split = root.index("-")
    if (root[2] != text["root_dev"] or root[3] != "/" or root[split + 1] != "ext4"
            or "rw" not in root[5].split(",") or not re.fullmatch(r"\d+:\d+", text["root_dev"])):
        raise ValueError("root mount does not match writable mmcblk0p2 ext4")
    pairs = [line.split("=", 1) for line in text["root_uevent"].splitlines()]
    uevent = dict(pairs)
    if (len(uevent) != len(pairs) or uevent.get("DEVNAME") != "mmcblk0p2" or uevent.get("PARTN") != "2"
            or uevent.get("MAJOR", "") + ":" + uevent.get("MINOR", "") != text["root_dev"]):
        raise ValueError("root partition sysfs identity differs")
    mac = text["mac"]
    if (not re.fullmatch(r"(?:[0-9a-f]{2}:){5}[0-9a-f]{2}", mac)
            or mac == "00:00:00:00:00:00" or int(mac[:2], 16) & 1 or text["carrier"] != "1"):
        raise ValueError("Ethernet has no valid unicast MAC/carrier")
    addresses = [ipaddress.IPv4Interface(item).ip for item in re.findall(r"\binet ([0-9./]+)", text["addresses"])]
    connection = text["ssh_connection"].split()
    if (len(connection) != 4 or ipaddress.ip_address(connection[2]) not in addresses
            or not all(item.isdecimal() and 1 <= int(item) <= 65535 for item in connection[1::2])):
        raise ValueError("authenticated SSH server address is not an observed eth0 IPv4 address")
    ipaddress.ip_address(connection[0])
    if not re.search(r"^MemTotal:\s+[1-9][0-9]* kB$", text["meminfo"], re.M):
        raise ValueError("booted memory observation missing")
    return {"boot_id": boot_id, "architecture": text["architecture"], "kernel_release": text["kernel_release"],
            "model": tree["model"], "mac": mac, "ipv4": [str(item) for item in addresses],
            "ssh_connection": connection, "root_device_number": text["root_dev"],
            "root_mount_source": root[split + 2], "meminfo": text["meminfo"], "routes": text["routes"],
            "installed_file_sha256": hashes, "whole_image_observed_sha256": None,
            "image_identity_scope": "selected diagnostic files match; whole SD image not attested",
            "dhcp_lease": "not established by address observation"}


def verify_transport(log, key, authentication, host, port, user, ssh_connection):
    validate_ssh_endpoint(host, port, user)
    text = log.decode("utf-8", errors="replace").replace("\r\n", "\n").replace("\r", "\n")
    keys = re.findall(r"Server host key: (\S+) (SHA256:[A-Za-z0-9+/]+)", text)
    if keys != [(key["algorithm"], key["fingerprint"])]:
        raise ValueError("authenticated SSH server key differs or is missing")
    banners = re.findall(
        r"^debug[0-9]+: Remote protocol version 2\.0, remote software version dropbear_[A-Za-z0-9._-]+$",
        text, re.M)
    if len(banners) != 1:
        raise ValueError("expected SSH-2.0 Dropbear protocol")
    if authentication not in ("publickey", "password"):
        raise ValueError("unsupported SSH authentication method")
    requests = re.findall(r"^debug[0-9]+: Authenticating to (\S+):([0-9]+) as '([^'\r\n]+)'$", text, re.M)
    if requests != [(host, str(port), user)]:
        raise ValueError("SSH authentication request differs from receipt endpoint")
    authenticated = re.findall(
        r'^(?:debug[0-9]+: )?Authenticated to (\S+) \(\[([^\]]+)\]:([0-9]+)\) using "([^"\r\n]+)"\.$',
        text, re.M)
    if len(authenticated) != 1:
        raise ValueError("authenticated SSH endpoint is missing or ambiguous")
    connected_host, connected_ip, connected_port, method = authenticated[0]
    if (connected_host != host or int(connected_port) != port or method != authentication
            or len(ssh_connection) != 4 or int(ssh_connection[3]) != port
            or ipaddress.ip_address(connected_ip) != ipaddress.ip_address(ssh_connection[2])):
        raise ValueError("authenticated SSH endpoint differs from receipt or observed SSH_CONNECTION")


def serial_observation(data, expected):
    # Import an existing log; never open a serial device or change DTR/termios.
    if not data or len(data) > 8 * 1024 * 1024:
        raise ValueError("serial log must be nonempty and bounded")
    text = data.decode("utf-8", errors="replace")
    releases = re.findall(r"Linux version ([^\s]+)", text)
    if not releases or set(releases) != {expected["kernel_release"]}:
        raise ValueError("serial log kernel banner missing or differs")
    return {"sha256": sha(data), "kernel_banners": len(releases),
            "binding": "caller-supplied log; same-boot correlation not independently established"}


class CaptureFailure(ValueError):
    def __init__(self, message, stdout, stderr):
        super().__init__(message)
        self.stdout, self.stderr = stdout, stderr


def run_ssh(argv, *, input, timeout, env):
    """Bound both pipe buffers while running only our child; never accumulate unbounded output."""
    captured = {"stdout": bytearray(), "stderr": bytearray()}
    deadline = time.monotonic() + timeout
    process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               env=env, start_new_session=True)
    try:
        with selectors.DefaultSelector() as selector:
            for name in ("stdout", "stderr"):
                stream = getattr(process, name)
                os.set_blocking(stream.fileno(), False)
                selector.register(stream, selectors.EVENT_READ, name)
            os.set_blocking(process.stdin.fileno(), False)
            selector.register(process.stdin, selectors.EVENT_WRITE, "stdin")
            pending = memoryview(input)
            while selector.get_map():
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError("SSH timed out")
                for ready, _events in selector.select(remaining):
                    stream, name = ready.fileobj, ready.data
                    if name == "stdin":
                        try:
                            pending = pending[os.write(stream.fileno(), pending[:4096]):]
                        except BrokenPipeError:
                            pending = pending[:0]
                        if not pending:
                            selector.unregister(stream)
                            stream.close()
                        continue
                    data = os.read(stream.fileno(), 4096)
                    if not data:
                        selector.unregister(stream)
                        stream.close()
                        continue
                    available = CAPTURE_LIMIT - len(captured[name])
                    captured[name].extend(data[:available])
                    if len(data) > available:
                        raise ValueError("SSH " + name + " exceeded capture limit")
            try:
                status = process.wait(timeout=max(0, deadline - time.monotonic()))
            except subprocess.TimeoutExpired as error:
                raise TimeoutError("SSH timed out") from error
        return subprocess.CompletedProcess(argv, status, bytes(captured["stdout"]), bytes(captured["stderr"]))
    except (ValueError, TimeoutError) as error:
        raise CaptureFailure(str(error), bytes(captured["stdout"]), bytes(captured["stderr"])) from error
    finally:
        if process.poll() is None:
            process.kill()  # This exact Popen child only; no process-name or process-group signals.
        process.wait()
        for stream in (process.stdin, process.stdout, process.stderr):
            stream.close()


def collect(args):
    collector_sha256 = sha(Path(__file__).read_bytes())
    expected_bytes = regular_bytes(args.expectation)
    expected = load_json(expected_bytes)
    validate_expectation(expected)
    known = regular_bytes(args.known_hosts, 65536)
    key = host_key(known, args.host, args.port)
    identity = Path(args.identity_file).absolute() if args.identity_file else None
    password = Path(args.password_file).absolute() if getattr(args, "password_file", None) else None
    if (identity is None) == (password is None):
        raise ValueError("provide exactly one explicit private key or password file")
    credential = identity or password
    if credential.is_symlink() or not credential.is_file() or credential.stat().st_mode & 0o077:
        raise ValueError("credential must be a private regular file with mode 0600 or stricter")
    if password:
        secret = regular_bytes(password, 1024)
        if not secret.rstrip(b"\n") or b"\n" in secret.rstrip(b"\n") or b"\0" in secret or secret.endswith(b"\n\n"):
            raise ValueError("password file must contain one nonempty password line")
    if (not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}", args.board_id)
            or not 1 <= args.timeout <= 300):
        raise ValueError("invalid board ID or timeout")
    serial = regular_bytes(args.serial_log)
    serial_info = serial_observation(serial, expected)
    output = Path(args.output).absolute()
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    (output / "expectation.json").write_bytes(expected_bytes)
    (output / "known_hosts").write_bytes(known)
    (output / "serial.log").write_bytes(serial)
    (output / "read-only.sh").write_bytes(SHELL)
    argv = ssh_argv(args.host, args.port, args.user, output / "known_hosts", identity, key)
    try:
        environment = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "LC_ALL": "C"}
        with tempfile.TemporaryDirectory(prefix="de25-first-boot-auth-") as temporary:
            if password:
                askpass = Path(temporary) / "askpass"
                askpass.write_text('#!/bin/sh\nexec /bin/cat "$FES_DE25_PASSWORD_FILE"\n')
                askpass.chmod(0o700)
                environment.update(SSH_ASKPASS=str(askpass), SSH_ASKPASS_REQUIRE="force",
                                   DISPLAY="de25-first-boot:0", FES_DE25_PASSWORD_FILE=str(password))
            result = run_ssh(argv, input=SHELL, timeout=args.timeout, env=environment)
    except CaptureFailure as error:
        (output / "ssh.stdout").write_bytes(error.stdout or b"")
        (output / "ssh.stderr").write_bytes(error.stderr or b"")
        raise ValueError(str(error) + "; partial evidence retained, no successful receipt") from error
    (output / "ssh.stdout").write_bytes(result.stdout)
    (output / "ssh.stderr").write_bytes(result.stderr)
    if result.returncode:
        raise ValueError(f"SSH failed ({result.returncode}); evidence retained, no successful receipt")
    authentication = "publickey" if identity else "password"
    observed = observe(result.stdout, expected)
    verify_transport(result.stderr, key, authentication, args.host, args.port, args.user,
                     observed["ssh_connection"])
    receipt = {"format": 1, "kind": "de25-first-boot-observation", "board_id": args.board_id,
               "collected_utc": datetime.now(timezone.utc).isoformat(),
               "classification": "read-only-board-observation", "host": args.host, "port": args.port,
               "user": args.user, "authentication": authentication,
               "ssh_host_key": key, "serial": serial_info, "observed": observed,
               "collector_sha256": collector_sha256,
               "files": {name: sha(regular_bytes(output / name)) for name in
                         ("expectation.json", "known_hosts", "serial.log", "read-only.sh", "ssh.stdout", "ssh.stderr")}}
    save_json(output / "receipt.json", receipt)
    return receipt


def verify(directory):
    directory = Path(directory)
    receipt = load_json(regular_bytes(directory / "receipt.json"))
    names = {"expectation.json", "known_hosts", "serial.log", "read-only.sh", "ssh.stdout", "ssh.stderr"}
    if (receipt.get("format") != 1 or receipt.get("kind") != "de25-first-boot-observation"
            or receipt.get("classification") != "read-only-board-observation"
            or set(receipt["files"]) != names):
        raise ValueError("unsupported observation receipt")
    data = {name: regular_bytes(directory / name) for name in names}
    if any(sha(data[name]) != receipt["files"][name] for name in names) or data["read-only.sh"] != SHELL:
        raise ValueError("captured evidence hash or fixed collection script differs")
    expected = load_json(data["expectation.json"])
    validate_ssh_endpoint(receipt.get("host"), receipt.get("port"), receipt.get("user"))
    if (not isinstance(receipt.get("board_id"), str)
            or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}", receipt["board_id"])):
        raise ValueError("receipt board ID is malformed")
    if receipt.get("collector_sha256") != sha(Path(__file__).read_bytes()):
        raise ValueError("receipt collector identity differs from this collector")
    collected = receipt.get("collected_utc")
    try:
        collected_at = datetime.fromisoformat(collected)
    except (TypeError, ValueError) as error:
        raise ValueError("receipt collection time is malformed") from error
    if (collected_at.utcoffset() != timedelta(0) or collected_at.isoformat() != collected
            or collected_at > datetime.now(timezone.utc) + timedelta(minutes=5)):
        raise ValueError("receipt collection time is not a sane UTC timestamp")
    key = host_key(data["known_hosts"], receipt["host"], receipt["port"])
    observed = observe(data["ssh.stdout"], expected)
    verify_transport(data["ssh.stderr"], key, receipt["authentication"], receipt["host"], receipt["port"],
                     receipt["user"], observed["ssh_connection"])
    if (key != receipt["ssh_host_key"] or observed != receipt["observed"]
            or serial_observation(data["serial.log"], expected) != receipt["serial"]):
        raise ValueError("receipt identity/observations differ from captured bytes")
    return receipt


def compare(before, after):
    first, second = verify(before), verify(after)
    checks = {"same_board_label": first["board_id"] == second["board_id"],
              "same_expectation": first["files"]["expectation.json"] == second["files"]["expectation.json"],
              "changed_boot_id": first["observed"]["boot_id"] != second["observed"]["boot_id"],
              "stable_ssh_host_key": first["ssh_host_key"] == second["ssh_host_key"],
              "stable_mac": first["observed"]["mac"] == second["observed"]["mac"]}
    return {"format": 1, "kind": "de25-manual-reboot-comparison", "checks": checks,
            "passed": all(checks.values()), "before_receipt_sha256": sha(regular_bytes(Path(before) / "receipt.json")),
            "after_receipt_sha256": sha(regular_bytes(Path(after) / "receipt.json")),
            "whole_image_identity": "not attested", "reboot_action": "external/manual; never issued by this tool",
            "dhcp_address_may_change": True, "hardware_acceptance": "requires review of exact-artifact board evidence"}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    preparation = commands.add_parser("prepare", help="inspect retained image and freeze expected identities offline")
    preparation.add_argument("--image-output", type=Path, required=True)
    preparation.add_argument("--output", type=Path, required=True)
    capture = commands.add_parser("collect", help="explicit read-only SSH collection and existing serial-log import")
    for name in ("expectation", "known-hosts", "serial-log", "output"):
        capture.add_argument("--" + name, type=Path, required=True)
    authentication = capture.add_mutually_exclusive_group(required=True)
    authentication.add_argument("--identity-file", type=Path, help="explicit private key; no agent/default keys")
    authentication.add_argument("--password-file", type=Path, help="explicit private password file; no interactive prompt")
    for name in ("host", "user", "board-id"):
        capture.add_argument("--" + name, required=True)
    capture.add_argument("--port", type=int, default=22)
    capture.add_argument("--timeout", type=int, default=45)
    inspection = commands.add_parser("verify", help="reparse/hash captured evidence offline")
    inspection.add_argument("--receipt", type=Path, required=True)
    comparison = commands.add_parser("compare", help="verify two captures across a caller-performed reboot offline")
    comparison.add_argument("--before", type=Path, required=True)
    comparison.add_argument("--after", type=Path, required=True)
    comparison.add_argument("--output", type=Path)
    args = parser.parse_args(argv)
    try:
        if args.command == "prepare":
            save_json(args.output, prepare(args.image_output))
            return 0
        if args.command == "collect":
            result = collect(args)
        elif args.command == "verify":
            result = verify(args.receipt)
        else:
            result = compare(args.before, args.after)
        if getattr(args, "output", None) and args.command == "compare":
            save_json(args.output, result)
        else:
            print(json.dumps(result, indent=2, sort_keys=True))
        return 1 if result.get("passed") is False else 0
    except (OSError, ValueError, KeyError, IndexError, TypeError, UnicodeError, struct.error,
            subprocess.SubprocessError, tarfile.TarError) as error:
        parser.exit(1, "de25-first-boot: " + str(error) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
