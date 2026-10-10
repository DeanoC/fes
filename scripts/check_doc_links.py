"""Fail when a tracked Markdown file links to a repository path that does not exist.

Only relative file targets are checked; URLs and in-page anchors are not.
Fenced code blocks are ignored. CI runs this for every change, including
documentation-only changes that select no test lane.
"""
import argparse
from pathlib import Path
import re
import subprocess

LINK = re.compile(r'\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)')
FENCE = re.compile(r'^(```|~~~).*?^\1', re.S | re.M)
SCHEME = re.compile(r'^[A-Za-z][A-Za-z0-9+.-]*:')
# Byte-pinned copies whose links resolve at their source location.
VERBATIM_COPIES = frozenset({
    # mister-packages docs/media-stream.md; sha256 asserted by test_build_fes_sms.
    'sources/misteross/docs/contracts/media-stream-1.0.md',
})


def broken_links(root):
    files = subprocess.check_output(['git', '-C', str(root), 'ls-files', '-z', '*.md'])
    broken = []
    for name in sorted(p.decode() for p in files.split(b'\0') if p):
        path = root / name
        if name in VERBATIM_COPIES or not path.is_file():
            continue
        text = path.read_text(errors='replace')
        # Blank fenced blocks but keep their newlines so line numbers stay exact.
        text = FENCE.sub(lambda m: '\n' * m.group(0).count('\n'), text)
        for match in LINK.finditer(text):
            target = match.group(1).strip('<>')
            if SCHEME.match(target) or target.startswith('#'):
                continue
            relative = target.split('#', 1)[0].split('?', 1)[0]
            if not relative:
                continue
            base = root if relative.startswith('/') else path.parent
            if not (base / relative.lstrip('/')).exists():
                line = text.count('\n', 0, match.start()) + 1
                broken.append(f'{name}:{line}: {target}')
    return broken


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path.cwd())
    args = parser.parse_args()
    broken = broken_links(args.root.resolve())
    if broken:
        parser.exit(1, 'broken relative Markdown links:\n' + '\n'.join(broken) + '\n')
    print('doc links: ok')


if __name__ == '__main__':
    main()
