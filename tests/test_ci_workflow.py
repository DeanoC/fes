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
from types import SimpleNamespace
from unittest import mock

from scripts.affected import plan
from scripts.ci_simulations import simulation_matrix, tier_for_event


def workflow_events(path):
    lines = path.read_text().splitlines()
    start = lines.index('on:')
    events = set()
    for line in lines[start + 1:]:
        if line and not line.startswith((' ', '\t')):
            break
        match = re.fullmatch(r'  ([A-Za-z_][A-Za-z0-9_-]*):(?:\s*#.*)?', line)
        if match:
            events.add(match.group(1))
    return events


class ParentWorkflowTests(unittest.TestCase):
    def test_simulation_tier_docs_match_workflow_events_and_matrix(self):
        repository = Path(__file__).resolve().parents[1]
        smoke = simulation_matrix(['coleco'], 'smoke')['include']
        full = simulation_matrix(['coleco'], 'full')['include']
        check_workflow = repository / '.github/workflows/check.yml'
        nightly_workflow = repository / '.github/workflows/nightly.yml'
        check_events = workflow_events(check_workflow)
        nightly_events = workflow_events(nightly_workflow)
        self.assertEqual(check_events, {
            'push', 'pull_request', 'merge_group', 'workflow_dispatch', 'workflow_call',
        })
        self.assertEqual(nightly_events, {'schedule', 'workflow_dispatch'})
        nightly = nightly_workflow.read_text()
        self.assertIn('uses: ./.github/workflows/check.yml', nightly)

        # workflow_call enables reuse, but github.event_name remains the event
        # that called this workflow (schedule or workflow_dispatch).
        effective_events = (check_events - {'workflow_call'}) | nightly_events
        self.assertEqual({event: tier_for_event(event) for event in effective_events}, {
            'pull_request': 'smoke', 'merge_group': 'smoke', 'push': 'smoke',
            'schedule': 'full', 'workflow_dispatch': 'full',
        })

        readme = ' '.join((repository / 'README.md').read_text().split())
        changed = ' '.join((repository / 'docs/test-changed.md').read_text().split())
        development = ' '.join((repository / 'docs/development.md').read_text().split())
        self.assertIn('nightly and manual runs cover the complete suite', readme)
        self.assertIn('nightly and manual runs cover the complete suite', changed)
        self.assertIn(f"Coleco's full tier has {len(full)} jobs", changed)
        self.assertIn(f'smoke tier runs {len(smoke)} of those jobs', changed)
        self.assertIn('Outside Coleco, the smoke matrix reduces coverage for ZX81, C64, Atari ST, Spectrum, PC/XT and PC/386', changed)
        self.assertIn(f"Coleco's full simulation tier runs {len(full)} jobs", development)
        self.assertIn(f'Its smoke tier runs {len(smoke)} jobs', development)
        self.assertNotIn('16 focused simulation jobs', development)
        self.assertNotIn('weekly/manual', (repository / 'scripts/test_policy.py').read_text())
        self.assertNotIn('weekly/manual', (repository / 'scripts/host_tests.py').read_text())

    def test_pcxt_system_delay_docs_match_simulator(self):
        repository = Path(__file__).resolve().parents[1]
        readme = ' '.join((repository / 'sources/misteross/cores/fes-pcxt/README.md').read_text().split())
        simulator = (repository / 'sources/misteross/scripts/sim_fes_pcxt.py').read_text()
        delay_selection = re.search(r"\(2,\) if suite in \(([^)]*)\)", ' '.join(simulator.split()))
        self.assertIsNotNone(delay_selection)
        for suite in ('system', 'system-mda', 'system-board50', 'system-board50-mda'):
            self.assertIn(repr(suite), delay_selection.group(1))
        self.assertIn('the strictest SDRAM mask delay (2)', readme)
        self.assertIn('each runs at DQM delay 2', readme)

    def test_pc386_image_simulations_install_mtools_on_their_runner(self):
        workflow = (Path(__file__).resolve().parents[1] / '.github/workflows/check.yml').read_text()
        step = workflow.split('      - name: Install PC/XT and PC386 image tools\n', 1)[1].split(
            '      - name: Simulate selected core scenarios\n', 1)[0]
        condition = ' '.join(step.split('if: >-', 1)[1].split('run:', 1)[0].split())
        self.assertEqual(condition, "matrix.target == 'sim-fes-pcxt-dos' || matrix.target == 'sim-fes-pc386-fesenv' "
                                    "|| matrix.target == 'sim-fes-pc386-a20' || matrix.target == 'sim-fes-pc386-c2xx' "
                                    "|| matrix.target == 'sim-fes-pc386-wait' || matrix.target == 'sim-fes-pc386-scroll' "
                                    "|| matrix.target == 'sim-fes-pc386-speaker' || "
                                    "matrix.target == 'sim-fes-pc386-cpu-profile'")
        self.assertIn('sudo apt-get update && sudo apt-get install -y mtools', step)

    def test_pcxt_fixture_test_has_image_tools(self):
        workflow = (Path(__file__).resolve().parents[1] / '.github/workflows/check.yml').read_text()
        producer = workflow.split('  fpga-producer:\n', 1)[1].split('  simulation-tools:\n', 1)[0]
        self.assertIn('sudo apt-get install -y binutils mtools', producer)

    def test_pcxt_com_nightly_is_main_only_and_reports_failure(self):
        repository = Path(__file__).resolve().parents[1]
        nightly = (repository / '.github/workflows/nightly.yml').read_text()
        job = nightly.split('  pcxt-com:\n', 1)[1].split('  report:\n', 1)[0]
        report = nightly.split('  report:\n', 1)[1]
        self.assertEqual(workflow_events(repository / '.github/workflows/nightly.yml'),
                         {'schedule', 'workflow_dispatch'})
        for condition in ("github.repository == 'DeanoC/fes'", "github.ref == 'refs/heads/main'",
                          "github.event_name == 'schedule'", "github.event_name == 'workflow_dispatch'"):
            self.assertIn(condition, job)
        self.assertIn('environment: pcxt-nightly-main', job)
        self.assertIn('runs-on: [self-hosted, linux, x64, chip-3090]', job)
        self.assertIn('persist-credentials: false', job)
        self.assertIn('ref: ${{ github.sha }}', job)
        self.assertIn('test "$(git rev-parse HEAD)" = "$GITHUB_SHA"', job)
        self.assertIn('timeout --signal=TERM --kill-after=30s 11700s', job)
        self.assertIn('test -s sources/misteross/build/sim/fes-pcxt/dos-com/result.json', job)
        self.assertIn('dos-com/result-pending.json', job)
        self.assertIn('name: pcxt-com-evidence-', job)
        self.assertNotIn('sudo ', job)
        self.assertIn('needs: [regression, pcxt-com]', report)
        self.assertIn("needs.regression.result == 'failure' || needs.pcxt-com.result == 'failure'", report)
        for tier in ('smoke', 'full'):
            self.assertNotIn('sim-fes-pcxt-com',
                             [row['target'] for row in simulation_matrix(['pcxt'], tier)['include']])

    def test_pcxt_com_preflight_and_cleanup_are_scoped(self):
        nightly = (Path(__file__).resolve().parents[1] / '.github/workflows/nightly.yml').read_text()
        job = nightly.split('  pcxt-com:\n', 1)[1].split('  report:\n', 1)[0]
        def shell_step(name):
            step = job.split('      - name: ' + name + '\n', 1)[1].split('\n      - ', 1)[0]
            return textwrap.dedent(step.split('        run: |\n', 1)[1])
        setup = shell_step('Create isolated directories and check free space')
        collect = shell_step('Collect COM diagnostics')
        cleanup = shell_step("Remove only this job's directories")
        disk_code = re.search(r"python3 -c '\n(.*?)\n'", setup, re.S).group(1)
        for free in (30 * 1024**3 - 1, 30 * 1024**3):
            with self.subTest(free=free), mock.patch.dict(os.environ, {
                'GITHUB_WORKSPACE': '/workspace', 'RUNNER_TEMP': '/scratch',
            }), mock.patch('shutil.disk_usage', return_value=SimpleNamespace(free=free)):
                if free < 30 * 1024**3:
                    with self.assertRaises(SystemExit):
                        exec(disk_code, {})
                else:
                    exec(disk_code, {})
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            workspace, scratch, bin_dir = (root / name for name in ('workspace', 'scratch', 'bin'))
            for path in (workspace, scratch, bin_dir): path.mkdir()
            (bin_dir / 'python3').write_text('#!/bin/sh\nexit 0\n')
            (bin_dir / 'python3').chmod(0o755)
            output = root / 'output'
            output.touch()
            env = {**os.environ, 'PATH': str(bin_dir) + os.pathsep + os.environ['PATH'],
                   'GITHUB_WORKSPACE': str(workspace), 'RUNNER_TEMP': str(scratch),
                   'GITHUB_OUTPUT': str(output), 'GITHUB_RUN_ID': '123',
                   'GITHUB_RUN_ATTEMPT': '1', 'GITHUB_REF': 'refs/heads/other'}
            self.assertNotEqual(subprocess.run(['bash', '-c', setup], env=env).returncode, 0)
            self.assertEqual(list(workspace.iterdir()), [])
            env['GITHUB_REF'] = 'refs/heads/main'
            subprocess.run(['bash', '-c', setup], env=env, check=True)
            paths = dict(line.split('=', 1) for line in output.read_text().splitlines())
            result_dir = Path(paths['workspace']) / 'sources/misteross/build/sim/fes-pcxt/dos-com'
            result_dir.mkdir(parents=True)
            for name in ('inputs.json', 'result.json'):
                (result_dir / name).write_text(name)
            (Path(paths['private']) / 'simulation.log').write_text('simulation')
            env.update(WORKSPACE=paths['workspace'], PRIVATE=paths['private'])
            subprocess.run(['bash', '-c', collect], env=env, check=True)
            evidence = Path(paths['private']) / 'evidence'
            self.assertEqual({path.name for path in evidence.iterdir()},
                             {'simulation.log', 'inputs.json', 'result.json'})
            sentinel = root / 'unrelated'
            sentinel.mkdir()
            env.update(WORKSPACE=str(sentinel), PRIVATE=paths['private'])
            self.assertNotEqual(subprocess.run(['bash', '-c', cleanup], env=env).returncode, 0)
            self.assertTrue(sentinel.exists())
            env['WORKSPACE'] = paths['workspace']
            subprocess.run(['bash', '-c', cleanup], env=env, check=True)
            self.assertFalse(Path(paths['workspace']).exists())
            self.assertFalse(Path(paths['private']).exists())
            self.assertTrue(sentinel.exists())

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

    def test_parent_capture_tests_have_ffmpeg(self):
        workflow = (Path(__file__).resolve().parents[1] / '.github/workflows/check.yml').read_text()
        parent = workflow.split('  parent-tests:\n', 1)[1].split('  consistency:\n', 1)[0]
        self.assertIn('sudo apt-get update && sudo apt-get install -y ffmpeg', parent)
        self.assertLess(parent.index('sudo apt-get install -y ffmpeg'),
                        parent.index('python3 scripts/parent_tests.py'))

    def test_parent_media_container_provisioning_is_bounded(self):
        repository = Path(__file__).resolve().parents[1]
        workflow = (repository / '.github/workflows/check.yml').read_text()
        parent = workflow.split('  parent-tests:\n', 1)[1].split('  consistency:\n', 1)[0]
        self.assertRegex(parent, r'(?m)^    timeout-minutes: 45$')
        self.assertIn('timeout --signal=TERM --kill-after=30s 600s make media-test-provision', parent)

        for name in ('containers/boot-media/Dockerfile',
                     'image/containers/target-image/Dockerfile'):
            dockerfile = (repository / name).read_text()
            for setting in ('Acquire::http::Timeout "30";',
                            'Acquire::https::Timeout "30";',
                            'Acquire::Retries "5";'):
                with self.subTest(dockerfile=name, setting=setting):
                    self.assertIn(setting, dockerfile)
