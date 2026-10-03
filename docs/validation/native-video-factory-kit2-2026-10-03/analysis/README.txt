Prepared native factory/library capture analysis

Run:
python3 out/native-video-factory/analysis/analyze_factory.py <capture-directory> --output out/native-video-factory/analysis/results/<set>/analysis.json

Required files are direct, scanlines, direct-relaunch, direct-sgm and scanlines-sgm, each with .mkv and .wav extensions. Add --include-sgm-relaunch when the direct-sgm-relaunch pair is present.

The default capture contract is 180 frames at unscaled 1280 x 720 / 60 fps and six seconds of 48 kHz stereo signed 16-bit PCM, after the operator's three-second settling period. Every settled video frame is checked against the aligned native Graphics I oracle. The common settled audio window is 2.5 to 5.5 seconds.

Use fixture/graphics-sn.rom without SGM and fixture/sgm-probe.rom with SGM. These reproduce existing public open fixtures byte for byte. The SGM picture appears only after eight memory reads, seven writes and four AY readbacks pass. This tests RAM-window boundaries and console-RAM isolation, not every SRAM address. SN at 216.78446 Hz and AY at 440.39678 Hz must each be detected independently, including in each settled second.

Preparation is offline: all 15 checks passed, including absent/broken SGM rejection. The six-case synthetic CLI run passed 79 gates. These results do not establish current hardware acceptance. The original native geometry helpers are unchanged, with their public source hashes bound in prepared.json.

The root operator alone owns kit, capture, upload, selection and cleanup. This analyzer reads regular local files. Image, artifact, ROM, lease and host-restart identity require separate operator receipts.
