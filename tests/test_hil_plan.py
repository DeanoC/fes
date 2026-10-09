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


def core_archive(core_id, revision='1' * 40):
    import tarfile
    manifest = (CORE_FIXTURES / 'manifests/valid-basic.toml').read_text()
    manifest = manifest.replace('id = "fes.pong"', f'id = "{core_id}"', 1)
    manifest = manifest.replace('revision = "' + '1' * 40 + '"', f'revision = "{revision}"', 1).encode()
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


def hil_module():
    import importlib.util
    spec = importlib.util.spec_from_file_location('hil_plan_under_test', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


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
        self.artifact_bytes = {}

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

    def write_artifact(self, name, content=b'artifact', head='a' * 40, stamp=True):
        # Binaries embed the head: Go via version.Revision, runtime via git-<12 hex>.
        if stamp:
            content = content + b'\0' + head.encode() + b'\0git-' + head[:12].encode() + b'\0'
        path = self.work / name
        path.write_bytes(content)
        return path

    def make_manifest(self, entries, head=None):
        result = self.work / 'manifest.json'
        payload = []
        for component, side, target, content in entries:
            local = self.write_artifact(component.replace(':', '_'), content, head or self.head)
            content = local.read_bytes()
            self.artifact_bytes[component] = content
            row = {'component': component, 'side': side, 'target': target,
                            'local': str(local), 'sha256': digest(content),
                            'size': len(content)}
            if component.startswith('core:'):
                core_id = component.removeprefix('core:')
                content = core_archive(core_id, head or self.head)
                local.write_bytes(content)
                self.artifact_bytes[component] = content
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
                             'generation': f'g{index}'}, 'recorded_at': f'2026-10-06T12:{index:02d}:00+00:00'})
        path.write_text(''.join(json.dumps(row) + '\n' for row in records))
        return path

    def capture(self, status, held_index=0, before=None, after=None,
                captured_at=None, finished_at=None):
        update = {'boot_id': BOOT_ID, 'good': BASE_SHA, 'image_sha256': BASE_SHA,
                  'corrupt': False, 'trial': False}
        return json.dumps({
            'format': 'fes-hil-core-capture-v1', 'target': 'http://kit',
            'captured_at': captured_at or f'2026-10-06T12:{held_index:02d}:10+00:00',
            'before': before or update, 'status': status, 'after': after or update,
            'finished_at': finished_at or f'2026-10-06T12:{held_index:02d}:20+00:00'})

    def kit_output(self, entries, supervisor='SUPERVISORS runtime=1 agent=1 kit=1',
                   exe_overrides=None, missing_exe=(), kit_ui='tenfoot'):
        exe_overrides = exe_overrides or {}
        entries = [(component, side, target, self.artifact_bytes.get(component, content))
                   for component, side, target, content in entries]
        lines = [f'boot_id {BOOT_ID}']
        for component, side, target, content in entries:
            if side == 'kit':
                lines.append(f'{digest(content)}  {target}')
        lines.append(supervisor)
        mapping = {'mister-runtime': ('mister-runtime', b'artifact'),
                   'mister-agent': ('mister-agent', b'artifact')}
        for component, side, target, content in entries:
            if side != 'kit' or component not in mapping:
                continue
            exe, _ = mapping[component]
            value = 'MISSING' if exe in missing_exe else exe_overrides.get(exe, digest(content))
            lines.append(f'exe {exe} {value}')
        launchers = {component: content for component, side, target, content in entries
                     if side == 'kit' and component in ('fogcast-kit', 'fogcast-tenfoot')}
        if 'fogcast-kit' in launchers:
            lines.append(f'kit_ui {kit_ui}')
        if launchers:
            child = launchers.get('fogcast-tenfoot', launchers.get('fogcast-kit'))
            exe = 'fogcast-kit-child'
            value = 'MISSING' if exe in missing_exe else exe_overrides.get(exe, digest(child))
            lines.append(f'exe {exe} {value}')
        path = self.work / 'kit.sha256'
        path.write_text('\n'.join(lines) + '\n')
        return path

    def host_output(self, entries, override=None):
        entries = [(component, side, target, self.artifact_bytes.get(component, content))
                   for component, side, target, content in entries]
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
                 output=None, extra=(), host_exe_value=None, host_owner='host-owner',
                 include_host_owner=True, lease_owner_override=None,
                 reacquired_owner=None):
        manifest_entries = json.loads(Path(manifest).read_text())['entries']
        locals_ = {row['component']: Path(row['local']) for row in manifest_entries}
        entries = [(component, side, target,
                    locals_[component].read_bytes()
                    if component in locals_ and locals_[component].exists()
                    else self.artifact_bytes.get(component, content))
                   for component, side, target, content in entries]
        kit_file = self.kit_output(entries) if any(row[1] == 'kit' for row in entries) else None
        host_file = self.host_output(entries) if any(row[1] == 'host' for row in entries) else None
        if host_file and host_exe_value is not None:
            host_file.write_text(host_file.read_text().replace(
                f'exe host:fogcast-api {digest(self.artifact_bytes.get("host:fogcast-api", b"api"))}',
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
            has_core = any(row[0].startswith('core:') for row in entries)
            if kit_file and has_core:
                steps = [('claimed', 'held', 'owner'), ('released-for-restart', 'free', None),
                         ('reacquired', 'held', reacquired_owner or host_owner), ('released', 'free', None)]
            elif kit_file:
                steps = None
            else:
                steps = [('claimed', 'held', host_owner), ('released', 'free', None)]
            if lease_owner_override is not None and not (kit_file and has_core):
                steps = [('claimed', 'held', lease_owner_override), ('released', 'free', None)]
            args += ['--lease-log', self.lease_file(steps), '--lease-owner', 'owner']
            if has_core and include_host_owner:
                args += ['--host-lease-owner', host_owner]
        for component, side, target, content in entries:
            if component.startswith('core:'):
                status = self.work / f'{component.replace(":", "_")}.status.json'
                row = next(row for row in manifest_entries if row['component'] == component)
                status.write_text(self.capture({'state': 'active', 'development': True, 'core_package': {
                    'package_id': row['package_id'], 'build_id': 'build-test'}},
                    held_index=2 if kit_file else 0))
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
                    'pr565.files': ('core:ramtest',)}
        # #567 rebuilt the Atari ST video part archive, which the overlay cannot attest.
        result = run('classify', '--paths-file', FIXTURES / 'pr567.files', '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')
        for filename, components in expected.items():
            with self.subTest(filename=filename):
                result = run('classify', '--paths-file', FIXTURES / filename, '--json')
                self.assertEqual(result.returncode, 0, result.stderr)
                data = json.loads(result.stdout)
                self.assertEqual(data['class'], 'overlay')
                for component in components:
                    self.assertIn(component, data['components'])

    def test_evidence_refuses_core_archive_from_other_revision(self):
        self.git_repo(['sources/misteross/cores/ramtest/change.v'])
        entries = [('core:ramtest', 'host', '/tmp/ramtest.fcore', b'ignored')]
        manifest = self.make_manifest(entries, head=self.head)
        data = json.loads(manifest.read_text())
        row = data['entries'][0]
        stale = core_archive('ramtest', self.base)
        Path(row['local']).write_bytes(stale)
        row.update(sha256=digest(stale), size=len(stale),
                   package_id=read_package(Path(row['local'])).package_id)
        manifest.write_text(json.dumps(data))
        output = self.work / 'stale.md'
        result = self.evidence(manifest, entries, output=output)
        self.assert_refused_without_evidence(result, output)
        self.assertIn('was not built from head', result.stderr)

    @unittest.skipUnless(shutil.which('go'), 'go toolchain not installed')
    def test_go_packages_used_only_by_unmapped_commands_force_full(self):
        import importlib.util
        spec = importlib.util.spec_from_file_location('hil_plan_rules', SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        mapped = {pattern.split('/')[3] for pattern, kind, _ in module.RULES
                  if pattern.startswith('sources/FogCast/cmd/') and kind == 'overlay'}
        prefix = 'github.com/DeanoC/FogCast/'
        listing = subprocess.run(['go', 'list', '-f', '{{.ImportPath}} {{join .Deps " "}}', './cmd/...'],
                                 cwd=ROOT / 'sources/FogCast', text=True, capture_output=True)
        if listing.returncode:
            self.skipTest('go list failed: ' + listing.stderr[:200])
        used = {}
        for line in listing.stdout.splitlines():
            command, *deps = line.split()
            used[command.rsplit('/', 1)[1]] = {dep[len(prefix):] for dep in deps if dep.startswith(prefix)}
        overlay = set().union(*(deps for command, deps in used.items() if command in mapped))
        other = set().union(*(deps for command, deps in used.items() if command not in mapped))
        for package in sorted(other - overlay):
            with self.subTest(package=package):
                result = run('classify', '--paths-file', self.path_file(f'sources/FogCast/{package}/x.go'), '--json')
                self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')
        # Every overlay command that compiles a package must be in that package's plan.
        component = {'fogcast-api': 'host:fogcast-api', 'fogcast': 'host:fogcast'}
        kit_go = {'mister-agent', 'fogcast-kit', 'fogcast-tenfoot', 'host:fogcast-api', 'host:fogcast'}
        paths = sorted(overlay)
        for package in paths:
            with self.subTest(package=package, check='consumers'):
                data = json.loads(run('classify', '--paths-file',
                                      self.path_file(f'sources/FogCast/{package}/x.go'), '--json').stdout)
                if data['decision'] == 'FULL_IMAGE':
                    continue
                planned = set(data['components'])
                if 'kit-go+host' in planned:
                    planned |= kit_go
                consumers = {component.get(command, command) for command, deps in used.items()
                             if command in mapped and package in deps}
                self.assertLessEqual(consumers, planned)

    def test_shared_core_rtl_expands_to_every_consuming_producer(self):
        plan = hil_module()
        texts = plan.script_texts()
        pll = 'sources/misteross/cores/fes-pong/rtl/pixel_pll.v'
        consumers = {row['component'] for row in plan.core_consumers(pll, texts)}
        # build_fes_catch reads it only through `from scripts import build_fes_demo`.
        for component in ('core:pong', 'core:demo', 'core:ramtest', 'core:riscv', 'core:catch'):
            self.assertIn(component, consumers)
        # build_fes_splash imports build_fes_pong, whose names reach this file, so
        # the boot /idle.rbf may change: the splash rule forces a full image.
        result = run('classify', '--paths-file', self.path_file(pll), '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')
        # The splash pins fes_application.vh through fes_de10nano_evidence.HPS_DDR_HEADER.
        result = run('classify', '--paths-file',
                     self.path_file('sources/misteross/cores/fes-common/generated/fes_application.vh'), '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')

        self.git_repo([])
        scripts = self.repo / 'sources/misteross/scripts'
        scripts.mkdir(parents=True)
        (scripts / 'build_fes_alpha.py').write_text('INPUTS = ("cores/fes-beta/rtl/shared.v",)\n')
        (scripts / 'helper.py').write_text('ROOTS = ["cores/fes-gamma/rtl"]\n')
        (scripts / 'sim_beta.py').write_text('X = "cores/fes-delta/rtl/only.v"\n')
        for name in ('fes-beta/rtl/shared.v', 'fes-gamma/rtl/x.v', 'fes-delta/rtl/only.v'):
            path = self.repo / 'sources/misteross/cores' / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('base\n')
        self.git('add', '.')
        self.git('commit', '-m', 'scripts')
        base = self.git('rev-parse', 'HEAD')
        expected = {'fes-beta/rtl/shared.v': ['core:alpha', 'core:beta'],
                    'fes-gamma/rtl/x.v': ['core:ALL', 'core:gamma'],
                    'fes-delta/rtl/only.v': ['core:delta']}
        for name, components in expected.items():
            with self.subTest(name=name):
                self.git('checkout', '-q', base)
                (self.repo / 'sources/misteross/cores' / name).write_text('change\n')
                self.git('commit', '-qam', 'change ' + name)
                head = self.git('rev-parse', 'HEAD')
                result = run('classify', '--repo', self.repo, '--base-image-commit', base,
                             '--head', head, '--json')
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(json.loads(result.stdout)['components'], components)

    def test_producer_imports_at_head_pass_their_inputs_on(self):
        plan = hil_module()
        texts = plan.script_texts()
        modules = plan.script_modules(texts)
        self.assertEqual([item for module in modules.values() for item in module.unresolved], [])
        imports = [('build_fes_catch', 'build_fes_demo'),
                   ('build_fes_menu_package', 'build_fes_menu'),
                   ('build_fes_coleco_socket_dev', 'build_fes_coleco_oss'),
                   ('build_fes_coleco_socket_v2', 'build_fes_coleco_oss'),
                   ('build_fes_coleco_socket_v2', 'video_parts'),
                   ('build_fes_coleco_socket_v2', 'native_video_parts'),
                   ('build_fes_coleco_megacart', 'build_fes_coleco_oss'),
                   ('build_fes_coleco_megacart', 'build_fes_coleco_socket_v2'),
                   ('build_fes_splash', 'build_fes_pong'),
                   ('build_fes_splash', 'fes_de10nano_evidence')]
        for importer, imported in imports:
            with self.subTest(importer=importer, imported=imported):
                own = set().union(*(literals for literals, _ in modules[importer].defs.values()))
                inherited = sorted(literal for literal in plan.script_reads(modules, imported)
                                   if literal.startswith('cores/') and literal not in own)
                self.assertTrue(inherited, 'expected inputs named only by ' + imported)
                path = 'sources/misteross/' + inherited[0]
                rows = plan.core_consumers(path, texts)
                self.assertIn('sources/misteross/scripts/' + importer + '.py', {row['path'] for row in rows})
                if importer == 'build_fes_splash':
                    self.assertEqual(plan.plan([path], texts)['decision'], 'FULL_IMAGE')

    def test_core_consumers_follow_script_imports(self):
        plan = hil_module()
        target = 'sources/misteross/cores/fes-beta/rtl/shared.v'
        base = {'scripts/inputs.py': 'SHARED = "cores/fes-beta/rtl/shared.v"\nOTHER = "cores/fes-x/y.v"\n'
                                     'def unrelated():\n    return OTHER\n'}
        cases = {
            'module attribute': ('from scripts import inputs as board\nPINNED = (*board.SHARED,)\n', True),
            'from import constant': ('from scripts.inputs import SHARED\n', True),
            'relative import': ('from .inputs import SHARED\n', True),
            'import scripts.module': ('import scripts.inputs\nX = scripts.inputs.OTHER\n', True),
            'function default': ('from scripts import inputs\ndef f(x=inputs.SHARED):\n    return x\n', True),
            'bare module alias': ('from scripts import inputs as board\nX = getattr(board, "S" + "HARED")\n', True),
            'unrelated name only': ('from scripts import inputs\nX = inputs.unrelated()\n', False),
            'no import': ('X = "cores/fes-x/y.v"\n', False),
        }
        for name, (text, consumes) in cases.items():
            with self.subTest(case=name):
                texts = dict(base, **{'scripts/build_fes_alpha.py': text})
                components = {row['component'] for row in plan.core_consumers(target, texts)}
                self.assertEqual('core:alpha' in components, consumes)
                self.assertEqual('core:alpha' in plan.plan([target], texts)['components'], consumes)
        # Transitive: alpha -> middle -> inputs, and a re-exported module alias.
        chains = {
            'scripts/middle.py': 'from scripts import inputs\nPINNED = (inputs.SHARED,)\n',
            'scripts/build_fes_alpha.py': 'from scripts import middle\nX = middle.PINNED\n',
            'scripts/build_fes_gamma.py': 'from scripts import middle\nY = middle.inputs.OTHER\n',
        }
        components = {row['component'] for row in plan.core_consumers(target, dict(base, **chains))}
        self.assertIn('core:alpha', components)
        self.assertIn('core:gamma', components)
        # A full-image producer that imports the input forces FULL_IMAGE.
        texts = dict(base, **{'scripts/build_fes_splash.py': 'from scripts import inputs\nX = inputs.SHARED\n'})
        self.assertEqual(plan.plan([target], texts)['decision'], 'FULL_IMAGE')

    def test_changed_script_expands_to_importing_producers(self):
        plan = hil_module()
        texts = plan.script_texts()
        scripts = 'sources/misteross/scripts/'
        # The splash imports build_fes_pong and fes_de10nano_evidence; a video-part
        # producer imports build_fes_coleco_oss.
        for name in ('build_fes_pong.py', 'fes_de10nano_evidence.py', 'build_fes_coleco_oss.py'):
            with self.subTest(script=name):
                self.assertEqual(plan.plan([scripts + name], texts)['decision'], 'FULL_IMAGE')
        result = plan.plan([scripts + 'build_fes_menu.py'], texts)
        self.assertIn('core:menu-package', result['components'])
        result = plan.plan([scripts + 'build_fes_demo.py'], texts)
        self.assertIn('core:catch', result['components'])

        base = {'scripts/helper.py': 'X = 1\n',
                'scripts/build_fes_beta.py': 'Y = 2\n',
                'scripts/sim_beta.py': 'from scripts import build_fes_beta\n'}
        cases = {
            'direct import': ({'scripts/build_fes_alpha.py': 'from scripts import build_fes_beta\n'},
                              'OVERLAY core:alpha core:beta'),
            'transitive import': ({'scripts/middle.py': 'from scripts.build_fes_beta import Y\n',
                                   'scripts/build_fes_alpha.py': 'from scripts import middle\n'},
                                  'OVERLAY core:ALL core:alpha core:beta'),
            'relative import': ({'scripts/build_fes_alpha.py': 'from .build_fes_beta import Y\n'},
                                'OVERLAY core:alpha core:beta'),
            'full importer': ({'scripts/build_fes_splash.py': 'import scripts.build_fes_beta\n'}, 'FULL_IMAGE'),
            'no importer': ({'scripts/build_fes_alpha.py': 'from scripts import helper\n'}, 'OVERLAY core:beta'),
            'unresolved elsewhere': ({'scripts/build_fes_alpha.py': 'from scripts import missing\n'}, 'FULL_IMAGE'),
        }
        for name, (extra, decision) in cases.items():
            with self.subTest(case=name):
                result = plan.plan([scripts + 'build_fes_beta.py'], dict(base, **extra))
                self.assertEqual(result['decision'], decision)
        # A sim_* change stays out of the plan unless a deploying script imports it.
        texts = dict(base, **{'scripts/sim_gamma.py': 'X = 1\n'})
        self.assertEqual(plan.plan([scripts + 'sim_gamma.py'], texts)['decision'], 'NO_DEPLOY_CHANGE')
        texts['scripts/build_fes_alpha.py'] = 'from scripts import sim_gamma\n'
        self.assertEqual(plan.plan([scripts + 'sim_gamma.py'], texts)['decision'], 'OVERLAY core:alpha')

    def test_unresolved_script_imports_fail_closed(self):
        plan = hil_module()
        target = 'sources/misteross/cores/fes-beta/rtl/shared.v'
        cases = {
            'missing scripts module': 'from scripts import missing\n',
            'missing from-module': 'from scripts.missing import X\n',
            'nested scripts module': 'import scripts.sub.mod\n',
            'parent relative import': 'from ..other import X\n',
            'computed dynamic import': 'import importlib\nM = importlib.import_module("scripts." + "x")\n',
            'computed __import__': 'N = "x"\nM = __import__(N)\n',
            'syntax error': 'def broken(:\n',
        }
        for name, text in cases.items():
            with self.subTest(case=name):
                texts = {'scripts/helper.py': text, 'scripts/build_fes_beta.py': 'X = 1\n'}
                result = plan.plan([target], texts)
                self.assertEqual(result['decision'], 'FULL_IMAGE')
                self.assertEqual(result['rows'][0]['component'], 'unresolved-script-import')
        # Outside modules and literal imports of real scripts are fine.
        texts = {'scripts/helper.py': 'import json\nimport importlib\nM = importlib.import_module("json")\n',
                 'scripts/build_fes_beta.py': 'from scripts import helper\n'}
        self.assertEqual(plan.plan([target], texts)['decision'], 'OVERLAY core:beta')
        # sim_* scripts never deploy, so their imports do not matter.
        texts = {'scripts/sim_beta.py': 'from scripts import missing\n', 'scripts/build_fes_beta.py': 'X = 1\n'}
        self.assertEqual(plan.plan([target], texts)['decision'], 'OVERLAY core:beta')

    def test_binaries_must_embed_the_head_revision(self):
        head = 'a' * 40
        args = ['manifest', '--head', head, '--out', self.work / 'm.json']
        cases = [
            ('mister-agent', 'kit:/usr/sbin/mister-agent', b'go' + ('b' * 40).encode(), 'does not embed revision'),
            ('host:fogcast-api', 'host:/tmp/api', b'go', 'does not embed revision'),
            ('mister-runtime', 'kit:/usr/sbin/mister-runtime', b'git-' + ('b' * 12).encode(),
             'does not embed version'),
            # The first 12 hex of the head followed by a different commit's tail.
            ('mister-runtime', 'kit:/usr/sbin/mister-runtime',
             b'git-' + head[:12].encode() + ('b' * 28).encode(), 'does not embed version'),
            ('mister-runtime', 'kit:/usr/sbin/mister-runtime', b'git-' + ('b' * 40).encode(),
             'does not embed version'),
            ('host:other', 'host:/tmp/other', head.encode(), 'no head revision check'),
        ]
        for component, destination, content, reason in cases:
            with self.subTest(component=component, reason=reason):
                binary = self.write_artifact('bin', content, stamp=False)
                result = run(*args, f'{component}={binary}={destination}')
                self.assertEqual(result.returncode, 2)
                self.assertIn(reason, result.stderr)
        for version in (b'git-' + head.encode(), b'git-' + head[:12].encode(),
                        b'git-' + head[:12].encode() + b'-dirty'):
            with self.subTest(version=version):
                runtime = self.write_artifact('rt', b'\0' + version + b'\0', stamp=False)
                result = run(*args, f'mister-runtime={runtime}=kit:/usr/sbin/mister-runtime')
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_runtime_versions_from_the_real_build_recipes_are_accepted(self):
        head = subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip()
        args = ['manifest', '--head', head, '--out', self.work / 'm.json']
        # Image recipe: MISTER_RUNTIME_VERSION="git-$(FOGCAST_MISTER_RUNTIME_COMMIT)",
        # where target-image-container.sh sets the commit with rev-parse --verify HEAD.
        recipe = (ROOT / 'image/buildroot/package/mister-runtime/mister-runtime.mk').read_text()
        self.assertIn('MISTER_RUNTIME_VERSION="git-$(FOGCAST_MISTER_RUNTIME_COMMIT)"', recipe)
        container = (ROOT / 'image/scripts/target-image-container.sh').read_text()
        self.assertIn('native_runtime_commit=$(git -C "$native_runtime_source" rev-parse --verify HEAD)', container)
        versions = [b'git-' + head.encode()]
        # Component Makefile: evaluate MISTER_RUNTIME_VERSION with the real Makefile.
        probe = self.work / 'print-version.mk'
        probe.write_text('hil-print-version:\n\t@echo $(MISTER_RUNTIME_VERSION)\n')
        made = subprocess.run(['make', '-s', '-C', str(ROOT / 'sources/libmister-runtime'),
                               '-f', 'Makefile', '-f', str(probe), 'hil-print-version'],
                              text=True, capture_output=True)
        if made.returncode == 0:
            version = made.stdout.strip()
            self.assertRegex(version, r'^git-' + head[:12] + r'(-dirty)?$')
            versions.append(version.encode())
        for version in versions:
            with self.subTest(version=version):
                runtime = self.write_artifact('rt', b'\0' + version + b'\0', stamp=False)
                result = run(*args, f'mister-runtime={runtime}=kit:/usr/sbin/mister-runtime')
                self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(versions), 2, made.stderr)

    def test_evidence_rechecks_local_binary_and_revision(self):
        self.git_repo(['sources/FogCast/cmd/mister-agent/change.go'])
        entries = [('mister-agent', 'kit', '/usr/sbin/mister-agent', b'agent')]
        manifest = self.make_manifest(entries)
        data = json.loads(manifest.read_text())
        local = Path(data['entries'][0]['local'])
        stale = b'agent built at another commit'
        for mutate, reason in ((lambda: local.unlink(), 'local artifact for mister-agent is missing'),
                               (lambda: local.write_bytes(stale), 'sha256 changed')):
            with self.subTest(reason=reason):
                self.make_manifest(entries)
                mutate()
                output = self.work / 'stale.md'
                result = self.evidence(manifest, entries, output=output)
                self.assert_refused_without_evidence(result, output)
                self.assertIn(reason, result.stderr)
        # A hand-edited manifest that re-hashes a stale binary still fails the revision check.
        self.make_manifest(entries)
        data = json.loads(manifest.read_text())
        local.write_bytes(stale)
        data['entries'][0].update(sha256=digest(stale), size=len(stale))
        manifest.write_text(json.dumps(data))
        self.artifact_bytes['mister-agent'] = stale
        output = self.work / 'stale.md'
        result = self.evidence(manifest, entries, output=output)
        self.assert_refused_without_evidence(result, output)
        self.assertIn('does not embed revision', result.stderr)

    def test_video_part_inputs_force_full_image(self):
        result = run('classify', '--paths-file',
                     self.path_file('sources/misteross/cores/fes-common/rtl/fes_video_part_direct.v'), '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')
        self.git_repo([])
        scripts = self.repo / 'sources/misteross/scripts'
        scripts.mkdir(parents=True)
        (scripts / 'build_x_video_part.py').write_text('RTL = "cores/fes-common/rtl/part.v"\n')
        (scripts / 'build_fes_splash.py').write_text('RTL = "cores/fes-beta/rtl/logo.v"\n')
        for name in ('fes-common/rtl/part.v', 'fes-common/rtl/other.v', 'fes-beta/rtl/logo.v'):
            path = self.repo / 'sources/misteross/cores' / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('base\n')
        self.git('add', '.')
        self.git('commit', '-m', 'scripts')
        base = self.git('rev-parse', 'HEAD')
        for name, decision in (('fes-common/rtl/part.v', 'FULL_IMAGE'), ('fes-beta/rtl/logo.v', 'FULL_IMAGE'),
                               ('fes-common/rtl/other.v', 'OVERLAY core:ALL')):
            with self.subTest(name=name):
                self.git('checkout', '-q', base)
                (self.repo / 'sources/misteross/cores' / name).write_text('change\n')
                self.git('commit', '-qam', 'change ' + name)
                head = self.git('rev-parse', 'HEAD')
                result = run('classify', '--repo', self.repo, '--base-image-commit', base,
                             '--head', head, '--json')
                self.assertEqual(json.loads(result.stdout)['decision'], decision)

    def test_full_image_unrecognised_and_docs_only(self):
        for path in ('image/buildroot/board/x/etc/init.d/S42x', 'unknown/file.bin',
                     'sources/FogCast/cmd/fes-update/main.go',
                     'sources/FogCast/cmd/mister-bridge/x.go',
                     'sources/misteross/sealed/fes-splash.rbf',
                     'sources/misteross/sealed/fes-splash.build-summary.json',
                     'sources/misteross/cores/fes-splash/rtl/top.v',
                     'sources/misteross/scripts/build_fes_splash.py',
                     'sources/misteross/scripts/build_atari_st_video_part.py',
                     'sources/misteross/scripts/build_video_part.py',
                     'sources/misteross/scripts/video_parts.py',
                     'sources/FogCast/internal/targetimage/lock.go'):
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
                      digest(binary.read_bytes()), '[ "$(sha256sum'):
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
        self.assertEqual(result.stdout, f'{digest(binary.read_bytes())}  mister-agent\n')

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

    def test_fogcast_kit_child_is_attested(self):
        self.git_repo(['sources/FogCast/cmd/fogcast-kit/change.go'])
        entries = [('fogcast-kit', 'kit', '/usr/sbin/fogcast-kit', b'kit')]
        manifest = self.make_manifest(entries)
        command = run('kit-command', '--manifest', manifest).stdout
        self.assertIn('kit_ui $(/usr/sbin/fogcast-kit --config /media/fat/fogcast/launcher.json --print-kit-ui', command)
        output = self.work / 'kit.md'
        self.assertEqual(self.evidence(manifest, entries, output=output).returncode, 0)
        for overrides, missing, reason in (
                ({'fogcast-kit-child': digest(b'base-tenfoot')}, (), 'expected fogcast-kit'),
                ({}, ('fogcast-kit-child',), 'MISSING for fogcast-kit-child')):
            kit = self.kit_output(entries, exe_overrides=overrides, missing_exe=missing)
            output.unlink(missing_ok=True)
            result = run('evidence', '--repo', self.repo, '--base-image-commit', self.base,
                         '--head', self.head, '--base-image-sha256', BASE_SHA,
                         '--manifest', manifest, '--kit-sha256', kit,
                         '--base-release-json', self.release_file(), '--kit-update-json', self.update_file(),
                         '--lease-log', self.lease_file(), '--lease-owner', 'owner', '--out', output)
            self.assert_refused_without_evidence(result, output)
            self.assertIn(reason, result.stderr)

    def test_kit_and_tenfoot_need_tenfoot_child_and_kit_ui(self):
        self.git_repo(['sources/FogCast/cmd/fogcast-kit/change.go',
                       'sources/FogCast/cmd/fogcast-tenfoot/change.go'])
        entries = [('fogcast-kit', 'kit', '/usr/sbin/fogcast-kit', b'kit'),
                   ('fogcast-tenfoot', 'kit', '/usr/sbin/fogcast-tenfoot', b'tenfoot')]
        manifest = self.make_manifest(entries)
        output = self.work / 'both.md'
        self.assertEqual(self.evidence(manifest, entries, output=output).returncode, 0)
        for overrides, kit_ui, reason in (
                ({'fogcast-kit-child': digest(b'kit')}, 'grid', 'expected fogcast-tenfoot'),
                ({}, 'FAILED', 'did not complete --print-kit-ui'),
                ({}, 'grid', 'did not complete --print-kit-ui')):
            kit = self.kit_output(entries, exe_overrides=overrides, kit_ui=kit_ui)
            output.unlink(missing_ok=True)
            result = run('evidence', '--repo', self.repo, '--base-image-commit', self.base,
                         '--head', self.head, '--base-image-sha256', BASE_SHA,
                         '--manifest', manifest, '--kit-sha256', kit,
                         '--base-release-json', self.release_file(), '--kit-update-json', self.update_file(),
                         '--lease-log', self.lease_file(), '--lease-owner', 'owner', '--out', output)
            self.assert_refused_without_evidence(result, output)
            self.assertIn(reason, result.stderr)

    def test_core_capture_requires_token_and_writes_bundle(self):
        import http.server
        import os
        import threading
        seen = []

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                seen.append((self.path, self.headers.get('Authorization')))
                body = {'/v1/update': {'boot_id': BOOT_ID, 'good': BASE_SHA, 'image_sha256': BASE_SHA,
                                       'corrupt': False, 'trial': False},
                        '/v1/status': {'state': 'active'}}[self.path]
                data = json.dumps(body).encode()
                self.send_response(200)
                self.send_header('Content-Length', str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def log_message(self, *args):
                pass

        server = http.server.HTTPServer(('127.0.0.1', 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        url = f'http://127.0.0.1:{server.server_port}'
        out = self.work / 'capture.json'
        env = {key: value for key, value in os.environ.items() if key != 'FOGCAST_TOKEN'}
        missing = subprocess.run([sys.executable, str(SCRIPT), 'core-capture', '--target-url', url,
                                  '--out', str(out)], text=True, capture_output=True, env=env)
        self.assertEqual(missing.returncode, 2)
        self.assertFalse(out.exists())
        env['FOGCAST_TOKEN'] = 'secret-token'
        result = subprocess.run([sys.executable, str(SCRIPT), 'core-capture', '--target-url', url,
                                 '--out', str(out)], text=True, capture_output=True, env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('secret-token', result.stdout + result.stderr + out.read_text())
        bundle = json.loads(out.read_text())
        self.assertEqual(bundle['format'], 'fes-hil-core-capture-v1')
        self.assertEqual([path for path, _ in seen], ['/v1/update', '/v1/status', '/v1/update'])
        self.assertTrue(all(auth == 'Bearer secret-token' for _, auth in seen))
        self.assertEqual(bundle['status'], {'state': 'active'})

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

    def test_host_command_omits_hash_for_kit_only_manifest(self):
        manifest = self.work / 'kit-only.json'
        manifest.write_text(json.dumps({'entries': [{
            'component': 'mister-runtime', 'side': 'kit',
            'target': '/usr/sbin/mister-runtime', 'sha256': 'ab'}]}))
        command = run('host-command', '--manifest', manifest)
        self.assertEqual(command.returncode, 0, command.stderr)
        self.assertNotIn('sha256sum', command.stdout)
        self.assertEqual(command.stdout, '')
        generated = self.work / 'kit-only-host-command.sh'
        generated.write_text(command.stdout)
        self.assertEqual(subprocess.run(['sh', '-n', generated]).returncode, 0)

    def test_evidence_refusal_removes_preexisting_output(self):
        self.git_repo(['sources/libmister-runtime/change.cpp'])
        entries = [('mister-runtime', 'kit', '/usr/sbin/mister-runtime', b'rt')]
        manifest = self.make_manifest(entries)
        output = self.work / 'stale-evidence.md'
        output.write_text('previous successful evidence\n')
        result = self.evidence(manifest, entries, output=output, lease=False)
        self.assert_refused_without_evidence(result, output)
        self.assertIn('--lease-log', result.stderr)

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
        good_status = {'state': 'active', 'development': True, 'core_package': {
            'package_id': core_package_id, 'build_id': 'build-test'}}
        status.write_text(self.capture(good_status))
        log = self.lease_file([('claimed', 'held', 'host-owner'), ('released', 'free', None)])
        archive_bytes = Path(json.loads(manifest.read_text())['entries'][0]['local']).read_bytes()
        host_hashes = self.host_output([('core:ramtest', 'host', '/tmp/ramtest.fcore', archive_bytes)])
        args = ['evidence', '--repo', self.repo, '--base-image-commit', self.base,
                '--head', self.head, '--base-image-sha256', BASE_SHA, '--manifest', manifest,
                '--base-release-json', self.release_file(), '--kit-update-json', self.update_file(),
                '--host-sha256', host_hashes,
                '--lease-log', log, '--host-lease-owner', 'host-owner', '--core-status', f'core:ramtest={status}']
        missing_status_args = args[:-2]
        output = self.work / 'missing-status.md'
        self.assert_refused_without_evidence(run(*missing_status_args, '--out', output), output)
        output = self.work / 'accepted.md'
        result = run(*args, '--out', output)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('| Core component | Package ID | Build ID | Archive sha256 |', output.read_text())
        other_boot = {'boot_id': 'different-boot', 'good': BASE_SHA,
                      'image_sha256': BASE_SHA, 'corrupt': False, 'trial': False}
        for capture, reason in (
                (json.dumps(good_status), 'not a fes-hil-core-capture-v1 bundle'),
                (json.dumps({**json.loads(self.capture(good_status)), 'format': 'other'}),
                 'not a fes-hil-core-capture-v1 bundle'),
                (self.capture(good_status, after=other_boot), 'after boot_id does not match'),
                (self.capture(good_status, before=other_boot), 'before boot_id does not match'),
                (self.capture(good_status, after={**other_boot, 'boot_id': BOOT_ID, 'trial': True}),
                 'after update is corrupt or trial'),
                (self.capture(good_status, before={'error': 'unauthorized'}), 'error body'),
                (self.capture(good_status, captured_at='2026-10-06T11:59:00+00:00'),
                 'not taken inside the host lease window'),
                (self.capture(good_status, finished_at='2026-10-06T12:05:00+00:00'),
                 'not taken inside the host lease window'),
                (self.capture(good_status, captured_at='2026-10-06T12:00:10'), 'invalid timestamp')):
            status.write_text(capture)
            output.unlink(missing_ok=True)
            result = run(*args, '--out', output)
            self.assert_refused_without_evidence(result, output)
            self.assertIn(reason, result.stderr)
        status.write_text(self.capture(good_status))
        output = self.work / 'four-step.md'
        result = run(*args[:-4], '--lease-log', self.lease_file(), '--lease-owner', 'owner',
                     '--core-status', f'core:ramtest={status}', '--out', output)
        self.assert_refused_without_evidence(result, output)

        # lease_file() above rewrote the shared lease log with four steps; restore two.
        args[args.index('--lease-log') + 1] = self.lease_file(
            [('claimed', 'held', 'host-owner'), ('released', 'free', None)])
        for payload, reason in (
                ({'state': 'active', 'development': True, 'core_package': {'package_id': 'd' * 64}}, 'package_id mismatch'),
                ({'state': 'idle', 'development': True, 'core_package': {'package_id': core_package_id}}, 'not an active development core'),
                ({'state': 'error', 'development': True, 'core_package': {'package_id': core_package_id}}, 'not an active development core'),
                ({'state': 'active', 'core_package': {'package_id': core_package_id}}, 'lacks required fields'),
                ({'state': 'active', 'development': False, 'core_package': {'package_id': core_package_id}}, 'not an active development core'),
                *[({'state': state, 'development': True, 'core_package': {'package_id': core_package_id}},
                   'not an active development core') for state in ('launching', 'stopping', 'failed', 'local')]):
            status.write_text(self.capture(payload))
            output = self.work / 'bad-status.md'
            result = run(*args, '--out', output)
            self.assert_refused_without_evidence(result, output)
            self.assertIn(reason, result.stderr)
        for payload in ({'error': 'unauthorized'}, {'code': 'unauthorized'}, {'state': 'active'}):
            status.write_text(self.capture(payload))
            result = run(*args, '--out', output)
            self.assert_refused_without_evidence(result, output)
            self.assertIn('error body or lacks required fields', result.stderr)
        output = self.work / 'unplanned-status.md'
        args[args.index('--lease-log') + 1] = self.lease_file(
            [('claimed', 'held', 'host-owner'), ('released', 'free', None)])
        result = run(*args[:-2], '--core-status', f'core:other={status}', '--out', output)
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertFalse(output.exists())

    def test_mixed_plan_uses_host_lease_after_restart(self):
        self.git_repo(['sources/libmister-runtime/change.cpp',
                       'sources/misteross/cores/fes-atari-st/change.v'])
        entries = [('mister-runtime', 'kit', '/usr/sbin/mister-runtime', b'rt'),
                   ('core:atari-st', 'host', '/tmp/atari-st.fcore', b'core')]
        manifest = self.make_manifest(entries)
        output = self.work / 'mixed.md'
        accepted = self.evidence(manifest, entries, output=output)
        self.assertEqual(accepted.returncode, 0, accepted.stderr)
        output.unlink(missing_ok=True)
        bad_owner = self.evidence(manifest, entries, reacquired_owner='owner', output=output)
        self.assert_refused_without_evidence(bad_owner, output)
        output.unlink(missing_ok=True)
        missing_owner = self.evidence(manifest, entries, include_host_owner=False, output=output)
        self.assert_refused_without_evidence(missing_owner, output)

    def test_core_only_rejects_claim_by_wrong_host_owner(self):
        self.git_repo(['sources/misteross/cores/ramtest/change.v'])
        entries = [('core:ramtest', 'host', '/tmp/ramtest.fcore', b'core')]
        manifest = self.make_manifest(entries)
        output = self.work / 'wrong-owner.md'
        result = self.evidence(manifest, entries, lease_owner_override='owner', output=output)
        self.assert_refused_without_evidence(result, output)

    def test_core_manifest_derives_identity_and_rejects_wrong_core(self):
        stale = self.write_artifact('stale', core_archive('fes.pong'), stamp=False)
        args = ['manifest', '--head', 'a' * 40, '--out', self.work / 'manifest.json']
        old = run(*args, f'core:pong={stale}=host:/tmp/pong.fcore')
        self.assertEqual(old.returncode, 2)
        self.assertIn('built from revision 1111111111111111111111111111111111111111', old.stderr)
        core = self.write_artifact('core', core_archive('fes.pong', 'a' * 40), stamp=False)
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
