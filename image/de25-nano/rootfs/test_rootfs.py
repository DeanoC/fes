"""Check the headless target configuration and real post-build hook."""
from pathlib import Path
import hashlib
import os
import shutil
import subprocess
import tempfile
import unittest


ROOTFS = Path(__file__).resolve().parent
FES_ROOT = ROOTFS.parents[2]


class RootfsTests(unittest.TestCase):
    def test_diagnostic_selects_aarch64_console_network_and_ssh(self):
        config = (ROOTFS / "defconfig").read_text().splitlines()
        selected = {line.split("=", 1)[0]: line.split("=", 1)[1]
                    for line in config if line.startswith("BR2_") and "=" in line}
        for option in ("BR2_aarch64", "BR2_cortex_a53", "BR2_TOOLCHAIN_BUILDROOT_GLIBC",
                       "BR2_GCC_VERSION_9_X", "BR2_TOOLCHAIN_BUILDROOT_CXX",
                       "BR2_KERNEL_HEADERS_5_10", "BR2_INIT_BUSYBOX",
                       "BR2_ROOTFS_DEVICE_CREATION_DYNAMIC_MDEV", "BR2_PACKAGE_DROPBEAR",
                       "BR2_TARGET_GENERIC_GETTY_BAUDRATE_115200",
                       "BR2_TARGET_GENERIC_REMOUNT_ROOTFS_RW", "BR2_TARGET_ROOTFS_EXT2_4",
                       "BR2_TARGET_ROOTFS_TAR"):
            self.assertEqual(selected[option], "y", option)
        self.assertEqual(selected["BR2_TARGET_GENERIC_GETTY_PORT"], '"ttyS0"')
        self.assertEqual(selected["BR2_TARGET_GENERIC_HOSTNAME"], '"fes-de25-nano"')
        self.assertEqual(selected["BR2_SYSTEM_DHCP"], '"eth0"')
        self.assertNotIn("BR2_arm", selected)
        self.assertNotIn("BR2_PACKAGE_FOGCAST_MISTER_RUNTIME", selected)
        self.assertNotIn("BR2_LINUX_KERNEL", selected)
        native = FES_ROOT / "image/buildroot/configs/fogcast_target_native_dev_defconfig"
        root_password = next(line for line in config if line.startswith("BR2_TARGET_GENERIC_ROOT_PASSWD="))
        self.assertIn(root_password, native.read_text().splitlines())

    def test_overlay_has_board_network_and_fpga_initialization(self):
        files = {path.relative_to(ROOTFS / "overlay").as_posix()
                 for path in (ROOTFS / "overlay").rglob("*") if path.is_file()}
        self.assertEqual(files, {"etc/fes-board", "etc/network/interfaces", "etc/init.d/S15fpga",
                                 "etc/init.d/S38fes-network-id"})
        self.assertEqual((ROOTFS / "overlay/etc/fes-board").read_text(), "de25-nano\n")
        interfaces = (ROOTFS / "overlay/etc/network/interfaces").read_text()
        self.assertIn("auto eth0\niface eth0 inet dhcp\n", interfaces)

    def test_post_build_preserves_persistent_per_board_ssh_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary) / "target"
            (target / "etc/init.d").mkdir(parents=True)
            shutil.copy2(ROOTFS / "overlay/etc/init.d/S38fes-network-id",
                         target / "etc/init.d/S38fes-network-id")
            (target / "etc/dropbear").symlink_to("/var/run/dropbear")
            result = subprocess.run(["sh", str(ROOTFS / "post-build.sh"), str(target)],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            keys = target / "etc/dropbear"
            self.assertFalse(keys.is_symlink())
            self.assertEqual(keys.stat().st_mode & 0o777, 0o700)
            self.assertEqual(list(keys.iterdir()), [])
            # A host key in a reused tree must not be baked into a new image.
            (keys / "dropbear_ed25519_host_key").write_bytes(b"not a deployable key")
            result = subprocess.run(["sh", str(ROOTFS / "post-build.sh"), str(target)],
                                    capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("generated on the board", result.stderr)

    def test_post_build_rejects_missing_or_stale_network_script(self):
        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary)
            (target / "etc/init.d").mkdir(parents=True)
            for contents in (None, "#!/bin/sh\nexit 0\n"):
                if contents is not None:
                    script = target / "etc/init.d/S38fes-network-id"
                    script.write_text(contents)
                    script.chmod(0o755)
                result = subprocess.run(["sh", str(ROOTFS / "post-build.sh"), str(target)],
                                        capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("network identity", result.stderr)

    def test_post_build_rejects_invalid_target(self):
        for target in ("relative/target", "/nonexistent/fes-de25-rootfs"):
            with self.subTest(target=target):
                result = subprocess.run(["sh", str(ROOTFS / "post-build.sh"), target],
                                        capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)


class NetworkIdentityTests(unittest.TestCase):
    CID = "275048534436344760702d33e0017c00"

    @staticmethod
    def expected(cid):
        digest = hashlib.sha256(cid.lower().encode()).hexdigest()[:10]
        return "02:" + ":".join(digest[n:n+2] for n in range(0, 10, 2))

    def run_script(self, address, cid=CID, action="start", ip_status=0,
                   board="de25-nano"):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for name, value in (("address", address), ("cid", cid), ("board", board)):
                if value is not None:
                    (root / name).write_text(value + "\n")
            (root / "cat").write_text("""#!/bin/sh
case "$1" in
 /etc/fes-board) exec /bin/cat "$TEST_ROOT/board" ;;
 /sys/class/net/eth0/address) exec /bin/cat "$TEST_ROOT/address" ;;
 /sys/block/mmcblk0/device/cid) exec /bin/cat "$TEST_ROOT/cid" ;;
 *) exit 90 ;;
esac
""")
            (root / "ip").write_text("""#!/bin/sh
printf '%s\\n' "$*" >> "$TEST_ROOT/ip-calls"
exit "$TEST_IP_STATUS"
""")
            for name in ("cat", "ip"):
                (root / name).chmod(0o755)
            env = dict(os.environ, PATH=str(root) + ":" + os.environ["PATH"],
                       TEST_ROOT=str(root), TEST_IP_STATUS=str(ip_status))
            result = subprocess.run(["sh", str(ROOTFS / "overlay/etc/init.d/S38fes-network-id"),
                                     action], env=env, capture_output=True, text=True)
            calls = root / "ip-calls"
            return result, calls.read_text().splitlines() if calls.exists() else []

    def test_random_firmware_addresses_converge_before_dhcp(self):
        for address in ("56:8e:f4:b5:64:14", "b2:f9:54:6c:7e:4f"):
            result, calls = self.run_script(address)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(calls, ["link set dev eth0 address " + self.expected(self.CID)])

    def test_distinct_cards_and_uppercase_identity(self):
        other = "123456789abcdef0123456789abcdef0"
        self.assertNotEqual(self.expected(self.CID), self.expected(other))
        for cid in (self.CID.upper(), other):
            result, calls = self.run_script("b2:f9:54:6c:7e:4f", cid)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(calls, ["link set dev eth0 address " + self.expected(cid)])

    def test_hardware_address_and_already_stable_address_are_preserved(self):
        for address, cid in (("00:11:22:33:44:55", None),
                             (self.expected(self.CID), self.CID)):
            result, calls = self.run_script(address, cid)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(calls, [])

    def test_invalid_inputs_never_change_network(self):
        cases = [(value, self.CID, "de25-nano") for value in
                 ("", "broken", "00:00:00:00:00:00", "ff:ff:ff:ff:ff:ff",
                  "01:00:00:00:00:01", "b2:xx:54:6c:7e:4f")]
        cases += [("b2:f9:54:6c:7e:4f", value, "de25-nano") for value in
                  (None, "", "0" * 32, "f" * 32, "z" * 32, "a" * 31, "a" * 33)]
        cases.append(("b2:f9:54:6c:7e:4f", self.CID, "de10-nano"))
        for address, cid, board in cases:
            with self.subTest(address=address, cid=cid, board=board):
                result, calls = self.run_script(address, cid, board=board)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])

    def test_stop_does_nothing_and_ip_failure_propagates(self):
        result, calls = self.run_script(None, None, action="stop")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(calls, [])
        result, calls = self.run_script("b2:f9:54:6c:7e:4f", ip_status=17)
        self.assertEqual(result.returncode, 17)
        self.assertEqual(len(calls), 1)


if __name__ == "__main__":
    unittest.main()
