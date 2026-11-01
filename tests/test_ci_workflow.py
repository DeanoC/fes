"""Exercise the workflow's parent shell with representative affected plans."""
import os
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import textwrap
import unittest

from scripts.affected import plan


class ParentWorkflowTests(unittest.TestCase):
    def test_pc386_fesenv_simulation_installs_mtools_on_its_runner(self):
        workflow = (Path(__file__).resolve().parents[1] / '.github/workflows/check.yml').read_text()
        step = workflow.split('      - name: Install PC/XT and PC386 image tools\n', 1)[1].split(
            '      - name: Simulate selected core scenarios\n', 1)[0]
        self.assertIn("if: matrix.target == 'sim-fes-pcxt-dos' || matrix.target == 'sim-fes-pc386-fesenv'", step)
        self.assertIn('sudo apt-get update && sudo apt-get install -y mtools', step)

    def test_host_workflow_delegates_build_and_ui_selection_and_keeps_test_shards(self):
        workflow = (Path(__file__).resolve().parents[1] / '.github/workflows/check.yml').read_text()
        host = workflow.split('  host:\n', 1)[1].split('  runtime:\n', 1)[0]
        self.assertIn('if: matrix.shard == 3\n        run: sudo apt-get update && sudo apt-get install -y ripgrep', host)
        script = textwrap.dedent(re.search(r'        run: \|\n(.*)', host, re.S).group(1))
        for full in (False, True):
            for shard in range(1, 4):
                with self.subTest(full=full, shard=shard), tempfile.TemporaryDirectory() as temporary:
                    root = Path(temporary)
                    for command in ('make', 'python3'):
                        stub = root / command
                        stub.write_text('#!/bin/sh\nprintf "%s\\n" "' + command +
                                        ' $*" >> "$COMMAND_LOG"\n')
                        stub.chmod(0o755)
                    paths = '["sources/FogCast/ui/tenfoot/library_retry_test.go"]'
                    subprocess.run(['bash', '-e', '-c', script], check=True, env={**os.environ,
                        'PATH': str(root) + os.pathsep + os.environ['PATH'],
                        'COMMAND_LOG': str(root / 'commands'), 'SHARD': str(shard),
                        'CHANGED_PATHS': paths, 'TEST_FULL_RACE': str(full).lower()})
                    commands = (root / 'commands').read_text().splitlines()
                    runner = next(c for c in commands if c.startswith('python3 scripts/host_tests.py '))
                    self.assertIn('--build-host-if-needed', runner)
                    self.assertIn('--ui-if-needed', runner)
                    self.assertIn(f'--shard {shard}/3', runner)
                    self.assertIn('--paths-json ' + paths, runner)
                    self.assertEqual('--full' in runner, full)
                    self.assertNotIn('make host', commands)
                    self.assertFalse(any(c.startswith('make ') for c in commands))

    def test_deadcode_step_forwards_paths_to_real_selector(self):
        repository = Path(__file__).resolve().parents[1]
        workflow = (repository / '.github/workflows/check.yml').read_text()
        step = workflow.split('      - name: Unused Go code\n', 1)[1].split('  host:\n', 1)[0]
        self.assertIn('CHANGED_PATHS: ${{ needs.plan.outputs.paths }}', step)
        command = re.search(r'        run: (.+)', step).group(1)
        for paths, expected in ((['scripts/appliance.py'], False),
                                (['tests/test_receipt.py', 'docs/test-changed.md'], False),
                                (['platform/boot.go'], True),
                                (['<new-branch>'], True)):
            with self.subTest(paths=paths), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                # Exercise the actual workflow command/quoting and real selector,
                # adding only plan-only to avoid installing analysis tools.
                stub = root / 'python3'
                stub.write_text('#!' + sys.executable + '\nimport os, sys\n'
                                'os.execv(sys.executable, [sys.executable, *sys.argv[1:], "--plan-only"])\n')
                stub.chmod(0o755)
                result = subprocess.run(['bash', '-e', '-c', command], cwd=repository,
                    env={**os.environ, 'CHANGED_PATHS': json.dumps(paths),
                         'PATH': str(root) + os.pathsep + os.environ['PATH']},
                    capture_output=True, text=True, check=True)
                self.assertEqual(json.loads(result.stdout), {'selected': expected, 'paths': paths})

    def test_platform_and_image_commands_follow_media_inputs(self):
        workflow = (Path(__file__).resolve().parents[1] /
                    '.github/workflows/check.yml').read_text()
        parent = workflow.split('  parent-tests:\n', 1)[1].split('  consistency:\n', 1)[0]
        script = textwrap.dedent(re.search(r'        run: \|\n(.*)', parent, re.S).group(1))
        for path, media in (
            ('sources/FogCast/ui/rooms/examples/fes-cores/main.lua', False),
            ('sources/FogCast/internal/hostapi/ui_app.js', False),
            ('platform/internal/applianceboot/boot.go', True),
            ('<new-branch>', True),
        ):
            with self.subTest(path=path), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                for command in ('make', 'python3'):
                    stub = root / command
                    stub.write_text('#!/bin/sh\nprintf "%s\\n" "' + command +
                                    ' $*" >> "$COMMAND_LOG"\n')
                    stub.chmod(0o755)
                modes = plan([path])['test_modes']
                environment = {
                    **os.environ, 'CHANGED_PATHS': json.dumps([path]), 'PATH': str(root) + os.pathsep + os.environ['PATH'],
                    'COMMAND_LOG': str(root / 'commands'),
                    'TEST_MEDIA': str(modes['media']).lower(),
                    'TEST_VIDEO': str(modes['video']).lower(),
                }
                subprocess.run(['bash', '-e', '-c', script], env=environment, check=True)
                commands = (root / 'commands').read_text().splitlines()
                runner = next(c for c in commands if c.startswith('python3 scripts/parent_tests.py '))
                self.assertIn('--paths-json ' + json.dumps([path]), runner)
                self.assertEqual('make platform-provision' in commands, media)
                self.assertEqual('make media-test-provision' in commands, media)
                self.assertEqual('make platform-test' in commands, media)
                self.assertEqual(any(c.startswith('make -C image test ') for c in commands), media)
