#!/usr/bin/env python3
"""admit_seed.py MISTEROSS SEEDROOT VENDOR_ARCHIVE: method.md's G3 admission of one seed copy.

SEEDROOT is laid out as the producer lays out its own build (build-inputs.json, seed 1's record,
beside signoff/combined/hps/project, compiled). This runs the producer's own admission, unchanged,
as Machine.build runs it: async_inputs.collect and audio_reference.collect on SEEDROOT/signoff, then
build_menu_package.timing_evidence with both. Writes SEEDROOT/signoff-timing.json and prints
package_timing_admission. Run it under a Pro slot of the Quartus lease (it runs quartus_sta).
"""
import json
import os
import sys
from pathlib import Path

root, seedroot, archive = Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve(), Path(sys.argv[3])
sys.path.insert(0, str(root))
from boards.de25nano.producers import build_menu_package as common  # noqa: E402
from boards.de25nano.scanout import async_inputs, audio_reference  # noqa: E402

selected = seedroot / 'signoff'
sta = Path(os.environ['QUARTUS_ROOTDIR']) / 'bin/quartus_sta'
review = async_inputs.collect(selected, sta, vendor_archive=archive)
audio = audio_reference.collect(selected, sta, archive)
timing = common.timing_evidence(selected, async_review=review, audio_review=audio)
(seedroot / 'signoff-timing.json').write_text(json.dumps(timing, indent=2, sort_keys=True) + '\n')
print('package_timing_admission', timing['package_timing_admission'])
sys.exit(0 if timing['package_timing_admission'] else 1)
