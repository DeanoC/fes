import hashlib
from pathlib import Path
import tempfile
import unittest
from scripts.quartus_tools import QuartusLock, RebuildError, load_quartus_lock, locate_quartus

LITE = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"


def lock_text(edition="lite", version=LITE, family="cyclonev", digest="0" * 64, extra=""):
    return (f'[quartus]\nedition = "{edition}"\nversion = "{version}"\n'
            f'device_family = "{family}"\nquartus_sh_sha256 = "{digest}"\n' + extra)


class QuartusLockTests(unittest.TestCase):
    def load(self, text):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "quartus.lock").write_text(text)
            return load_quartus_lock(root, "quartus.lock")

    def test_derived_strings(self):
        lock = self.load(lock_text())
        self.assertEqual((lock.release, lock.series, lock.product, lock.compiler, lock.qsf_version),
                         ("17.0.2", "17.0", "Quartus Prime Lite 17.0.2", "quartus-17.0.2",
                          "17.0.2 Lite Edition"))
        self.assertIsNone(lock.yosys_commit)

    def test_yosys_section(self):
        lock = self.load(lock_text(extra=f'[yosys]\ncommit = "{"a" * 40}"\nsha256 = "{"b" * 64}"\n'))
        self.assertEqual((lock.yosys_commit, lock.yosys_sha256), ("a" * 40, "b" * 64))

    def test_rejects(self):
        for text in (
            lock_text(edition="pro"),                                   # edition vs version line
            lock_text(version="Version 17.0.2 Lite"),                   # not a full version line
            lock_text(family="agilex5"),                                # family vs edition
            lock_text(digest="0" * 63),                                 # digest shape
            lock_text() + 'extra = "x"\n',                              # unknown key
            lock_text(extra='[nextpnr]\ncommit = "x"\n'),               # unknown section
            lock_text(extra=f'[yosys]\ncommit = "{"a" * 40}"\n'),       # partial [yosys]
            'version = "' + LITE + '"\n',                               # old flat shape
        ):
            with self.subTest(text=text), self.assertRaises(RebuildError):
                self.load(text)


class LocateQuartusTests(unittest.TestCase):
    def test_authenticates_digest_and_exact_version_line(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            tool = root / "bin/quartus_sh"
            tool.parent.mkdir()

            def install(lines):
                tool.write_text("#!/bin/sh\n" + "".join(f'echo "{line}"\n' for line in lines))
                tool.chmod(0o755)
                return QuartusLock("cores/x/quartus.lock", "lite", LITE, "cyclonev",
                                   hashlib.sha256(tool.read_bytes()).hexdigest())

            lock = install(["Quartus Prime Shell", LITE, "Copyright"])
            self.assertEqual(locate_quartus(str(root), root, lock), (root, tool, LITE))
            for lines in (["Version 17.0.2 Build 602 07/19/2017 SJ Standard Edition"],
                          ["Version 17.1.0 Build 590 10/25/2017 SJ Lite Edition"],
                          [LITE + " "], [LITE, LITE], []):
                with self.subTest(lines=lines), self.assertRaises(RebuildError):
                    locate_quartus(str(root), root, install(lines))
            other = install([LITE])
            tool.write_text(tool.read_text() + "# changed\n")
            with self.assertRaises(RebuildError):
                locate_quartus(str(root), root, other)
            with self.assertRaises(RebuildError):
                locate_quartus("", root, other)
            tool.unlink()
            tool.symlink_to("/bin/true")
            with self.assertRaises(RebuildError):
                locate_quartus(str(root), root, other)


if __name__ == "__main__":
    unittest.main()
