"""The optional EL1 CPU overlay rejects unknown inputs before publication."""
import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('native_cpu_features', ROOT/'platform/embedded/tamago/cpu_features.py')
cpu = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(cpu)
ORIGINAL = b'''// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build arm64 && !linux && !freebsd && !android && (!darwin || ios) && !openbsd

package cpu

func osInit() {
\t// Other operating systems do not support reading HWCap from auxiliary vector,
\t// reading privileged aarch64 system registers or sysctl in user space to detect
\t// CPU features at runtime.
}
'''


class CPUFeatureOverlayTests(unittest.TestCase):
    def test_pinned_source_uses_existing_register_parser(self):
        result = cpu.source(ORIGINAL).decode()
        self.assertIn('//go:build arm64 && tamago\n', result)
        self.assertIn('parseARM64SystemRegisters(getisar0(), getpfr0())', result)
        self.assertNotIn('= true', result)

    def test_changed_toolchain_cannot_be_silently_patched(self):
        for source in (b'', ORIGINAL+b'\n', ORIGINAL.replace(b'osInit', b'osinit')):
            with self.subTest(source=source[:20]), self.assertRaises(ValueError):
                cpu.source(source)

    def test_other_execution_environments_reject_before_io(self):
        for env in ({}, {'GOOS':'linux','GOARCH':'arm64'}, {'GOOS':'tamago','GOARCH':'amd64'}):
            with self.subTest(env=env), patch.object(cpu.subprocess,'check_output',side_effect=AssertionError('compiler invoked')):
                with self.assertRaises(ValueError):
                    cpu.prepare(Path('/not/a/compiler'), Path('/not/an/output'), env)

    def test_changed_parser_rejects_before_writing_overlay(self):
        with tempfile.TemporaryDirectory() as name:
            root=Path(name);directory=root/'src/internal/cpu';directory.mkdir(parents=True)
            (directory/'cpu_arm64_other.go').write_bytes(ORIGINAL)
            (directory/'cpu_arm64.go').write_bytes(b'changed register decoder')
            output=root/'output';output.mkdir()
            with patch.object(cpu.subprocess,'check_output',return_value=str(root)+'\n'):
                with self.assertRaisesRegex(ValueError,'cpu_arm64.go'):
                    cpu.prepare(Path('/not/a/compiler'), output, {'GOOS':'tamago','GOARCH':'arm64'})
            self.assertEqual(list(output.iterdir()),[])
            self.assertEqual((directory/'cpu_arm64_other.go').read_bytes(),ORIGINAL)

    def test_nano_option_rejects_qemu_before_creating_output(self):
        with tempfile.TemporaryDirectory(dir=ROOT/'out',prefix='cpu-option-test-') as name:
            output=Path(name)/'not-created'
            result=subprocess.run(['python3',str(ROOT/'platform/embedded/tamago/run.py'),'probe',
                '--arm64-features','--compiler','/not/a/compiler','--output',str(output)],capture_output=True,text=True)
            self.assertEqual(result.returncode,2)
            self.assertIn('--arm64-features requires --board de25-nano',result.stderr)
            self.assertFalse(output.exists())


if __name__ == '__main__':
    unittest.main()
