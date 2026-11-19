#!/usr/bin/env python3
"""Carry this record's line numbers forward after the tree has moved.

    python3 .../carry_forward.py <anchor-checkout> [--apply]

`<anchor-checkout>` is a checkout of a commit at which `verify_census.py`
exits 0 -- the tree the record was last verified in. This script is run from
the tree the record is being carried to, whose copy of the record must still
be the anchor's, unedited.

Every asserted line -- each inventory row's primary line and each `path:line`
endpoint the checker extracts -- is carried on one rule: `git diff` between the
anchor's file and this tree's file says the line was not touched, and the line
it lands on is byte-identical to the anchor's. That is a proof per line, not a
search: a text search cannot tell five identical lines apart, and re-recording
fingerprints over whatever now sits on an old number pins the wrong line while
the checker exits 0. The 16 November refresh did the second and left 124
citations on other constructs (see README.md, "The 18 November refresh").

A line that fails the rule is printed and left alone: it was edited, deleted,
or its file is gone, and a person has to read it.

With `--apply` the fingerprint file is carried too: each carried endpoint
keeps the digest it had, under its new line number, and an endpoint that did
not carry gets no entry. So `verify_census.py`, run straight afterwards,
reports exactly the lines left to settle, and re-recording once they are
settled can only add entries for them. `git diff` of the fingerprint file then
shows every digest this refresh introduced.

Exit status is 0 when every asserted line carried, 1 otherwise. A fault in the
line mapping cannot move a citation onto the wrong line: the byte comparison
rejects it, and the line is listed instead.
"""
import importlib.util
import json
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, '..', '..', '..'))
RECORD = os.path.relpath(HERE, ROOT)
ROW = re.compile(r'^(\| `([^`]+)` \| )(\d+)( \|)')
HUNK = re.compile(r'^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@', re.M)


class Tree:
    """One checkout, read through its own copy of the checker."""

    def __init__(self, root, name):
        self.root = os.path.realpath(root)
        path = os.path.join(self.root, RECORD, 'verify_census.py')
        spec = importlib.util.spec_from_file_location(name, path)
        self.checker = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.checker)

    def relative(self, cited):
        target = self.checker.resolve(cited)
        return os.path.relpath(target, self.root) if target else None

    def lines(self, relative):
        return self.checker.lines_of(os.path.join(self.root, relative))

    def record(self, doc):
        return self.lines(os.path.join(RECORD, doc))


def untouched_line_map(old_path, new_path):
    """old line -> new line for the lines `git diff -U0` leaves outside every hunk."""
    diff = subprocess.run(
        ['git', 'diff', '--no-index', '-U0', '--no-color', old_path, new_path],
        capture_output=True, text=True).stdout
    touched, shifts = set(), []
    for hunk in HUNK.finditer(diff):
        # A hunk header omits a count of one: `@@ -5 +5,2 @@`.
        old, old_count = int(hunk.group(1)), int(hunk.group(2) or 1)
        new, new_count = int(hunk.group(3)), int(hunk.group(4) or 1)
        touched.update(range(old, old + old_count))
        # With a zero count git names the line *before* the change.
        after_old = old + old_count if old_count else old + 1
        after_new = new + new_count if new_count else new + 1
        shifts.append((after_old, after_new - after_old))

    def carried(number):
        if number in touched:
            return None
        offset = 0
        for start, delta in shifts:
            if number >= start:
                offset = delta
        return number + offset
    return carried


class Carrier:
    def __init__(self, anchor, head):
        self.anchor, self.head = anchor, head
        self.maps = {}

    def carry(self, cited, number):
        """(new line, None) when the line carried, else (None, why)."""
        relative = self.anchor.relative(cited)
        if relative is None:
            return None, 'does not resolve in the anchor'
        here = self.head.relative(cited)
        if here is None:
            return None, f'names {relative}, which is gone or no longer resolves here'
        if here != relative:
            return None, f'is {relative} in the anchor and {here} here'
        old = self.anchor.lines(relative)
        if not 1 <= number <= len(old):
            return None, 'is past the end of the file in the anchor'
        if relative not in self.maps:
            self.maps[relative] = untouched_line_map(
                os.path.join(self.anchor.root, relative),
                os.path.join(self.head.root, relative))
        new = self.head.lines(relative)
        moved = self.maps[relative](number)
        if moved is None or not 1 <= moved <= len(new) or new[moved - 1] != old[number - 1]:
            return None, f'was edited or removed; it held {old[number - 1].strip()[:70]!r}'
        return moved, None


def asserted_numbers(checker, line):
    """[(start, end, cited path, number)] for each endpoint `line` asserts.

    The spans are found by asking the checker: a number is an asserted
    endpoint when replacing just that number changes exactly that endpoint in
    `citations_of`. Re-implementing the extraction here would only locate what
    this script believes a citation is, and the masking of stale numbers in
    correction clauses is the part that has been got wrong before.
    """
    def endpoints(text):
        return [(cited, number)
                for _, cited, numbers in checker.citations_of(text.replace('\\|', '|'))
                for number in numbers]

    asserted = endpoints(line)
    located = {}
    for token in re.finditer(r'\d+', line):
        probe = 7000000 + token.start()
        got = endpoints(line[:token.start()] + str(probe) + line[token.end():])
        if len(got) != len(asserted):
            continue
        changed = [i for i, (was, now) in enumerate(zip(asserted, got)) if was != now]
        if (len(changed) == 1 and got[changed[0]] == (asserted[changed[0]][0], probe)
                and int(token.group(0)) == asserted[changed[0]][1]):
            located[changed[0]] = (token.start(), token.end(), *asserted[changed[0]])
    if sorted(located) != list(range(len(asserted))):
        return None
    return [located[i] for i in range(len(asserted))]


def main():
    arguments = [a for a in sys.argv[1:] if a != '--apply']
    if len(arguments) != 1:
        sys.exit(__doc__)
    anchor, head = Tree(arguments[0], 'anchor_checker'), Tree(ROOT, 'head_checker')
    if anchor.root == head.root:
        sys.exit('the anchor must be a different checkout from this tree')
    verified = subprocess.run(
        [sys.executable, os.path.join(anchor.root, RECORD, 'verify_census.py')],
        capture_output=True, text=True)
    if verified.returncode != 0:
        sys.exit('the record does not verify in the anchor checkout, so it cannot '
                 'say what each line held:\n' + verified.stdout[:400])
    carrier = Carrier(anchor, head)
    moved = same = 0
    unresolved = []
    with open(anchor.checker.FINGERPRINTS) as handle:
        digests = json.load(handle)
    carried = {}
    for doc in head.checker.DOCS:
        lines = head.record(doc)
        if lines != anchor.record(doc):
            sys.exit(f'{doc} here differs from the anchor\'s copy; carry forward '
                     'before editing the record by hand')
        out = []
        for row, line in enumerate(lines, 1):
            spans = asserted_numbers(head.checker, line)
            if spans is None:
                unresolved.append(f'{doc}:{row}: could not locate every cited number in this line')
                spans = []
            cited_endpoints = len(spans)
            primary = ROW.match(line) if doc == 'census-tables.md' else None
            if primary:
                spans.append((primary.start(3), primary.end(3), primary.group(2), int(primary.group(3))))
            edits = []
            for index, (start, end, cited, number) in enumerate(spans):
                new, why = carrier.carry(cited, number)
                if new is not None and index < cited_endpoints:
                    # A primary line has no fingerprint; its `Matched` cell pins it.
                    carried[f'{cited}:{new}'] = digests[f'{cited}:{number}']
                if new is None:
                    unresolved.append(f'{doc}:{row}: {cited}:{number} {why}')
                elif new != number:
                    edits.append((start, end, str(new)))
                    moved += 1
                else:
                    same += 1
            for start, end, text in sorted(edits, reverse=True):
                line = line[:start] + text + line[end:]
            out.append(line)
        if '--apply' in sys.argv[1:]:
            with open(os.path.join(head.root, RECORD, doc), 'w') as handle:
                handle.write('\n'.join(out))
    if '--apply' in sys.argv[1:]:
        with open(head.checker.FINGERPRINTS, 'w') as handle:
            json.dump(carried, handle, indent=1, sort_keys=True)
            handle.write('\n')
    print(f'{same} asserted lines unmoved, {moved} '
          f'{"moved" if "--apply" in sys.argv[1:] else "to move (run with --apply)"}, '
          f'{len(unresolved)} to settle by hand')
    for entry in unresolved:
        print(f'  {entry}')
    return 1 if unresolved else 0


if __name__ == '__main__':
    sys.exit(main())
