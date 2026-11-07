"""First-boot collection is tested with captured bytes and fake SSH, never a board."""
import base64
import io
import json
from datetime import datetime, timedelta, timezone
from pathlib import Path
import struct
import subprocess
import sys
import tarfile
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest import mock

from scripts import de25_first_boot as boot


def expectation():
    return {"format": 1, "kind": "de25-first-boot-expectation",
            "kernel_release": "6.12.11-gd7d192a9ddd9",
            "file_sha256": {name: boot.sha(name.encode()) for name in boot.FILES},
            "boot_contract": {"format": 1, "board": "de25-nano", "classification": "host-only-boot-contract",
                              "image_producer": "a" * 40, "image_sha256": "b" * 64,
                              "boot": {"root_device": "/dev/mmcblk0p2", "linux_device_tree": {
                                  "model": "SoCFPGA Agilex5 Terasic DE25-Nano",
                                  "compatible": ["terasic,de25-nano", "intel,socfpga-agilex5"]}}}}


def known_hosts(host="192.0.2.25", port=22, seed=0):
    name = b"ssh-ed25519"
    blob = struct.pack(">I", len(name)) + name + struct.pack(">I", 32) + bytes([seed]) * 32
    token = host if port == 22 else f"[{host}]:{port}"
    return f"{token} ssh-ed25519 {base64.b64encode(blob).decode()}\n".encode()


def transport(key=None, host="192.0.2.25", port=22, user="root", connected_host=None):
    key = key or boot.host_key(known_hosts(host, port), host, port)
    connected_host = connected_host or host
    return (f"debug1: Remote protocol version 2.0, remote software version dropbear_2020.81\n"
            f"debug1: Server host key: {key['algorithm']} {key['fingerprint']}\n"
            f"debug1: Authenticating to {host}:{port} as '{user}'\n"
            f'Authenticated to {host} ([{connected_host}]:{port}) using "publickey".\n').encode()


def observation(changes=None, expected=None):
    expected = expected or expectation()
    fields = {
        "boot_id_start": b"de250000-0000-4000-8000-000000000001\n",
        "architecture": b"aarch64", "kernel_release": expected["kernel_release"].encode(),
        "board": b"de25-nano\n", "model": b"SoCFPGA Agilex5 Terasic DE25-Nano\0",
        "compatible": b"terasic,de25-nano\0intel,socfpga-agilex5\0",
        "cmdline": b"console=ttyS0,115200 root=/dev/mmcblk0p2 rw rootwait\n",
        "meminfo": b"MemTotal:        1930000 kB\nMemFree: 1800000 kB\n",
        "mountinfo": b"20 1 179:2 / / rw,relatime - ext4 /dev/root rw\n",
        "root_dev": b"179:2\n", "root_uevent": b"MAJOR=179\nMINOR=2\nDEVNAME=mmcblk0p2\nPARTN=2\n",
        "mac": b"02:25:00:00:00:01\n", "carrier": b"1\n",
        "addresses": b"2: eth0: <BROADCAST,MULTICAST,UP>\n    inet 192.0.2.25/24 brd 192.0.2.255 scope global eth0",
        "routes": b"default via 192.0.2.1 dev eth0", "ssh_connection": b"192.0.2.1 23456 192.0.2.25 22",
        "file_hashes": "\n".join(f"{value}  {name}" for name, value in expected["file_sha256"].items()).encode(),
        "boot_id_end": b"de250000-0000-4000-8000-000000000001\n"}
    fields.update(changes or {})
    return ("FES-DE25-FIRST-BOOT\t1\n" + "".join(f"{name}\t{fields[name].hex()}\n" for name in boot.FIELDS)
            + "FES-DE25-FIRST-BOOT-END\t1\n").encode()


class PrepareTests(unittest.TestCase):
    def image_fixture(self, directory):
        expected = expectation()
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w") as target:
            for name in boot.FILES:
                content = name.encode()
                member = tarfile.TarInfo("." + name)
                member.size = len(content)
                target.addfile(member, io.BytesIO(content))
        (directory / "rootfs.tar").write_bytes(archive.getvalue())
        (directory / "vendor-boot").mkdir()
        kernel = b"header\0Linux version 6.12.11-gd7d192a9ddd9 (build@host)\0"
        (directory / "vendor-boot/Image").write_bytes(kernel)
        record = {"fes_revision": "a" * 40, "files": {"rootfs.tar": boot.sha(archive.getvalue()),
                  "vendor-boot/Image": boot.sha(kernel), "fes-de25-nano.img": "b" * 64}}
        (directory / "image.json").write_text(json.dumps(record))
        return expected

    def test_preparation_reuses_inspector_and_exact_image_file_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            expected = self.image_fixture(directory)
            with mock.patch.object(boot.subprocess, "run", return_value=SimpleNamespace(
                    stdout=json.dumps(expected["boot_contract"]).encode())) as run:
                result = boot.prepare(directory)
            self.assertEqual(result["file_sha256"], expected["file_sha256"])
            self.assertEqual(result["kernel_release"], expected["kernel_release"])
            self.assertIn("scripts/de25_boot_contract.py", run.call_args.args[0][1])
            self.assertEqual(run.call_args.args[0][-2:], ["--image-output", str(directory)])

    def test_preparation_rejects_changed_artifact_or_receipt_during_inspection(self):
        for filename in ("image.json", "rootfs.tar", "vendor-boot/Image"):
            with self.subTest(filename=filename), tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                expected = self.image_fixture(directory)
                def replace(*_args, **_kwargs):
                    (directory / filename).write_bytes(b"changed")
                    return SimpleNamespace(stdout=json.dumps(expected["boot_contract"]).encode())
                with mock.patch.object(boot.subprocess, "run", side_effect=replace), self.assertRaises(ValueError):
                    boot.prepare(directory)

    def test_preparation_surfaces_inspector_stderr(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            self.image_fixture(directory)
            error = subprocess.CalledProcessError(1, ["de25_boot_contract.py"], stderr=b"inspector detail\n")
            with mock.patch.object(boot.subprocess, "run", side_effect=error), self.assertRaisesRegex(
                    ValueError, "inspector detail"):
                boot.prepare(directory)

    def test_exclusive_output_and_duplicate_json_keys(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "receipt.json"
            boot.save_json(path, {})
            with self.assertRaises(FileExistsError):
                boot.save_json(path, {})
        with self.assertRaises(ValueError):
            boot.load_json('{"format":1,"format":2}')


class ProtocolTests(unittest.TestCase):
    def test_valid_observations_keep_full_image_and_dhcp_limits_explicit(self):
        result = boot.observe(observation(), expectation())
        self.assertEqual(result["mac"], "02:25:00:00:00:01")
        self.assertEqual(result["root_device_number"], "179:2")
        self.assertIsNone(result["whole_image_observed_sha256"])
        self.assertIn("not established", result["dhcp_lease"])

    def test_every_required_identity_and_root_boundary_fails_closed(self):
        changes = {"architecture": b"armv7l", "kernel_release": b"6.1", "board": b"de10-nano\n",
                   "model": b"different\0", "compatible": b"intel,socfpga-agilex5\0",
                   "cmdline": b"console=ttyS0,115200 root=/dev/sda2", "mac": b"01:25:00:00:00:01",
                   "carrier": b"0", "addresses": b"inet 192.0.2.99/24", "file_hashes": b"0  /bin/busybox",
                   "root_dev": b"8:2", "root_uevent": b"DEVNAME=sda2\nPARTN=2\n", "meminfo": b"MemFree: 1 kB",
                   "mountinfo": b"20 1 179:2 / / ro - ext4 /dev/root ro\n",
                   "ssh_connection": b"192.0.2.1 12345 192.0.2.25 0"}
        for field, value in changes.items():
            with self.subTest(field=field), self.assertRaises(ValueError):
                boot.observe(observation({field: value}), expectation())

    def test_reboot_during_capture_is_refused(self):
        with self.assertRaisesRegex(ValueError, "boot ID"):
            boot.observe(observation({"boot_id_end": b"de250000-0000-4000-8000-000000000002"}), expectation())
        zero = b"00000000-0000-0000-0000-000000000000"
        with self.assertRaisesRegex(ValueError, "boot ID"):
            boot.observe(observation({"boot_id_start": zero, "boot_id_end": zero}), expectation())

    def test_partition_major_minor_and_unique_sysfs_keys_match_mount(self):
        for data in (b"MAJOR=8\nMINOR=2\nDEVNAME=mmcblk0p2\nPARTN=2\n",
                     b"MAJOR=179\nMINOR=2\nDEVNAME=mmcblk0p2\nPARTN=1\nPARTN=2\n"):
            with self.assertRaisesRegex(ValueError, "sysfs identity"):
                boot.observe(observation({"root_uevent": data}), expectation())

    def test_protocol_rejects_missing_duplicate_reordered_truncated_nonhex_and_extra_fields(self):
        good = observation()
        bad = [good.replace(b"FES-DE25-FIRST-BOOT\t1", b"FES-DE25-FIRST-BOOT\t2"),
               good.replace(b"boot_id_end", b"boot_id_start"), good[:-10], good + b"banner\n",
               good.replace(b"architecture\t", b"architecture\tgg"), good.replace(b"mac\t", b"carrier\t"),
               good + b"x" * 1024 * 1024]
        for data in bad:
            with self.subTest(data=data[:60]), self.assertRaises(ValueError):
                boot.parse_wire(data)

    def test_expectation_mismatch_is_refused(self):
        for field, value in (("format", 2), ("kind", "other"), ("kernel_release", "bad\nrelease")):
            expected = expectation()
            expected[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                boot.validate_expectation(expected)

    def test_serial_import_is_bounded_and_does_not_claim_same_boot_binding(self):
        result = boot.serial_observation(b"Linux version 6.12.11-gd7d192a9ddd9 (test)\n", expectation())
        self.assertIn("not independently", result["binding"])
        for data in (b"", b"Linux version 6.1 (other)", b"x" * (8 * 1024 * 1024 + 1)):
            with self.assertRaises(ValueError):
                boot.serial_observation(data, expectation())

    def test_host_key_requires_one_exact_host_port_and_no_enrollment(self):
        for data in (known_hosts() * 2, known_hosts("other"), known_hosts(port=2222),
                     known_hosts().replace(b"192.0.2.25", b"*"), b"@cert-authority host ssh-ed25519 key\n",
                     known_hosts().replace(b"ssh-ed25519", b"ssh-dss", 1)):
            with self.subTest(data=data[:60]), self.assertRaises(ValueError):
                boot.host_key(data, "192.0.2.25", 22)
        key = boot.host_key(known_hosts(port=2222), "192.0.2.25", 2222)
        self.assertEqual(key["algorithm"], "ssh-ed25519")

    def test_protocol_key_authentication_and_dropbear_banner_required(self):
        key = boot.host_key(known_hosts(), "192.0.2.25", 22)
        good = transport(key)
        connection = ["192.0.2.1", "23456", "192.0.2.25", "22"]
        boot.verify_transport(good, key, "publickey", "192.0.2.25", 22, "root", connection)
        # This is OpenSSH's pipe output: one debug-prefixed Dropbear server banner and CRLF records.
        crlf = good.replace(b"\n", b"\r\n")
        boot.verify_transport(crlf, key, "publickey", "192.0.2.25", 22, "root", connection)
        authenticated_line = next(line for line in good.splitlines(keepends=True)
                                  if line.startswith(b"Authenticated to "))
        request_line = next(line for line in good.splitlines(keepends=True)
                            if b"Authenticating to " in line)
        unanchored_banner = good.replace(b"debug1: Remote protocol", b"debug1: text Remote protocol", 1)
        for data in (good.replace(key["fingerprint"].encode(), b"SHA256:bad"),
                     good.replace(b"dropbear_2020.81", b"OpenSSH_10"),
                     good.replace(b'using "publickey"', b'using "password"'), good + good,
                     good + authenticated_line, good + request_line, unanchored_banner):
            with self.subTest(data=data[:60]), self.assertRaises(ValueError):
                boot.verify_transport(data, key, "publickey", "192.0.2.25", 22, "root", connection)


class CollectionTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.args = SimpleNamespace(expectation=self.root / "expected.json", known_hosts=self.root / "pin",
                                    identity_file=self.root / "private", serial_log=self.root / "serial",
                                    host="192.0.2.25", port=22, user="root", board_id="DE25-revB-1",
                                    timeout=45, output=self.root / "boot1")
        self.args.expectation.write_text(json.dumps(expectation()))
        self.args.known_hosts.write_bytes(known_hosts())
        self.args.identity_file.write_bytes(b"fixture-private-key-not-used")
        self.args.identity_file.chmod(0o600)
        self.args.serial_log.write_bytes(b"Linux version 6.12.11-gd7d192a9ddd9 (builder)\n")

    def capture(self, changes=None, stderr=None):
        with mock.patch.object(boot, "run_ssh", return_value=SimpleNamespace(
                returncode=0, stdout=observation(changes), stderr=stderr or transport())) as run:
            result = boot.collect(self.args)
        self.last_run = run
        return result

    def test_fake_capture_offline_verify_and_strict_ssh_options(self):
        result = self.capture()
        self.assertEqual(boot.verify(self.args.output), result)
        argv = self.last_run.call_args.args[0]
        self.assertEqual(argv[:5], ["ssh", "-F", "/dev/null", "-T", "-v"])
        for option in ("IdentityAgent=none", "BatchMode=yes", "StrictHostKeyChecking=yes",
                       "IdentitiesOnly=yes", "PasswordAuthentication=no", "ProxyCommand=none",
                       "ControlPath=none", "UpdateHostKeys=no", "GlobalKnownHostsFile=/dev/null"):
            self.assertIn(option, argv)
        self.assertNotIn("IdentityFile=none", argv)
        self.assertNotIn("-i", argv)
        self.assertEqual(argv[-2:], ["192.0.2.25", "sh -s"])
        self.assertEqual(self.last_run.call_args.kwargs["input"], boot.SHELL)
        self.assertNotIn("SSH_AUTH_SOCK", self.last_run.call_args.kwargs["env"])
        self.assertNotIn("fixture-private-key-not-used", json.dumps(result))

    def test_no_overwrite_and_failed_identity_before_contact(self):
        self.capture()
        with mock.patch.object(boot, "run_ssh") as run, self.assertRaises(FileExistsError):
            boot.collect(self.args)
        run.assert_not_called()
        self.args.output = self.root / "new"
        self.args.identity_file.chmod(0o644)
        with mock.patch.object(boot, "run_ssh") as run, self.assertRaises(ValueError):
            boot.collect(self.args)
        run.assert_not_called()

    def test_failed_ssh_retains_raw_evidence_without_success_receipt(self):
        with mock.patch.object(boot, "run_ssh", return_value=SimpleNamespace(
                returncode=255, stdout=b"partial", stderr=b"host key changed")), self.assertRaises(ValueError):
            boot.collect(self.args)
        self.assertEqual((self.args.output / "ssh.stderr").read_bytes(), b"host key changed")
        self.assertFalse((self.args.output / "receipt.json").exists())

    def test_stock_password_auth_is_explicit_private_and_never_in_argv_or_receipt(self):
        self.args.identity_file = None
        self.args.password_file = self.root / "password"
        self.args.password_file.write_text("fixture-password\n")
        self.args.password_file.chmod(0o600)
        log = transport().replace(b'using "publickey"', b'using "password"')
        receipt = self.capture(stderr=log)
        self.assertEqual(boot.verify(self.args.output), receipt)
        self.assertEqual(receipt["authentication"], "password")
        call = self.last_run.call_args
        self.assertIn("PreferredAuthentications=password", call.args[0])
        self.assertIn("PubkeyAuthentication=no", call.args[0])
        self.assertIn("IdentityFile=none", call.args[0])
        self.assertIn("NumberOfPasswordPrompts=1", call.args[0])
        self.assertNotIn("-i", call.args[0])
        env = call.kwargs["env"]
        self.assertEqual(env["SSH_ASKPASS_REQUIRE"], "force")
        self.assertEqual(env["FES_DE25_PASSWORD_FILE"], str(self.args.password_file))
        self.assertFalse(Path(env["SSH_ASKPASS"]).exists())
        self.assertNotIn("fixture-password", json.dumps(receipt))
        self.assertNotIn("fixture-password", repr(call.args[0]))

    def test_password_mode_effective_config_suppresses_default_identity_files(self):
        key = boot.host_key(known_hosts(), "192.0.2.25", 22)
        argv = boot.ssh_argv("192.0.2.25", 22, "root", self.root / "known_hosts", None, key)
        result = subprocess.run(argv[:1] + ["-G"] + argv[1:], capture_output=True, check=True)
        text = result.stdout.decode()
        identity_files = [line.removeprefix("identityfile ") for line in text.splitlines()
                          if line.startswith("identityfile ")]
        self.assertEqual(identity_files, ["none"])
        self.assertNotIn("~/.ssh/", text)

    def test_missing_or_multiple_credentials_are_rejected_before_contact(self):
        self.args.password_file = self.args.identity_file
        for identity in (self.args.identity_file, None):
            self.args.identity_file = identity
            if identity is None:
                self.args.password_file = None
            with mock.patch.object(boot, "run_ssh") as run, self.assertRaises(ValueError):
                boot.collect(self.args)
            run.assert_not_called()

    def test_timeout_retains_partial_bytes_without_success_receipt(self):
        with mock.patch.object(boot, "run_ssh", side_effect=boot.CaptureFailure(
                "SSH timed out", b"partial", b"timeout")), self.assertRaises(ValueError):
            boot.collect(self.args)
        self.assertEqual((self.args.output / "ssh.stdout").read_bytes(), b"partial")
        self.assertFalse((self.args.output / "receipt.json").exists())

    def test_overflow_retains_capped_evidence_without_success_receipt(self):
        with mock.patch.object(boot, "run_ssh", side_effect=boot.CaptureFailure(
                "SSH stdout exceeded capture limit", b"x" * boot.CAPTURE_LIMIT, b"")), self.assertRaises(ValueError):
            boot.collect(self.args)
        self.assertEqual((self.args.output / "ssh.stdout").stat().st_size, boot.CAPTURE_LIMIT)
        self.assertFalse((self.args.output / "receipt.json").exists())

    def test_identity_or_protocol_failure_does_not_publish_success_receipt(self):
        with self.assertRaises(ValueError):
            self.capture({"board": b"de10-nano"})
        self.assertFalse((self.args.output / "receipt.json").exists())

    def test_tampered_raw_bytes_and_falsified_derived_observations_are_refused(self):
        self.capture()
        receipt = self.args.output / "receipt.json"
        value = json.loads(receipt.read_text())
        value["observed"]["mac"] = "02:25:00:00:00:99"
        receipt.write_text(json.dumps(value))
        with self.assertRaises(ValueError):
            boot.verify(self.args.output)
        value["observed"]["mac"] = "02:25:00:00:00:01"
        receipt.write_text(json.dumps(value))
        (self.args.output / "ssh.stdout").write_bytes(b"replaced")
        with self.assertRaises(ValueError):
            boot.verify(self.args.output)

    def test_verify_binds_receipt_endpoint_collector_and_collection_time(self):
        edits = (
            ("user", {"user": "admin"}),
            ("host", {"host": "192.0.2.99"}),
            ("port", {"port": 2222}),
            ("collector", {"collector_sha256": "0" * 64}),
            ("malformed-time", {"collected_utc": "yesterday"}),
            ("future-time", {"collected_utc": (datetime.now(timezone.utc)
                                                 + timedelta(days=1)).isoformat()}),
            ("malformed-board-id", {"board_id": "DE25 revB 1"}),
        )
        for name, fields in edits:
            with self.subTest(field=name):
                self.args.output = self.root / name
                self.capture()
                receipt_path = self.args.output / "receipt.json"
                value = json.loads(receipt_path.read_text())
                value.update(fields)
                if name in ("host", "port"):
                    host = fields.get("host", value["host"])
                    port = fields.get("port", value["port"])
                    pin = known_hosts(host, port)
                    (self.args.output / "known_hosts").write_bytes(pin)
                    value["files"]["known_hosts"] = boot.sha(pin)
                receipt_path.write_text(json.dumps(value))
                with self.assertRaises(ValueError):
                    boot.verify(self.args.output)

    def test_rewritten_endpoint_transcript_still_must_match_observed_ssh_connection(self):
        self.capture()
        receipt_path = self.args.output / "receipt.json"
        value = json.loads(receipt_path.read_text())
        value["host"] = "192.0.2.99"
        value["port"] = 2222
        pin = known_hosts(value["host"], value["port"])
        (self.args.output / "known_hosts").write_bytes(pin)
        value["files"]["known_hosts"] = boot.sha(pin)
        stderr = transport(host=value["host"], port=value["port"])
        (self.args.output / "ssh.stderr").write_bytes(stderr)
        value["files"]["ssh.stderr"] = boot.sha(stderr)
        receipt_path.write_text(json.dumps(value))
        with self.assertRaisesRegex(ValueError, "SSH_CONNECTION"):
            boot.verify(self.args.output)

    def test_manual_reboot_comparison_allows_address_change_but_requires_new_boot(self):
        before = self.args.output
        self.capture()
        self.args.output = self.root / "boot2"
        self.args.host = "192.0.2.26"
        self.args.known_hosts.write_bytes(known_hosts(self.args.host, self.args.port))
        new_id = b"de250000-0000-4000-8000-000000000002\n"
        self.capture({"boot_id_start": new_id, "boot_id_end": new_id,
                      "addresses": b"inet 192.0.2.26/24", "ssh_connection": b"192.0.2.1 45678 192.0.2.26 22"},
                     transport(host=self.args.host, port=self.args.port))
        result = boot.compare(before, self.args.output)
        self.assertTrue(result["passed"])
        self.assertEqual(result["whole_image_identity"], "not attested")

    def test_unchanged_boot_mac_or_pinned_key_change_fail_comparison(self):
        before = self.args.output
        self.capture()
        for case in ("same-boot", "mac", "key"):
            self.args.output = self.root / case
            changes = {}
            if case != "same-boot":
                new_id = b"de250000-0000-4000-8000-000000000002\n"
                changes.update(boot_id_start=new_id, boot_id_end=new_id)
            if case == "mac":
                changes["mac"] = b"02:25:00:00:00:99\n"
            key = None
            if case == "key":
                self.args.known_hosts.write_bytes(known_hosts(seed=1))
                key = boot.host_key(self.args.known_hosts.read_bytes(), self.args.host, 22)
            self.capture(changes, transport(key))
            with self.subTest(case=case):
                self.assertFalse(boot.compare(before, self.args.output)["passed"])

    def test_fixed_script_is_valid_shell_and_has_no_device_or_mutation_commands(self):
        subprocess.run(["sh", "-n"], input=boot.SHELL, check=True)
        for forbidden in (b"/dev/mem", b"reboot", b"sudo", b"mount ", b"dd ", b"flash", b"stty", b" > "):
            self.assertNotIn(forbidden, boot.SHELL)

    def test_host_and_user_cannot_inject_options_or_shell(self):
        key = boot.host_key(known_hosts(), "192.0.2.25", 22)
        for host, user in (("-oProxyCommand=bad", "root"), ("host;touch", "root"), ("host", "root;touch")):
            with self.assertRaises(ValueError):
                boot.ssh_argv(host, 22, user, "/pin", "/private", key)

    def test_ssh_paths_reject_openssh_token_expansion(self):
        key = boot.host_key(known_hosts(), "192.0.2.25", 22)
        for pin, identity in (("/pin%h", "/private"), ("/pin${HOME}", "/private"),
                              ("/pin", "/private%h"), ("/pin\nother", "/private")):
            with self.subTest(pin=pin, identity=identity), self.assertRaises(ValueError):
                boot.ssh_argv("192.0.2.25", 22, "root", pin, identity, key)

    def test_openssh_effective_config_has_only_explicit_key_and_pinned_file(self):
        key = boot.host_key(known_hosts(), "192.0.2.25", 22)
        # -G prints effective configuration and exits without connecting.
        directory = self.root / "directory with spaces"
        pinned = directory / "known_hosts"
        identity = directory / "private key"
        argv = boot.ssh_argv("192.0.2.25", 22, "root", pinned, identity, key)
        result = subprocess.run(argv[:1] + ["-G"] + argv[1:], capture_output=True, check=True)
        text = result.stdout.decode()
        self.assertIn("userknownhostsfile " + str(pinned) + "\n", text)
        identity_files = [line.removeprefix("identityfile ") for line in text.splitlines()
                          if line.startswith("identityfile ")]
        self.assertEqual(identity_files, [str(identity)])
        self.assertIn("identityagent none\n", text)
        self.assertIn("stricthostkeychecking true\n", text)


class BoundedProcessTests(unittest.TestCase):
    def test_local_process_exact_limit_and_input_transfer(self):
        program = "import sys; data=sys.stdin.buffer.read(); sys.stdout.buffer.write(data); sys.stderr.buffer.write(b'e'*1048576)"
        result = boot.run_ssh([sys.executable, "-c", program], input=b"fixed-script" * 10000, timeout=5, env={})
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, b"fixed-script" * 10000)
        self.assertEqual(len(result.stderr), boot.CAPTURE_LIMIT)

    def test_local_stdout_and_stderr_floods_are_capped_and_child_stopped(self):
        for name in ("stdout", "stderr"):
            program = (f"import sys,time; sys.{name}.buffer.write(b'x'*2097152); "
                       f"sys.{name}.flush(); time.sleep(10)")
            started = time.monotonic()
            with self.subTest(stream=name), self.assertRaisesRegex(boot.CaptureFailure, "capture limit") as failure:
                boot.run_ssh([sys.executable, "-c", program], input=b"script", timeout=5, env={})
            self.assertEqual(len(getattr(failure.exception, name)), boot.CAPTURE_LIMIT)
            self.assertLess(time.monotonic() - started, 5)

    def test_local_stalled_process_retains_partial_output_and_times_out(self):
        program = "import sys,time; print('partial',flush=True); time.sleep(10)"
        started = time.monotonic()
        with self.assertRaisesRegex(boot.CaptureFailure, "timed out") as failure:
            boot.run_ssh([sys.executable, "-c", program], input=b"script", timeout=.2, env={})
        self.assertEqual(failure.exception.stdout, b"partial\n")
        self.assertLess(time.monotonic() - started, 5)


if __name__ == "__main__":
    unittest.main()
