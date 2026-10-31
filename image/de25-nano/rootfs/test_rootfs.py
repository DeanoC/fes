"""Check the headless target configuration and real post-build hook."""
from pathlib import Path
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

    def test_overlay_has_only_headless_board_and_dhcp_configuration(self):
        files = {path.relative_to(ROOTFS / "overlay").as_posix()
                 for path in (ROOTFS / "overlay").rglob("*") if path.is_file()}
        self.assertEqual(files, {"etc/fes-board", "etc/network/interfaces"})
        self.assertEqual((ROOTFS / "overlay/etc/fes-board").read_text(), "de25-nano\n")
        interfaces = (ROOTFS / "overlay/etc/network/interfaces").read_text()
        self.assertIn("auto eth0\niface eth0 inet dhcp\n", interfaces)

    def test_post_build_preserves_persistent_per_board_ssh_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary) / "target"
            (target / "etc").mkdir(parents=True)
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

    def test_post_build_rejects_invalid_target(self):
        for target in ("relative/target", "/nonexistent/fes-de25-rootfs"):
            with self.subTest(target=target):
                result = subprocess.run(["sh", str(ROOTFS / "post-build.sh"), target],
                                        capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
