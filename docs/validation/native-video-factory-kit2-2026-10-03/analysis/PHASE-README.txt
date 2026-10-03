Use the unchanged run_analysis.py with normal analysis/results output paths. Frozen scripts, original fixture receipts and thresholds are unchanged. No additional globals or BASE overrides are used.

python3 out/native-video-factory/candidate-6ed1dad4/analysis/run_analysis.py <capture-directory> --include-sgm-relaunch --output out/native-video-factory/candidate-6ed1dad4/analysis/results/<set>/analysis.json

Fixture files belong to native_library and were read only after its ready message: graphics-sn.rom, sgm-probe.rom, LICENSE, prepare.py and prepared.json match candidate-3ffe989f exactly. frozen.json is its new phase-local binding; the historical preparation.log was not copied. The derived analysis fixture-input.json only updates phase paths, preserving original eaa emitter provenance. Selected 6ed1 source bytes match those original fixture emitters and license.

Prior positive 79/79 and 60-frame geometry-negative controls are retained and hash-verified, not rerun. The original receipts retain historical paths intentionally. Do not execute the historical fixture prepare.py from this deeper phase. No current capture analysis has started; wait for the root capture-ready signal.
