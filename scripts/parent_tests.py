#!/usr/bin/env python3
"""Run the parent Python regression suite with routine/extended mode filtering.

The default keeps every class; routine callers pass explicit false flags,
which omit the full-device video producer/publication classes and the
real-media container drivers while every lightweight class in the same
modules still runs. Disabled modes are reported, not silently skipped; media
execution requires a working Docker up front.
"""
import argparse
import contextlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import unittest

# (module basename, class name) keys for suites omitted unless their inputs
# changed or the run is full. Sibling classes in the same files stay routine.
VIDEO_CLASSES = frozenset({
    ('test_factory_video_parts', 'RealProducerEvidenceTests'),
    ('test_factory_video_parts', 'NativeProducerEvidenceTests'),
    ('test_factory_video_parts', 'STProducerEvidenceTests'),
    ('test_factory_video_publication', 'FactoryVideoPublicationTests'),
    ('test_factory_video_publication', 'RasterFactoryVideoPublicationTests'),
})
MEDIA_CLASSES = frozenset({
    ('test_media_image', 'ContainerImageTests'),
    ('test_media_image', 'RealImageTests'),
    ('test_appliance', 'ContainerTests'),
    ('test_appliance', 'RealBootstrapTests'),
    ('test_appliance_media', 'ContainerTests'),
    ('test_appliance_media', 'RealCardTests'),
})


def filter_suite(suite, video=True, media=True):
    """Return (filtered suite, selected ids, omitted ids) before any setUpClass."""
    omitted_keys = set()
    if not video:
        omitted_keys |= VIDEO_CLASSES
    if not media:
        omitted_keys |= MEDIA_CLASSES
    selected_ids, omitted_ids = [], []

    def walk(source):
        kept = unittest.TestSuite()
        for entry in source:
            if isinstance(entry, unittest.TestSuite):
                kept.addTest(walk(entry))
            else:
                key = (entry.__class__.__module__.rsplit('.', 1)[-1],
                       entry.__class__.__name__)
                if key in omitted_keys:
                    omitted_ids.append(entry.id())
                else:
                    selected_ids.append(entry.id())
                    kept.addTest(entry)
        return kept

    return walk(suite), selected_ids, omitted_ids


def docker_available():
    if shutil.which('docker') is None:
        return False
    return subprocess.run(['docker', 'info'], capture_output=True).returncode == 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--video', choices=('true', 'false'), default='true')
    parser.add_argument('--media', choices=('true', 'false'), default='true')
    parser.add_argument('--plan-only', action='store_true')
    args = parser.parse_args(argv)
    video = args.video == 'true'
    media = args.media == 'true'
    root = Path(__file__).resolve().parents[1]
    sys.path.insert(0, str(root))
    suite = unittest.defaultTestLoader.discover(str(root / 'tests'))
    selected, selected_ids, omitted_ids = filter_suite(suite, video=video, media=media)
    report = {'format': 1, 'video': video, 'media': media,
              'selected': selected_ids, 'omitted': omitted_ids}
    if args.plan_only:
        report['status'] = 'planned'
        print(json.dumps(report, indent=2))
        return 0
    if media and not docker_available():
        report['status'] = 'error'
        report['error'] = 'media tests require a working docker; refusing to skip silently'
        print(json.dumps(report, indent=2))
        return 2
    with contextlib.redirect_stdout(sys.stderr):
        result = unittest.TextTestRunner(verbosity=2).run(selected)
    report['status'] = 'passed' if result.wasSuccessful() else 'failed'
    print(json.dumps(report, indent=2))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
