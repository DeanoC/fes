from __future__ import annotations

import importlib.util
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PROGRAM = ROOT / "scripts" / "program.py"


def _load_program():
    spec = importlib.util.spec_from_file_location("program_busybox_under_test", PROGRAM)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {PROGRAM}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


class BusyBoxMetadataTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        if shutil.which("busybox") is None:
            raise unittest.SkipTest("BusyBox is required for the remote metadata contract tests")
        cls.program = _load_program()

    def _run_metadata(
        self,
        script: str,
        path: str,
        *,
        follow: bool = False,
        pass_fds: tuple[int, ...] = (),
    ) -> list[str]:
        option = " -L" if follow else ""
        command = f"misteross_metadata{option} {path!r}"
        result = subprocess.run(
            ["/bin/sh", "-c", script + "\n" + command],
            text=True,
            capture_output=True,
            pass_fds=pass_fds,
            env={**os.environ, "LC_ALL": "C"},
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = result.stdout.splitlines()
        self.assertEqual(len(lines), 1, result.stdout)
        fields = lines[0].split("|")
        self.assertEqual(len(fields), 6, lines[0])
        return fields

    def test_generated_helper_reports_normalized_busybox_metadata_for_fixtures(self) -> None:
        helper = self.program._remote_metadata_helper()
        with tempfile.TemporaryDirectory(prefix="misteross-busybox-") as directory:
            root = Path(directory)
            fixture_dir = root / "directory"
            fixture_file = root / "file"
            fixture_fifo = root / "fifo"
            fixture_link = root / "link"
            fixture_dir.mkdir(mode=0o700)
            fixture_file.write_bytes(b"fixture")
            fixture_file.chmod(0o400)
            os.mkfifo(fixture_fifo, 0o600)
            fixture_link.symlink_to(fixture_file.name)

            directory_fields = self._run_metadata(helper, str(fixture_dir))
            directory_info = fixture_dir.lstat()
            self.assertEqual(directory_fields[0], "directory")
            self.assertEqual(int(directory_fields[1]), directory_info.st_uid)
            self.assertEqual(int(directory_fields[2]), directory_info.st_gid)
            self.assertEqual(int(directory_fields[3], 8), 0o700)
            self.assertEqual(int(directory_fields[4]), directory_info.st_ino)
            self.assertEqual(int(directory_fields[5]), directory_info.st_nlink)

            file_fields = self._run_metadata(helper, str(fixture_file))
            file_info = fixture_file.lstat()
            self.assertEqual(file_fields[0], "regular file")
            self.assertEqual(int(file_fields[1]), file_info.st_uid)
            self.assertEqual(int(file_fields[2]), file_info.st_gid)
            self.assertEqual(int(file_fields[3], 8), 0o400)
            self.assertEqual(int(file_fields[4]), file_info.st_ino)
            self.assertEqual(int(file_fields[5]), file_info.st_nlink)

            fifo_fields = self._run_metadata(helper, str(fixture_fifo))
            fifo_info = fixture_fifo.lstat()
            self.assertEqual(fifo_fields[0], "fifo")
            self.assertEqual(int(fifo_fields[1]), fifo_info.st_uid)
            self.assertEqual(int(fifo_fields[2]), fifo_info.st_gid)
            self.assertEqual(int(fifo_fields[3], 8), 0o600)
            self.assertEqual(int(fifo_fields[4]), fifo_info.st_ino)
            self.assertEqual(int(fifo_fields[5]), fifo_info.st_nlink)

            link_fields = self._run_metadata(helper, str(fixture_link))
            link_info = fixture_link.lstat()
            self.assertEqual(link_fields[0], "symbolic link")
            self.assertEqual(int(link_fields[1]), link_info.st_uid)
            self.assertEqual(int(link_fields[2]), link_info.st_gid)
            self.assertEqual(int(link_fields[3], 8), stat.S_IMODE(link_info.st_mode))
            self.assertEqual(int(link_fields[4]), link_info.st_ino)
            self.assertEqual(int(link_fields[5]), link_info.st_nlink)

            followed_fields = self._run_metadata(helper, str(fixture_link), follow=True)
            self.assertEqual(followed_fields[0], "regular file")
            self.assertEqual(int(followed_fields[3], 8), 0o400)
            self.assertEqual(int(followed_fields[4]), fixture_file.stat().st_ino)

    def test_generated_helper_follows_proc_fd_and_executable_links(self) -> None:
        helper = self.program._remote_metadata_helper()
        with tempfile.TemporaryDirectory(prefix="misteross-busybox-fd-") as directory:
            fixture_file = Path(directory) / "file"
            fixture_file.write_bytes(b"fixture")
            fixture_file.chmod(0o400)
            fd = os.open(fixture_file, os.O_RDONLY)
            try:
                fd_fields = self._run_metadata(
                    helper,
                    f"/proc/self/fd/{fd}",
                    follow=True,
                    pass_fds=(fd,),
                )
            finally:
                os.close(fd)
            self.assertEqual(fd_fields[0], "regular file")
            self.assertEqual(int(fd_fields[3], 8), 0o400)
            self.assertEqual(int(fd_fields[4]), fixture_file.stat().st_ino)
            self.assertGreater(int(fd_fields[5]), 0)

        executable_script = helper + "\n" + "misteross_metadata -L /proc/$$/exe"
        result = subprocess.run(
            ["/bin/sh", "-c", executable_script],
            text=True,
            capture_output=True,
            env={**os.environ, "LC_ALL": "C"},
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        fields = result.stdout.splitlines()
        self.assertEqual(len(fields), 1, result.stdout)
        metadata = fields[0].split("|")
        self.assertEqual(len(metadata), 6, fields[0])
        self.assertEqual(metadata[0], "regular file")
        self.assertGreater(int(metadata[3], 8) & 0o111, 0)
        self.assertGreater(int(metadata[4]), 0)
        self.assertGreater(int(metadata[5]), 0)

    def test_remote_scripts_use_no_external_stat_and_load_uses_wc_size(self) -> None:
        scripts = (
            self.program._remote_preflight_script(),
            self.program._remote_mkdir_script("/tmp/misteross-" + "a" * 32),
            self.program._remote_verify_script(
                "/tmp/misteross-" + "a" * 32,
                "/tmp/misteross-" + "a" * 32 + "/artifact.rbf",
            ),
            self.program._remote_load_script(
                "/tmp/misteross-" + "a" * 32 + "/artifact.rbf",
                4242,
                100,
                "a" * 64,
                artifact_sha256="b" * 64,
                artifact_size=7,
            ),
        )
        for script in scripts:
            with self.subTest(script=script[:40]):
                self.assertIsNone(
                    re.search(
                        r"(?m)(?:^|[;&|])\s*(?:[A-Za-z0-9_./-]+/)?stat(?:\s|$)",
                        script,
                    )
                )
        self.assertIn("wc -c", scripts[-1])

    def test_generated_helper_converts_special_bits_and_rejects_dangling_follow(self) -> None:
        helper = self.program._remote_metadata_helper()
        with tempfile.TemporaryDirectory(prefix="misteross-busybox-special-") as directory:
            root = Path(directory)
            fixture = root / "special"
            fixture.write_bytes(b"fixture")
            for mode in (0o4755, 0o2755, 0o1755):
                fixture.chmod(mode)
                fields = self._run_metadata(helper, str(fixture))
                self.assertEqual(int(fields[3], 8), mode)

            dangling = root / "dangling"
            dangling.symlink_to(root / "missing")
            result = subprocess.run(
                ["/bin/sh", "-c", helper + "\nmisteross_metadata -L " + repr(str(dangling))],
                text=True,
                capture_output=True,
                env={**os.environ, "LC_ALL": "C"},
                check=False,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
