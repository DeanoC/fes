#!/usr/bin/env python3
"""Local artifact preparation/analysis only: no device, network or kit access.

Requires Python stdlib and ffmpeg. compare deliberately requires native 1280x720
captures so scaling does not turn alternating HDMI scanlines into an average.
"""
import argparse
import array
import hashlib
import importlib.util
import json
import math
from pathlib import Path
import statistics
import subprocess
import sys

W, H = 1280, 720
SN_HZ = 3579545 / (32 * 516)
AY_HZ = 1789772.5 / (16 * 254)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def prepare(source, output):
    diagnostics = source / "cores/fes-coleco/diagnostic"
    sys.path.insert(0, str(diagnostics))
    spec = importlib.util.spec_from_file_location("generate", diagnostics / "generate.py")
    generate = importlib.util.module_from_spec(spec)
    sys.modules["generate"] = generate
    spec.loader.exec_module(generate)
    spec = importlib.util.spec_from_file_location("sgm_probe", diagnostics / "sgm_probe.py")
    sgm = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(sgm)
    output.mkdir(parents=True, exist_ok=True)
    golden = generate.preview()
    header = b"P6\n1280 720\n255\n"
    assert golden.startswith(header)
    pixels = bytearray(golden[len(header):])
    for y in range(1, H, 2):
        start = y * W * 3
        pixels[start:start + W * 3] = bytes(v >> 1 for v in pixels[start:start + W * 3])
    files = {
        "graphics.rom": generate.cartridge(),
        "sgm-probe.rom": sgm.cartridge(),
        "direct.ppm": golden,
        "scanlines.ppm": header + pixels,
        "LICENSE": (diagnostics / "LICENSE").read_bytes(),
    }
    for name, data in files.items():
        (output / name).write_bytes(data)
    manifest = {
        "source": str(diagnostics.resolve()),
        "source_sha256": {name: digest((diagnostics / name).read_bytes())
                          for name in ("generate.py", "sgm_probe.py")},
        "files": {name: {"size": len(data), "sha256": digest(data)} for name, data in files.items()},
        "geometry": {"width": W, "height": H, "image_box_exclusive": [384, 168, 896, 552]},
        "scanlines": {"bright_y_parity": 0, "dim_y_parity": 1, "operation": "each RGB channel >> 1"},
        "sgm_tones_nominal_hz": {"SN": SN_HZ, "AY": AY_HZ},
    }
    (output / "prepared.json").write_text(json.dumps(manifest, indent=2) + "\n")
    return manifest


def image_bytes(path):
    metadata = subprocess.check_output([
        "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries",
        "stream=width,height", "-of", "json", str(path)], text=True)
    stream = json.loads(metadata)["streams"][0]
    if (stream["width"], stream["height"]) != (W, H):
        raise ValueError(f"{path}: expected native {W}x{H}, got {stream}; retain an unscaled capture")
    pixels = subprocess.check_output([
        "ffmpeg", "-hide_banner", "-loglevel", "error", "-i", str(path),
        "-frames:v", "1", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1"])
    if len(pixels) != W * H * 3:
        raise ValueError("unexpected decoded frame size")
    return pixels


def region_means(pixels, box, parity=None):
    x0, y0, x1, y1 = box
    sums = [0, 0, 0]
    count = 0
    for y in range(y0, y1):
        if parity is not None and y % 2 != parity:
            continue
        for x in range(x0, x1):
            i = 3 * (y * W + x)
            for channel in range(3):
                sums[channel] += pixels[i + channel]
            count += 1
    return [v / count for v in sums]


def bbox(pixels, black):
    x0, y0, x1, y1 = W, H, -1, -1
    for y in range(H):
        for x in range(W):
            i = (y * W + x) * 3
            # Green outline, including dim green lines, safely exceeds black.
            r, g, b = pixels[i:i+3]
            if g - black[1] > 30 and g > r + 15 and g > b + 15:
                x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
    return [x0, y0, x1 + 1, y1 + 1] if x1 >= 0 else None


def compare(direct, scanlines):
    captures = {"direct": image_bytes(direct), "scanlines": image_bytes(scanlines)}
    black_box = (16, 16, 256, 144)
    # Safely inside the green left border for hundreds of consecutive rows.
    green_box = (390, 176, 398, 544)
    # Safely inside a static red checkerboard square, avoiding chroma edges.
    red_box = (420, 188, 428, 196)
    result = {"native_capture_size": [W, H], "expected_image_box_exclusive": [384, 168, 896, 552],
              "files": {name: {"path": str(path.resolve()), "sha256": digest(path.read_bytes())}
                        for name, path in (("direct", direct), ("scanlines", scanlines))},
              "captures": {}, "regions": {"black": black_box, "green": green_box, "red": red_box}}
    for name, pixels in captures.items():
        black = region_means(pixels, black_box)
        regions = {}
        for color, box in (("green", green_box), ("red", red_box)):
            means = [region_means(pixels, box, parity) for parity in (0, 1)]
            regions[color] = {"even_rgb": means[0], "odd_rgb": means[1],
                              "black_subtracted_odd_over_even": [
                                  (means[1][c] - black[c]) / (means[0][c] - black[c])
                                  if abs(means[0][c] - black[c]) > 5 else None for c in range(3)]}
        result["captures"][name] = {"black_rgb": black, "green_bbox_exclusive": bbox(pixels, black), **regions}
    result["expected"] = {
        "direct_green_rgb": [33, 200, 66], "dim_green_rgb": [16, 100, 33],
        "direct_red_rgb": [212, 82, 77], "dim_red_rgb": [106, 41, 38],
        "direct_odd_over_even": 1.0, "scanline_odd_over_even": 0.5,
        "note": "Capture color/range conversion may change absolute RGB; use black-subtracted ratios. JPEG, chroma filtering and scaling limit precision.",
    }
    # Even rows should remain unchanged between separately loaded compositions.
    # Physical capture tolerances are reported, not asserted as FPGA bus equality.
    for color in ("green", "red"):
        d = result["captures"]["direct"][color]
        s = result["captures"]["scanlines"][color]
        result.setdefault("comparison", {})[color] = {
            "even_rgb_delta": [s["even_rgb"][c] - d["even_rgb"][c] for c in range(3)],
            "odd_rgb_delta": [s["odd_rgb"][c] - d["odd_rgb"][c] for c in range(3)],
        }
    for parity in (0, 1):
        sums = [0.0, 0.0, 0.0]
        counts = 0
        for y in range(168 + parity, 552, 2):
            for x in range(384, 896):
                i = (y * W + x) * 3
                for channel in range(3):
                    d = captures["direct"][i + channel]
                    s = captures["scanlines"][i + channel]
                    if parity:
                        db = result["captures"]["direct"]["black_rgb"][channel]
                        sb = result["captures"]["scanlines"]["black_rgb"][channel]
                        prediction = (d - db) / 2 + sb
                    else:
                        prediction = d
                    sums[channel] += abs(s - prediction)
                counts += 1
        result["comparison"]["image_" + ("odd" if parity else "even") + "_model_mae_rgb"] = [v / counts for v in sums]
    return result


def goertzel(values, rate, frequency):
    coefficient = 2 * math.cos(2 * math.pi * frequency / rate)
    previous = earlier = 0.0
    for value in values:
        current = value + coefficient * previous - earlier
        earlier, previous = previous, current
    power = max(0, earlier * earlier + previous * previous - coefficient * earlier * previous)
    return 2 * math.sqrt(power) / len(values)


def audio(path, start_seconds=0.5, duration_seconds=3):
    raw = subprocess.check_output([
        "ffmpeg", "-hide_banner", "-loglevel", "error", "-i", str(path), "-ss", str(start_seconds),
        "-t", str(duration_seconds), "-ar", "48000", "-ac", "2", "-f", "s16le", "pipe:1"])
    samples = array.array("h", raw)
    if sys.byteorder != "little":
        samples.byteswap()
    if len(samples) < 48000:
        raise ValueError("recording too short: capture at least one second, preferably five")
    result = {"path": str(path.resolve()), "sha256": digest(path.read_bytes()),
              "analysis_window_seconds": {"start": start_seconds, "duration": len(samples) / 96000}, "channels": []}
    for channel in range(2):
        values = samples[channel::2]
        dc = statistics.mean(values)
        centered = [v - dc for v in values]
        decimated = centered[::6]
        tones = {}
        for name, nominal in (("SN", SN_HZ), ("AY", AY_HZ)):
            candidates = [(nominal + offset / 20, goertzel(decimated, 8000, nominal + offset / 20))
                          for offset in range(-60, 61)]
            frequency, amplitude = max(candidates, key=lambda pair: pair[1])
            tones[name] = {"nominal_hz": nominal, "peak_near_nominal_hz": frequency,
                           "fundamental_peak_amplitude_s16": amplitude,
                           "nominal_bin_amplitude_s16": goertzel(decimated, 8000, nominal)}
        result["channels"].append({"dc_s16": dc, "ac_rms_s16": math.sqrt(statistics.mean(v*v for v in centered)),
                                   "min_s16": min(values), "max_s16": max(values), "tones": tones})
    result["note"] = "ASUS capture filters/rescales PCM. Peak levels and DC are observed capture values, not bit-exact FPGA samples; basic graphics ROM should be silent, SGM probe should have both peaks."
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    p = commands.add_parser("prepare")
    p.add_argument("--source", type=Path, required=True)
    p.add_argument("--output", type=Path, required=True)
    p = commands.add_parser("compare")
    p.add_argument("direct", type=Path)
    p.add_argument("scanlines", type=Path)
    p.add_argument("--out", type=Path)
    p = commands.add_parser("audio")
    p.add_argument("input", type=Path)
    p.add_argument("--out", type=Path)
    args = parser.parse_args()
    if args.command == "prepare":
        result = prepare(args.source, args.output)
    elif args.command == "compare":
        result = compare(args.direct, args.scanlines)
    else:
        result = audio(args.input)
    encoded = json.dumps(result, indent=2) + "\n"
    if getattr(args, "out", None):
        args.out.write_text(encoded)
    print(encoded, end="")


if __name__ == "__main__":
    main()
