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
        # 10 PRINT "FES TAPE OK": BEEP 2,9
        self.assertIn(b'\xF5"FES TAPE OK":\xD7' + b"2\x0E\x00\x00\x02\x00\x00," + b"9\x0E\x00\x00\x09\x00\x00\x0D", body)

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

    def test_plus3_dsk_layout(self):
        data = conf.plus3_blank_dsk()
        self.assertTrue(data.startswith(b"EXTENDED CPC DSK File\r\nDisk-Info\r\n"))
        self.assertEqual((data[48], data[49]), (40, 1))
        self.assertEqual(len(data), 256 + 40 * (256 + 9 * 512))
        track0 = data[256:512]
        self.assertEqual(track0[:12], b"Track-Info\r\n")
        self.assertEqual([track0[24 + 8 * k + 2] for k in range(9)], list(range(1, 10)))
        self.assertEqual(set(data[512:512 + 9 * 512]), {0xE5})

    def test_trd_layout(self):
        data = conf.trdos_blank_trd()
        self.assertEqual(len(data), 655360)
        info = data[0x800:0x900]
        self.assertEqual((info[0xE3], info[0xE4], info[0xE7]), (0x16, 0, 0x10))
        self.assertEqual(struct.unpack_from("<H", info, 0xE5)[0], 2544)

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
        audio = "fes.audio.pcm-s16-stereo-48k"
        # What each sealed DE25 package declares (fes.media/mouse/audio only).
        declared = {
            "fes.zx81": [audio, "fes.media.zx81-tape", "fes.media.zx81-tape-write"],
            "fes.spectrum": [audio, "fes.media.spectrum-tape"],
            "fes.spectrum128": [audio, "fes.media.spectrum-tape"],
            "fes.spectrum-plus3": [audio, "fes.media.spectrum-tape", "fes.media.spectrum-disk",
                                   "fes.media.spectrum-disk-plus3"],
            "fes.pentagon": [audio, "fes.media.spectrum-tape", "fes.media.spectrum-disk",
                             "fes.media.spectrum-disk-trdos"],
            "fes.c64": [audio, "fes.media.c64-disk"],
            "fes.atari-st": [audio, "fes.mouse.relative", "fes.media.atari-st-floppy",
                             "fes.media.atari-st-floppy-write", "fes.media.atari-st-floppy-geometry"],
            "fes.pcxt-dos": [audio, "fes.media.pcxt-floppy", "fes.media.pcxt-floppy-write"],
        }
        # Checks not written yet must show as pending, never as covered.
        pending = {
            "fes.zx81": ["fes.media.zx81-tape-write"],
            "fes.atari-st": ["fes.media.atari-st-floppy-write"],
        }
        spec = conf.load_spec()["cores"]
        self.assertEqual(set(spec), set(declared))
        for core, interfaces in declared.items():
            self.assertEqual(conf.pending_interfaces(spec[core]["steps"], interfaces,
                                                     spec[core].get("not_applicable")),
                             pending.get(core, []), core)
        self.assertEqual(list(spec["fes.zx81"]["not_applicable"]), [audio])

    def test_not_applicable_needs_a_reason(self):
        for na in ({"fes.audio.x": "short"}, {"fes.keyboard.hid": "a reason that is long enough here"}):
            path = self.write({"fes.x": {"not_applicable": na, "steps": [{"name": "a", "keys": ["p"]}]}})
            with self.subTest(na=na), self.assertRaises(ValueError):
                conf.load_spec(path)

    def test_text_keys(self):
        self.assertEqual(conf.expand_keys(["text:poke54276,17", "enter"]),
                         ["p", "o", "k", "e", "5", "4", "2", "7", "6", "comma", "1", "7", "enter"])
        self.assertEqual(conf.expand_keys(["text:beep 2,9"])[4], "space")
        with self.assertRaises(ValueError):
            conf.expand_keys(['text:load"$"'])
        self.assertEqual(conf.key_tap("ctrl+alt+delete"), (0x104C, 0x05))

    def test_bind_needs_cli(self):
        path = self.write({"fes.x": {"steps": [{"name": "a", "keys": ["p"], "bind": {"g": "game_id"}}]}})
        with self.assertRaises(ValueError):
            conf.load_spec(path)

    def test_audio_expectation_needs_min_rms(self):
        for audio in ({"tone_hz": 440}, {"min_rms": 1, "pitch": 2}):
            path = self.write({"fes.x": {"steps": [{"name": "a", "keys": ["p"], "expect": {"audio": audio}}]}})
            with self.subTest(audio=audio), self.assertRaises(ValueError):
                conf.load_spec(path)


class Audio(unittest.TestCase):
    def wav(self, seconds, tone=None, amplitude=8000):
        import math, wave
        path = Path(tempfile.mkdtemp()) / "a.wav"
        frames = bytearray()
        for i in range(int(48000 * seconds)):
            v = int(amplitude * math.sin(2 * math.pi * tone * i / 48000)) if tone else 0
            frames += struct.pack("<hh", v, v)
        with wave.open(str(path), "wb") as f:
            f.setnchannels(2); f.setsampwidth(2); f.setframerate(48000); f.writeframes(bytes(frames))
        return path

    def test_tone_at_the_right_rate(self):
        windows = conf.audio_windows(self.wav(1.0, 807.3))
        self.assertTrue(all(abs(f - 807.3) < 807.3 * 0.03 for _, f in windows))
        ok, note = conf.judge_audio(windows, {"tone_hz": 807.3, "tolerance": 0.03, "min_rms": 500})
        self.assertTrue(ok, note)
        # The same tone played 5% fast is the wrong rate.
        ok, note = conf.judge_audio(conf.audio_windows(self.wav(1.0, 847.7)),
                                    {"tone_hz": 807.3, "tolerance": 0.03, "min_rms": 500})
        self.assertFalse(ok, note)

    def test_silence_and_quiet_fail(self):
        self.assertFalse(conf.judge_audio(conf.audio_windows(self.wav(1.0)), {"min_rms": 300})[0])
        quiet = conf.audio_windows(self.wav(1.0, 440, amplitude=100))
        self.assertFalse(conf.judge_audio(quiet, {"min_rms": 300})[0])
        self.assertTrue(conf.judge_audio(conf.audio_windows(self.wav(1.0, 440)), {"min_rms": 300})[0])

    def test_a_short_tone_is_not_held(self):
        ok, note = conf.judge_audio(conf.audio_windows(self.wav(0.3, 440)),
                                    {"tone_hz": 440, "min_rms": 500})
        self.assertFalse(ok, note)


class Frames(unittest.TestCase):
    def test_text_rows_finds_and_splits_rows(self):
        width, height = 64, 60
        raw = bytearray(width * height * 3)
        def stripes(y0, y1):
            for y in range(y0, y1):
                for x in range(0, width, 4):
                    i = (y * width + x) * 3
                    raw[i:i + 6] = bytes([255]) * 6
        stripes(2, 12)    # one row
        stripes(20, 30)   # another
        stripes(36, 56)   # two rows with no gap between them
        bands = conf.text_rows(bytes(raw), width, min_edges=4, pad=0)
        self.assertEqual(bands, [(2, 12), (20, 30), (36, 46), (46, 56)])
        self.assertEqual(conf.text_rows(bytes(width * height * 3), width), [])

    def test_frame_delta(self):
        a = bytes(300)
        self.assertEqual(conf.frame_delta(a, a), 0.0)
        b = bytes([255]) * 300
        self.assertEqual(conf.frame_delta(a, b), 1.0)
        self.assertIsNone(conf.frame_delta(a, bytes(3)))
        self.assertIsNone(conf.frame_delta(b"", b""))

    def test_normalise(self):
        self.assertIn(conf.normalise("blocks free"), conf.normalise("664 BLOCKS\nFREE."))


def units(state, interface="fes.media.x-disk"):
    return {"state": "active", "core_package": {"media_units": [
        {"unit": 0, "interface": {"id": interface, "major": 1, "minor": 0}, "state": state}]}}


class FakeHost:
    def __init__(self, tap_ok=True, cli_rc=0, unit_state="ready", later=None):
        self.taps, self.calls = [], []
        self.tap_ok, self.cli_rc, self.unit_state = tap_ok, cli_rc, unit_state
        self.later = list(later or [])

    def req(self, path):
        return units(self.later.pop(0) if self.later else self.unit_state)

    def input_session(self):
        return "s1"

    def tap(self, session_id, token):
        self.taps.append(token)
        return {"ok": self.tap_ok}

    replies = {}

    def cli(self, *args):
        self.calls.append(args)
        reply = self.replies.get(args[0], units(self.unit_state))
        return self.cli_rc, reply, "ok"


class FakeRecorder:
    tone = None

    def __init__(self, evidence, kit):
        self.path = Path(evidence) / "a.wav"

    def start(self):
        pass

    def stop(self):
        Audio.wav(Audio(), 1.0, self.tone).replace(self.path)


class Run(unittest.TestCase):
    def run_steps(self, steps, host, frames, ocr_text="", **kw):
        out = Path(tempfile.mkdtemp())
        seq = iter(frames)
        with mock.patch.object(conf, "grab", lambda video, dest: dest.write_bytes(b"png") or True), \
             mock.patch.object(conf, "rgb", lambda path: next(seq)), \
             mock.patch.object(conf, "ocr", lambda path: ocr_text), \
             mock.patch.object(conf.time, "sleep", lambda s: None):
            return conf.run("fes.x", steps, ["fes.media.x-disk"], host, "/dev/null", out, **kw)

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

    def test_media_needs_the_unit_ready(self):
        steps = [{"name": "m", "media": ["change-disk", "c64-fes-blank.d64"], "covers": ["fes.media.x-disk"]}]
        host = FakeHost()
        report = self.run_steps(steps, host, [])
        self.assertTrue(report["checks"]["m"]["ok"], report)
        self.assertEqual(report["pending"], [])
        self.assertEqual(host.calls[0][0], "change-disk")
        self.assertTrue(host.calls[0][1].endswith("c64-fes-blank.d64"))
        self.assertTrue(Path(host.calls[0][1]).is_file())
        loading = FakeHost(unit_state="loading", later=["loading", "ready"])
        self.assertTrue(self.run_steps(steps, loading, [])["checks"]["m"]["ok"], "loading then ready")
        self.assertFalse(self.run_steps(steps, FakeHost(unit_state="empty"), [])["checks"]["m"]["ok"])
        self.assertFalse(self.run_steps(steps, FakeHost(cli_rc=1), [])["checks"]["m"]["ok"])
        other = [{"name": "m", "media": ["change-disk", "c64-fes-blank.d64"], "covers": ["fes.media.y-disk"]}]
        self.assertFalse(self.run_steps(other, FakeHost(), [])["checks"]["m"]["ok"], "no unit for the covered kind")

    def test_cli_binds_values_for_later_steps(self):
        host = FakeHost()
        host.replies = {"shelf-new": {"game_id": "g1", "media_id": "m1"},
                        "shelf": {"disks": [{"game_id": "g1", "kept_revision": "r1"}]}}
        steps = [{"name": "made", "cli": ["shelf-new", "{core_id}", "T {stamp}", "{package_id}", "freedos"],
                  "bind": {"game": "game_id", "media": "media_id"}},
                 {"name": "in", "cli": ["insert-library-disk", "{game}", "{media}"],
                  "covers": ["fes.media.x-disk"], "expect": {"units": True}},
                 {"name": "kept", "cli": ["shelf", "{core_id}"], "expect": {"shelf_kept": "{game}"}}]
        report = self.run_steps(steps, host, [], package_id="p" * 64)
        self.assertEqual(report["verdict"], "PASS", report)
        self.assertEqual(host.calls[0][1], "fes.x")
        self.assertEqual(host.calls[0][3], "p" * 64)
        self.assertEqual(host.calls[1], ("insert-library-disk", "g1", "m1"))
        host.replies["shelf"] = {"disks": [{"game_id": "g1"}]}
        self.assertFalse(self.run_steps(steps, host, [])["checks"]["kept"]["ok"], "nothing kept")
        host.replies["shelf-new"] = {}
        self.assertFalse(self.run_steps(steps, host, [])["checks"]["made"]["ok"], "nothing to bind")

    def test_audio_expectation_records_the_step(self):
        steps = [{"name": "t", "keys": ["enter"], "expect": {"audio": {"tone_hz": 440, "min_rms": 500}}}]
        FakeRecorder.tone = 440
        report = self.run_steps(steps, FakeHost(), [], kit="D25", recorder=FakeRecorder)
        self.assertTrue(report["checks"]["t"]["ok"], report)
        FakeRecorder.tone = 600
        self.assertFalse(self.run_steps(steps, FakeHost(), [], kit="D25", recorder=FakeRecorder)["checks"]["t"]["ok"])
        self.assertFalse(self.run_steps(steps, FakeHost(), [], recorder=FakeRecorder)["checks"]["t"]["ok"],
                         "no kit capture named")

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
