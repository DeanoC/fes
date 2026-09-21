import os
from pathlib import Path
import tempfile
import unittest
from scripts.quartus_tools import RebuildError, locate_quartus, quartus_version_line

class QuartusToolsTests(unittest.TestCase):
    def test_explicit_installation_and_exact_version(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            tool = root / 'bin/quartus_sh'
            tool.parent.mkdir()
            for version in ('17.0.2', '17.1.0', '117.0.2'):
                tool.write_text('#!/bin/sh\necho "Version ' + version + ' Lite"\n')
                tool.chmod(0o755)
                self.assertEqual(locate_quartus(str(root), root), (root, tool))
                if version == '17.0.2':
                    self.assertEqual(quartus_version_line(tool), 'Version 17.0.2 Lite')
                else:
                    with self.assertRaises(RebuildError):
                        quartus_version_line(tool)
            with self.assertRaises(RebuildError):
                locate_quartus('', root)
            tool.unlink()
            tool.symlink_to('/bin/true')
            with self.assertRaises(RebuildError):
                locate_quartus(str(root), root)
