#!/usr/bin/env python3
"""Offline replay of the experimental V4L2 fanout; synthetic timestamps only."""
import argparse
from dataclasses import asdict
import hashlib
import io
import json
from pathlib import Path
import subprocess
import sys
import time

if __package__:
    from . import pcxt_capture_fanout as capture
else:
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    import pcxt_capture_fanout as capture

FRAME_SIZE = 1920*1080*2


class PacketStream:
    """Present bounded decoded video frames in the acquisition wire format."""
    def __init__(self, proc, count, interval):
        self.proc, self.count, self.interval = proc, count, interval
        self.seq = 0
        self.pending = io.BytesIO()

    def read(self, size):
        data = self.pending.read(size)
        if data:
            return data
        if self.seq == self.count:
            return b''
        pixels = capture.read_exact(self.proc.stdout, FRAME_SIZE)
        # Replay timestamps describe this host's simulated delivery only.
        now = time.monotonic_ns()
        header = capture.HEADER.pack(capture.MAGIC, self.seq, now, now,
                                     capture.V4L2_TIMESTAMP_MONOTONIC, len(pixels))
        self.seq += 1
        self.pending = io.BytesIO(header+pixels)
        time.sleep(self.interval)
        return self.pending.read(size)


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--video', type=Path, required=True)
    ap.add_argument('--output', type=Path, required=True, help='new private output directory')
    ap.add_argument('--frames', type=int, default=30)
    ap.add_argument('--offset', type=float, default=43.0)
    args = ap.parse_args()
    if not 1 <= args.frames <= 300 or args.offset < 0:
        ap.error('invalid frame count or offset')
    args.output.mkdir(mode=0o700, parents=False, exist_ok=False)
    before = digest(args.video)
    decoder = subprocess.Popen(['ffmpeg', '-nostdin', '-hide_banner', '-loglevel', 'error',
        '-ss', str(args.offset), '-i', str(args.video), '-frames:v', str(args.frames),
        '-pix_fmt', 'yuyv422', '-f', 'rawvideo', 'pipe:1'],
        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, bufsize=0)
    try:
        recorder = capture.FFV1Recorder(args.output)
    except BaseException:
        decoder.kill(); decoder.wait(timeout=5); decoder.stdout.close()
        raise
    samples = []
    try:
        stream = PacketStream(decoder, args.frames, .1)
        frames = capture.v4l2_frames(stream)
        def detect(frame):
            samples.append({'sequence':frame.sequence, 'raw_sha256':hashlib.sha256(frame.pixels).hexdigest()})
        result = capture.fanout_frames(frames, recorder.write, detect, sample_every=5,
            record_capacity=16, join_timeout=10, source_timeout=15, allow_source_eof=True,
            on_recorder_stall=recorder.interrupt)
        if stream.seq != args.frames or decoder.wait(timeout=15) != 0:
            raise capture.CaptureError('replay source did not finish as expected')
        if result.frames_recorded != args.frames:
            raise capture.CaptureError('replay did not record every source frame')
        recorder.finish()
    except BaseException as exc:
        if not getattr(exc, 'recorder_worker_running', False):
            recorder.abort()
        raise
    finally:
        if decoder.poll() is None:
            decoder.kill(); decoder.wait(timeout=5)
        decoder.stdout.close()
    if digest(args.video) != before:
        raise capture.CaptureError('source video changed during replay')
    summary = {'scope':'offline transport replay; synthetic acquisition times; no kit or gameplay acceptance',
               'source_sha256':before, 'source_offset_seconds':args.offset,
               'source_frames':args.frames, 'simulated_frame_period_seconds':.1,
               'fanout':asdict(result), 'detector_samples':samples,
               'limitations':['Timestamps and sequence are synthesized by the replay.',
                              'This test is not a live camera, target or input-latency measurement.']}
    (args.output/'replay.json').write_text(json.dumps(summary, indent=2)+'\n')
    print(json.dumps(summary, indent=2))


if __name__ == '__main__':
    main()
