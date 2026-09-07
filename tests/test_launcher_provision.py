import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import media
import prepare_launcher

ID = '73dc9f5f-1a12-4a95-a820-a9b4e600769a'

class LauncherProvisionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.host = self.root / 'config.toml'
        media.write_private(self.host, f'token="agent-secret"\ntarget_id="{ID}"\n'.encode())
        self.scratch = self.root / 'scratch'
        self.scratch.mkdir()

    def test_generated_pair_private_stable_and_matching(self):
        host, kit = prepare_launcher.prepare(self.host, '192.168.10.2')
        h, k = json.loads(host.read_text()), json.loads(kit.read_text())
        self.assertEqual(h['token'], k['token'])
        self.assertGreaterEqual(len(h['token']), 32)
        self.assertEqual(h['target_id'], ID)
        self.assertEqual(k['api'], 'http://192.168.10.2:8789')
        for path in (host, kit): self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        prepare_launcher.prepare(self.host, 'host.example')
        self.assertEqual(json.loads(kit.read_text())['token'], k['token'])

    def test_reject_missing_identity_and_malformed_endpoint(self):
        self.host.write_text('token="agent-secret"\n')
        with self.assertRaises(ValueError): prepare_launcher.prepare(self.host, 'host')
        for endpoint in ('http://host', 'host/path', 'host:8789', '', 'foo\nbar'):
            with self.assertRaises(ValueError): prepare_launcher.validate_address(endpoint)

    def test_snapshot_identity_and_ci_guards(self):
        _, kit = prepare_launcher.prepare(self.host, 'host.example')
        agent, _ = media.generate_agent_config(self.host, self.scratch)
        with patch.dict(os.environ, {'FES_HOST_CONFIG': str(self.host), 'CI': '', 'FES_UNPROVISIONED': ''}):
            snapshot, sha = media.resolve_launcher_config(agent, self.scratch, auto=True)
            self.assertEqual(snapshot.read_bytes(), kit.read_bytes())
            self.assertEqual(sha, media.digest(kit))
            for flag in ('CI', 'FES_UNPROVISIONED'):
                with patch.dict(os.environ, {flag: '1'}):
                    self.assertEqual(media.resolve_launcher_config(agent, self.scratch, auto=True), (None, None))
            snapshot.unlink()
            value = json.loads(kit.read_text()); value['target_id'] = 'different'
            kit.write_text(json.dumps(value))
            with self.assertRaises(ValueError): media.resolve_launcher_config(agent, self.scratch, auto=True)

    def test_existing_incomplete_pair_and_public_secret_rejected(self):
        host, kit = prepare_launcher.prepare(self.host, 'host')
        kit.chmod(0o644)
        with self.assertRaises(ValueError): prepare_launcher.prepare(self.host, 'host')
        kit.unlink()
        with self.assertRaises(ValueError): prepare_launcher.prepare(self.host, 'host')

    def test_existing_pair_rejects_oversized_and_agent_tokens_without_writes(self):
        host, kit = prepare_launcher.prepare(self.host, 'host')
        agent_token = 'a' * 43
        self.host.write_text(f'token="{agent_token}"\ntarget_id="{ID}"\n')
        for token in ('z' * 257, agent_token):
            with self.subTest(token_length=len(token)):
                for path in (host, kit):
                    value = json.loads(path.read_text()); value['token'] = token
                    path.write_text(json.dumps(value))
                before = [path.read_bytes() for path in (host, kit)]
                with self.assertRaises(ValueError): prepare_launcher.prepare(self.host, 'host')
                self.assertEqual([path.read_bytes() for path in (host, kit)], before)

    def test_snapshot_rejects_oversized_and_agent_tokens_before_publishing(self):
        _, kit = prepare_launcher.prepare(self.host, 'host')
        agent_token = 'a' * 43
        self.host.write_text(f'token="{agent_token}"\ntarget_id="{ID}"\n')
        agent, _ = media.generate_agent_config(self.host, self.scratch)
        with patch.dict(os.environ, {'FES_HOST_CONFIG': str(self.host), 'CI': '', 'FES_UNPROVISIONED': ''}):
            for token in ('z' * 257, agent_token):
                with self.subTest(token_length=len(token)):
                    value = json.loads(kit.read_text()); value['token'] = token
                    kit.write_text(json.dumps(value))
                    with self.assertRaises(ValueError): media.resolve_launcher_config(agent, self.scratch, auto=True)
                    self.assertFalse((self.scratch / 'launcher.json').exists())
