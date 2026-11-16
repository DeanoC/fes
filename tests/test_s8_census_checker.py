"""Pin what each correction shape in the S8 reader census asserts.

`verify_census.py` masks the stale half of a `old -> new` correction so the
record's own wrong numbers are not checked against the tree. That masking is
the one part of the script that can *remove* coverage, and it has done so three
times (DeanoC/fes#1829 review): once by blanking everything before the last
arrow, which took the path its destinations leaned on; once by keeping only the
last arrow in a clause, which masked the first correction's destination; and
once, before either, by reading only the first number of a comma list.

Every one of those exited 0 while checking fewer citations than it claimed,
which is why the shapes are pinned here rather than left to the totals the
script prints. A masking change can hide one destination while exposing one
stale source, leaving the citation and endpoint counts unchanged -- and because
fingerprints are keyed by `path:line`, that stale endpoint can be fingerprinted
by another citation and pass the content check too. Only the extracted set
distinguishes the two, so these fixtures assert the set.

They read the extraction through `citations_of`, the function the check itself
uses. A fixture with its own copy of the extraction would pin only itself.
"""
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
import hashlib
import importlib.util
import json
import os
import re
import unittest

RECORD = (Path(__file__).resolve().parents[1]
          / "docs/validation/2026-11-14-1581-s8-reader-census")
_spec = importlib.util.spec_from_file_location(
    "verify_census", RECORD / "verify_census.py")
verify_census = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(verify_census)


def asserted(text):
    """The set of `path:line` endpoints one line of the record asserts."""
    return {f"{path}:{number}"
            for _, path, numbers in verify_census.citations_of(text)
            for number in numbers}


class CorrectionShapeTests(unittest.TestCase):
    def test_plain_citation_is_asserted(self):
        self.assertEqual(asserted("see build.py:12 for the guard"),
                         {"build.py:12"})

    def test_range_asserts_its_endpoints(self):
        self.assertEqual(asserted("build.py:12-14"),
                         {"build.py:12", "build.py:14"})

    def test_comma_list_asserts_every_number(self):
        self.assertEqual(asserted("build.py:12,34-36,40"),
                         {"build.py:12", "build.py:34", "build.py:36",
                          "build.py:40"})

    def test_correction_asserts_the_destination_only(self):
        self.assertEqual(asserted("build.py:10 -> :20"), {"build.py:20"})

    def test_destination_keeps_the_comma_continuation(self):
        self.assertEqual(asserted("build.py:387,401,440 -> :424,:438,:477"),
                         {"build.py:424", "build.py:438", "build.py:477"})

    def test_two_corrections_in_one_clause_both_count(self):
        # The b60791d04 regression: split on the *last* arrow, and the first
        # destination sat in the masked head. Six checklist locations went
        # unchecked.
        self.assertEqual(
            asserted("test_build_fes_coleco.py:387,401,440 -> :424,:438,:477 "
                     "and the FES_*_OSS cites :253-254,275,390 -> "
                     ":290,:291,:312,:427"),
            {"test_build_fes_coleco.py:424", "test_build_fes_coleco.py:438",
             "test_build_fes_coleco.py:477", "test_build_fes_coleco.py:290",
             "test_build_fes_coleco.py:291", "test_build_fes_coleco.py:312",
             "test_build_fes_coleco.py:427"})

    def test_path_carries_across_arrows(self):
        # Both destinations are in the zx81 test file; only the first is
        # written next to a path.
        self.assertEqual(
            asserted("test_build_fes_zx81.py:155 -> :68 "
                     "(and the QSF cite -> :256)"),
            {"test_build_fes_zx81.py:68", "test_build_fes_zx81.py:256"})

    def test_each_clause_corrects_on_its_own_path(self):
        self.assertEqual(asserted("a.py:1 -> :2; b.py:3 -> :4"),
                         {"a.py:2", "b.py:4"})

    def test_a_clause_without_an_arrow_is_untouched(self):
        self.assertEqual(asserted("a.py:1 is right; b.py:3 -> :4"),
                         {"a.py:1", "b.py:4"})

    def test_prose_locations_assert_nothing(self):
        # A citation the record quotes in order to call it wrong is written as
        # prose precisely so it asserts no location.
        self.assertEqual(asserted("lines 333-337 of build.py are stale"),
                         set())

    def test_prose_arrow_is_not_a_correction(self):
        # The record's own sentence reads "Verified mapping, hint -> real:".
        # Reading that as a correction made the mapping that follows it look
        # like a destination, so its stale source was asserted against the
        # tree. An arrow is a correction only with a number on both sides.
        self.assertEqual(
            asserted("Verified mapping, hint -> real: M/Makefile:625-630 "
                     "-> :633"),
            {"M/Makefile:633"})

    def test_arrow_without_numbers_leaves_the_clause_alone(self):
        self.assertEqual(asserted("hint -> real, as at a.py:1"), {"a.py:1"})

    def test_a_mapping_list_asserts_only_its_destinations(self):
        # census-tables.md:236 maps eighteen stale Makefile hints to their real
        # lines. Written without a path the destinations were extracted by
        # nothing, so the whole verified mapping was unchecked.
        self.assertEqual(
            asserted("M/Makefile:1048-1050 -> :1032,:1034 (two lines, not "
                     "three; line 1033 is a -Wno- line); M/Makefile:1739 "
                     "-> :1732"),
            {"M/Makefile:1032", "M/Makefile:1034", "M/Makefile:1732"})

    def test_destination_must_follow_its_arrow(self):
        # `shards.py:62-69 -> the assertion is at :69` was not read as a
        # correction at all, so the stale range was asserted beside the
        # corrected line and counted as current.
        self.assertEqual(
            asserted("test_coleco_sim_shards.py:62-69 -> :69, the assertion "
                     "rather than the whole range"),
            {"test_coleco_sim_shards.py:69"})

    def test_a_prose_delayed_destination_is_reported(self):
        # Widening the arrow to reach across prose would let any `->` mask the
        # citations before it, so the shape is reported, not guessed at.
        stray = "test_coleco_sim_shards.py:62-69 -> the assertion is at :69"
        self.assertEqual(list(verify_census.unreachable_destination(stray)),
                         [stray])
        self.assertEqual(
            asserted(stray),
            {"test_coleco_sim_shards.py:62", "test_coleco_sim_shards.py:69"},
            "the unfixable reading is why this shape has to be reported")

    def test_the_prose_arrow_clause_is_not_reported(self):
        self.assertEqual(
            list(verify_census.unreachable_destination(
                "Verified mapping, hint -> real: M/Makefile:625-630 -> :633")),
            [])

    def test_a_stale_source_is_never_asserted(self):
        # The failure this file exists to catch: a masking change that trades a
        # destination for a stale source leaves both totals unchanged, so only
        # the set shows it.
        shape = ("test_build_fes_sms.py:232 -> :269 and :103,117-119 -> "
                 ":140,:154-156")
        self.assertEqual(
            asserted(shape),
            {"test_build_fes_sms.py:269", "test_build_fes_sms.py:140",
             "test_build_fes_sms.py:154", "test_build_fes_sms.py:156"})
        for stale in ("test_build_fes_sms.py:232", "test_build_fes_sms.py:103",
                      "test_build_fes_sms.py:117",
                      "test_build_fes_sms.py:119"):
            self.assertNotIn(stale, asserted(shape))


class SectionTotalTests(unittest.TestCase):
    """The per-area counts inside census-tables.md, which nothing checked.

    Every `### ... -- N sites` heading states its own count, one area note
    states a count and a number of distinct files, and the sum has to be the
    row total the README states. The checker validated no total outside the
    README, so a section note read "23 sites in 19 scripts" over 24 rows in 15
    scripts and stayed that way through ten review rounds (DeanoC/fes#1829).
    """

    @classmethod
    def setUpClass(cls):
        cls.lines = verify_census.lines_of(str(RECORD / "census-tables.md"))
        heads = [(i, line) for i, line in enumerate(cls.lines)
                 if line.startswith('### ')]
        cls.sections = []
        for (start, head), (end, _) in zip(
                heads, heads[1:] + [(len(cls.lines), '')]):
            rows, paths = 0, []
            for line in cls.lines[start + 1:end]:
                if not line.startswith('| `'):
                    continue
                cells = [c.strip() for c in
                         verify_census.UNESCAPED_PIPE.split(line.strip())[1:-1]]
                if len(cells) >= 5 and cells[1] not in ('---', 'Line'):
                    rows += 1
                    paths.append(cells[0].strip('`'))
            cls.sections.append((start + 1, head, rows, len(set(paths))))

    def test_every_heading_states_its_row_count(self):
        self.assertTrue(self.sections, 'no sections found')
        for row, head, rows, _ in self.sections:
            claimed = re.search(r'—\s*(\d+)\s*sites', head)
            self.assertIsNotNone(
                claimed, f'census-tables.md:{row}: {head!r} states no count')
            self.assertEqual(int(claimed.group(1)), rows,
                             f'census-tables.md:{row}: {head!r} over {rows} rows')

    def test_the_sections_sum_to_the_row_total(self):
        total, _ = verify_census.check_primary()
        self.assertEqual(sum(s[2] for s in self.sections), total)

    def test_an_area_note_states_its_sites_and_files(self):
        found = 0
        for row, line in enumerate(self.lines, 1):
            note = re.search(r'(\d+) sites in (\d+) (?:scripts|files)', line)
            if not note:
                continue
            found += 1
            section = [s for s in self.sections if s[0] < row]
            self.assertTrue(section, f'census-tables.md:{row}: note before any heading')
            _, head, rows, files = section[-1]
            self.assertEqual((int(note.group(1)), int(note.group(2))), (rows, files),
                             f'census-tables.md:{row}: note under {head!r}')
        self.assertTrue(found, 'no area note found; drop this test if the form changed')


class RangeTests(unittest.TestCase):
    """A cited range must not end before it starts.

    Re-pointing after a base move is per number, so moving a range's start and
    leaving its end turns `:349-356` into `:369-356`. It still resolves, still
    points at real lines, and both endpoints are content-checked, so nothing
    else reports it; I shipped exactly that for one commit (DeanoC/fes#1829).
    """

    def claim(self, text):
        written, _, numbers = list(verify_census.citations_of(text))[0]
        return verify_census.backwards_range(written, numbers)

    def test_a_backwards_range_is_caught(self):
        self.assertTrue(self.claim('a.py:369-356'))

    def test_a_forwards_range_is_fine(self):
        self.assertFalse(self.claim('a.py:369-376'))

    def test_a_comma_list_is_not_a_range(self):
        # Descending commas are a list of separate lines, not a span.
        self.assertFalse(self.claim('a.py:40,12'))
        self.assertFalse(self.claim('a.py:12,34-36,40'))

    def test_the_record_holds_no_backwards_range(self):
        for doc in verify_census.DOCS:
            path = RECORD / doc
            for row, line in enumerate(verify_census.lines_of(str(path)), 1):
                for written, _, numbers in verify_census.citations_of(
                        line.replace('\\|', '|')):
                    self.assertFalse(
                        verify_census.backwards_range(written, numbers),
                        f'{doc}:{row}: {written} runs backwards')


@unittest.skipUnless(
    os.environ.get("FES_S8_CENSUS_LIVE") == "1",
    "set FES_S8_CENSUS_LIVE=1 to check claims against the live tree",
)
class ClaimedCountTests(unittest.TestCase):
    """The README states its coverage as numbers; keep every one of them true.

    The record claimed 723 citations and 954 endpoints while the script
    reported 728 and 961 -- the same staleness the census exists to correct,
    in the paragraph that asks the reader not to take it on trust. A first fix
    asserted only the headline, so three further current claims stayed stale
    and CI stayed green (DeanoC/fes#1829 review, twice).

    So every pattern below must match, and *every* number it captures must
    equal the live value. A sentence stating a historical count is written so
    it cannot match -- correction 14's "711 citations and 831 endpoints to 723
    and 954 as the record then stood" is a fact about that correction, not a
    claim about this tree.

    Stating the numbers at all is worth keeping: a reader should see the size
    of the claim without running anything.
    """

    # Patterns are matched against README.md with whitespace collapsed, since
    # several of these sentences wrap mid-claim.
    ROWS = (r'all \*\*(\d+)\*\* inventory rows',
            r'\((\d+) of (\d+), one of them matched')
    CITATIONS = (r'all \*\*(\d+)\*\* `path:line` citations',
                 r'every one of those (\d+) citations',
                 r'against the tree \((\d+) of (\d+), now by content')
    ENDPOINTS = (r'\(\*\*(\d+)\*\* line endpoints',
                 r'All (\d+) line endpoints are now content-checked')
    FINGERPRINTS = (r'(\d+) entries holding the sha256',)

    @classmethod
    def setUpClass(cls):
        cls.readme = ' '.join((RECORD / "README.md").read_text().split())
        cls.rows, _ = verify_census.check_primary()
        cls.citations, cls.endpoints, _, _ = verify_census.check_citations()
        cls.fingerprints = len(json.loads(
            (RECORD / "citation-fingerprints.json").read_text()))

    def claims(self, patterns, live, what):
        for pattern in patterns:
            found = re.findall(pattern, self.readme)
            self.assertTrue(
                found, f'README.md no longer states {what} as {pattern!r}; '
                'if the wording changed, update this pattern rather than '
                'dropping the claim')
            for match in found:
                for number in (match if isinstance(match, tuple) else (match,)):
                    self.assertEqual(
                        int(number), live,
                        f'README.md states {number} {what}, the record holds '
                        f'{live} ({pattern!r})')

    def test_every_row_count_is_current(self):
        self.claims(self.ROWS, self.rows, 'inventory rows')

    def test_every_citation_count_is_current(self):
        self.claims(self.CITATIONS, self.citations, 'citations')

    def test_every_endpoint_count_is_current(self):
        self.claims(self.ENDPOINTS, self.endpoints, 'line endpoints')

    def test_fingerprint_count_is_current(self):
        self.claims(self.FINGERPRINTS, self.fingerprints, 'fingerprint entries')


class FingerprintTests(unittest.TestCase):
    """The stored digest is of the cited line as it stands.

    It used to be taken from `.strip()`ed text, so a cited Python line could
    change its indentation -- and with it which block the statement belongs to
    -- with an unchanged digest, while the file claimed to hash the line's
    whole text (DeanoC/fes#1829 review).
    """

    @classmethod
    def setUpClass(cls):
        cls.prints = json.loads(
            (RECORD / "citation-fingerprints.json").read_text())

    def test_every_entry_is_cited(self):
        """One entry per cited line, in both directions.

        Re-pointing a citation onto an endpoint another citation already uses
        leaves the old key behind with no content check failing, and the count
        the README states is this file's length, so the orphan is counted as
        coverage (DeanoC/fes#1829 review).
        """
        cited = set()
        for doc in verify_census.DOCS:
            path = RECORD / doc
            for line in verify_census.lines_of(str(path)):
                for _, where, numbers in verify_census.citations_of(
                        line.replace('\\|', '|')):
                    cited.update(f'{where}:{n}' for n in numbers)
        self.assertEqual(
            sorted(set(self.prints) - cited), [],
            'fingerprints no citation uses; re-record the file')
        self.assertEqual(
            sorted(cited - set(self.prints)), [],
            'cited lines with no fingerprint; re-record the file')

    @unittest.skipUnless(
        os.environ.get("FES_S8_CENSUS_LIVE") == "1",
        "set FES_S8_CENSUS_LIVE=1 to verify live citation digests",
    )
    def test_digests_cover_leading_whitespace(self):
        indented = 0
        for key, digest in self.prints.items():
            path, _, number = key.rpartition(":")
            target = verify_census.resolve(path)
            self.assertIsNotNone(target, f"{key} no longer resolves")
            raw = verify_census.lines_of(target)[int(number) - 1]
            self.assertEqual(
                hashlib.sha256(raw.encode()).hexdigest(), digest,
                f"{key} is not fingerprinted by its unstripped text")
            if raw != raw.lstrip():
                indented += 1
        # Without an indented cited line the assertion above would pass on a
        # stripped digest too, and prove nothing.
        self.assertGreater(indented, 0, "no cited line is indented")


class RecordTests(unittest.TestCase):
    @unittest.skipUnless(
        os.environ.get("FES_S8_CENSUS_LIVE") == "1",
        "set FES_S8_CENSUS_LIVE=1 to verify citations against the live tree",
    )
    def test_the_record_verifies_against_this_tree(self):
        """The record's own check, run here so drift fails in CI.

        The census cites several hundred lines across the tree; without this,
        an edit that moves one of them is caught only when somebody thinks to
        re-run the script by hand.
        """
        output = StringIO()
        with redirect_stdout(output):
            status = verify_census.main()
        self.assertEqual(status, 0, output.getvalue())


if __name__ == "__main__":
    unittest.main()
