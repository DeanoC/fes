#!/usr/bin/env python3
"""Prepare private host/kit launcher credentials; never print their values."""
import argparse
import fcntl
import ipaddress
import importlib.util
import json
import re
import secrets
import os
import tempfile
import tomllib
from pathlib import Path
import media


def validate_address(address):
    try:
        ipaddress.IPv4Address(address)
    except ValueError:
        if not isinstance(address, str) or len(address) > 253 or not all(
                re.fullmatch(r'[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?', part)
                for part in address.split('.')):
            raise ValueError('host address must be an explicit IPv4 address or hostname') from None
    return address


def prepare(host_config, address, port=8789):
    validate_address(address)
    if type(port) is not int or not 1 <= port <= 65535:
        raise ValueError('host port must be between 1 and 65535')
    host_config = Path(host_config)
    descriptor = os.open(host_config.parent / '.launcher-setup.lock',
                         os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX)
        return _prepare(host_config, address, port)
    finally:
        os.close(descriptor)


def _prepare(host_config, address, port):
    address = validate_address(address)
    host_config = Path(host_config)
    try:
        raw = tomllib.loads(media._read_private_config(host_config, 'host configuration').decode())
        agent_token, target_id = media._host_provisioning(raw)
    except (ValueError, UnicodeError):
        raise ValueError('host configuration has no unambiguous selected target') from None
    if not target_id:
        raise ValueError('prepare the selected target identity before preparing its launcher')
    host = host_config.parent / 'launcher-host.json'
    kit = host_config.parent / 'launcher.json'
    # Existing credentials must be consistent; rerunning setup preserves the secret.
    if host.exists() or kit.exists():
        try:
            h = json.loads(media._read_private_config(host, 'launcher host configuration'))
            k = json.loads(media._read_private_config(kit, 'launcher kit configuration'))
            if (set(h) != {'listen', 'token', 'target_id'} or set(k) != {'api', 'token', 'target_id'}
                    or h['target_id'] != target_id or k['target_id'] != target_id
                    or h['token'] != k['token']):
                raise ValueError()
            token = h['token']
        except (ValueError, OSError, KeyError, TypeError):
            raise ValueError('existing launcher files are incomplete or inconsistent; restore the matching pair first') from None
    else:
        token = secrets.token_urlsafe(32)
    media.validate_launcher_token(token, agent_token)
    for path, value in ((host, {'listen': f'0.0.0.0:{port}', 'token': token, 'target_id': target_id}),
                        (kit, {'api': f'http://{address}:{port}', 'token': token, 'target_id': target_id})):
        fd, staged = tempfile.mkstemp(prefix='.launcher-', dir=path.parent)
        try:
            with os.fdopen(fd, 'wb') as stream:
                stream.write((json.dumps(value, sort_keys=True) + '\n').encode())
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(staged, path)
        finally:
            if os.path.exists(staged):
                os.unlink(staged)
    return host, kit


def apply(host_config, kit_config):
    """Save a prepared pairing through the selected target's normal kit lease."""
    raw = tomllib.loads(media._read_private_config(host_config, 'host configuration').decode())
    agent_token, target_id = media._host_provisioning(raw)
    targets = [target for target in raw.get('targets', [])
               if target.get('name') == raw.get('selected_target')]
    if len(targets) != 1 or not target_id:
        raise ValueError('live setup requires a selected target address and identity')
    pairing = json.loads(media._read_private_config(kit_config, 'launcher kit configuration'))
    if pairing.get('target_id') != target_id:
        raise ValueError('prepared launcher identity differs from the selected target')
    media.validate_launcher_token(pairing.get('token'), agent_token)
    path = Path(__file__).resolve().parents[1] / 'sources/misteross/scripts/kit.py'
    spec = importlib.util.spec_from_file_location('fes_launcher_kit', path)
    kit = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(kit)
    try:
        client = kit.Client(targets[0]['address'], agent_token)
        health = client.request('/v1/health')
        if health.get('target_id') != target_id or health.get('ready') is not True:
            raise ValueError('selected target identity is unverified or not ready')
        session = kit.Session(client, 'launcher-setup', 'update launcher pairing')
        try:
            session.claim(wait=0)
            current = client.request('/v1/health')
            status = client.request('/v1/status')
            if (current.get('target_id') != target_id or current.get('ready') is not True
                    or current.get('boot_id') != health.get('boot_id') or status.get('state') != 'idle'):
                raise ValueError('live setup requires the same ready idle target')
            if session.failed.is_set() or not session.token:
                raise ValueError('launcher setup lost kit ownership')
            result = client.request('/v1/launcher/pairing', pairing, token=session.token)
            if (result.get('target_id') != target_id or result.get('api') != pairing.get('api')
                    or result.get('restart_required') is not True
                    or not re.fullmatch('[0-9a-f]{64}', result.get('sha256', ''))):
                raise ValueError('target did not confirm the prepared pairing; inspect before retrying')
            return {key: result[key] for key in ('api', 'target_id', 'sha256', 'restart_required')}
        finally:
            session.close()
    except kit.KitError as error:
        raise ValueError(f'live pairing failed ({error.code or error.status or "transport error"}); inspect before retrying') from None


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--host-address', required=True)
    parser.add_argument('--host-config', type=Path, default=media.DEFAULT_HOST_CONFIG)
    parser.add_argument('--host-port', type=int, default=8789)
    parser.add_argument('--apply', action='store_true', help='save pairing on the ready idle selected kit; reboot separately')
    args = parser.parse_args()
    try:
        _, kit_config = prepare(args.host_config, args.host_address, args.host_port)
        print('Prepared private launcher-host.json and launcher.json beside the host configuration.')
        if args.apply:
            apply(args.host_config, kit_config)
            print('Saved launcher pairing on the selected kit. Reboot the kit to apply it.')
    except (ValueError, OSError) as error:
        parser.exit(1, f'launcher setup: {error}\n')
