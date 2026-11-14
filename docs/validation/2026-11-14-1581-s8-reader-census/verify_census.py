#!/usr/bin/env python3
"""Re-check this census record against the tree it is read in.

Two checks, both mechanical, and together they are what the record's
"Verification" paragraph claims:

1. every inventory row's primary line still holds the `Matched` text recorded
   for it (a row whose `Matched` cell was abbreviated with an ellipsis is
   matched up to the ellipsis, which is all the text the sweep kept);
2. every `path:line` citation written inside a `What` cell, and inside the
   prose of README.md and de25.md, resolves to a real file and still holds the
   text it held when the record was written.

Check 2's content half works from `citation-fingerprints.json` beside this
script: one entry per cited line, holding that line's text. Without it a
citation could only be checked for existing and being non-blank, and a base
edit that shifted a secondary reference onto another non-blank line would pass
-- the very defect two earlier revisions of this record shipped (#1829 review).
Regenerate the file deliberately, never to make a failure go away:

    python3 .../verify_census.py --record-fingerprints

What the fingerprints prove and what they do not: they pin what each cited
line said when they were taken, so any later drift in the tree is reported.
They are not a retroactive claim that every citation was apt when written.

A range may legitimately cite a few lines from inside a longer call -- the
lines that name a guarded file, not the whole statement -- so only the
endpoints are checked, not whether the span is a complete statement. A
citation the record quotes in order to call it *wrong* is written in prose
("lines 333-337 of x.py"), not as `x.py:333-337`, so that it asserts no
location and this check does not read it as one.

Citations that deliberately quote a *stale* number are skipped, and they are
the ones to the left of a `->` in a clause that records a correction (`:1230
-> :1707`). Those numbers are wrong on purpose: the point of the clause is
that the design record carried them. The number to the right of the arrow is
checked like any other.

Check 1 is the one that matters: a line number can stay in range while
pointing at the wrong line, and that is exactly how two earlier revisions of
this record went wrong. Run from the repo root:

    python3 docs/validation/2026-11-14-1581-s8-reader-census/verify_census.py

Exit status is 0 when both checks pass, 1 otherwise. Paths are resolved
against the repo root first, then against sources/misteross, then by basename
if that is unique under sources/misteross -- a bare `Makefile:123` must not
silently resolve to the repo-root Makefile.
"""
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, '..', '..', '..'))
M = os.path.join(ROOT, 'sources', 'misteross')
DOCS = ('census-tables.md', 'de25.md', 'README.md')
FINGERPRINTS = os.path.join(HERE, 'citation-fingerprints.json')

UNESCAPED_PIPE = re.compile(r'(?<!\\)\|')
CITATION = re.compile(
    r'((?:[A-Za-z0-9_][A-Za-z0-9_./-]*\.(?:py|v|sv|cpp|h|svh|md|json|sh|files|txt|lock|yml|toml|tcl))'
    r'|(?:[A-Za-z0-9_][A-Za-z0-9_./-]*/)?Makefile)'
    r':(\d+)(?:-(\d+))?((?:,\d+(?:-\d+)?)*)')

_index = {}
for dirpath, dirnames, filenames in os.walk(M):
    dirnames[:] = [d for d in dirnames if d not in ('build', 'out', '.git')]
    for name in filenames:
        _index.setdefault(name, []).append(os.path.join(dirpath, name))


# Roots a citation may be written relative to. de25.md spells its paths
# relative to boards/de25nano, and a few cells name a core README as
# `fes-sms/README.md`, so both of those roots are tried before falling back to
# a unique basename.
ROOTS = (
    ROOT,
    M,
    os.path.join(M, 'boards', 'de25nano'),
    os.path.join(M, 'cores'),
    os.path.join(M, 'scripts'),
)


def resolve(path):
    # `M/` is this record's spelling of sources/misteross, so an `M/` path must
    # resolve there and nowhere else. Falling through to the repository root
    # made `M/AGENTS.md:82` resolve to the root AGENTS.md and pass, which is
    # the very substitution the roots list exists to prevent (DeanoC/fes#1839).
    if path.startswith('M/'):
        candidate = os.path.join(M, path[2:])
        return candidate if os.path.isfile(candidate) else None
    if os.sep not in path and os.path.isfile(os.path.join(ROOT, path)) \
            and os.path.isfile(os.path.join(M, path)):
        # Two files of this name, one at each root: `Makefile:123` cannot be
        # resolved without being told which, so it is reported, not guessed.
        return None
    for root in ROOTS:
        candidate = os.path.join(root, path)
        if os.path.isfile(candidate):
            return candidate
    hits = _index.get(os.path.basename(path), [])
    return hits[0] if len(hits) == 1 else None


def lines_of(path):
    with open(path, errors='replace') as handle:
        return handle.read().split('\n')


def rows():
    path = os.path.join(HERE, 'census-tables.md')
    for number, line in enumerate(lines_of(path), 1):
        if not line.startswith('| `'):
            continue
        cells = [c.strip() for c in UNESCAPED_PIPE.split(line.strip())[1:-1]]
        if len(cells) < 5 or cells[1] in ('---', 'Line'):
            continue
        yield number, cells


def check_primary():
    problems = []
    total = 0
    for row, cells in rows():
        total += 1
        path, number = cells[0].strip('`'), cells[1]
        matched = cells[3].strip('`').strip().replace('\\|', '|')
        target = resolve(path)
        if target is None:
            problems.append(f'census-tables.md:{row}: {path} does not resolve')
            continue
        if not number.isdigit():
            problems.append(f'census-tables.md:{row}: line {number!r} is not a number')
            continue
        body = lines_of(target)
        n = int(number)
        if not 1 <= n <= len(body):
            problems.append(
                f'census-tables.md:{row}: {path}:{n} past end ({len(body)} lines)')
            continue
        wanted = re.split(r'\.\.\.|…', matched)[0].strip()
        if wanted and wanted not in body[n - 1]:
            problems.append(
                f'census-tables.md:{row}: {path}:{n} holds {body[n - 1].strip()[:60]!r}, '
                f'not the recorded {matched[:60]!r}')
    return total, problems


def current_only(text):
    """Blank out the stale side of every `old -> new` correction clause.

    A clause is delimited by `;` or by the end of the cell. Where it holds an
    arrow, everything before the last arrow is a number the record is
    reporting as wrong, so it must not be checked against the tree.
    """
    out = []
    for clause in re.split(r'(;)', text):
        if '->' in clause:
            head, _, tail = clause.rpartition('->')
            clause = ' ' * len(head + '->') + tail
        out.append(clause)
    return ''.join(out)


def load_fingerprints():
    try:
        with open(FINGERPRINTS) as handle:
            return json.load(handle)
    except FileNotFoundError:
        return None


def check_citations(record=False):
    """Check every citation, and its content where a fingerprint exists."""
    prints = {} if record else load_fingerprints()
    problems = []
    citations = endpoints = checked = 0
    for doc in DOCS:
        path = os.path.join(HERE, doc)
        for row, line in enumerate(lines_of(path), 1):
            for match in CITATION.finditer(current_only(line.replace('\\|', '|'))):
                cited = match.group(1)
                citations += 1
                target = resolve(cited)
                if target is None:
                    problems.append(f'{doc}:{row}: {match.group(0)} does not resolve')
                    continue
                body = lines_of(target)
                # A citation may be a comma list, `foo.py:12,34-36,40`. Every
                # number after the first leans on the same path, and reading
                # only the first is how two stale numbers in this record's own
                # text survived three review rounds (DeanoC/fes#1839).
                tail = [n for part in (match.group(4) or '').split(',') if part
                        for n in part.split('-')]
                for group in (match.group(2), match.group(3), *tail):
                    if group is None:
                        continue
                    endpoints += 1
                    n = int(group)
                    if not 1 <= n <= len(body):
                        problems.append(
                            f'{doc}:{row}: {cited}:{n} past end ({len(body)} lines)')
                        continue
                    text = body[n - 1].strip()
                    if not text:
                        problems.append(f'{doc}:{row}: {cited}:{n} is blank')
                        continue
                    key = f'{cited}:{n}'
                    if record:
                        prints[key] = text[:200]
                    elif prints is not None and key in prints:
                        checked += 1
                        if text[:200] != prints[key]:
                            problems.append(
                                f'{doc}:{row}: {cited}:{n} now holds {text[:60]!r}, '
                                f'not the {prints[key][:60]!r} it was cited for')
    if record:
        with open(FINGERPRINTS, 'w') as handle:
            json.dump(prints, handle, indent=1, sort_keys=True)
            handle.write('\n')
        print(f'recorded {len(prints)} citation fingerprints')
    return citations, endpoints, checked, problems


def main():
    record = '--record-fingerprints' in sys.argv[1:]
    total, primary = check_primary()
    citations, endpoints, checked, cited = check_citations(record=record)
    print(f'{total} inventory rows, {len(primary)} primary-line problems')
    note = (f', {checked} content-checked against citation-fingerprints.json'
            if checked else ', content NOT checked (no fingerprint file)')
    print(f'{citations} path:line citations ({endpoints} line endpoints'
          f'{note}), {len(cited)} problems')
    for problem in primary + cited:
        print(f'  {problem}')
    return 1 if primary or cited else 0


if __name__ == '__main__':
    sys.exit(main())
