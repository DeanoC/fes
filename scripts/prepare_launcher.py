#!/usr/bin/env python3
"""Prepare private host/kit launcher credentials; never print their values."""
import argparse
import fcntl
import ipaddress
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


def prepare(host_config, address):
    host_config = Path(host_config)
    descriptor = os.open(host_config.parent / '.launcher-setup.lock',
                         os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX)
        return _prepare(host_config, address)
    finally:
        os.close(descriptor)


def _prepare(host_config, address):
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
    for path, value in ((host, {'listen': '0.0.0.0:8789', 'token': token, 'target_id': target_id}),
                        (kit, {'api': f'http://{address}:8789', 'token': token, 'target_id': target_id})):
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


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--host-address', required=True)
    parser.add_argument('--host-config', type=Path, default=media.DEFAULT_HOST_CONFIG)
    args = parser.parse_args()
    try:
        prepare(args.host_config, args.host_address)
        print('Prepared private launcher-host.json and launcher.json beside the host configuration.')
    except (ValueError, OSError) as error:
        parser.exit(1, f'launcher setup: {error}\n')
