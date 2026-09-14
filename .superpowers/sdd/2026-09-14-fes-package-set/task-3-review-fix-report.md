# Task 3 review-fix evidence

## Findings addressed

The independent Task 3 review reported SPEC/QUALITY FAIL because four current
guides still described a Pong-only image or said that ZX81 was not selected,
and the regression test did not scan those guides.

The current guide set now describes the ordered closed package-only set in the
documentation index, component boundaries, project map and ZX81 page. The
historical Mega Drive/SNES/NES statements remain explicitly historical. The
image assembly test now scans all eight current guide files and rejects the
stale Pong-only phrases identified by review.

## Verification

~~~text
python3 -m unittest tests.test_core_build tests.test_image_assembly
Ran 55 tests in 0.052s
OK

git diff --check
PASS
~~~

A repository scan of the current guide set found no remaining stale phrases:
`installs the described FES Pong package`,
`The current profile selects the described FES Pong package`,
`It is not in the selected image`, or the ZX81 native-image exclusion.

No cold image build, Quartus run, QEMU acceptance, or physical hardware
acceptance was performed or claimed.
