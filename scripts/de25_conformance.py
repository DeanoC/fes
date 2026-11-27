#!/usr/bin/env python3
"""Run the Kit D25 conformance steps for one live core session.

hil-de25-core.sh calls this after the core's boot frames, while the session
is still active.  The steps are data (config/de25-conformance.json): HID key
taps through the host's confirmed tap, a relative mouse move, and media
changes through the FogCast CLI with fixtures this tool generates.  A step
passes on its command result and, when it says so, on the HDMI capture: a
pixel change against the frame taken just before the step, text that
tesseract reads off the screen, or sound on the kit's HDMI audio capture
(non-silent, and at a stated tone frequency when the step plays a known
tone, which checks the machine's audio rate).

Every fes.media.*, fes.mouse.* and fes.audio.* interface the package declares must be
covered by a step, or named not_applicable with the reason.  Interfaces
nobody covers are listed as pending, so a missing check is visible rather
than silently passing.

Exit status: 0 all checks pass, 1 a check failed, 3 the core has no steps.
The JSON report goes to <out>/conformance.json and to stdout.
"""
from __future__ import annotations

import argparse
import json
import struct
import subprocess
import sys
import tarfile
import time
import tomllib
import urllib.error
import urllib.request
import wave
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from pcxt_game_audio_capture import AudioError, AudioRecorder  # noqa: E402

SPEC = Path(__file__).resolve().parent.parent / "config" / "de25-conformance.json"
HID_BASE = 0x1000
COVERED_PREFIXES = ("fes.media.", "fes.mouse.", "fes.audio.")

# USB HID keyboard usages (page 0x07).
USAGES = {**{chr(ord("a") + i): 0x04 + i for i in range(26)},
          **{str(i): 0x1E + i - 1 for i in range(1, 10)}, "0": 0x27,
          "enter": 0x28, "escape": 0x29, "backspace": 0x2A, "tab": 0x2B, "space": 0x2C,
          "minus": 0x2D, "equal": 0x2E, "comma": 0x36, "period": 0x37, "slash": 0x38,
          "delete": 0x4C, "right": 0x4F, "left": 0x50, "down": 0x51, "up": 0x52}
TEXT_KEYS = {" ": "space", ",": "comma", ".": "period", "-": "minus", "=": "equal", "/": "slash"}
MODIFIERS = {"ctrl": 0x01, "shift": 0x02, "alt": 0x04}


def expand_keys(tokens: list[str]) -> list[str]:
    """Key tokens with any "text:..." token spelled out one unshifted key per
    character (letters, digits, space and , . - = /)."""
    out = []
    for token in tokens:
        if not token.startswith("text:"):
            out.append(token)
            continue
        for ch in token[5:].lower():
            key = TEXT_KEYS.get(ch, ch)
            if key not in USAGES:
                raise ValueError(f"no plain key for {ch!r} in {token!r}")
            out.append(key)
    return out


def key_tap(token: str) -> tuple[int, int]:
    """Return (event code, modifier byte) for a token such as "shift+p"."""
    *mods, key = token.split("+")
    if key not in USAGES or any(m not in MODIFIERS for m in mods):
        raise ValueError(f"unknown key {token!r}")
    bits = 0
    for m in mods:
        bits |= MODIFIERS[m]
    return HID_BASE + USAGES[key], bits


# ---------------------------------------------------------------- fixtures

def zx81_text(text: str) -> bytes:
    """ZX81 character codes: space 0x00, digits 0x1C.., letters 0x26.."""
    out = bytearray()
    for ch in text.upper():
        if ch == " ":
            out.append(0x00)
        elif ch.isdigit():
            out.append(0x1C + int(ch))
        elif "A" <= ch <= "Z":
            out.append(0x26 + ord(ch) - ord("A"))
        else:
            raise ValueError(f"no ZX81 code for {ch!r}")
    return bytes(out)


def zx81_p(message: str) -> bytes:
    """A program-less ZX81 .p whose saved display holds `message` on line 0.

    LOAD restores the display file, so the message is on screen once the
    tape has loaded.  The file is memory 0x4009..E_LINE-1.
    """
    d_file = 0x407D
    display = bytearray([0x76])
    for row in range(24):
        line = zx81_text(message) if row == 0 else b""
        display += line.ljust(32, b"\x00") + b"\x76"
    vars_ = d_file + len(display)
    e_line = vars_ + 1
    sv = bytearray()
    sv += bytes([0])                                   # VERSN
    sv += struct.pack("<H", 0)                         # E_PPC
    sv += struct.pack("<HH", d_file, d_file + 1)       # D_FILE, DF_CC
    sv += struct.pack("<HH", vars_, 0)                 # VARS, DEST
    sv += struct.pack("<HHH", e_line, e_line + 4, 0)   # E_LINE, CH_ADD, X_PTR
    sv += struct.pack("<HH", e_line + 5, e_line + 5)   # STKBOT, STKEND
    sv += bytes([0]) + struct.pack("<H", 0x405D)       # BERG, MEM
    sv += bytes([0, 2]) + struct.pack("<H", 0)         # unused, DF_SZ, S_TOP
    sv += struct.pack("<H", 0xFFFF) + bytes([0x00, 55])  # LAST_K, DB_ST, MARGIN
    sv += struct.pack("<HH", d_file, 0) + bytes([0])   # NXTLIN (no autorun), OLDPPC, FLAGX
    sv += struct.pack("<HH", 0, 0x0C8D)                # STRLEN, T_ADDR
    sv += struct.pack("<HHH", 0, 0xFFFF, 0)            # SEED, FRAMES, COORDS
    sv += bytes([0xBC, 0x21, 0x18, 0x40])              # PR_CC, S_POSN, CDFLAG
    sv += bytes(32) + b"\x76"                          # PRBUFF
    sv += bytes(30) + bytes(2)                         # MEMBOT, unused
    assert 0x4009 + len(sv) == d_file, hex(0x4009 + len(sv))
    return bytes(sv + display + b"\x80")


def _tap_block(flag: int, body: bytes) -> bytes:
    data = bytes([flag]) + body
    check = 0
    for b in data:
        check ^= b
    data += bytes([check])
    return struct.pack("<H", len(data)) + data


def spectrum_number(value: int) -> bytes:
    """A small integer as BASIC stores it in a line: digits, then 0x0E and
    the five-byte integer form."""
    return str(value).encode() + b"\x0E\x00\x00" + struct.pack("<H", value) + b"\x00"


def spectrum_tap(message: str) -> bytes:
    """A .tap holding `10 PRINT "message": BEEP 2,9`, autostarting at line 10.

    BEEP 2,9 sounds A above middle C (440 Hz) for two seconds once the
    program has loaded, which checks the machine's audio rate."""
    line = (b"\xF5\"" + message.encode("ascii") + b"\":\xD7" + spectrum_number(2) + b","
            + spectrum_number(9) + b"\x0D")
    program = struct.pack(">H", 10) + struct.pack("<H", len(line)) + line
    header = (bytes([0]) + b"FES".ljust(10, b" ")
              + struct.pack("<HHH", len(program), 10, len(program)))
    return _tap_block(0x00, header) + _tap_block(0xFF, program)


C64_TRACK_SECTORS = [21] * 17 + [19] * 7 + [18] * 6 + [17] * 5


def c64_blank_d64(name: str = "FES TEST", disk_id: bytes = b"FE") -> bytes:
    """A formatted, empty 35-track D64 (664 blocks free)."""
    image = bytearray(256 * sum(C64_TRACK_SECTORS))
    bam_offset = 256 * sum(C64_TRACK_SECTORS[:17])
    bam = bytearray(256)
    bam[0:4] = bytes([18, 1, 0x41, 0])
    for track, sectors in enumerate(C64_TRACK_SECTORS, 1):
        free = (1 << sectors) - 1
        if track == 18:
            free &= ~0b11
        entry = bytes([bin(free).count("1")]) + free.to_bytes(3, "little")
        bam[4 * track:4 * track + 4] = entry
    bam[0x90:0xA0] = name.encode("ascii").ljust(16, b"\xA0")
    bam[0xA0:0xAB] = b"\xA0\xA0" + disk_id + b"\xA0" + b"2A" + b"\xA0" * 4
    image[bam_offset:bam_offset + 256] = bam
    image[bam_offset + 256 + 1] = 0xFF
    return bytes(image)


def atari_st_blank(label: str = "FES TEST") -> bytes:
    """A 720 KiB data disk laid out as FogCast's own blank ST disk."""
    image = bytearray(737280)
    boot = memoryview(image)[:512]
    boot[0:2] = b"\xEB\x3C"
    boot[2:8] = b"FES1.0"
    boot[8:11] = b"FES"
    boot[11:28] = struct.pack("<HBHBHHBHHH", 512, 2, 1, 2, 112, 1440, 0xF9, 5, 9, 2)
    # EmuTOS non-executable checksum: the word sum must not be 0x1234.
    total = sum(struct.unpack(">255H", bytes(boot[:510])))
    check = (0x1234 - total) & 0xFFFF
    boot[510:512] = bytes([check >> 8, (check + 1) & 0xFF])
    for fat in range(2):
        start = 512 * (1 + 5 * fat)
        image[start:start + 3] = b"\xF9\xFF\xFF"
    root = 512 * 11
    image[root:root + 11] = label.encode("ascii").ljust(11, b" ")
    image[root + 11] = 0x08
    return bytes(image)


def plus3_blank_dsk() -> bytes:
    """An extended CPC DSK of a freshly formatted +3 disk: 40 tracks, one side,
    nine 512-byte sectors numbered 1-9, every byte 0xE5."""
    tracks, sectors = 40, 9
    track_size = 256 + sectors * 512
    header = bytearray(256)
    header[0:34] = b"EXTENDED CPC DSK File\r\nDisk-Info\r\n"
    header[34:48] = b"FES conform   "
    header[48], header[49] = tracks, 1
    for t in range(tracks):
        header[52 + t] = track_size // 256
    out = bytearray(header)
    for t in range(tracks):
        info = bytearray(256)
        info[0:12] = b"Track-Info\r\n"
        info[16], info[17] = t, 0
        info[20], info[21], info[22], info[23] = 2, sectors, 0x4E, 0xE5
        for k in range(sectors):
            info[24 + 8 * k:32 + 8 * k] = bytes([t, 0, k + 1, 2, 0, 0]) + struct.pack("<H", 512)
        out += info + b"\xE5" * (sectors * 512)
    return bytes(out)


def trdos_blank_trd(label: str = "FES TEST") -> bytes:
    """An empty 80-track double-sided TR-DOS disk (2544 free sectors)."""
    image = bytearray(80 * 2 * 16 * 256)
    info = memoryview(image)[8 * 256:9 * 256]
    info[0xE1], info[0xE2], info[0xE3], info[0xE4] = 0, 1, 0x16, 0
    info[0xE5:0xE7] = struct.pack("<H", 2544)
    info[0xE7] = 0x10
    info[0xEA:0xF3] = b" " * 9
    info[0xF5:0xFD] = label.encode("ascii").ljust(8, b" ")
    return bytes(image)


FIXTURES = {
    "zx81-fes-tape-ok.p": lambda: zx81_p("FES TAPE OK"),
    "spectrum-fes-tape-ok.tap": lambda: spectrum_tap("FES TAPE OK"),
    "c64-fes-blank.d64": c64_blank_d64,
    "atari-st-fes-blank.st": atari_st_blank,
    "plus3-fes-blank.dsk": plus3_blank_dsk,
    "trdos-fes-blank.trd": trdos_blank_trd,
}


# ---------------------------------------------------------------- spec

def load_spec(path: Path = SPEC) -> dict:
    spec = json.loads(path.read_text())
    if spec.get("format") != 1 or not isinstance(spec.get("cores"), dict):
        raise ValueError("unsupported conformance spec")
    for core, entry in spec["cores"].items():
        for interface, reason in (entry.get("not_applicable") or {}).items():
            if not interface.startswith(COVERED_PREFIXES) or not isinstance(reason, str) or len(reason) < 20:
                raise ValueError(f"{core}: not_applicable needs a covered interface and a stated reason")
        names = set()
        for step in entry["steps"]:
            name = step.get("name")
            if name:
                if name in names:
                    raise ValueError(f"{core}: step {name} repeated")
                names.add(name)
            elif "expect" in step:
                raise ValueError(f"{core}: a step with an expectation needs a name")
            actions = [k for k in ("keys", "mouse", "media", "cli") if k in step]
            if len(actions) != 1:
                raise ValueError(f"{core}: each step does exactly one of keys, mouse, media or cli")
            for token in expand_keys(step.get("keys", [])):
                key_tap(token)
            if "bind" in step and "cli" not in step:
                raise ValueError(f"{core}: only a cli step binds values")
            media = step.get("media")
            if media and len(media) > 1 and media[1] not in FIXTURES:
                raise ValueError(f"{core}: unknown fixture {media[1]}")
            expect = step.get("expect", {})
            if set(expect) - {"idle_max", "change", "ocr", "audio", "units", "shelf_kept"}:
                raise ValueError(f"{core}: unknown expectation in {name}")
            audio = expect.get("audio")
            if audio is not None and (set(audio) - {"tone_hz", "tolerance", "min_rms"} or "min_rms" not in audio):
                raise ValueError(f"{core}: audio expectation in {name} needs min_rms, and only tone_hz/tolerance besides")
            if set(expect) & {"change", "ocr", "audio", "units", "shelf_kept"} and not name:
                raise ValueError(f"{core}: unnamed step with an expectation")
    return spec


def package_interfaces(package: Path) -> list[str]:
    with tarfile.open(package, "r:*") as archive:
        member = archive.extractfile("manifest.toml")
        if member is None:
            raise ValueError("package manifest missing")
        manifest = tomllib.loads(member.read().decode())
    return [i["id"] for i in manifest.get("interfaces", [])]


def pending_interfaces(steps: list[dict], interfaces: list[str], not_applicable: dict | None = None) -> list[str]:
    covered = {c for step in steps for c in step.get("covers", [])} | set(not_applicable or {})
    return sorted(i for i in interfaces if i.startswith(COVERED_PREFIXES) and i not in covered)


# ---------------------------------------------------------------- frames

def grab(video: str, dest: Path) -> bool:
    for attempt in range(6):
        cmd = ["timeout", "-k", "5", "25", "ffmpeg", "-loglevel", "error", "-y", "-f", "v4l2"]
        cmd += ["-input_format", "mjpeg"] if attempt % 2 else []
        cmd += ["-i", video, "-vf", "select=gte(n\\,15)", "-frames:v", "1", str(dest)]
        if subprocess.run(cmd, capture_output=True).returncode == 0 and dest.exists() and dest.stat().st_size > 2000:
            return True
        time.sleep(1.5)
    return False


def rgb(path: Path) -> bytes:
    return subprocess.run(["ffmpeg", "-loglevel", "error", "-i", str(path), "-f", "rawvideo",
                           "-pix_fmt", "rgb24", "-"], capture_output=True).stdout


def frame_delta(a: bytes, b: bytes) -> float | None:
    """Fraction of sampled pixels whose RGB moved by more than 96 in total."""
    if not a or len(a) != len(b):
        return None
    n = d = 0
    for i in range(0, len(a) - 2, 12):
        n += 1
        d += abs(a[i] - b[i]) + abs(a[i + 1] - b[i + 1]) + abs(a[i + 2] - b[i + 2]) > 96
    return d / n


def normalise(text: str) -> str:
    return "".join(ch for ch in text.upper() if ch.isalnum())


def text_rows(raw: bytes, width: int, min_edges: int = 6, pad: int = 4) -> list[tuple[int, int]]:
    """Bands of scanlines that carry text, found by colour edges along each line.

    Borders and plain fields give a handful of edges per line; a line through
    characters gives many.  Bands are padded and clipped to the frame.
    """
    height = len(raw) // (3 * width)
    inked = []
    for y in range(height):
        row = raw[y * width * 3:(y + 1) * width * 3]
        edges = 0
        for x in range(6, width * 3 - 2, 6):
            edges += abs(row[x] - row[x - 6]) + abs(row[x + 1] - row[x - 5]) + abs(row[x + 2] - row[x - 4]) > 96
        inked.append(edges >= min_edges)
    runs, start = [], None
    for y, ink in enumerate(inked + [False]):
        if ink and start is None:
            start = y
        elif not ink and start is not None:
            if y - start >= 8:
                runs.append((start, y))
            start = None
    if not runs:
        return []
    # Rows with no gap between them (a reverse-video line above a text line)
    # merge into one run: cut runs taller than 1.5 rows into row-sized pieces.
    row = sorted(b - a for a, b in runs)[len(runs) // 2]
    bands = []
    for a, b in runs:
        pieces = max(1, round((b - a) / row)) if b - a > 1.5 * row else 1
        for k in range(pieces):
            top, bottom = a + (b - a) * k // pieces, a + (b - a) * (k + 1) // pieces
            bands.append((max(0, top - pad), min(height, bottom + pad)))
    return bands


def tesseract(png: Path, psm: int) -> str:
    return subprocess.run(["tesseract", str(png), "-", "--psm", str(psm)], capture_output=True, text=True).stdout


def ocr(path: Path) -> str:
    """Every reading of the frame, joined: the whole frame as captured,
    inverted and at half size, then each text row on its own.  Tesseract
    misreads blocky machine fonts in a full-screen layout (a reverse-video
    line can swallow the line below it) but reads them reliably one row at
    a time."""
    work = path.with_suffix(".ocr")
    work.mkdir(exist_ok=True)
    texts = []
    for n, filt in enumerate(("format=gray", "format=gray,negate", "format=gray,scale=iw/2:-1:flags=area")):
        png = work / f"frame-{n}.png"
        subprocess.run(["ffmpeg", "-loglevel", "error", "-y", "-i", str(path), "-vf", filt, str(png)],
                       capture_output=True)
        texts.append(tesseract(png, 6))
    raw = rgb(path)
    width = 1920 if len(raw) >= 1920 * 1080 * 3 else 1280
    for top, bottom in text_rows(raw, width):
        png = work / f"row-{top:04d}.png"
        subprocess.run(["ffmpeg", "-loglevel", "error", "-y", "-i", str(path), "-vf",
                        f"crop=iw:{bottom - top}:0:{top},format=gray", str(png)], capture_output=True)
        texts.append(tesseract(png, 7))
    return "\n".join(texts)


def audio_windows(path: Path, window: int = 4800) -> list[tuple[float, float | None]]:
    """(rms, frequency) per 100 ms window of a 48 kHz stereo s16 WAV.

    The frequency is from zero crossings of the mono mix about its mean; it
    is None for windows too quiet to measure.
    """
    with wave.open(str(path), "rb") as f:
        if (f.getnchannels(), f.getsampwidth()) != (2, 2):
            raise ValueError("expected 16-bit stereo audio")
        rate = f.getframerate()
        raw = f.readframes(f.getnframes())
    samples = struct.unpack(f"<{len(raw) // 2}h", raw)
    mono = [(samples[i] + samples[i + 1]) / 2 for i in range(0, len(samples) - 1, 2)]
    out = []
    for start in range(0, len(mono) - window + 1, window):
        chunk = mono[start:start + window]
        mean = sum(chunk) / window
        centred = [x - mean for x in chunk]
        rms = (sum(x * x for x in centred) / window) ** 0.5
        crossings = sum(1 for a, b in zip(centred, centred[1:]) if (a < 0) != (b < 0))
        out.append((rms, crossings * rate / (2 * window) if rms >= 50 else None))
    return out


def judge_audio(windows: list[tuple[float, float | None]], expect: dict) -> tuple[bool, str]:
    """Loud enough somewhere; and, for a tone, held for half a second at its frequency."""
    loud = [w for w in windows if w[0] >= expect["min_rms"]]
    peak = max((w[0] for w in windows), default=0.0)
    if "tone_hz" not in expect:
        return bool(loud), f"peak_rms={peak:.0f} loud_windows={len(loud)}/{len(windows)}"
    tone, tol = expect["tone_hz"], expect.get("tolerance", 0.03)
    best = run = 0
    for rms, freq in windows:
        run = run + 1 if rms >= expect["min_rms"] and freq and abs(freq - tone) <= tone * tol else 0
        best = max(best, run)
    freqs = sorted(round(f) for r, f in windows if f and r >= expect["min_rms"])
    median = freqs[len(freqs) // 2] if freqs else None
    return best >= 5, f"peak_rms={peak:.0f} tone {tone}Hz held {best / 10:.1f}s median_loud_hz={median}"


# ---------------------------------------------------------------- host

class Host:
    def __init__(self, api: str, fogcast: str, out: Path):
        self.api, self.fogcast, self.out = api.rstrip("/"), fogcast, out
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def req(self, path: str, data: dict | None = None, timeout: float = 45) -> dict:
        body = None if data is None else json.dumps(data).encode()
        headers = {} if data is None else {"Content-Type": "application/json"}
        try:
            with self.opener.open(urllib.request.Request(self.api + path, data=body, headers=headers),
                                  timeout=timeout) as r:
                raw = r.read(1 << 20)
        except urllib.error.HTTPError as e:
            return {"error": e.code, "body": e.read(4096).decode(errors="replace")}
        return json.loads(raw) if raw else {}

    def input_session(self) -> str:
        for _ in range(10):
            inp = self.req("/api/v1/session/input")
            if inp.get("state") == "attached" and inp.get("ready") and inp.get("session_id"):
                return inp["session_id"]
            time.sleep(1)
        return ""

    def tap(self, session_id: str, token: str) -> dict:
        code, mods = key_tap(token)
        body = {"session_id": session_id,
                "event": {"Player": 0, "Device": 0, "Kind": 0, "Action": 1, "Code": code, "Value": 0}}
        if mods:
            body["modifiers"] = mods
        return self.req("/api/v1/session/input/tap", body)

    def cli(self, *args: str) -> tuple[int, dict, str]:
        # A media change can outlive its reply (the host finishes it under its
        # own deadline, up to 150 s), and the session answers BUSY meanwhile.
        # Retry through that, and keep every attempt: the first one says why.
        attempts = []
        deadline = time.monotonic() + 180
        while True:
            started = time.monotonic()
            r = subprocess.run([self.fogcast, "--api", self.api, "--json", *args],
                               capture_output=True, text=True, timeout=300)
            text = (r.stdout + r.stderr).strip()
            attempts.append({"seconds": round(time.monotonic() - started, 1), "rc": r.returncode, "out": text[:400]})
            if r.returncode == 0 or '"code":"BUSY"' not in text or time.monotonic() > deadline:
                break
            time.sleep(2)
        with (self.out / "cli-attempts.jsonl").open("a") as log:
            log.write(json.dumps({"args": [Path(a).name if "/" in a else a for a in args], "attempts": attempts}) + "\n")
        if len(attempts) > 1:
            text = f"[{len(attempts)} attempts; first rc={attempts[0]['rc']} after {attempts[0]['seconds']}s: {attempts[0]['out'][:160]}] " + text
        try:
            value = json.loads(r.stdout) if r.stdout.strip().startswith("{") else {}
        except ValueError:
            value = {}
        return r.returncode, value, text


def media_unit_states(session: dict) -> dict[str, str]:
    """Interface id -> unit state from a session or CLI status reply. The
    library-disk commands print the host client's session struct, whose
    outer fields keep their Go names (CorePackage)."""
    package = session.get("core_package") or session.get("CorePackage") or {}
    units = package.get("media_units") or []
    return {u.get("interface", {}).get("id", ""): u.get("state", "") for u in units if isinstance(u, dict)}


def wait_units(host, value: dict, covers: list[str]) -> tuple[bool, str]:
    """The covered media units read "ready", polling while one is "loading"."""
    units = media_unit_states(value)
    wanted = [c for c in covers if c in units]
    for _ in range(20):
        if not wanted or all(units[c] == "ready" for c in wanted):
            break
        time.sleep(1)
        units = media_unit_states(host.req("/api/v1/session"))
    if not wanted:
        return False, f"no unit for {covers} in {sorted(units)}"
    return all(units[c] == "ready" for c in wanted), "units " + ", ".join(f"{c}={units[c]}" for c in wanted)


# ---------------------------------------------------------------- run

def run(core: str, steps: list[dict], interfaces: list[str], host: Host, video: str, out: Path,
        kit: str | None = None, recorder=AudioRecorder, package_id: str = "",
        not_applicable: dict | None = None) -> dict:
    out.mkdir(parents=True, exist_ok=True)
    fixtures = out / "fixtures"
    fixtures.mkdir(exist_ok=True)
    checks: dict[str, dict] = {}
    report = {"core": core, "checks": checks, "pending": pending_interfaces(steps, interfaces, not_applicable),
              "not_applicable": not_applicable or {}}

    def check(name, ok, detail):
        checks[name] = {"ok": bool(ok), "detail": detail}

    variables = {"core_id": core, "package_id": package_id, "stamp": time.strftime("%H%M%S")}
    session_id = ""
    if any("keys" in s for s in steps):
        session_id = host.input_session()
        check("input_attached", bool(session_id), session_id or "no attached input session")
    previous = out / "f-start.png"
    grab(video, previous)
    for index, step in enumerate(steps):
        name = step.get("name") or f"step{index}"
        expect = step.get("expect", {})
        before = previous
        if "idle_max" in expect:
            time.sleep(2)
            idle = out / f"f{index:02d}-idle.png"
            grab(video, idle)
            idle_delta = frame_delta(rgb(before), rgb(idle))
            before = idle
        detail, ok = [], True
        rec = None
        recorder_started = False
        cleanup_incomplete = False
        if "audio" in expect:
            try:
                if not kit:
                    raise AudioError("no kit audio capture named")
                evidence = out / f"audio-{index:02d}-{name}"
                evidence.mkdir()
                rec = recorder(evidence, kit)
                rec.start()
                recorder_started = True
            except (AudioError, OSError) as exc:
                rec, ok = None, False
                cleanup_incomplete = bool(getattr(exc, "audio_cleanup_incomplete", False))
                if cleanup_incomplete:
                    detail.append(f"audio cleanup incomplete: {exc}; aborting remaining steps")
                else:
                    detail.append(f"audio capture did not start: {exc}")
        if not cleanup_incomplete:
            try:
                if "keys" in step:
                    for token in expand_keys(step["keys"]):
                        if not session_id:
                            ok = False
                            detail.append("no input session")
                            break
                        r = host.tap(session_id, token)
                        if r.get("ok") is not True:
                            ok = False
                            detail.append(f"{token}: {r}")
                            break
                        time.sleep(0.15)
                    detail.append("keys " + " ".join(step["keys"]))
                elif "mouse" in step:
                    dx, dy, buttons = step["mouse"]
                    rc, _, text = host.cli("mouse", str(dx), str(dy), str(buttons))
                    ok = rc == 0
                    detail.append(f"mouse {dx},{dy} rc={rc} {text[:120]}")
                elif "media" in step:
                    command, *rest = step["media"]
                    args = [command]
                    if rest:
                        path = fixtures / rest[0]
                        path.write_bytes(FIXTURES[rest[0]]())
                        args.append(str(path))
                    rc, value, text = host.cli(*args)
                    ok = rc == 0
                    if ok and rest:
                        good, note = wait_units(host, value, step.get("covers", []))
                        ok = good
                        detail.append(note)
                    detail.append(f"{' '.join(args[:1] + [Path(a).name for a in args[1:]])} rc={rc} {text[:160]}")
                else:
                    args = [a.format(**variables) for a in step["cli"]]
                    rc, value, text = host.cli(*args)
                    ok = rc == 0
                    for var, field in step.get("bind", {}).items():
                        variables[var] = str(value.get(field, ""))
                        ok = ok and bool(variables[var])
                    if ok and expect.get("units"):
                        good, note = wait_units(host, value, step.get("covers", []))
                        ok = good
                        detail.append(note)
                    if "shelf_kept" in expect:
                        game = expect["shelf_kept"].format(**variables)
                        disks = {d.get("game_id"): d for d in value.get("disks", []) if isinstance(d, dict)}
                        kept = (disks.get(game) or {}).get("kept_revision", "")
                        ok = ok and bool(kept)
                        detail.append(f"shelf {game} kept_revision={kept or 'none'}")
                    detail.append(f"{' '.join(args)} rc={rc} {text[:160]}")
                time.sleep(step.get("settle", 1))
                after = out / f"f{index:02d}-{name}.png"
                grabbed = grab(video, after)
                if "idle_max" in expect:
                    ok = ok and idle_delta is not None and idle_delta <= expect["idle_max"]
                    detail.append(f"idle_delta={idle_delta}")
                if "change" in expect:
                    delta = frame_delta(rgb(before), rgb(after)) if grabbed else None
                    ok = ok and delta is not None and delta >= expect["change"]
                    detail.append(f"delta={delta}")
                if "ocr" in expect:
                    text = ocr(after) if grabbed else ""
                    (out / f"f{index:02d}-{name}.txt").write_text(text)
                    found = normalise(expect["ocr"]) in normalise(text)
                    ok = ok and found
                    detail.append(f"ocr {expect['ocr']!r} {'found' if found else 'missing'}")
            finally:
                if recorder_started:
                    try:
                        rec.stop()
                    except (AudioError, OSError, ValueError, wave.Error) as exc:
                        ok = False
                        cleanup_incomplete = bool(getattr(exc, "audio_cleanup_incomplete", False))
                        detail.append(f"audio capture failed: {exc}")
                        if cleanup_incomplete:
                            detail.append("audio cleanup incomplete; aborting remaining steps")
                    else:
                        try:
                            good, note = judge_audio(audio_windows(rec.path), expect["audio"])
                            ok = ok and good
                            detail.append(note)
                        except (AudioError, OSError, ValueError, wave.Error) as exc:
                            ok = False
                            detail.append(f"audio capture failed: {exc}")
        if not cleanup_incomplete:
            previous = after if grabbed else previous
        if step.get("name"):
            check(name, ok, "; ".join(detail))
        elif not ok:
            check(name, False, "; ".join(detail))
        if cleanup_incomplete:
            break
    report["verdict"] = "PASS" if checks and all(c["ok"] for c in checks.values()) else "FAIL"
    return report


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--core", required=True)
    parser.add_argument("--package", type=Path, required=True)
    parser.add_argument("--api", required=True)
    parser.add_argument("--fogcast", required=True)
    parser.add_argument("--video", required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--spec", type=Path, default=SPEC)
    parser.add_argument("--package-id", default="", help="installed package ID, for {package_id} in cli steps")
    parser.add_argument("--kit", help="kit whose HDMI audio capture the audio steps record (D25)")
    args = parser.parse_args(argv)
    entry = load_spec(args.spec)["cores"].get(args.core)
    if not entry:
        print(json.dumps({"core": args.core, "verdict": "NO_STEPS"}))
        return 3
    report = run(args.core, entry["steps"], package_interfaces(args.package),
                 Host(args.api, args.fogcast, args.out), args.video, args.out, kit=args.kit,
                 package_id=args.package_id, not_applicable=entry.get("not_applicable"))
    text = json.dumps(report, indent=2)
    (args.out / "conformance.json").write_text(text + "\n")
    print(text)
    return 0 if report["verdict"] == "PASS" else 1


if __name__ == "__main__":
    sys.exit(main())
