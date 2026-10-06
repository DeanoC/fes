"""Tests for HIL classification, manifests, and evidence verification."""

import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'scripts/hil_plan.py'
FIXTURES = ROOT / 'tests/fixtures/hil_plan'
BASE_SHA = 'a' * 64
BOOT_ID = '12345678-1234-5678-1234-123456789012'
CORE_FIXTURES = ROOT / 'sources/misteross/tests/fixtures/core-bundle-v2'
sys.path.insert(0, str(ROOT / 'sources/misteross'))
from scripts.core_package import read_package


def core_archive(core_id):
    import tarfile
    manifest = (CORE_FIXTURES / 'manifests/valid-basic.toml').read_text()
    manifest = manifest.replace('id = "fes.pong"', f'id = "{core_id}"', 1).encode()
    payload = (CORE_FIXTURES / 'payloads/fes-fixture.rbf').read_bytes()
    def member(name, data):
        info = tarfile.TarInfo(name)
        info.mode = 0o644
        info.uid = info.gid = info.mtime = 0
        info.size = len(data)
        info.uname = info.gname = ''
        return (info.tobuf(format=tarfile.USTAR_FORMAT, encoding='ascii') + data +
                b'\0' * ((-len(data)) % 512))
    return member('manifest.toml', manifest) + member('core.rbf', payload) + b'\0' * 1024


def run(*args, cwd=None):
    return subprocess.run([sys.executable, str(SCRIPT), *map(str, args)],
                          cwd=cwd, text=True, capture_output=True)


def digest(data):
    return hashlib.sha256(data).hexdigest()


class HilPlanTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.repo), *args], text=True).strip()

    def git_repo(self, paths=None):
        if hasattr(self, 'repo') and self.repo.exists():
            shutil.rmtree(self.repo)
        self.repo = self.work / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Test')
        (self.repo / 'docs').mkdir()
        (self.repo / 'docs/base.md').write_text('base\n')
        self.git('add', '.')
        self.git('commit', '-m', 'base')
        self.base = self.git('rev-parse', 'HEAD')
        for path in (paths if paths is not None else ['sources/libmister-runtime/change.cpp']):
            target = self.repo / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text('overlay\n')
        if paths is None or paths:
            self.git('add', '.')
            self.git('commit', '-m', 'overlay')
        self.head = self.git('rev-parse', 'HEAD')
        return self.base, self.head

    def write_artifact(self, name, content=b'artifact'):
        path = self.work / name
        path.write_bytes(content)
        return path

    def make_manifest(self, entries, head=None):
        result = self.work / 'manifest.json'
        payload = []
        for component, side, target, content in entries:
            local = self.write_artifact(component.replace(':', '_'), content)
            row = {'component': component, 'side': side, 'target': target,
                            'local': str(local), 'sha256': digest(content),
                            'size': len(content)}
            if component.startswith('core:'):
                core_id = component.removeprefix('core:')
                content = core_archive(core_id)
                local.write_bytes(content)
                row.update(local=str(local.resolve()), sha256=digest(content), size=len(content),
                           package_id=None, core_id=core_id)
                # Derive immutable identity with the production reader.
                package = read_package(local)
                row['package_id'] = package.package_id
                row['core_id'] = package.fields['core']['id']
            payload.append(row)
        result.write_text(json.dumps({'head': head or self.head, 'entries': payload}, indent=2))
        return result

    def release_file(self, **updates):
        path = self.work / 'release.json'
        data = {'fes_revision': self.base, 'image_sha256': BASE_SHA, 'version': 'test'}
        data.update(updates)
        path.write_text(json.dumps(data))
        return path

    def update_file(self, **updates):
        path = self.work / 'update.json'
        data = {'boot_id': BOOT_ID, 'good': BASE_SHA, 'image_sha256': BASE_SHA,
                'corrupt': False, 'trial': False}
        data.update(updates)
        path.write_text(json.dumps(data))
        return path

    def lease_file(self, steps=None, owner='owner'):
        states = [('claimed', 'held', owner), ('released-for-restart', 'free', None),
                  ('reacquired', 'held', owner), ('released', 'free', None)]
        if steps is not None:
            states = steps
        path = self.work / 'lease.jsonl'
        records = []
        for index, (step, state, lease_owner) in enumerate(states):
            records.append({'step': step, 'status': {'state': state, 'owner': lease_owner,
                             'generation': f'g{index}'}, 'recorded_at': f't{index}'})
        path.write_text(''.join(json.dumps(row) + '\n' for row in records))
        return path

    def kit_output(self, entries, supervisor='SUPERVISORS runtime=1 agent=1 kit=1',
                   exe_overrides=None, missing_exe=()):
        exe_overrides = exe_overrides or {}
        lines = [f'boot_id {BOOT_ID}']
        for component, side, target, content in entries:
            if side == 'kit':
                lines.append(f'{digest(content)}  {target}')
        lines.append(supervisor)
        mapping = {'mister-runtime': ('mister-runtime', b'artifact'),
                   'mister-agent': ('mister-agent', b'artifact'),
                   'fogcast-tenfoot': ('fogcast-kit-child', b'artifact')}
        for component, side, target, content in entries:
            if side != 'kit' or component not in mapping:
                continue
            exe, _ = mapping[component]
            value = 'MISSING' if exe in missing_exe else exe_overrides.get(exe, digest(content))
            lines.append(f'exe {exe} {value}')
        path = self.work / 'kit.sha256'
        path.write_text('\n'.join(lines) + '\n')
        return path

    def host_output(self, entries, override=None):
        lines = []
        for component, side, target, content in entries:
            if side == 'host':
                lines.append(f'{(override or {}).get(target, digest(content))}  {target}')
                if component == 'host:fogcast-api':
                    lines.append(f'exe {component} {(override or {}).get(target, digest(content))} pid=42')
        path = self.work / 'host.sha256'
        path.write_text('\n'.join(lines) + ('\n' if lines else ''))
        return path

    def evidence(self, manifest, entries, *, update=True, lease=True, release=None,
                 output=None, extra=(), host_exe_value=None):
        manifest_entries = json.loads(Path(manifest).read_text())['entries']
        entries = [(component, side, target,
                    Path(next(row['local'] for row in manifest_entries
                              if row['component'] == component)).read_bytes()
                    if component.startswith('core:') else content)
                   for component, side, target, content in entries]
        kit_file = self.kit_output(entries) if any(row[1] == 'kit' for row in entries) else None
        host_file = self.host_output(entries) if any(row[1] == 'host' for row in entries) else None
        if host_file and host_exe_value is not None:
            host_file.write_text(host_file.read_text().replace(
                f'exe host:fogcast-api {digest(b"api")}',
                f'exe host:fogcast-api {host_exe_value}'))
        args = ['evidence', '--repo', self.repo, '--base-image-commit', self.base,
                '--head', self.head, '--base-image-sha256', BASE_SHA,
                '--manifest', manifest, '--base-release-json', release or self.release_file()]
        if kit_file:
            args += ['--kit-sha256', kit_file]
        if host_file:
            args += ['--host-sha256', host_file]
        if update:
            args += ['--kit-update-json', self.update_file()]
        if lease and (kit_file or any(row[0].startswith('core:') for row in entries)):
            steps = None if kit_file else [('claimed', 'held', 'owner'), ('released', 'free', None)]
            args += ['--lease-log', self.lease_file(steps), '--lease-owner', 'owner']
        for component, side, target, content in entries:
            if component.startswith('core:'):
                status = self.work / f'{component.replace(":", "_")}.status.json'
                row = next(row for row in manifest_entries if row['component'] == component)
                status.write_text(json.dumps({'state': 'active', 'development': True, 'core_package': {
                    'package_id': row['package_id'], 'build_id': 'build-test'}}))
                args += ['--core-status', f'{component}={status}']
        if output:
            args += ['--out', output]
        args.extend(extra)
        return run(*args)

    def assert_refused_without_evidence(self, result, output):
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertFalse(output.exists())

    def test_real_pr_fixtures(self):
        expected = {'pr569.files': ('mister-runtime', 'kit-go+host'),
                    'pr567.files': ('mister-runtime', 'core:atari-st'),
                    'pr565.files': ('core:ramtest',)}
        for filename, components in expected.items():
            with self.subTest(filename=filename):
                result = run('classify', '--paths-file', FIXTURES / filename, '--json')
                self.assertEqual(result.returncode, 0, result.stderr)
                data = json.loads(result.stdout)
                self.assertEqual(data['class'], 'overlay')
                for component in components:
                    self.assertIn(component, data['components'])

    def test_full_image_unrecognised_and_docs_only(self):
        for path in ('image/buildroot/board/x/etc/init.d/S42x', 'unknown/file.bin',
                     'sources/FogCast/cmd/fes-update/main.go',
                     'sources/FogCast/cmd/mister-bridge/x.go'):
            result = run('classify', '--paths-file', self.path_file(path), '--json')
            self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')
        for path in ('sources/FogCast/internal/example.go', 'sources/FogCast/catalog/example.go'):
            result = run('classify', '--paths-file', self.path_file(path), '--json')
            self.assertEqual(json.loads(result.stdout)['decision'], 'OVERLAY kit-go+host')
        result = run('classify', '--paths-file', self.path_file('docs/guide.md'), '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'NO_DEPLOY_CHANGE')

    def test_short_sha_and_manifest_destination_rules(self):
        binary = self.write_artifact('binary')
        self.assertEqual(run('manifest', '--head', '1234', '--out', self.work / 'm',
                             f'mister-agent={binary}=kit:/usr/sbin/mister-agent').returncode, 2)
        for target in ('/usr/bin/mister-agent', '/tmp/mister-agent'):
            result = run('manifest', '--head', 'a' * 40, '--out', self.work / 'm',
                         f'mister-agent={binary}=kit:{target}')
            self.assertEqual(result.returncode, 2)
            self.assertIn('/usr/sbin/mister-agent', result.stderr)
        result = run('manifest', '--head', 'a' * 40, '--out', self.work / 'm',
                     f'host:fogcast-api={binary}=kit:/home/api')
        self.assertEqual(result.returncode, 2)
        result = run('manifest', '--head', 'a' * 40, '--out', self.work / 'm',
                     f'mister-agent={binary}=kit:/usr/sbin/fogcast-kit')
        self.assertEqual(result.returncode, 2)

    def test_deploy_script_syntax_and_order(self):
        binary = self.write_artifact('binary', b'agent')
        manifest = self.work / 'manifest.json'
        result = run('manifest', '--head', 'a' * 40, '--out', manifest,
                     f'mister-agent={binary}=kit:/usr/sbin/mister-agent')
        self.assertEqual(result.returncode, 0, result.stderr)
        script = run('kit-deploy-script', '--manifest', manifest, '--stage-dir', '/run/hil-x')
        self.assertEqual(script.returncode, 0, script.stderr)
        generated = self.work / 'deploy.sh'
        generated.write_text(script.stdout)
        self.assertEqual(subprocess.run(['sh', '-n', generated]).returncode, 0)
        bash = shutil.which('bash')
        if bash:
            self.assertEqual(subprocess.run([bash, '-n', generated]).returncode, 0)
        for token in ('S60fogcast-kit stop', 'S50mister-agent stop', 'S40mister-runtime stop',
                      'sleep 1', 'mount --bind', 'REFUSE_START', 'sleep 2',
                      'sleep 3', 'SUPERVISORS runtime=1 agent=1 kit=1',
                      digest(b'agent'), '[ "$(sha256sum'):
            self.assertIn(token, script.stdout)
        self.assertLess(script.stdout.index('S60fogcast-kit start'),
                        script.stdout.index('startall || exit 1'))
        self.assertIn('EXPECTED_mister_agent=', script.stdout)

    def test_deploy_sha256sums_output(self):
        binary = self.write_artifact('binary', b'agent')
        manifest = self.work / 'm.json'
        run('manifest', '--head', 'a' * 40, '--out', manifest,
            f'mister-agent={binary}=kit:/usr/sbin/mister-agent')
        result = run('kit-deploy-script', '--manifest', manifest, '--stage-dir', '/run/hil', '--sha256sums')
        self.assertEqual(result.stdout, f'{digest(b"agent")}  mister-agent\n')

    def test_kit_command_handles_missing_pid_files(self):
        binary = self.write_artifact('binary', b'agent')
        manifest = self.work / 'm.json'
        run('manifest', '--head', 'a' * 40, '--out', manifest,
            f'mister-agent={binary}=kit:/usr/sbin/mister-agent')
        result = run('kit-command', '--manifest', manifest)
        self.assertEqual(result.returncode, 0, result.stderr)
        for name in ('mister-runtime', 'mister-agent', 'fogcast-kit-child'):
            self.assertIn(f'exe {name} MISSING', result.stdout)

    def test_full_evidence_happy_path_records_all_identity_and_components(self):
        self.git_repo(['sources/libmister-runtime/change.cpp',
                       'sources/FogCast/cmd/mister-agent/change.go',
                       'sources/FogCast/cmd/fogcast-api/change.go',
                       'sources/misteross/cores/ramtest/change.v'])
        # A core:ramtest artifact is directly required; add the other plan component artifacts.
        entries = [('mister-runtime', 'kit', '/usr/sbin/mister-runtime', b'rt'),
                   ('mister-agent', 'kit', '/usr/sbin/mister-agent', b'agent'),
                   ('host:fogcast-api', 'host', '/home/test/fogcast-api', b'api'),
                   ('core:ramtest', 'host', '/home/test/ramtest.fcore', b'core')]
        manifest = self.make_manifest(entries)
        output = self.work / 'evidence.md'
        result = self.evidence(manifest, entries, output=output)
        self.assertEqual(result.returncode, 0, result.stderr)
        text = output.read_text()
        for value in (self.head, self.base, BASE_SHA, BOOT_ID, 'mister-runtime',
                      'mister-agent', 'host:fogcast-api', 'core:ramtest', 'g0', 'g1', 'g2', 'g3'):
            self.assertIn(value, text)

    def test_plan_component_coverage_and_kit_go_host_expansion(self):
        self.git_repo(['sources/libmister-runtime/change.cpp',
                       'sources/FogCast/cmd/mister-agent/change.go'])
        entries = [('mister-runtime', 'kit', '/usr/sbin/mister-runtime', b'rt')]
        manifest = self.make_manifest(entries)
        output = self.work / 'evidence.md'
        result = self.evidence(manifest, entries, output=output)
        self.assert_refused_without_evidence(result, output)
        self.assertIn('mister-agent', result.stderr)

        self.git_repo(['sources/FogCast/internal/change.go'])
        entries = [('mister-agent', 'kit', '/usr/sbin/mister-agent', b'a'),
                   ('fogcast-kit', 'kit', '/usr/sbin/fogcast-kit', b'k'),
                   ('fogcast-tenfoot', 'kit', '/usr/sbin/fogcast-tenfoot', b't'),
                   ('host:fogcast-api', 'host', '/home/test/api', b'api'),
                   ('host:fogcast', 'host', '/home/test/fogcast', b'host')]
        manifest = self.make_manifest(entries)
        result = self.evidence(manifest, entries)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_core_all_expands_profile_cores(self):
        self.git_repo([])
        profile = self.repo / 'profiles/native-integration-dev.toml'
        profile.parent.mkdir(parents=True)
        profile.write_text('[[fpga_packages]]\ncore_id = "fes.one"\n[[fpga_packages]]\ncore_id = "two"\n')
        self.git('add', '.')
        self.git('commit', '-m', 'profile')
        self.base = self.git('rev-parse', 'HEAD')
        core_change = self.repo / 'sources/misteross/cores/fes-common/change.v'
        core_change.parent.mkdir(parents=True)
        core_change.write_text('change')
        self.git('add', '.')
        self.git('commit', '-m', 'core change')
        self.head = self.git('rev-parse', 'HEAD')
        entries = [('core:one', 'host', '/tmp/one', b'1')]
        manifest = self.make_manifest(entries)
        output = self.work / 'evidence.md'
        result = self.evidence(manifest, entries, output=output)
        self.assert_refused_without_evidence(result, output)
        self.assertIn('core:two', result.stderr)

    def test_host_hash_mismatch_missing_hash_and_unplanned_component_refuse(self):
        self.git_repo(['sources/libmister-runtime/change.cpp'])
        entries = [('mister-runtime', 'kit', '/usr/sbin/mister-runtime', b'rt'),
                   ('host:extra', 'host', '/home/test/api', b'api')]
        manifest = self.make_manifest(entries)
        output = self.work / 'evidence.md'
        result = self.evidence(manifest, entries, output=output)
        self.assert_refused_without_evidence(result, output)
        self.assertIn('unplanned component', result.stderr)

        # Valid planned host component exercises missing and mismatched host sums.
        self.git_repo(['sources/FogCast/cmd/fogcast-api/change.go'])
        entries = [('host:fogcast-api', 'host', '/home/test/api', b'api')]
        manifest = self.make_manifest(entries)
        output = self.work / 'missing.md'
        result = run('evidence', '--repo', self.repo, '--base-image-commit', self.base,
                     '--head', self.head, '--base-image-sha256', BASE_SHA,
                     '--manifest', manifest, '--base-release-json', self.release_file(),
                     '--out', output)
        self.assert_refused_without_evidence(result, output)
        self.assertIn('--host-sha256', result.stderr)
        host = self.host_output(entries, {entries[0][2]: '0' * 64})
        output = self.work / 'mismatch.md'
        result = run('evidence', '--repo', self.repo, '--base-image-commit', self.base,
                     '--head', self.head, '--base-image-sha256', BASE_SHA,
                     '--manifest', manifest, '--host-sha256', host,
                     '--base-release-json', self.release_file(), '--out', output)
        self.assert_refused_without_evidence(result, output)

    def test_evidence_revalidates_hand_edited_manifest_rules(self):
        self.git_repo(['sources/libmister-runtime/change.cpp'])
        entries = [('mister-runtime', 'kit', '/usr/sbin/mister-runtime', b'rt')]
        manifest = self.make_manifest(entries)
        data = json.loads(manifest.read_text())
        data['entries'][0]['target'] = '/usr/bin/mister-runtime'
        manifest.write_text(json.dumps(data))
        output = self.work / 'evidence.md'
        result = self.evidence(manifest, entries, output=output)
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertFalse(output.exists())

    def test_supervisor_and_executable_mismatches_refuse(self):
        self.git_repo(['sources/FogCast/cmd/mister-agent/change.go'])
        entries = [('mister-agent', 'kit', '/usr/sbin/mister-agent', b'agent')]
        manifest = self.make_manifest(entries)
        for supervisor, exe in (('SUPERVISORS runtime=0 agent=1 kit=1', {}),
                                ('SUPERVISORS runtime=1 agent=1 kit=1', {'mister-agent': '0' * 64})):
            output = self.work / 'evidence.md'
            kit = self.kit_output(entries, supervisor=supervisor, exe_overrides=exe)
            args = ['evidence', '--repo', self.repo, '--base-image-commit', self.base,
                    '--head', self.head, '--base-image-sha256', BASE_SHA,
                    '--manifest', manifest, '--kit-sha256', kit,
                    '--base-release-json', self.release_file(), '--kit-update-json', self.update_file(),
                    '--lease-log', self.lease_file(), '--lease-owner', 'owner', '--out', output]
            self.assert_refused_without_evidence(run(*args), output)

    def test_missing_running_executable_refuses(self):
        self.git_repo(['sources/FogCast/cmd/mister-agent/change.go'])
        entries = [('mister-agent', 'kit', '/usr/sbin/mister-agent', b'agent')]
        manifest = self.make_manifest(entries)
        kit = self.kit_output(entries, missing_exe=('mister-agent',))
        output = self.work / 'evidence.md'
        result = run('evidence', '--repo', self.repo, '--base-image-commit', self.base,
                     '--head', self.head, '--base-image-sha256', BASE_SHA,
                     '--manifest', manifest, '--kit-sha256', kit,
                     '--base-release-json', self.release_file(), '--kit-update-json', self.update_file(),
                     '--lease-log', self.lease_file(), '--lease-owner', 'owner', '--out', output)
        self.assert_refused_without_evidence(result, output)

    def test_release_and_update_identity_failures_refuse_without_traceback(self):
        self.git_repo(['sources/libmister-runtime/change.cpp'])
        entries = [('mister-runtime', 'kit', '/usr/sbin/mister-runtime', b'rt')]
        manifest = self.make_manifest(entries)
        cases = [
            ({'fes_revision': 'b' * 40}, {}),
            ({}, {'image_sha256': 'b' * 64}),
            ({}, {'good': 'b' * 64}),
            ({}, {'boot_id': 'wrong'}),
            ({}, {'trial': True}),
            ({}, {'error': 'unauthorized'}),
            ({}, {'code': 'unauthorized'}),
        ]
        for index, (release_changes, update_changes) in enumerate(cases):
            release = self.work / f'release-{index}.json'
            release.write_text(json.dumps({'fes_revision': self.base,
                                           'image_sha256': BASE_SHA,
                                           'version': 'test', **release_changes}))
            update = self.work / f'update-{index}.json'
            update.write_text(json.dumps({'boot_id': BOOT_ID, 'good': BASE_SHA,
                                          'image_sha256': BASE_SHA, 'corrupt': False,
                                          'trial': False, **update_changes}))
            output = self.work / 'evidence.md'
            result = self.evidence(manifest, entries, release=release, output=output,
                                   extra=('--kit-update-json', update))
            self.assert_refused_without_evidence(result, output)
            self.assertNotIn('Traceback', result.stderr)

    def test_host_only_update_without_kit_hash_skips_boot_comparison(self):
        self.git_repo(['sources/FogCast/cmd/fogcast-api/change.go'])
        entries = [('host:fogcast-api', 'host', '/home/test/api', b'api')]
        manifest = self.make_manifest(entries)
        result = self.evidence(manifest, entries, update=True)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_host_server_executable_attestation_and_one_shot_cli(self):
        self.git_repo(['sources/FogCast/cmd/fogcast-api/change.go'])
        entries = [('host:fogcast-api', 'host', '/home/test/api', b'api')]
        manifest = self.make_manifest(entries)
        for value in ('0' * 64, 'MISSING'):
            output = self.work / 'refused.md'
            result = self.evidence(manifest, entries, output=output, host_exe_value=value)
            self.assert_refused_without_evidence(result, output)
        host = self.host_output(entries)
        output = self.work / 'accepted.md'
        result = self.evidence(manifest, entries, output=output, extra=('--host-sha256', host))
        self.assertEqual(result.returncode, 0, result.stderr)

        self.git_repo(['sources/FogCast/cmd/fogcast/change.go'])
        cli_entries = [('host:fogcast', 'host', '/home/test/fogcast', b'cli')]
        cli_manifest = self.make_manifest(cli_entries)
        command = run('host-command', '--manifest', cli_manifest)
        self.assertEqual(command.returncode, 0, command.stderr)
        self.assertNotIn('exe ', command.stdout)
        cli_result = self.evidence(cli_manifest, cli_entries)
        self.assertEqual(cli_result.returncode, 0, cli_result.stderr)

    def test_host_command_scans_proc_for_api_server(self):
        binary = self.write_artifact('api', b'api')
        manifest = self.work / 'manifest.json'
        run('manifest', '--head', 'a' * 40, '--out', manifest,
            f'host:fogcast-api={binary}=host:/home/test/fogcast-api')
        command = run('host-command', '--manifest', manifest).stdout
        self.assertIn('sha256sum /home/test/fogcast-api', command)
        self.assertIn('/proc/[0-9]*', command)
        self.assertIn('pid=', command)
        self.assertIn('MISSING', command)
        generated = self.work / 'host-command.sh'
        generated.write_text(command)
        self.assertEqual(subprocess.run(['sh', '-n', generated]).returncode, 0)

    def test_core_only_requires_update_lease_and_status(self):
        self.git_repo(['sources/misteross/cores/ramtest/change.v'])
        entries = [('core:ramtest', 'host', '/tmp/ramtest.fcore', b'core')]
        manifest = self.make_manifest(entries)
        output = self.work / 'missing-update.md'
        result = self.evidence(manifest, entries, update=False, output=output)
        self.assert_refused_without_evidence(result, output)
        self.assertIn('--kit-update-json', result.stderr)
        output = self.work / 'missing-lease.md'
        result = self.evidence(manifest, entries, lease=False, output=output)
        self.assert_refused_without_evidence(result, output)
        self.assertIn('--lease-log', result.stderr)

        status = self.work / 'running.json'
        core_package_id = json.loads(manifest.read_text())['entries'][0]['package_id']
        status.write_text(json.dumps({'state': 'active', 'development': True, 'core_package': {
            'package_id': core_package_id, 'build_id': 'build-test'}}))
        log = self.lease_file([('claimed', 'held', 'owner'), ('released', 'free', None)])
        archive_bytes = Path(json.loads(manifest.read_text())['entries'][0]['local']).read_bytes()
        host_hashes = self.host_output([('core:ramtest', 'host', '/tmp/ramtest.fcore', archive_bytes)])
        args = ['evidence', '--repo', self.repo, '--base-image-commit', self.base,
                '--head', self.head, '--base-image-sha256', BASE_SHA, '--manifest', manifest,
                '--base-release-json', self.release_file(), '--kit-update-json', self.update_file(),
                '--host-sha256', host_hashes,
                '--lease-log', log, '--lease-owner', 'owner', '--core-status', f'core:ramtest={status}']
        missing_status_args = args[:-2]
        output = self.work / 'missing-status.md'
        self.assert_refused_without_evidence(run(*missing_status_args, '--out', output), output)
        output = self.work / 'accepted.md'
        result = run(*args, '--out', output)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('| Core component | Package ID | Build ID | Archive sha256 |', output.read_text())
        output = self.work / 'four-step.md'
        result = run(*args[:-4], '--lease-log', self.lease_file(), '--lease-owner', 'owner',
                     '--core-status', f'core:ramtest={status}', '--out', output)
        self.assert_refused_without_evidence(result, output)

        # lease_file() above rewrote the shared lease log with four steps; restore two.
        args[args.index('--lease-log') + 1] = self.lease_file(
            [('claimed', 'held', 'owner'), ('released', 'free', None)])
        for payload, reason in (
                ({'state': 'active', 'development': True, 'core_package': {'package_id': 'd' * 64}}, 'package_id mismatch'),
                ({'state': 'idle', 'development': True, 'core_package': {'package_id': core_package_id}}, 'not an active development core'),
                ({'state': 'error', 'development': True, 'core_package': {'package_id': core_package_id}}, 'not an active development core'),
                ({'state': 'active', 'core_package': {'package_id': core_package_id}}, 'lacks required fields'),
                ({'state': 'active', 'development': False, 'core_package': {'package_id': core_package_id}}, 'not an active development core'),
                *[({'state': state, 'development': True, 'core_package': {'package_id': core_package_id}},
                   'not an active development core') for state in ('launching', 'stopping', 'failed', 'local')]):
            status.write_text(json.dumps(payload))
            output = self.work / 'bad-status.md'
            result = run(*args, '--out', output)
            self.assert_refused_without_evidence(result, output)
            self.assertIn(reason, result.stderr)
        for payload in ({'error': 'unauthorized'}, {'code': 'unauthorized'}, {'state': 'active'}):
            status.write_text(json.dumps(payload))
            result = run(*args, '--out', output)
            self.assert_refused_without_evidence(result, output)
            self.assertIn('error body or lacks required fields', result.stderr)
        output = self.work / 'unplanned-status.md'
        args[args.index('--lease-log') + 1] = self.lease_file(
            [('claimed', 'held', 'owner'), ('released', 'free', None)])
        result = run(*args[:-2], '--core-status', f'core:other={status}', '--out', output)
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertFalse(output.exists())

    def test_core_manifest_derives_identity_and_rejects_wrong_core(self):
        core = self.write_artifact('core', core_archive('fes.pong'))
        args = ['manifest', '--head', 'a' * 40, '--out', self.work / 'manifest.json']
        accepted = run(*args, f'core:pong={core}=host:/tmp/pong.fcore')
        self.assertEqual(accepted.returncode, 0, accepted.stderr)
        entry = json.loads((self.work / 'manifest.json').read_text())['entries'][0]
        self.assertEqual(entry['core_id'], 'fes.pong')
        self.assertEqual(entry['package_id'], read_package(core).package_id)
        wrong = run(*args, f'core:ramtest={core}=host:/tmp/ramtest.fcore')
        self.assertEqual(wrong.returncode, 2)
        self.assertIn('archive is core fes.pong, not core:ramtest', wrong.stderr)

    def test_evidence_rechecks_core_archive_bytes(self):
        self.git_repo(['sources/misteross/cores/ramtest/change.v'])
        entries = [('core:ramtest', 'host', '/tmp/ramtest.fcore', b'ignored')]
        manifest = self.make_manifest(entries)
        row = json.loads(manifest.read_text())['entries'][0]
        Path(row['local']).write_bytes(core_archive('fes.ramtest'))
        output = self.work / 'evidence.md'
        result = self.evidence(manifest, entries, output=output)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertFalse(output.exists())
        self.assertIn('local core archive', result.stderr)

    def test_lease_order_owner_and_missing_step_refuse(self):
        self.git_repo(['sources/libmister-runtime/change.cpp'])
        entries = [('mister-runtime', 'kit', '/usr/sbin/mister-runtime', b'rt')]
        manifest = self.make_manifest(entries)
        bad_logs = [self.lease_file([('claimed', 'held', 'owner')]),
                    self.lease_file([('released-for-restart', 'free', None),
                                     ('claimed', 'held', 'owner'), ('reacquired', 'held', 'owner'),
                                     ('released', 'free', None)]),
                    self.lease_file([('claimed', 'held', 'owner'), ('released-for-restart', 'free', None),
                                     ('reacquired', 'held', 'someone-else'), ('released', 'free', None)])]
        for log in bad_logs:
            output = self.work / 'evidence.md'
            result = run('evidence', '--repo', self.repo, '--base-image-commit', self.base,
                         '--head', self.head, '--base-image-sha256', BASE_SHA,
                         '--manifest', manifest, '--kit-sha256', self.kit_output(entries),
                         '--base-release-json', self.release_file(), '--kit-update-json', self.update_file(),
                         '--lease-log', log, '--lease-owner', 'owner', '--out', output)
            self.assert_refused_without_evidence(result, output)

    def test_manifest_empty_duplicate_head_mismatch_and_hash_mismatch(self):
        self.git_repo(['sources/libmister-runtime/change.cpp'])
        entries = [('mister-runtime', 'kit', '/usr/sbin/mister-runtime', b'rt')]
        manifest = self.make_manifest(entries)
        good = json.loads(manifest.read_text())
        output = self.work / 'evidence.md'
        for data in ({'head': self.head, 'entries': []},
                     {'head': self.head, 'entries': good['entries'] * 2},
                     {'head': self.base, 'entries': good['entries']}):
            manifest.write_text(json.dumps(data))
            result = self.evidence(manifest, entries, output=output)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(output.exists())
        manifest.write_text(json.dumps(good))
        kit = self.work / 'wrong.sha'
        kit.write_text('0' * 64 + '  /usr/sbin/mister-runtime\n')
        output = self.work / 'evidence.md'
        result = run('evidence', '--repo', self.repo, '--base-image-commit', self.base,
                     '--head', self.head, '--base-image-sha256', BASE_SHA,
                     '--manifest', manifest, '--kit-sha256', kit,
                     '--base-release-json', self.release_file(), '--kit-update-json', self.update_file(),
                     '--lease-log', self.lease_file(), '--lease-owner', 'owner', '--out', output)
        self.assert_refused_without_evidence(result, output)

    def test_two_tree_drift_forces_full_and_rename_out_of_image(self):
        self.repo = self.work / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Test')
        (self.repo / 'base.txt').write_text('base')
        self.git('add', '.')
        self.git('commit', '-m', 'base')
        base = self.git('rev-parse', 'HEAD')
        (self.repo / 'image').mkdir()
        (self.repo / 'image/config').write_text('drift')
        self.git('add', '.')
        self.git('commit', '-m', 'drift')
        merge_base = self.git('rev-parse', 'HEAD')
        (self.repo / 'sources/libmister-runtime').mkdir(parents=True)
        (self.repo / 'sources/libmister-runtime/change').write_text('code')
        self.git('add', '.')
        self.git('commit', '-m', 'overlay')
        head = self.git('rev-parse', 'HEAD')
        self.assertEqual(json.loads(run('classify', '--repo', self.repo,
                                        '--base-image-commit', merge_base, '--head', head,
                                        '--json').stdout)['class'], 'overlay')
        result = run('classify', '--repo', self.repo, '--base-image-commit', base,
                     '--head', head, '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')

        image_file = self.repo / 'image/S50agent'
        image_file.parent.mkdir(exist_ok=True)
        image_file.write_text('x' * 20)
        self.git('add', '.')
        self.git('commit', '-m', 'image file')
        image_commit = self.git('rev-parse', 'HEAD')
        self.git('mv', 'image/S50agent', 'sources/libmister-runtime/S50agent')
        self.git('commit', '-m', 'rename from image')
        renamed = self.git('rev-parse', 'HEAD')
        result = run('classify', '--repo', self.repo, '--base-image-commit', image_commit,
                     '--head', renamed, '--json')
        data = json.loads(result.stdout)
        self.assertIn('image/S50agent', [row['path'] for row in data['rows']])
        self.assertEqual(data['decision'], 'FULL_IMAGE')

    def test_lease_record_appends_json(self):
        status = self.work / 'status.json'
        status.write_text(json.dumps({'state': 'held', 'owner': 'owner', 'generation': 'g'}))
        log = self.work / 'lease.jsonl'
        result = run('lease-record', '--log', log, '--step', 'claimed',
                     '--status-json', status, '--owner', 'owner')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(log.read_text())['step'], 'claimed')

    def path_file(self, path):
        filename = self.work / 'paths.txt'
        filename.write_text(path + '\n')
        return filename


if __name__ == '__main__':
    unittest.main()
