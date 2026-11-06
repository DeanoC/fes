import copy
from dataclasses import replace
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

from scripts import export_de25_static_persona as exporter
from scripts.core_package import read_package

ROOT = Path(__file__).resolve().parents[1]


class StaticPersonaExportTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.work = Path(self.temporary.name)
        fixture = ROOT / 'tests/fixtures/core-bundle-v5'
        self.package_dir = self.work / 'package'
        self.package_dir.mkdir()
        shutil.copyfile(fixture / 'manifests/valid-basic.toml', self.package_dir / 'manifest.toml')
        for name in ('core.rbf', 'persona.rbf'):
            source = 'base.rbf' if name == 'core.rbf' else name
            shutil.copyfile(fixture / 'payloads' / source, self.package_dir / name)
        self.package = read_package(self.package_dir)
        selected = self.package.fields['static_expansion']
        self.shell = {name: selected[name] for name in (
            'control_build_id', 'root_qdb_sha256', 'interface_sha256', 'clock_reset_sha256')}
        self.shell['vendor_pof'] = dict(hash=selected['pof0_hash'])
        self.shell['base'] = dict(path='base.rbf', size=len(self.package.payload_bytes),
                                 sha256=exporter.digest(self.package.payload_bytes))
        self.payload = b'opaque synthetic 16K persona, not an FPGA image'
        self.persona = dict(id='zx81_ram16k', hardware_id=0xc002b002, path='ram.rbf',
            size=len(self.payload), sha256=exporter.digest(self.payload),
            **{k: self.shell[k] for k in ('root_qdb_sha256', 'interface_sha256', 'clock_reset_sha256')},
            vendor_pof=dict(hash=selected['pof0_hash']))
        self.descriptor = dict(shell=self.shell, personas=[self.persona])

    def manifest(self, package=None, descriptor=None, persona=None, payload=None):
        return exporter.asset_manifest(package or self.package, descriptor or self.descriptor,
            persona or self.persona, self.payload if payload is None else payload,
            revision='a' * 40, recipe_sha256='b' * 64)

    def test_archive_round_trips_existing_go_reader_and_composer(self):
        if shutil.which('go') is None:
            self.skipTest('Go is required for expansion-reader interoperability')
        manifest = self.manifest()
        archive = self.work / 'ram.tar'
        archive.write_bytes(exporter.archive_bytes(manifest, self.payload))
        # The real reader reconstructs Go's canonical JSON before accepting it;
        # this catches reordered fields as well as payload/identity changes.
        program = self.work / 'main.go'
        program.write_text('''package main
import (
 "encoding/json"
 "os"
 "github.com/DeanoC/misteross/expansion"
)
func main() {
 f,e:=os.Open(os.Args[1]);if e!=nil {panic(e)};defer f.Close()
 a,e:=expansion.ReadAsset(f);if e!=nil {panic(e)}
 p,e:=os.ReadFile(os.Args[2]);if e!=nil {panic(e)}
 var s expansion.StaticShell;if e=json.Unmarshal(p,&s);e!=nil {panic(e)}
 empty,e:=expansion.ComposeStatic(s,nil);if e!=nil {panic(e)}
 ram,e:=expansion.ComposeStatic(s,&a);if e!=nil {panic(e)}
 if e=json.NewEncoder(os.Stdout).Encode(map[string]any{"asset_id":a.ID,"empty":empty,"ram":ram});e!=nil {panic(e)}
}
''')
        import base64
        selected = self.package.fields['static_expansion']
        shell = dict(BaseSHA256=exporter.digest(self.package.payload_bytes),
            DefaultPersonaSHA256=exporter.digest(self.package.persona_bytes),
            PackageID=self.package.package_id, BuildID=self.package.fields['build']['id'],
            Base=base64.b64encode(self.package.payload_bytes).decode(),
            DefaultPersona=base64.b64encode(self.package.persona_bytes).decode(),
            Default={k: selected[k] for k in ('control_build_id', 'hardware_id',
                'root_qdb_sha256', 'interface_sha256', 'clock_reset_sha256', 'pof0_hash')})
        shell_file = self.work / 'shell.json'
        shell_file.write_text(json.dumps(shell))
        result = subprocess.run(['go', 'run', str(program), str(archive), str(shell_file)],
            cwd=ROOT / 'expansion', capture_output=True, text=True, timeout=90)
        self.assertEqual(result.returncode, 0, result.stderr)
        value = json.loads(result.stdout)
        self.assertEqual(value['asset_id'], exporter.digest(b'fes-expansion-v2\0' + manifest))
        self.assertEqual(value['empty']['hardware_id'], 0xc001a001)
        self.assertEqual(value['ram']['hardware_id'], 0xc002b002)
        self.assertEqual(value['ram']['persona_sha256'], exporter.digest(self.payload))
        self.assertNotEqual(value['empty']['composition_id'], value['ram']['composition_id'])

    def test_changed_frozen_root_package_base_and_partial_are_refused(self):
        for name in ('root_qdb_sha256', 'interface_sha256', 'clock_reset_sha256'):
            persona = copy.deepcopy(self.persona)
            persona[name] = 'e' * 64
            with self.subTest(name=name), self.assertRaises(exporter.BuildError):
                self.manifest(persona=persona)
        descriptor = copy.deepcopy(self.descriptor)
        descriptor['shell']['control_build_id'] = 'f' * 32
        with self.assertRaises(exporter.BuildError):
            self.manifest(descriptor=descriptor)
        with self.assertRaises(exporter.BuildError):
            self.manifest(payload=self.payload + b'changed')
        package = replace(self.package, payload_bytes=self.package.payload_bytes + b'changed')
        with self.assertRaises(exporter.BuildError):
            self.manifest(package=package)

    def test_compile_only_candidate_is_refused_before_package_read(self):
        output = self.work / 'producer'
        output.mkdir()
        (output / 'build-summary.json').write_text(json.dumps(dict(
            classification='de25-zx81-static-compile-candidate', package_timing_admission=False)))
        with self.assertRaisesRegex(exporter.BuildError, 'compile-only'):
            exporter.qualified_inputs(ROOT, output, self.work / 'missing.fcore')

    def test_raw_whole_board_failure_is_not_bypassed_by_an_existing_package(self):
        output = self.work / 'producer'
        output.mkdir()
        (output / 'build-summary.json').write_text(json.dumps(dict(
            classification='de25-zx81-static-package-candidate', package_timing_admission=True)))
        (output / 'build-inputs.json').write_bytes(b'{}')
        fields = dict(source_roots=[], source_inputs={}, parameters=dict(
            static_profile=exporter.static.PROFILE,
            programming_profile=exporter.static.PROGRAMMING_PROFILE))
        with mock.patch.object(exporter, 'verify_record_source_at_revision', return_value=fields), \
             mock.patch.object(exporter.static, 'builder'), \
             mock.patch.object(exporter.static, 'selected_artifacts', return_value=self.descriptor), \
             mock.patch.object(exporter.static, 'timing_evidence',
                               return_value={r: dict(package_timing_admission=False)
                                             for r in ('base', 'persona_a', 'persona_b')}):
            with self.assertRaisesRegex(exporter.BuildError, 'whole-board timing'):
                exporter.qualified_inputs(ROOT, output, self.package_dir)

    def publication_context(self):
        project = self.work / 'project'
        project.mkdir()
        (project / 'artifacts.json').write_text(json.dumps(self.descriptor))
        (project / self.persona['path']).write_bytes(self.payload)
        return [
            mock.patch.object(exporter, '_require_clean_source', return_value=('https://github.com/DeanoC/fes', 'a' * 40)),
            mock.patch.object(exporter, 'script_imports', return_value=[]),
            mock.patch.object(exporter, 'source_input_closure', return_value={exporter.RECIPE: 'b' * 64}),
            mock.patch.object(exporter, 'qualified_inputs', return_value=(self.package, b'{}', project, self.descriptor, {})),
            mock.patch.object(exporter.static, 'builder'),
            mock.patch.object(exporter.static, 'selected_artifacts', return_value=self.descriptor),
        ]

    def test_publication_is_immutable_and_exact_reuse_is_checked(self):
        from contextlib import ExitStack
        with ExitStack() as stack:
            for context in self.publication_context():
                stack.enter_context(context)
            destination = self.work / 'exports'
            archive = exporter.export(ROOT, self.work, self.package_dir, destination)
            original = archive.read_bytes()
            self.assertEqual(archive, exporter.export(ROOT, self.work, self.package_dir, destination))
            self.assertEqual(archive.stat().st_mode & 0o222, 0)
            archive.chmod(0o644)
            archive.write_bytes(original + b'changed')
            with self.assertRaises(ValueError):
                exporter.export(ROOT, self.work, self.package_dir, destination)

    def test_failed_archive_publication_removes_only_its_own_evidence(self):
        from contextlib import ExitStack
        real_publish = exporter._publish_no_replace
        def fail_archive(staged, final):
            if final.suffix == '.tar':
                raise OSError('injected archive publication failure')
            real_publish(staged, final)
        with ExitStack() as stack:
            for context in self.publication_context():
                stack.enter_context(context)
            stack.enter_context(mock.patch.object(exporter, '_publish_no_replace', side_effect=fail_archive))
            destination = self.work / 'exports'
            destination.mkdir()
            unrelated = destination / 'other-owner'
            unrelated.write_bytes(b'preserve')
            with self.assertRaisesRegex(OSError, 'publication failure'):
                exporter.export(ROOT, self.work, self.package_dir, destination)
            self.assertEqual(list(destination.iterdir()), [unrelated])
            self.assertEqual(unrelated.read_bytes(), b'preserve')

    def test_source_change_during_publication_leaves_no_export(self):
        from contextlib import ExitStack
        with ExitStack() as stack:
            contexts = self.publication_context()
            source = stack.enter_context(contexts[0])
            for context in contexts[1:]:
                stack.enter_context(context)
            selected = ('https://github.com/DeanoC/fes', 'a' * 40)
            source.side_effect = [selected, selected, (selected[0], 'c' * 40)]
            destination = self.work / 'exports'
            with self.assertRaisesRegex(exporter.BuildError, 'source changed'):
                exporter.export(ROOT, self.work, self.package_dir, destination)
            self.assertEqual(list(destination.iterdir()), [])


if __name__ == '__main__':
    unittest.main()
