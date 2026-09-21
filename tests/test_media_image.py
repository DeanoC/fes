"""Real production-geometry tests run in the locked, unprivileged tool image."""
import dataclasses
import datetime
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import tomllib
import unittest
from unittest import mock

from scripts import media_container, media_inside
from scripts.media_inputs import MediaLock, Payload, digest

ROOT = Path(__file__).resolve().parents[1]
INSIDE = os.environ.get("FES_MEDIA_TEST_INSIDE") == "1"


@unittest.skipIf(INSIDE, "host container driver")
class ContainerImageTests(unittest.TestCase):
    def test_real_container_image_suite(self):
        if not shutil.which("docker"):
            self.skipTest("Docker required for real media tests")
        if subprocess.run(["docker", "info"], capture_output=True).returncode:
            self.skipTest("Docker daemon unavailable for real media tests")
        lock = MediaLock.load(ROOT / "boot-media.lock.toml")
        container = media_container.ensure_media_container(ROOT, "docker", lock)
        self.assertTrue(container.startswith("sha256:"))
        self.assertEqual(container, media_container.ensure_media_container(ROOT, "docker", lock))
        record = json.loads(subprocess.check_output(["docker", "image", "inspect", container], text=True))[0]
        for key in ("org.fes.media.base", "org.fes.media.context-sha256", "org.fes.media.packages-sha256"):
            with self.subTest(label=key):
                changed = json.loads(json.dumps(record))
                changed["Config"]["Labels"][key] = "tampered"
                inspected = subprocess.CompletedProcess([], 0, stdout=json.dumps([changed]), stderr="")
                with mock.patch.object(media_container.subprocess, "run", return_value=inspected):
                    with self.assertRaisesRegex(ValueError, "labels"):
                        media_container.ensure_media_container(ROOT, "docker", lock)
        subprocess.run(["docker", "run", "--rm", "--network=none", "--cap-drop=ALL",
                        "--security-opt=no-new-privileges", "-e", "FES_MEDIA_TEST_INSIDE=1",
                        "-v", f"{ROOT}:/work:ro", "--entrypoint", "python3", container,
                        "-m", "unittest", "tests.test_media_image.RealImageTests", "-v"], check=True)


@unittest.skipUnless(INSIDE, "executed by the real container driver")
class RealImageTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="media-image-tests-")
        cls.root = Path(cls.temp.name)
        cls.idle = cls.root / "idle.rbf"
        cls.idle.write_bytes(b"idle-rbf-fixture" * 1024)
        tree = cls.root / "tree/usr/share/mister-runtime"
        tree.mkdir(parents=True)
        shutil.copyfile(cls.idle, tree / "idle.rbf")
        cls.rootfs = cls.root / "linux.img"
        with cls.rootfs.open("wb") as f:
            f.truncate(8 * 1024 * 1024)
        subprocess.run(["mkfs.ext4", "-q", "-F", "-d", str(cls.root / "tree"), str(cls.rootfs)], check=True)
        cls.kernel = cls.root / "zImage_dtb"
        cls.kernel.write_bytes(b"kernel-fixture" * 2048)
        cls.uboot = cls.root / "uboot.img"
        lock = MediaLock.load(ROOT / "boot-media.lock.toml")
        cls.uboot.write_bytes(b"uboot-fixture" * 1024 + b"\0".join(value.encode() for value in lock.environment))
        cls.lock = dataclasses.replace(lock,
            kernel=Payload("zImage_dtb", cls.kernel.stat().st_size, digest(cls.kernel)),
            uboot=Payload("uboot.img", cls.uboot.stat().st_size, digest(cls.uboot)))
        cls.provenance = media_inside.Provenance('f' * 40, 'native-integration-dev', 'a' * 64,
            'b' * 64, 'c' * 64, 'https://github.com/MiSTer-devel/Distribution_MiSTer',
            'd' * 40, 'menu.rbf', cls.idle.stat().st_size, digest(cls.idle))
        cls.payloads = media_inside.ImageInputs(cls.rootfs, cls.idle, cls.kernel, cls.uboot, provenance=cls.provenance)
        cls.image, cls.manifest = media_inside.assemble(cls.root / "original", cls.payloads, cls.lock)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory(dir=self.root)
        self.addCleanup(self.scratch.cleanup)
        self.copy = Path(self.scratch.name) / "fes.img"
        shutil.copyfile(self.image, self.copy)

    def mutate(self, offset, data=b"\x01"):
        with self.copy.open("r+b") as f:
            f.seek(offset)
            f.write(data)

    def verify(self, manifest=None):
        return media_inside.verify_image(self.copy, manifest or self.manifest, self.payloads, self.lock)

    def test_real_sparse_image_has_exact_mbr_fat32_and_payloads(self):
        self.assertNotEqual(os.getuid(), 0)
        self.assertEqual(self.image.stat().st_size, 528384 * 512)
        result = self.verify()
        self.assertEqual(result.partition_types, (0x0c, 0xa2))
        self.assertEqual(result.fat_type, "FAT32")
        self.assertEqual(result.paths, ("/fogcast", "/linux", "/linux/linux.img", "/linux/zImage_dtb", "/menu.rbf"))
        with self.image.open("rb") as f:
            mbr = f.read(512)
        self.assertEqual(mbr[:440], bytes(440))
        self.assertEqual(mbr[440:446], struct.pack("<I", 0x46455331) + bytes(2))
        self.assertEqual(mbr[446:478], bytes.fromhex("80feffff0cfeffff000800000000080000feffffa2feffff0008080000080000"))
        self.assertEqual(mbr[478:], bytes(32) + b"\x55\xaa")

    def test_manifest_has_complete_provenance_and_separate_checks(self):
        data = tomllib.loads(self.manifest.read_text())
        self.assertEqual(set(data), {'format', 'target', 'layout', 'source_date_epoch', 'provisioned',
                         'hardware', 'fes', 'rootfs', 'kernel', 'uboot', 'splash', 'idle', 'disk', 'fat',
                         'partition_1', 'partition_2', 'output', 'assembly', 'checks'})
        self.assertEqual(data['target'], 'de10-nano')
        self.assertEqual(data['rootfs']['path'], 'out/native-integration-dev/linux.img')
        self.assertEqual(data['rootfs']['destination'], '/linux/linux.img')
        self.assertEqual(data['kernel']['destination'], '/linux/zImage_dtb')
        self.assertEqual(data['uboot']['destination'], 'partition_2')
        self.assertEqual(data['idle']['rootfs_destination'], '/usr/share/mister-runtime/idle.rbf')
        self.assertEqual(data['splash']['fat_destination'], '/menu.rbf')
        self.assertNotIn('fat_destination', data['idle'])
        self.assertEqual(data['splash']['sha256'], data['idle']['sha256'])
        self.assertEqual(data['output'], {'path': 'fes.img', 'size': self.image.stat().st_size, 'sha256': digest(self.image)})
        self.assertEqual(data['assembly']['sha256'], [digest(self.image)] * 2)
        self.assertEqual(data['checks'], {'structural_media': 'pass', 'assembly_reproducibility': 'pass',
                                        'rootfs_structural': 'not-run', 'rootfs_qemu': 'not-run'})
        self.assertEqual(data['hardware'], 'not-run')
        self.assertEqual(data['partition_1']['start_sector'], 2048)
        self.assertEqual(data['partition_2']['type'], 0xa2)
        for name in ('kernel', 'uboot', 'idle', 'splash'):
            self.assertTrue({'repository', 'revision', 'path', 'size', 'sha256'} <= set(data[name]))
        self.assertEqual(set(data['fes']), {'revision', 'profile', 'media_recipe_sha256'})
        self.assertTrue({'image_receipt_sha256', 'child_manifest_sha256'} <= set(data['rootfs']))

    def test_manifest_rejects_stale_provenance_paths_status_and_pass_evidence(self):
        original = tomllib.loads(self.manifest.read_text())
        self.assertIn('fes', original, 'full provenance manifest required')
        cases = [('fes', 'revision', '0' * 40), ('fes', 'profile', 'native-dev'),
                 ('fes', 'media_recipe_sha256', '0' * 64),
                 ('rootfs', 'path', '../linux.img'), ('rootfs', 'child_manifest_sha256', '0' * 64),
                 ('rootfs', 'image_receipt_sha256', '0' * 64),
                 ('kernel', 'repository', 'https://unselected.example'), ('kernel', 'destination', '/kernel'),
                 ('uboot', 'revision', '0' * 40), ('splash', 'fat_destination', '/idle.rbf'),
                 ('partition_1', 'active', False), ('partition_2', 'sector_count', 2047),
                 ('fat', 'serial', 1), ('output', 'path', '../fes.img'),
                 ('assembly', 'sha256', [digest(self.image), '0' * 64]),
                 ('checks', 'rootfs_qemu', 'pass'), ('checks', 'structural_media', 'not-run'),
                 ('rootfs', 'unknown', 'unexpected'), ('partition_1', 'active', 1)]
        for table, key, value in cases:
            with self.subTest(table=table, key=key):
                data = json.loads(json.dumps(original))
                data[table][key] = value
                manifest = Path(self.scratch.name) / 'invalid.toml'
                media_inside.write_manifest(manifest, data)
                with self.assertRaisesRegex(ValueError, 'manifest|assembly'):
                    self.verify(manifest)

    def test_manifest_records_hashes_read_from_each_actual_assembly_pass(self):
        observed = {}
        original = media_inside._assemble_once
        def assemble_once(image, inputs, lock, scratch):
            original(image, inputs, lock, scratch)
            observed[scratch.name] = digest(image)
        with mock.patch.object(media_inside, '_assemble_once', side_effect=assemble_once):
            _, manifest = media_inside.assemble(Path(self.scratch.name) / 'passes', self.payloads, self.lock)
        data = tomllib.loads(manifest.read_text())
        self.assertIn('assembly', data, 'actual pass evidence must be persisted')
        self.assertEqual(data['assembly']['sha256'], [observed['first'], observed['second']])

    def test_changed_second_pass_cannot_produce_assembly_evidence(self):
        original = media_inside._assemble_once
        def changed(image, inputs, lock, scratch):
            original(image, inputs, lock, scratch)
            if scratch.name == 'second':
                with image.open('r+b') as stream:
                    stream.seek(1024)
                    stream.write(b'changed')
        output = Path(self.scratch.name) / 'mismatched-passes'
        with mock.patch.object(media_inside, '_assemble_once', side_effect=changed):
            with self.assertRaisesRegex(ValueError, 'independent media assembly hashes differ'):
                media_inside.assemble(output, self.payloads, self.lock)
        self.assertFalse((output / 'fes.img').exists())
        self.assertFalse((output / 'fes-media.toml').exists())

    def test_independent_assemblies_hash_identically(self):
        other, manifest = media_inside.assemble(Path(self.scratch.name) / "second", self.payloads, self.lock)
        self.assertEqual(digest(self.image), digest(other))
        self.assertEqual(self.manifest.read_bytes(), manifest.read_bytes())

    def test_fat_timestamps_are_normalized_to_epoch(self):
        stamp = datetime.datetime.fromtimestamp(1751459412, datetime.timezone.utc)
        date = ((stamp.year - 1980) << 9) | (stamp.month << 5) | stamp.day
        time = (stamp.hour << 11) | (stamp.minute << 5) | (stamp.second // 2)
        with self.image.open("rb") as stream:
            stream.seek(1048576)
            bpb = stream.read(512)
            fat_sectors = struct.unpack_from("<I", bpb, 36)[0]
            base = 1048576 + (32 + 2 * fat_sectors) * 512
            pending = [2]
            while pending:
                cluster = pending.pop()
                stream.seek(base + (cluster - 2) * 512)
                entries = stream.read(512)
                for offset in range(0, 512, 32):
                    entry = entries[offset:offset + 32]
                    if not entry[0]:
                        break
                    if entry[11] == 15:
                        continue
                    self.assertEqual(struct.unpack_from("<HHH", entry, 14), (time, date, date), entry[:11])
                    self.assertEqual(struct.unpack_from("<HH", entry, 22), (time, date), entry[:11])
                    if entry[11] & 16 and entry[0] != ord("."):
                        pending.append(struct.unpack_from("<H", entry, 26)[0])

    def test_container_cli_assembles_and_verifies(self):
        original = MediaLock.load(ROOT / "boot-media.lock.toml")
        text = (ROOT / "boot-media.lock.toml").read_text()
        for old, new in ((original.kernel, self.lock.kernel), (original.uboot, self.lock.uboot)):
            text = text.replace(old.sha256, new.sha256).replace(f"size = {old.size}", f"size = {new.size}")
        lock_path = Path(self.scratch.name) / "fixture.lock.toml"
        lock_path.write_text(text)
        common = ["--lock", str(lock_path)]
        for name in ("rootfs", "idle", "kernel", "uboot"):
            common.extend(["--" + name, str(getattr(self.payloads, name))])
        common.extend(["--splash", str(self.payloads.splash_payload())])
        provenance = Path(self.scratch.name) / 'provenance.json'
        provenance.write_text(json.dumps(dataclasses.asdict(self.provenance)))
        common += ['--provenance', str(provenance)]
        output = Path(self.scratch.name) / "cli"
        command = [sys.executable, str(ROOT / "scripts/media_inside.py")]
        assembled = json.loads(subprocess.check_output(command + ["assemble", "--output", str(output)] + common, text=True))
        self.assertEqual(assembled["assembly_sha256"], [digest(output / "fes.img")] * 2)
        verified = json.loads(subprocess.check_output(command + ["verify", "--image", str(output / "fes.img"),
                              "--manifest", str(output / "fes-media.toml")] + common, text=True))
        self.assertEqual(verified["fat_type"], "FAT32")
        self.assertEqual(verified["image_sha256"], assembled["image_sha256"])

    def test_changed_mbr_rejected(self):
        self.mutate(450, b"\x0b")
        with self.assertRaisesRegex(ValueError, "MBR"):
            self.verify()

    def test_changed_fat_metadata_rejected(self):
        self.mutate(2048 * 512 + 67, b"\x02")
        with self.assertRaisesRegex(ValueError, "FAT"):
            self.verify()

    def test_changed_payload_rejected(self):
        with self.copy.open("rb") as stream:
            stream.seek(1048576 + 36)
            fat_sectors = struct.unpack("<I", stream.read(4))[0]
            base = 1048576 + (32 + 2 * fat_sectors) * 512
            stream.seek(base)
            entries = stream.read(512)
        menu = next(entries[n:n + 32] for n in range(0, 512, 32) if entries[n:n + 11] == b"MENU    RBF")
        cluster = struct.unpack_from("<H", menu, 26)[0]
        self.mutate(base + (cluster - 2) * 512)
        with self.assertRaisesRegex(ValueError, "payload"):
            self.verify()

    def test_nonzero_padding_rejected(self):
        self.mutate(1024)
        with self.assertRaisesRegex(ValueError, "padding"):
            self.verify()

    def rehash_manifest(self):
        manifest = Path(self.scratch.name) / "rehashed.toml"
        manifest.write_text(self.manifest.read_text().replace(digest(self.image), digest(self.copy)))
        return manifest

    def test_nonzero_free_fat_space_rejected_even_with_rehashed_manifest(self):
        self.mutate(526336 * 512 - 1)
        with self.assertRaisesRegex(ValueError, "unused FAT"):
            self.verify(self.rehash_manifest())

    def test_changed_fat_timestamp_rejected_even_with_rehashed_manifest(self):
        with self.copy.open("rb") as stream:
            stream.seek(1048576 + 36)
            fat_sectors = struct.unpack("<I", stream.read(4))[0]
        self.mutate(1048576 + (32 + 2 * fat_sectors) * 512 + 14)
        with self.assertRaisesRegex(ValueError, "FAT timestamp"):
            self.verify(self.rehash_manifest())

    def test_nonzero_fsinfo_reserved_padding_rejected(self):
        self.mutate(1048576 + 512 + 10)
        with self.assertRaisesRegex(ValueError, "FAT.*padding"):
            self.verify(self.rehash_manifest())

    def test_nonzero_boot_tail_rejected(self):
        self.mutate(526336 * 512 + self.uboot.stat().st_size + 1)
        with self.assertRaisesRegex(ValueError, "boot partition tail"):
            self.verify()

    def test_changed_boot_payload_rejected(self):
        self.mutate(526336 * 512)
        with self.assertRaisesRegex(ValueError, "boot payload"):
            self.verify()

    def test_extra_owned_path_rejected(self):
        subprocess.run(["mcopy", "-i", f"{self.copy}@@1048576", str(self.idle), "::/fogcast/extra"], check=True)
        with self.assertRaisesRegex(ValueError, "paths"):
            self.verify()

    def test_hidden_extra_file_rejected_with_rehashed_manifest(self):
        device = f"{self.copy}@@1048576"
        subprocess.run(["mcopy", "-i", device, str(self.idle), "::/fogcast/extra"], check=True)
        subprocess.run(["mattrib", "-i", device, "+h", "+s", "::/fogcast/extra"], check=True)
        with self.assertRaisesRegex(ValueError, "paths|directory"):
            self.verify(self.rehash_manifest())

    def test_hidden_extra_directory_rejected_with_rehashed_manifest(self):
        device = f"{self.copy}@@1048576"
        subprocess.run(["mmd", "-i", device, "::/fogcast/extra"], check=True)
        subprocess.run(["mattrib", "-i", device, "+h", "+s", "::/fogcast/extra"], check=True)
        with self.assertRaisesRegex(ValueError, "paths|directory"):
            self.verify(self.rehash_manifest())

    def test_owned_readonly_file_rejected_with_rehashed_manifest(self):
        subprocess.run(["mattrib", "-i", f"{self.copy}@@1048576", "+r", "::/menu.rbf"], check=True)
        with self.assertRaisesRegex(ValueError, "attributes|directory"):
            self.verify(self.rehash_manifest())

    def test_owned_system_directory_rejected_with_rehashed_manifest(self):
        subprocess.run(["mattrib", "-i", f"{self.copy}@@1048576", "+s", "::/fogcast"], check=True)
        with self.assertRaisesRegex(ValueError, "attributes|directory"):
            self.verify(self.rehash_manifest())

    def test_modified_boot_oem_rejected_with_rehashed_manifest(self):
        for sector in (0, 6):
            self.mutate(1048576 + sector * 512 + 3, b"CHANGED!")
        with self.assertRaisesRegex(ValueError, "FAT.*metadata"):
            self.verify(self.rehash_manifest())

    def test_modified_backup_fsinfo_rejected_with_rehashed_manifest(self):
        self.mutate(1048576 + 7 * 512, b"X")
        with self.assertRaisesRegex(ValueError, "FAT.*metadata"):
            self.verify(self.rehash_manifest())

    def test_modified_primary_fsinfo_hint_rejected_with_rehashed_manifest(self):
        self.mutate(1048576 + 512 + 492, struct.pack("<I", 2))
        with self.assertRaisesRegex(ValueError, "FAT.*metadata"):
            self.verify(self.rehash_manifest())

    def test_reserved_fat_entry_bits_rejected_with_rehashed_manifest(self):
        with self.copy.open("rb") as stream:
            stream.seek(1048576 + 36)
            fat_sectors = struct.unpack("<I", stream.read(4))[0]
            stream.seek(1048576 + 32 * 512 + 2 * 4)
            value = struct.unpack("<I", stream.read(4))[0]
        for first_sector in (32, 32 + fat_sectors):
            self.mutate(1048576 + first_sector * 512 + 2 * 4, struct.pack("<I", value | 0x10000000))
        with self.assertRaisesRegex(ValueError, "reserved FAT.*bits"):
            self.verify(self.rehash_manifest())

    def test_truncation_rejected(self):
        with self.copy.open("r+b") as f:
            f.truncate(self.copy.stat().st_size - 1)
        with self.assertRaisesRegex(ValueError, "size"):
            self.verify()

    def test_manifest_is_closed_and_rejects_duplicates(self):
        for extra in ('unknown = 1\n', 'format = 1\n', '[fes]\nrevision = "duplicate"\n'):
            with self.subTest(extra=extra):
                manifest = Path(self.scratch.name) / "bad.toml"
                manifest.write_text(extra + self.manifest.read_text())
                with self.assertRaisesRegex(ValueError, "manifest"):
                    self.verify(manifest)

    def test_optional_config_is_verified_by_hash(self):
        config = Path(self.scratch.name) / "agent.toml"
        config.write_bytes(b'token = "private-test-token"\n')
        inputs = dataclasses.replace(self.payloads, agent_config=config)
        image, manifest = media_inside.assemble(Path(self.scratch.name) / "provisioned", inputs, self.lock)
        config_sha = digest(config)
        self.assertNotIn("private-test-token", manifest.read_text())
        result = media_inside.verify_image(image, manifest, self.payloads, self.lock, config_sha)
        self.assertIn("/fogcast/agent.toml", result.paths)
        with self.assertRaisesRegex(ValueError, "config"):
            media_inside.verify_image(image, manifest, self.payloads, self.lock, "0" * 64)

    def test_launcher_payload_closed_paths_and_digest(self):
        scratch = Path(self.scratch.name)
        config = scratch / 'agent.toml'
        config.write_bytes(b'token="agent-test-token"\n')
        launcher = scratch / 'launcher.json'
        launcher.write_bytes(b'{"token":"private-launcher-secret"}\n')
        inputs = dataclasses.replace(self.payloads, agent_config=config,
            launcher_config=launcher, launcher_config_sha256=digest(launcher))
        image, manifest = media_inside.assemble(scratch / 'launcher-media', inputs, self.lock)
        self.assertNotIn('private-launcher-secret', manifest.read_text())
        verified = dataclasses.replace(inputs, agent_config=None, launcher_config=None)
        result = media_inside.verify_image(image, manifest, verified, self.lock, digest(config))
        self.assertIn('/fogcast/launcher.json', result.paths)
        with self.assertRaises(ValueError):
            media_inside.verify_image(image, manifest, dataclasses.replace(verified,
                launcher_config_sha256='0' * 64), self.lock, digest(config))

    def test_idle_must_match_embedded_rootfs(self):
        wrong = Path(self.scratch.name) / "wrong.rbf"
        wrong.write_bytes(b"wrong")
        with self.assertRaisesRegex(ValueError, "idle"):
            media_inside.assemble(Path(self.scratch.name) / "invalid", dataclasses.replace(self.payloads, idle=wrong), self.lock)

    def test_splash_may_differ_from_rootfs_idle(self):
        splash = Path(self.scratch.name) / "splash.rbf"
        splash.write_bytes(b"splash-rbf-fixture" * 1024)
        provenance = dataclasses.replace(
            self.provenance,
            splash_size=splash.stat().st_size,
            splash_sha256=digest(splash),
        )
        inputs = dataclasses.replace(self.payloads, splash=splash, provenance=provenance)
        image, manifest = media_inside.assemble(Path(self.scratch.name) / "diverged", inputs, self.lock)
        data = tomllib.loads(manifest.read_text())
        self.assertEqual(data['splash']['sha256'], digest(splash))
        self.assertEqual(data['idle']['sha256'], digest(self.idle))
        self.assertNotEqual(data['splash']['sha256'], data['idle']['sha256'])
        self.assertEqual(data['splash']['fat_destination'], '/menu.rbf')
        self.assertEqual(data['idle']['rootfs_destination'], '/usr/share/mister-runtime/idle.rbf')
        self.assertNotIn('fat_destination', data['idle'])
        result = media_inside.verify_image(image, manifest, inputs, self.lock)
        self.assertIn('/menu.rbf', result.paths)
