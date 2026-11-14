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
import importlib.util
import json
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


class ClaimedCountTests(unittest.TestCase):
    """The README states its coverage as numbers; keep them true.

    The record stated 723 citations and 954 endpoints while the script
    reported 728 and 961, which is the same staleness the census exists to
    correct, in the paragraph that asks the reader not to take it on trust
    (DeanoC/fes#1829 review). Stating them is worth keeping -- a reader should
    see the size of the claim without running anything -- so they are asserted
    here instead of being removed.
    """

    @classmethod
    def setUpClass(cls):
        cls.readme = (RECORD / "README.md").read_text()
        cls.rows, _ = verify_census.check_primary()
        cls.citations, cls.endpoints, _, _ = verify_census.check_citations()

    def claimed(self, pattern):
        found = re.search(pattern, self.readme)
        self.assertIsNotNone(found, f"README.md no longer states {pattern!r}")
        return int(found.group(1))

    def test_row_count_is_current(self):
        self.assertEqual(self.claimed(r"all \*\*(\d+)\*\* inventory rows"),
                         self.rows)

    def test_citation_count_is_current(self):
        self.assertEqual(
            self.claimed(r"all \*\*(\d+)\*\* `path:line` citations"),
            self.citations)

    def test_endpoint_count_is_current(self):
        self.assertEqual(self.claimed(r"\(\*\*(\d+)\*\* line endpoints"),
                         self.endpoints)

    def test_fingerprint_count_is_current(self):
        self.assertEqual(self.claimed(r"(\d+) entries holding the sha256"),
                         len(json.loads(
                             (RECORD / "citation-fingerprints.json").read_text())))


class RecordTests(unittest.TestCase):
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
