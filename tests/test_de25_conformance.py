import hashlib
import io
import json
import struct
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts import de25_conformance as conf

# The ST image matches FogCast's atarist.FormatSerial("FES TEST", 80, 2, 9, "FES")
# byte for byte (checked with a Go test when the fixture was written).
ATARI_ST_BLANK_SHA256 = "ace18d4f561886b05aa761198e5c1758fcae61d69c2061c36880978b10775b5f"


class Fixtures(unittest.TestCase):
    def test_zx81_p_layout(self):
        data = conf.zx81_p("FES TAPE OK")
        d_file, _, vars_, _, e_line = struct.unpack_from("<HHHHH", data, 3)
        self.assertEqual(d_file, 0x407D)
        self.assertEqual(0x4009 + len(data), e_line)
        self.assertEqual(vars_ + 1, e_line)
        self.assertEqual(data[-1], 0x80)
        display = data[d_file - 0x4009:vars_ - 0x4009]
        self.assertEqual(len(display), 1 + 24 * 33)
        self.assertEqual(display[1:12], conf.zx81_text("FES TAPE OK"))
        self.assertEqual(display.count(0x76), 25)
        nxtlin = struct.unpack_from("<H", data, 0x4029 - 0x4009)[0]
        self.assertEqual(nxtlin, d_file, "a program-less tape must not autorun")

    def test_zx81_text_rejects_unmapped(self):
        with self.assertRaises(ValueError):
            conf.zx81_text("OK!")

    def test_spectrum_tap_blocks_and_checksums(self):
        data = conf.spectrum_tap("FES TAPE OK")
        blocks, offset = [], 0
        while offset < len(data):
            (length,) = struct.unpack_from("<H", data, offset)
            block = data[offset + 2:offset + 2 + length]
            x = 0
            for b in block:
                x ^= b
            self.assertEqual(x, 0, "block checksum")
            blocks.append(block)
            offset += 2 + length
        header, body = blocks
        self.assertEqual((header[0], header[1]), (0x00, 0x00))
        self.assertEqual(header[2:12], b"FES       ")
        size, autostart, prog = struct.unpack_from("<HHH", header, 12)
        self.assertEqual((autostart, size, prog), (10, len(body) - 2, len(body) - 2))
        self.assertEqual(body[0], 0xFF)
        self.assertIn(b'\xF5"FES TAPE OK"\x0D', body)

    def test_c64_d64_is_empty_and_formatted(self):
        data = conf.c64_blank_d64()
        self.assertEqual(len(data), 174848)
        bam = data[0x16500:0x16600]
        self.assertEqual(bam[0:3], bytes([18, 1, 0x41]))
        free = sum(bam[4 * t] for t in range(1, 36) if t != 18)
        self.assertEqual(free, 664)
        for t, sectors in enumerate(conf.C64_TRACK_SECTORS, 1):
            bits = int.from_bytes(bam[4 * t + 1:4 * t + 4], "little")
            self.assertEqual(bin(bits).count("1"), bam[4 * t])
            self.assertLess(bits, 1 << sectors)
        self.assertEqual(bam[0x90:0x98], b"FES TEST")
        self.assertEqual(data[0x16600:0x16602], b"\x00\xFF")

    def test_atari_st_blank_matches_fogcast(self):
        data = conf.atari_st_blank()
        self.assertEqual(hashlib.sha256(data).hexdigest(), ATARI_ST_BLANK_SHA256)
        self.assertNotEqual(sum(struct.unpack(">256H", data[:512])) & 0xFFFF, 0x1234)

    def test_every_fixture_is_deterministic(self):
        for name, make in conf.FIXTURES.items():
            self.assertEqual(make(), make(), name)


class Spec(unittest.TestCase):
    def write(self, cores):
        tmp = Path(tempfile.mkdtemp()) / "spec.json"
        tmp.write_text(json.dumps({"format": 1, "cores": cores}))
        return tmp

    def test_checked_in_spec_loads(self):
        spec = conf.load_spec()
        for core in ("fes.zx81", "fes.spectrum", "fes.spectrum128", "fes.c64", "fes.atari-st"):
            self.assertIn(core, spec["cores"])

    def test_key_tokens(self):
        self.assertEqual(conf.key_tap("p"), (0x1013, 0))
        self.assertEqual(conf.key_tap("shift+2"), (0x101F, 0x02))
        self.assertEqual(conf.key_tap("ctrl+p"), (0x1013, 0x01))
        self.assertEqual(conf.key_tap("enter"), (0x1028, 0))
        for bad in ("hyper+p", "pp", "shift+"):
            with self.assertRaises(ValueError):
                conf.key_tap(bad)

    def test_rejects_bad_steps(self):
        bad = [
            [{"name": "a", "keys": ["p"], "mouse": [1, 1, 0]}],
            [{"name": "a"}],
            [{"keys": ["p"], "expect": {"change": 0.1}}],
            [{"name": "a", "keys": ["nope"]}],
            [{"name": "a", "media": ["change-disk", "missing.d64"]}],
            [{"name": "a", "keys": ["p"], "expect": {"pixels": 1}}],
            [{"name": "a", "keys": ["p"]}, {"name": "a", "keys": ["q"]}],
        ]
        for steps in bad:
            with self.subTest(steps=steps), self.assertRaises(ValueError):
                conf.load_spec(self.write({"fes.x": {"steps": steps}}))

    def test_pending_lists_uncovered_media_and_mouse(self):
        steps = [{"covers": ["fes.media.atari-st-floppy"]}]
        interfaces = ["fes.keyboard.hid", "fes.media.atari-st-floppy", "fes.media.atari-st-floppy-write",
                      "fes.mouse.relative", "fes.video.fixed-720p60"]
        self.assertEqual(conf.pending_interfaces(steps, interfaces),
                         ["fes.media.atari-st-floppy-write", "fes.mouse.relative"])

    def test_spec_covers_every_declared_kind_for_its_cores(self):
        declared = {
            "fes.zx81": ["fes.media.zx81-tape"],
            "fes.spectrum": ["fes.media.spectrum-tape"],
            "fes.spectrum128": ["fes.media.spectrum-tape"],
            "fes.c64": ["fes.media.c64-disk"],
            "fes.atari-st": ["fes.mouse.relative", "fes.media.atari-st-floppy",
                             "fes.media.atari-st-floppy-write", "fes.media.atari-st-floppy-geometry"],
        }
        spec = conf.load_spec()["cores"]
        for core, interfaces in declared.items():
            self.assertEqual(conf.pending_interfaces(spec[core]["steps"], interfaces), [], core)
        # ZX81 cassette SAVE is declared but not yet checked; it must show as pending.
        self.assertEqual(conf.pending_interfaces(spec["fes.zx81"]["steps"], ["fes.media.zx81-tape-write"]),
                         ["fes.media.zx81-tape-write"])


class Frames(unittest.TestCase):
    def test_frame_delta(self):
        a = bytes(300)
        self.assertEqual(conf.frame_delta(a, a), 0.0)
        b = bytes([255]) * 300
        self.assertEqual(conf.frame_delta(a, b), 1.0)
        self.assertIsNone(conf.frame_delta(a, bytes(3)))
        self.assertIsNone(conf.frame_delta(b"", b""))

    def test_normalise(self):
        self.assertIn(conf.normalise("blocks free"), conf.normalise("664 BLOCKS\nFREE."))


class FakeHost:
    def __init__(self, tap_ok=True, cli_rc=0, media_name=""):
        self.taps, self.calls = [], []
        self.tap_ok, self.cli_rc, self.media_name = tap_ok, cli_rc, media_name

    def input_session(self):
        return "s1"

    def tap(self, session_id, token):
        self.taps.append(token)
        return {"ok": self.tap_ok}

    def cli(self, *args):
        self.calls.append(args)
        return self.cli_rc, {"media_name": self.media_name, "media_ejected": False}, "ok"


class Run(unittest.TestCase):
    def run_steps(self, steps, host, frames, ocr_text=""):
        out = Path(tempfile.mkdtemp())
        seq = iter(frames)
        with mock.patch.object(conf, "grab", lambda video, dest: dest.write_bytes(b"png") or True), \
             mock.patch.object(conf, "rgb", lambda path: next(seq)), \
             mock.patch.object(conf, "ocr", lambda path: ocr_text), \
             mock.patch.object(conf.time, "sleep", lambda s: None):
            return conf.run("fes.x", steps, ["fes.media.x-disk"], host, "/dev/null", out)

    def test_keys_change_frame_and_idle(self):
        still, moved = bytes(300), bytes([255]) * 300
        steps = [{"name": "k", "keys": ["p"], "covers": ["fes.keyboard.hid"],
                  "expect": {"idle_max": 0.0001, "change": 0.0003}}]
        # start/idle, idle/after
        report = self.run_steps(steps, FakeHost(), [still, still, still, moved])
        self.assertEqual(report["checks"]["k"]["ok"], True, report)
        self.assertEqual(report["pending"], ["fes.media.x-disk"])
        report = self.run_steps(steps, FakeHost(), [still, still, still, still])
        self.assertFalse(report["checks"]["k"]["ok"])
        report = self.run_steps(steps, FakeHost(), [still, moved, moved, still])
        self.assertFalse(report["checks"]["k"]["ok"], "a moving screen is not an idle baseline")
        report = self.run_steps(steps, FakeHost(tap_ok=False), [still, still, still, moved])
        self.assertFalse(report["checks"]["k"]["ok"])

    def test_media_needs_the_fixture_mounted(self):
        steps = [{"name": "m", "media": ["change-disk", "c64-fes-blank.d64"], "covers": ["fes.media.x-disk"]}]
        host = FakeHost(media_name="c64-fes-blank")
        report = self.run_steps(steps, host, [])
        self.assertTrue(report["checks"]["m"]["ok"], report)
        self.assertEqual(report["pending"], [])
        self.assertEqual(host.calls[0][0], "change-disk")
        self.assertTrue(host.calls[0][1].endswith("c64-fes-blank.d64"))
        self.assertTrue(Path(host.calls[0][1]).is_file())
        self.assertFalse(self.run_steps(steps, FakeHost(media_name="other"), [])["checks"]["m"]["ok"])
        self.assertFalse(self.run_steps(steps, FakeHost(cli_rc=1, media_name="c64-fes-blank"), [])["checks"]["m"]["ok"])

    def test_ocr_expectation(self):
        steps = [{"name": "o", "keys": ["enter"], "expect": {"ocr": "BLOCKS FREE"}}]
        self.assertTrue(self.run_steps(steps, FakeHost(), [], "664 blocks free.")["checks"]["o"]["ok"])
        self.assertFalse(self.run_steps(steps, FakeHost(), [], "?FILE NOT FOUND")["checks"]["o"]["ok"])

    def test_unnamed_step_failure_is_reported(self):
        steps = [{"keys": ["enter"]}, {"name": "o", "keys": ["p"]}]
        report = self.run_steps(steps, FakeHost(tap_ok=False), [])
        self.assertIn("step0", report["checks"])
        self.assertEqual(report["verdict"], "FAIL")


class Main(unittest.TestCase):
    def test_core_without_steps_exits_3(self):
        tmp = Path(tempfile.mkdtemp())
        package = tmp / "x.fcore"
        with tarfile.open(package, "w") as archive:
            raw = b'[core]\nid = "fes.menu"\n'
            info = tarfile.TarInfo("manifest.toml")
            info.size = len(raw)
            archive.addfile(info, io.BytesIO(raw))
        with mock.patch("sys.stdout", io.StringIO()):
            rc = conf.main(["--core", "fes.menu", "--package", str(package), "--api", "http://x",
                            "--fogcast", "/bin/false", "--video", "/dev/null", "--out", str(tmp / "o")])
        self.assertEqual(rc, 3)

    def test_package_interfaces(self):
        tmp = Path(tempfile.mkdtemp())
        package = tmp / "x.fcore"
        raw = b'[core]\nid = "fes.c64"\n[[interfaces]]\nid = "fes.media.c64-disk"\nmajor = 1\nminor = 0\n'
        with tarfile.open(package, "w:gz") as archive:
            info = tarfile.TarInfo("manifest.toml")
            info.size = len(raw)
            archive.addfile(info, io.BytesIO(raw))
        self.assertEqual(conf.package_interfaces(package), ["fes.media.c64-disk"])


if __name__ == "__main__":
    unittest.main()
