#!/usr/bin/env python3
"""Analyze existing native factory/library captures, never a capture device.

The original strict native geometry/audio helpers are unchanged. Each case is
declared explicitly as Direct/Scanlines and with/without the existing SGM probe.
"""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import sys

import analyze_native as native
import fixture_oracle
import native_capture_grid as grid
import native_capture_support as support

BASE = Path(__file__).resolve().parent
CASES = [('direct', 'direct', False), ('scanlines', 'scanlines', False),
         ('direct-relaunch', 'direct', False), ('direct-sgm', 'direct', True),
         ('scanlines-sgm', 'scanlines', True)]
OPTIONAL = ('direct-sgm-relaunch', 'direct', True)


def peak_present(spectrum, nominal, minimum_ratio):
    peak = spectrum['measured_peak_near_nominal']
    return bool(peak and abs(peak['frequency_hz'] - nominal) <= .5 and
                peak['snr_over_median_bin_db'] >= 20 and
                peak['power_fraction_of_strongest_peak'] >= minimum_ratio)


def tone_present(measured, nominal, minimum_ratio):
    return all(channel['ac_rms_s16'] > 100 and
               peak_present(channel['spectral'], nominal, minimum_ratio)
               for channel in measured['channels'])


def tone_each_second(measured, nominal, minimum_ratio):
    return all(channel['settled_one_second_spectral_windows'] and
               all(window['ac_rms_s16'] > 100 and
                   peak_present(window['spectral'], nominal, minimum_ratio)
                   for window in channel['settled_one_second_spectral_windows'])
               for channel in measured['channels'])


def video_checks(video, expected_frames, profile):
    result = {
        'all_settled_geometry': video['decoded_frame_count'] == expected_frames and
            video['settled_frame_count'] >= 60 and not video['later_bad_frame_indexes'] and
            not video['full_luma_geometry']['later_bad_frame_indexes'] and
            all(v['green_bbox_exclusive'] == list(native.VIEWPORT)
                for v in video['unique_decoded_rgb_frames'].values() if not v['all_black']),
        'all_settled_full_frame_rgb_stable': max(video['settled_max_rgb_delta_from_png']) <= 2,
        'black_background': all(p['black'] <= 2 for p in video['full_luma_geometry']['prototypes_luma_by_hdmi_parity']),
        'green_red_palette_classes_retained': all(
            p['green'][1] > p['green'][0] + 15 and p['green'][1] > p['green'][2] + 15 and
            p['red'][0] > p['red'][1] + 15 and p['red'][0] > p['red'][2] + 15
            for p in video['observed_palette_by_hdmi_parity']),
        'settled_capture_cadence': video['timestamps']['non_monotonic_count'] == 0 and
                                  video['timestamps']['settled_max_delta_seconds'] <= .025,
    }
    if profile == 'direct':
        ratio = video['row_luma']['ratio_mean']
        result['direct_rows_equal_brightness'] = ratio is not None and abs(ratio - 1) <= .03
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('captures', type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--fixture', type=Path, default=BASE.parent / 'fixture/prepared.json')
    parser.add_argument('--include-sgm-relaunch', action='store_true')
    parser.add_argument('--audio-start', type=float, default=2.5)
    parser.add_argument('--audio-duration', type=float, default=3.)
    parser.add_argument('--expected-frames', type=int, default=180)
    args = parser.parse_args()
    output = (args.output or BASE / 'results' / args.captures.name / 'analysis.json').resolve()
    if not output.is_relative_to(BASE):
        raise ValueError('analysis outputs must stay inside this ignored analysis directory')
    output.parent.mkdir(parents=True, exist_ok=True)
    cases = CASES + ([OPTIONAL] if args.include_sgm_relaunch else [])
    expected, _, provenance = fixture_oracle.expected(args.fixture)
    tones = provenance['nominal_tones_hz']
    videos, references, audio, checks = {}, {}, {}, {}
    for name, profile, sgm in cases:
        path = native.require_file(args.captures / (name + '.mkv'))
        png, frame = native.reference(path, output.parent)
        references[name] = support.image_bytes(png)
        video = grid.video(path, png)
        grid.add_stability_details(video)
        video.update(reference_frame_index=frame, reference_png_sha256=native.digest(png),
                     observed_palette_by_hdmi_parity=grid.prototypes(references[name]),
                     row_luma=native.row_luma_profile(references[name]),
                     full_luma_geometry=native.gray_geometry(path, frame, expected))
        videos[name] = video
        checks.update({name + '_' + key: value for key, value in video_checks(video, args.expected_frames, profile).items()})
        wav = native.require_file(args.captures / (name + '.wav'))
        audio[name] = {tone: native.audio(wav, args.audio_start, args.audio_duration, nominal)
                       for tone, nominal in tones.items()}
        sn = audio[name]['SN']
        for tone in ('SN', 'AY') if sgm else ('SN',):
            minimum = .5
            checks[name + '_' + tone + '_tone_present'] = tone_present(audio[name][tone], tones[tone], minimum)
            checks[name + '_' + tone + '_tone_in_each_settled_second'] = tone_each_second(audio[name][tone], tones[tone], minimum)
        if not sgm:
            checks[name + '_no_material_AY_tone_without_sgm'] = not any(
                peak_present(channel['spectral'], tones['AY'], .01) for channel in audio[name]['AY']['channels'])
        checks[name + '_no_settled_audio_clipping'] = all(c['clipped_sample_count'] == 0 for c in sn['channels'])
        stereo = sn['stereo']
        checks[name + '_stereo_mono_mix_retained'] = stereo['pearson_correlation'] is not None and \
            stereo['pearson_correlation'] >= .99 and abs(stereo['rms_balance_right_over_left'] - 1) <= .02
        print(name, 'frames', video['decoded_frame_count'], 'leading_black', video['leading_black_frames'],
              'bad_rgb', len(video['later_bad_frame_indexes']),
              'bad_geometry', len(video['full_luma_geometry']['later_bad_frame_indexes']), flush=True)
    row_models, comparisons, rms = {}, {}, {}
    for name, profile, sgm in cases:
        baseline = 'direct-sgm' if sgm else 'direct'
        if profile == 'scanlines':
            model = native.row_comparison(references[baseline], references[name])
            row_models[name] = {'expected_HDMI_row_period2': model,
                               'rejected_native_row_period4_alternative': native.row_comparison(references[baseline], references[name], 2)}
            checks[name + '_half_brightness_on_odd_HDMI_rows'] = max(model['bright_rows_mae_rgb']) <= 2 and \
                max(model['dim_rows_half_brightness_mae_rgb']) <= 2
        else:
            comparison = grid.difference(references[name], references['direct'])
            comparisons[name] = comparison
            checks[name + '_same_native_picture_as_direct'] = max(comparison['whole_frame_max_absolute_rgb_delta']) <= 2
        rms[name] = [audio[name]['SN']['channels'][i]['ac_rms_s16'] / audio[baseline]['SN']['channels'][i]['ac_rms_s16']
                     if audio[baseline]['SN']['channels'][i]['ac_rms_s16'] else None for i in range(2)]
        checks[name + '_audio_RMS_preserved_for_same_expansion'] = all(r is not None and abs(r - 1) <= .05 for r in rms[name])
    result = {'classification': 'Independent regular-file capture analysis only; no kit, capture-device or network access.',
              'declared_cases': [{'name': n, 'profile': p, 'sgm_probe': s} for n, p, s in cases],
              'fixture_receipt_sha256': native.digest(args.fixture), 'fixture_oracle': provenance,
              'checks': checks, 'verdict': 'pass' if all(checks.values()) else 'investigate',
              'videos': videos, 'audio': audio, 'scanline_row_models': row_models,
              'direct_picture_comparisons': comparisons, 'audio_RMS_ratios_within_expansion': rms,
              'analysis_tools': {p.name: native.digest(p) for p in
                  (Path(__file__), Path(native.__file__), Path(grid.__file__), Path(support.__file__),
                   Path(fixture_oracle.__file__), Path(fixture_oracle.native.__file__))},
              'media_tools': {n: {'path': shutil.which(n), 'sha256': native.digest(Path(shutil.which(n))),
                                 'version': subprocess.check_output([n, '-version'], text=True).splitlines()[0]}
                              for n in ('ffmpeg', 'ffprobe')},
              'thresholds': {'frequency_error_hz_max': .5, 'tone_SNR_over_median_bin_db_min': 20,
                             'base_SN_power_ratio_min': .5, 'SGM_each_tone_power_ratio_min': .5,
                             'non_SGM_material_AY_power_ratio': .01,
                             'note': 'Both strongest spectral peaks are independently measured; nominal frequencies do not synthesize observations.'},
              'limitations': ['SGM checks are boundary sentinels, console-RAM isolation and four AY readbacks; they do not test every SRAM address.',
                  'The SGM pass picture is reachable only after the existing probe checks complete. Root operator package/ROM/selection receipts bind that intent to hardware.',
                  'Native picture checks retain zero horizontal offset and examine every settled frame, full viewport and outer background.',
                  'Capture conversion/filtering prevents bit-exact FPGA RGB/PCM claims; USB timestamps do not directly measure HS/VS.',
                  'No material AY peak means below the declared relative-power gate, not absolute electrical silence.',
                  'Source/image/selection/restart/cleanup identity requires separate operator receipts.']}
    output.write_text(json.dumps(result, indent=2, sort_keys=True, allow_nan=False) + '\n')
    print(json.dumps({'verdict': result['verdict'], 'gates': len(checks),
                      'passed': sum(checks.values()), 'failed': [k for k, v in checks.items() if not v],
                      'output': str(output)}, indent=2), flush=True)
    return 0 if result['verdict'] == 'pass' else 1


if __name__ == '__main__':
    sys.exit(main())
