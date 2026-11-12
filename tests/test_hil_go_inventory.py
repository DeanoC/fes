import os
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

from scripts import hil_go_inventory as inventory


class GoInventoryTests(unittest.TestCase):
    def run_generate(self, *, extra_command=False):
        root = Path('/tmp/fes-inventory-test')
        calls = []

        def go_list(argv, *, cwd, env, text, capture_output):
            calls.append(env.copy())
            platform = cwd == root / 'platform'
            roles = inventory.PLATFORM_ROLES if platform else inventory.FOGCAST_ROLES
            if '-deps' in argv:
                if platform:
                    package = 'github.com/DeanoC/FogCast/appliance/softwarecontrol'
                    path = root / 'sources/FogCast/appliance/softwarecontrol'
                else:
                    package = 'github.com/DeanoC/FogCast/ui/rooms'
                    path = root / 'sources/FogCast/ui/rooms'
                output = f'{package}\t{path}\texamples/a.md\texamples\n'
            else:
                prefix = ('github.com/DeanoC/fes/platform/cmd/' if platform else
                          'github.com/DeanoC/FogCast/cmd/')
                dependency = ('github.com/DeanoC/FogCast/appliance/softwarecontrol' if platform else
                              'github.com/DeanoC/FogCast/ui/rooms')
                output = ''.join(f'{prefix}{name} {dependency}\n' for name in roles)
                if extra_command and not platform:
                    output += f'{prefix}unknown {dependency}\n'
            return subprocess.CompletedProcess(argv, 0, output, '')

        with patch.object(inventory.subprocess, 'run', side_effect=go_list):
            result = inventory.generate(root)
        return result, calls

    def test_targets_and_embeds_are_recorded_for_both_modules(self):
        result, calls = self.run_generate()
        kit = result['commands']['mister-agent']['graphs']
        self.assertEqual({t for group in kit for t in group['targets']},
                         {target[0] for target in inventory.TARGETS})
        self.assertIn('sources/FogCast/ui/rooms/examples/a.md', kit[0]['embedded'])
        self.assertIn('sources/FogCast/ui/rooms/examples', kit[0]['embed_patterns'])
        platform = result['commands']['fes-kit-software']['graphs'][0]
        self.assertEqual(platform['targets'], ['linux-armv7'])
        self.assertIn('sources/FogCast/appliance/softwarecontrol/examples/a.md', platform['embedded'])
        self.assertTrue(all(env.get('GOPROXY') == os.environ.get('GOPROXY') for env in calls))

    def test_unknown_command_fails_closed(self):
        with self.assertRaisesRegex(ValueError, 'unknown or duplicate command'):
            self.run_generate(extra_command=True)

    def test_discovery_failure_is_not_an_empty_graph(self):
        failed = subprocess.CompletedProcess(['go', 'list'], 1, '', 'module lookup disabled\n')
        with patch.object(inventory.subprocess, 'run', return_value=failed):
            with self.assertRaises(subprocess.CalledProcessError):
                inventory.generate(Path('/tmp/fes-inventory-test'))

    def test_offline_proxy_is_preserved(self):
        with patch.dict(os.environ, {'GOPROXY': 'off'}):
            _, calls = self.run_generate()
        self.assertTrue(all(env['GOPROXY'] == 'off' for env in calls))


if __name__ == '__main__':
    unittest.main()
