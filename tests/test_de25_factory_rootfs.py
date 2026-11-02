"""Execute shared startup and early DE25 factory gates without a board."""
import os
import hashlib
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]
BOARD = ROOT / 'image/buildroot/board/fogcast-target'
OVERLAY = BOARD / 'native-rootfs-overlay'


class DE25FactoryRootfsTests(unittest.TestCase):
    def test_prebuilt_runtime_recipe_selects_cpu_and_checks_hash_before_replacement(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); site = root/'site'; site.mkdir()
            recipe = root/'Makefile'
            recipe.write_text('generic-package =\nINSTALL = install\ninclude ' +
                              str(ROOT/'image/buildroot/package/mister-runtime/mister-runtime.mk') +
                              '\n' + str(site/'install') + ':\n\t$(MISTER_RUNTIME_INSTALL_TARGET_CMDS)\n')
            for flag, suffix in [('BR2_arm=y', 'armv7'), ('BR2_aarch64=y', 'aarch64')]:
                payload = ('selected runtime '+suffix).encode()
                (site/('mister-runtime-linux-'+suffix)).write_bytes(payload)
                target = root/suffix
                args = ['make', '-s', '-f', str(recipe), str(site/'install'), flag,
                        'TARGET_DIR='+str(target), 'FES_PREBUILT_RUNTIME_SHA256='+hashlib.sha256(payload).hexdigest()]
                result = subprocess.run(args, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                binary = target/'usr/sbin/mister-runtime'
                self.assertEqual(binary.read_bytes(), payload)
                binary.write_bytes(b'retained previous runtime')
                result = subprocess.run([*args[:-1], 'FES_PREBUILT_RUNTIME_SHA256='+'0'*64], capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(binary.read_bytes(), b'retained previous runtime')
            result = subprocess.run(['make', '-s', '-f', str(recipe), str(site/'install'), 'BR2_x86_64=y',
                                     'TARGET_DIR='+str(root/'unknown'), 'FES_PREBUILT_RUNTIME_SHA256='+'0'*64], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((root/'unknown').exists())

    def test_foreign_board_and_cyclone_v_inputs_refused_before_target_writes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for script, extra, message in [
                ('prepare-native-rootfs.sh', {'FES_TARGET_BOARD':'other'}, 'unsupported target board'),
                ('native-post-build.sh', {'FES_TARGET_BOARD':'other'}, 'unsupported target board'),
                ('prepare-native-rootfs.sh', {'FES_TARGET_BOARD':'de25-nano', 'FES_BLUETOOTH_MODULES_ARCHIVE':'/missing-cv-modules'}, 'DE10 Bluetooth'),
                ('native-post-build.sh', {'FES_TARGET_BOARD':'de25-nano', 'FES_WINDOW_DRIVER_DIR':'/missing-cv-window'}, 'Cyclone V'),
            ]:
                target = root/'absent'
                result = subprocess.run(['sh', BOARD/script, target], env={**os.environ, 'FOGCAST_DIR':str(root/'fogcast'), **extra}, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertFalse(target.exists())

    def test_de25_ssh_directory_requires_exact_writable_card_mount(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); fat = root/'fat'; fat.mkdir()
            identity = root/'board'; identity.write_text('de25-nano\n')
            mounts = root/'mounts'
            env = {**os.environ, 'FES_DROPBEAR_BOARD_FILE':str(identity), 'FES_DROPBEAR_FAT':str(fat), 'FES_DROPBEAR_MOUNTS':str(mounts)}
            script = OVERLAY/'etc/init.d/S30mister-dropbear'
            def run():
                return subprocess.run(['sh', script, 'start'], env=env, capture_output=True, text=True)
            for device, kind, options in [('/dev/mmcblk0p1', 'vfat', 'ro'), ('/dev/mmcblk0p2', 'vfat', 'rw'), ('/dev/mmcblk0p1', 'ext4', 'rw')]:
                mounts.write_text(f'{device} {fat} {kind} {options} 0 0\n')
                self.assertNotEqual(run().returncode, 0)
                self.assertFalse((fat/'fogcast').exists())
            mounts.write_text(f'/dev/mmcblk0p1 {fat} vfat rw,fmask=0077,dmask=0077 0 0\n')
            first = run(); self.assertEqual(first.returncode, 0, first.stderr)
            keys = fat/'fogcast/ssh'; key = keys/'dropbear_ed25519_host_key'; key.write_bytes(b'fixture-key-bytes')
            second = run(); self.assertEqual(second.returncode, 0, second.stderr)
            self.assertEqual(key.read_bytes(), b'fixture-key-bytes')
            self.assertEqual(keys.stat().st_mode & 0o777, 0o700)
            shutil.rmtree(keys)
            outside = root/'outside'; outside.mkdir()
            keys.symlink_to(outside, target_is_directory=True)
            self.assertNotEqual(run().returncode, 0)
            self.assertEqual(list(outside.iterdir()), [])

    def test_shared_dhcp_client_uses_hostname_and_stays_available_to_renew(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); (root/'eth0').mkdir(); calls = root/'calls'; pidfile = root/'dhcp.pid'
            def command(name, body):
                path = root/name; path.write_text('#!/bin/sh\n'+body); path.chmod(0o755); return path
            ip = command('ip', 'printf "ip %s\\n" "$*" >> "$CALLS"\n')
            hostname = command('hostname', 'echo fes-de25-nano\n')
            client = command('udhcpc', 'printf "dhcp %s\\n" "$*" >> "$CALLS"\nexec sleep 30\n')
            text = (OVERLAY/'etc/init.d/S20mister-network').read_text()
            for before, after in [('/run/udhcpc.pid', str(pidfile)), ('/sys/class/net/eth0', str(root/'eth0')),
                                  ('/sbin/ip', str(ip)), ('/sbin/udhcpc', str(client)), ('/bin/hostname', str(hostname))]:
                text = text.replace(before, after)
            script = root/'network'; script.write_text(text)
            env = {**os.environ, 'CALLS':str(calls)}; pid = None
            try:
                subprocess.run(['sh', script, 'start'], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
                for _ in range(100):
                    if pidfile.exists() and calls.exists() and 'dhcp ' in calls.read_text(): break
                    time.sleep(0.01)
                pid = int(pidfile.read_text())
                os.kill(pid, 0)
                argv = calls.read_text().splitlines()
                self.assertIn('ip link set lo up', argv)
                dhcp = next(line.split() for line in argv if line.startswith('dhcp '))
                self.assertIn('-f', dhcp)
                self.assertIn('-p', dhcp)
                self.assertIn('hostname:fes-de25-nano', dhcp)
                self.assertNotIn('-q', dhcp)
                subprocess.run(['sh', script, 'stop'], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=8)
                self.assertFalse(pidfile.exists())
            finally:
                if pid is None and pidfile.exists():
                    pid = int(pidfile.read_text())
                if pid is not None:
                    try: os.kill(pid, 15)
                    except ProcessLookupError: pass


if __name__ == '__main__':
    unittest.main()
