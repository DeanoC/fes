"""Represent equivalent GitHub SSH clone origins as package HTTPS provenance.

No Git configuration is changed. Existing HTTPS strings retain their exact
bytes; unsupported SSH hosts, users and port spellings are not relabelled.
"""
import re


def canonical_repository(origin: str) -> str:
    match = re.fullmatch(
        r'(?:git@(?i:github\.com):|ssh://git@(?i:github\.com)/)'
        r'([A-Za-z0-9_-]+)/([A-Za-z0-9_.-]+)', origin)
    if match and match[2] not in ('.', '..'):
        return 'https://github.com/' + match[1] + '/' + match[2]
    if origin.startswith(('ssh://', 'git+ssh://')) or re.match(r'[^/\s:]+:(?!//)', origin):
        raise ValueError('unsupported SSH source origin; use an equivalent HTTPS origin or GitHub git@github.com:owner/repository')
    return origin
