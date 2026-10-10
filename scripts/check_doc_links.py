"""Fail when a tracked Markdown file links to a repository path that does not exist.

Inline link destinations (including balanced parentheses) and reference
definitions are checked when they are relative file targets; URLs and in-page
anchors are not. Fenced code blocks are ignored. CI runs this for every change,
including documentation-only changes that select no test lane.
"""
import argparse
from pathlib import Path
import re
import subprocess

# One level of balanced parentheses is allowed inside a destination.
LINK = re.compile(r'\[[^\]]*\]\(\s*((?:[^()\s]|\([^()\s]*\))+)(?:\s+"[^"]*")?\s*\)')
REFERENCE = re.compile(r'^ {0,3}\[[^\]]+\]:\s*(\S+)', re.M)
FENCE = re.compile(r'^\s*(`{3,}|~{3,})')
SCHEME = re.compile(r'^[A-Za-z][A-Za-z0-9+.-]*:')
# Byte-pinned copies whose links resolve at their source location.
VERBATIM_COPIES = frozenset({
    # mister-packages docs/media-stream.md; sha256 asserted by test_build_fes_sms.
    'sources/misteross/docs/contracts/media-stream-1.0.md',
})


def without_fences(text):
    """Blank fenced code but keep newlines so line numbers stay exact.

    A fence may be indented (list items) and closes only on the same character
    repeated at least as many times with nothing else on the line.
    """
    lines = text.split('\n')
    opener = None
    for index, line in enumerate(lines):
        match = FENCE.match(line)
        if opener is None:
            if match:
                opener = match.group(1)
                lines[index] = ''
        else:
            if (match and match.group(1)[0] == opener[0]
                    and len(match.group(1)) >= len(opener)
                    and not line.strip()[len(match.group(1)):].strip()):
                opener = None
            lines[index] = ''
    return '\n'.join(lines)


def broken_links(root):
    files = subprocess.check_output(['git', '-C', str(root), 'ls-files', '-z', '*.md'])
    broken = []
    for name in sorted(p.decode() for p in files.split(b'\0') if p):
        path = root / name
        if name in VERBATIM_COPIES or not path.is_file():
            continue
        text = without_fences(path.read_text(errors='replace'))
        matches = [*LINK.finditer(text), *REFERENCE.finditer(text)]
        for match in sorted(matches, key=lambda m: m.start()):
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
