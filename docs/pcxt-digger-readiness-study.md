# PC/XT Digger readiness study

This study is an offline aid for designing a future game test. It does not
establish that the game is playable on the kit, authorize a kit run or change
the existing game diagnostic. The two retained captures are from the same
source lineage and are very similar; they cannot establish general detector
accuracy. The first is a recorded failed attempt and the second has an
inconclusive movement review. In particular, a pair of opening-screen matches
is not proof of a successful left/right/left sequence.

`scripts/pcxt_digger_readiness.py` contains an experimental fixed-coordinate
classifier and a one-shot freshness model. The classifier uses private,
externally supplied image crops, not bundled game assets. The model accepts
sequence numbers and timestamps as inputs; it cannot prove that a device
supplied them honestly or that they bound the age of a frame. Its explicit
thresholds are parameters for experiments, not qualified kit defaults.

For the retained private recordings, `scripts/pcxt_digger_replay.py` verifies
the full video hashes, re-derives and checks the crop hashes, and cross-replays
the first and second run at half-second sample intervals. An example with the
locally retained data is:

```sh
python3 -I -B scripts/pcxt_digger_replay.py \
  --first-video /path/to/first/game-full.mkv \
  --last-video /path/to/last/game-full.mkv
```

The replay uses FFmpeg at 2 fps on the 1920x1080 recordings, followed by
`scale=640:360:flags=neighbor`, RGB24, and crops of `32:34:304:322`,
`80:76:350:280` and `70:30:190:0` respectively. FFmpeg's offline `fps` filter
may duplicate frames, so this replay is **not** evidence of two fresh source
observations. It checks the derived crop hashes against the retained study and
deletes the temporary crops after reading them.

The study detects opening candidates at 43.5 and 44.0 seconds, rejects the
sampled known negative and respawn intervals, and detects the particular
approaching threat at 49–50 seconds. These are observations about those two
recordings only. A different screen geometry, animation, enemy, later restart,
frozen capture or other unseen scene could defeat the classifier.

Before using any such gate for live input, a new implementation needs a
continuously recorded capture stream with a separately drained bounded
observation path, source-frame sequencing and defensible capture-age bounds.
It must show recording integrity despite a stalled or failed observer and
must establish a credible observation-to-guest-input bound. Arrival time and
FFmpeg progress messages provide neither guarantee. Validate the actual branch
against realistic delayed, duplicate, frozen, malformed and dropped inputs;
retain the unchanged ownership and input guards; independently review the
algorithm and controls; and preserve the prior attempt history. Only then can
a new source-bound kit attempt be separately assessed. Gameplay, directional
response and speaker-audio conclusions require their own retained physical
evidence.
