#!/usr/bin/env python3
"""Run parent regressions with component groups and routine/extended modes.

Without changed paths every group runs. Reviewed component and parent paths select
integration groups; shared/unknown inputs retain all groups. Routine callers pass false flags,
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

try:
    from scripts import parent_affected
except ImportError:
    import parent_affected

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


def filter_suite(suite, video=True, media=True, modules=None):
    """Return (filtered suite, selected ids, omitted ids) before any setUpClass."""
    omitted_keys = set()
    if not video:
        omitted_keys |= VIDEO_CLASSES
    if not media:
        omitted_keys |= MEDIA_CLASSES
    selected_ids, omitted_ids = [], []
    assigned = set().union(*parent_affected.GROUPS.values())

    def walk(source):
        kept = unittest.TestSuite()
        for entry in source:
            if isinstance(entry, unittest.TestSuite):
                kept.addTest(walk(entry))
            else:
                key = (entry.__class__.__module__.rsplit('.', 1)[-1],
                       entry.__class__.__name__)
                if key in omitted_keys or (modules is not None and key[0] in assigned
                                            and key[0] not in modules):
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
    parser.add_argument('--paths-json', help='changed repository paths; omitted means all groups')
    parser.add_argument('--full', action='store_true', help='run all groups and extended modes')
    args = parser.parse_args(argv)
    paths = None
    if args.paths_json is not None:
        try:
            paths = json.loads(args.paths_json)
        except ValueError:
            parser.error('--paths-json must be a JSON array of strings')
        if not isinstance(paths, list) or not all(isinstance(p, str) for p in paths):
            parser.error('--paths-json must be a JSON array of strings')
    video = args.full or args.video == 'true'
    media = args.full or args.media == 'true'
    root = Path(__file__).resolve().parents[1]
    sys.path.insert(0, str(root))
    suite = unittest.defaultTestLoader.discover(str(root / 'tests'))
    groups = None if args.full else parent_affected.select_groups(paths, video=video, media=media, root=root)
    modules = parent_affected.selected_modules(
        groups, [path.stem for path in (root / 'tests').glob('test_*.py')], paths)
    selected, selected_ids, omitted_ids = filter_suite(
        suite, video=video, media=media, modules=modules if groups is not None else None)
    report = {'format': 1, 'video': video, 'media': media,
              'selected': selected_ids, 'omitted': omitted_ids,
              'groups': sorted(groups) if groups is not None else 'all',
              'documentation_modules': sorted(parent_affected.DOCUMENTATION_MODULES)
                  if paths and any(parent_affected.documentation(path) for path in paths) else [],
              'selection_reason': ('reviewed input integration groups' if groups is not None else
                                   'full run, shared/unknown inputs or no path list')}
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
