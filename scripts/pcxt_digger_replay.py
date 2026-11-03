#!/usr/bin/env python3
"""Replay the private 9e Digger study; does not interact with a kit.

The two videos are private, external inputs. This script derives temporary RGB
crops, distributes no game assets and does not create hardware evidence.
"""
import argparse
import hashlib
import json
import subprocess
import sys
import tempfile
from pathlib import Path

if __package__:
    from . import pcxt_digger_readiness as readiness
else:
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    import pcxt_digger_readiness as readiness

CROPS = {
    'first': {'player':'e3313dd15768626a143e9cbad694c98f0a588a3c5b13f4a2a522ac1209f88ecc', 'threat':'4158cde94fc0282a89f70adf1f983e810da6e9ee5290290029490c9ed9fd866e', 'lives':'e24c1efead16f119fb9e9c64e5424d4b3d891c90d76ae62d017c11e5a265597f'},
    'last': {'player':'08dd300c72d22464ac75e64baafdc43c7f87b74e9b8be8c14d56900c28990ba4', 'threat':'8054bf275c570113567498dac71c6449abd603c3241e330c09a07fe5a517f5c3', 'lives':'b8decc15aee762358ac8c7bcdaa80e632b180b5a84f559b6d601ebdf79479d45'},
}
VIDEO_SHA = {'first':'7e491fc757f6a5ab2db647256dd004b50827b37ebfeb12cb8ef76d644dc5536e',
             'last':'40f9ff3884d5048a3e91a72a262dcacf3bee3a63014dd093bbff1050764a6f95'}


def extract(video, dest):
    graph = ('[0:v]fps=2,scale=640:360:flags=neighbor,split=3[p][t][l];'
             '[p]crop=32:34:304:322[po];[t]crop=80:76:350:280[to];'
             '[l]crop=70:30:190:0[lo]')
    cmd = ['ffmpeg', '-nostdin', '-hide_banner', '-loglevel', 'error', '-i', str(video),
           '-filter_complex', graph]
    for label, name in [('po', 'roi'), ('to', 'danger'), ('lo', 'lives')]:
        cmd.extend(['-map', '['+label+']', '-pix_fmt', 'rgb24', '-f', 'rawvideo',
                    str(dest/(name+'.rgb'))])
    subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=240)


def run(videos):
    dataset = {}
    for name in CROPS:
        with videos[name].open('rb') as f:
            if hashlib.file_digest(f, 'sha256').hexdigest() != VIDEO_SHA[name]:
                raise ValueError(f'{name} video identity differs')
        dataset[name] = {}
        with tempfile.TemporaryDirectory(prefix='pcxt-digger-replay-') as tmp:
            dest = Path(tmp)
            extract(videos[name], dest)
            for kind, suffix, size in [('player', 'roi', readiness.PLAYER_SIZE),
                                       ('threat', 'danger', readiness.THREAT_SIZE),
                                       ('lives', 'lives', readiness.LIVES_SIZE)]:
                data = (dest/f'{suffix}.rgb').read_bytes()
                if hashlib.sha256(data).hexdigest() != CROPS[name][kind] or len(data) % size:
                    raise ValueError(f'{name} {kind} derived crop identity or size differs')
                dataset[name][kind] = [data[i:i+size] for i in range(0, len(data), size)]
        with videos[name].open('rb') as f:
            if hashlib.file_digest(f, 'sha256').hexdigest() != VIDEO_SHA[name]:
                raise ValueError(f'{name} video changed during extraction')
        if len({len(v) for v in dataset[name].values()}) != 1:
            raise ValueError(f'{name} streams have different frame counts')
    results = []
    for train, test in [('first', 'last'), ('last', 'first')]:
        templates = [dataset[train]['player'][int(t*2)] for t in (44, 45, 46, 48, 49)]
        obs = [readiness.classify(p, t, l, templates) for p, t, l in
               zip(dataset[test]['player'], dataset[test]['threat'], dataset[test]['lives'])]
        def candidate(value):
            return value.player and value.initial_lives and not value.threat
        initial = [i/2 for i in range(87, 101) if candidate(obs[i])]
        negatives = [i/2 for i in list(range(0, 87))+list(range(101, 113)) if candidate(obs[i])]
        respawns = [i/2 for i in range(115, 159) if candidate(obs[i])]
        pairs = [i/2 for i in range(82, 101) if candidate(obs[i]) and candidate(obs[i+1])]
        threat = all(obs[i].threat for i in (98, 99, 100))
        if len(initial) < 6 or negatives or respawns or not pairs or pairs[0] != 43.5 or not threat:
            raise ValueError(f'{train}/{test} replay differs from the observed study')
        results.append({'trained_on': train, 'tested_on': test,
                        'first_candidate_pair_seconds': [pairs[0], pairs[0]+.5],
                        'positive_frames': len(initial), 'known_negative_matches': len(negatives),
                        'respawn_matches': len(respawns), 'threat_at_49_to_50_seconds': threat})
    return {'scope': 'offline cross-replay only; no fresh capture, controls or hardware proof',
            'videos_sha256': VIDEO_SHA, 'crop_sha256': CROPS, 'results': results,
            'limitations': ['The two captures are near-identical and cannot establish general detection accuracy.',
                            'Offline sampling does not prove live capture freshness.']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--first-video', type=Path, required=True)
    parser.add_argument('--last-video', type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(run({'first':args.first_video, 'last':args.last_video}), indent=2))


if __name__ == '__main__':
    main()
