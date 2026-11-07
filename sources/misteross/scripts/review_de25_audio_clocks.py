#!/usr/bin/env python3
"""Collect source-bound fitted I2S observations; never admit or program a core."""
import argparse
import csv
from decimal import Decimal, InvalidOperation
import json
from pathlib import Path
import shutil
import subprocess
import sys
import zipfile

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from boards.de25nano.scanout import async_inputs, vendor_reference
from scripts.fes_build_common import BuildError

HERE = Path(__file__).resolve().parents[1]
TCL = HERE / 'boards/de25nano/scanout/audio_clocks.tcl'
MASTER = 'fes_audio_inst|clock|iopll_0_outclk0'
PINS = {'CLOCK1_50': 'V16', 'HDMI_SCLK': 'BW1', 'HDMI_LRCLK': 'BR6',
        'HDMI_I2S': 'CB2', 'HDMI_MCLK': 'CF1'}
CORNERS = {'Slow fix4 100C Model', 'Slow fix4 0C Model',
           'Slow fix4a 0C Model', 'Fast fix4 0C Model'}


def require(condition, message):
    if not condition:
        raise BuildError('DE25 audio clock observations: ' + message)


def number(value):
    try:
        result = Decimal(value)
    except InvalidOperation as error:
        raise BuildError('DE25 audio clock observations: invalid number') from error
    require(result.is_finite() and result >= 0, 'nonfinite or negative number')
    return result


def observations(directory):
    """Check report coverage and compute conditional, rather than PCB, budgets."""
    clocks = {}
    for row in csv.DictReader((Path(directory) / 'clocks.tsv').read_text().splitlines(), delimiter='\t'):
        require(set(row) == {'clock', 'period_ns', 'waveform', 'edges', 'master'}, 'clock columns changed')
        name = row['clock']
        require(name not in clocks, 'duplicate clock')
        clocks[name] = row
    names = {MASTER, *('de25_i2s_' + label + suffix
                      for label in ('bit_clock', 'word_clock') for suffix in ('', '_output'))}
    require(set(clocks) == names, 'missing or unexpected audio clock')
    period = number(clocks[MASTER]['period_ns'])
    require(abs(period - Decimal(20) * 236 / 58) <= Decimal('.001'),
            'master period differs from the stock M58/C236 divider')
    for label, edges, divisor in (('bit_clock', (3, 7, 11), 4), ('word_clock', (255, 511, 767), 256)):
        name = 'de25_i2s_' + label
        require(clocks[name]['edges'].split() == list(map(str, edges)), 'serializer source edges differ')
        require(clocks[name]['master'] == MASTER, 'divider master differs')
        require(clocks[name + '_output']['master'] == name, 'forwarded-clock master differs')
        for selected in (name, name + '_output'):
            require(abs(number(clocks[selected]['period_ns']) - period * divisor) <= Decimal('.001'),
                    'divider period differs beyond report resolution')
            waveform = [number(v) for v in clocks[selected]['waveform'].split()]
            require(len(waveform) == 2 and all(abs(v - period * (edge - 1) / 2) <= Decimal('.001')
                    for v, edge in zip(waveform, edges)), 'divider waveform differs')
    paths = {}
    for row in csv.DictReader((Path(directory) / 'paths.tsv').read_text().splitlines(), delimiter='\t'):
        require(set(row) == {'corner', 'clock', 'path', 'bound', 'count', 'delay_ns'}, 'path columns changed')
        key = tuple(row[n] for n in ('corner', 'clock', 'path', 'bound'))
        require(key not in paths and row['count'] == '1', 'duplicate or missing fitted path')
        paths[key] = number(row['delay_ns'])
    require(set(paths) == {(c, clock, path, bound) for c in CORNERS
            for clock in ('bit_clock', 'word_clock') for path in ('network', 'output')
            for bound in ('min', 'max')}, 'incomplete fitted corner/path coverage')
    budgets = []
    for corner in sorted(CORNERS):
        for clock in ('bit_clock', 'word_clock'):
            for path in ('network', 'output'):
                require(paths[corner, clock, path, 'min'] <= paths[corner, clock, path, 'max'],
                        'inverted fitted path bounds')
        def delay(clock, bound):
            return sum(paths[corner, clock, kind, bound] for kind in ('network', 'output'))
        # LRCLK launches at falling SCLK, receiver captures at rising SCLK.
        # ADV7513 hardware guide pp12-13 specifies 2ns setup and hold. These
        # conditional budgets leave PCB skew/jitter unaccounted, not set to 0.
        budgets.append({'corner': corner,
            'setup_budget_before_unmodeled_skew_and_jitter_ns': str(
                period * 2 + delay('bit_clock', 'min') - delay('word_clock', 'max') - 2),
            'hold_budget_before_unmodeled_skew_and_jitter_ns': str(
                period * 2 + delay('word_clock', 'min') - delay('bit_clock', 'max') - 2)})
    return {'clocks': clocks, 'conditional_pin_budgets': budgets,
            'fitted_corner_count': len(CORNERS), 'fitted_path_count': len(paths)}


def stock_audio(project, archive):
    require(async_inputs.sha(archive) == vendor_reference.ARCHIVE_SHA256, 'vendor archive changed')
    with zipfile.ZipFile(archive) as package:
        for name in ('Demonstration/FPGA/Golden_top/golden_top.qsf',
                     vendor_reference.PREFIX + 'golden_top.qsf'):
            require(package.namelist().count(name) == 1, 'missing/duplicate vendor pin source')
            lines = {' '.join(line.split()) for line in package.read(name).decode().splitlines()}
            for port, pin in PINS.items():
                require('set_location_assignment PIN_' + pin + ' -to ' + port in lines and
                        'set_instance_assignment -name IO_STANDARD "3.3-V LVCMOS" -to ' + port in lines,
                        'vendor audio pin/voltage differs')
        name = vendor_reference.PREFIX + 'av_pll.ip'
        require(package.namelist().count(name) == 1, 'missing/duplicate vendor audio PLL')
        original = package.read(name)
    old = b'<ipxact:value>A5EB013BB23BE4SR1</ipxact:value>'
    require(original.count(old) == 2, 'stock PLL device substitutions changed')
    derived = original.replace(old, b'<ipxact:value>A5EB013BB23BE4SCS</ipxact:value>')
    require((project / 'fes_mailbox/av_pll.ip').read_bytes() == derived, 'compiled audio PLL differs')
    qsf = {' '.join(line.split()) for line in (project / 'golden_top.qsf').read_text().splitlines()}
    require('set_global_assignment -name IP_FILE fes_mailbox/av_pll.ip' in qsf,
            'project does not select the stock audio PLL')
    for port, pin in PINS.items():
        require('set_location_assignment PIN_' + pin + ' -to ' + port in qsf and
                'set_instance_assignment -name IO_STANDARD "3.3-V LVCMOS" -to ' + port in qsf,
                'project audio pin/voltage differs')
    fitted = {}
    for line in (project / 'output_files/golden_top.pin').read_text().splitlines():
        fields = [value.strip() for value in line.split(':')]
        if fields[0] in PINS:
            require(len(fields) >= 6 and fields[0] not in fitted, 'invalid fitted audio pin')
            fitted[fields[0]] = {'pin': fields[1], 'io_standard': fields[3]}
    require(fitted == {name: {'pin': pin, 'io_standard': '3.3-V LVCMOS'}
                       for name, pin in PINS.items()}, 'fitted audio pin/voltage differs')
    return {'pins': fitted, 'vendor_archive_sha256': async_inputs.sha(archive),
            'derived_pll_sha256': async_inputs.sha(project / 'fes_mailbox/av_pll.ip')}


def collect(candidate, output, quartus_sta, archive):
    return collect_selected(Path(candidate).resolve() / 'signoff', output, quartus_sta, archive)


def collect_selected(selected, output, quartus_sta, archive):
    """Observe one selected fit; signoff and repeat never share a receipt."""
    selected, output = Path(selected).resolve(), Path(output).resolve()
    candidate = selected.parent
    record = json.loads((candidate / 'build-inputs.json').read_text())
    require(record['parameters'].get('standard_core') is True and
            record['parameters'].get('expansion_socket') is False and
            record['parameters'].get('internal_ram_kib') == 16, 'requires standard internal16K base')
    before = async_inputs.bindings(selected)
    original_project = selected / 'combined/hps/project'
    inputs = json.loads((selected / 'combined/hps/inputs.json').read_text())
    staged = inputs['standard_zx81']['staged_source_sha256']
    require(staged, 'missing staged standard sources')
    for name, digest in staged.items():
        relative = Path(name)
        require(not relative.is_absolute() and '..' not in relative.parts,
                'unsafe staged source path')
        path = original_project / relative
        require(not path.is_symlink() and async_inputs.sha(path) == digest,
                'staged standard source differs: ' + name)
    tool, script = Path(quartus_sta).resolve(), TCL.resolve()
    identities = {str(p): async_inputs.sha(p) for p in (tool, script, Path(__file__).resolve())}
    output.mkdir(exist_ok=False)
    copied = output / 'project-inputs'
    shutil.copytree(selected, copied)
    shutil.copyfile(candidate / 'build-inputs.json', output / 'build-inputs.json')
    require(async_inputs.bindings(copied) == before, 'private copy differs from fitted inputs')
    project = copied / 'combined/hps/project'
    stock = stock_audio(project, archive)
    reference = vendor_reference.collect(selected, output / 'vendor-reference', archive)
    reports = output / 'reports'
    reports.mkdir()
    command = [str(tool), '-t', str(script), str(project), str(reports)]
    with (output / 'quartus-sta.log').open('wb') as stream:
        subprocess.run(command, stdout=stream, stderr=subprocess.STDOUT, check=True, timeout=240)
    result = observations(reports)
    require(async_inputs.bindings(selected) == before and identities == {
            str(p): async_inputs.sha(p) for p in (tool, script, Path(__file__).resolve())},
            'original fit or observation tools changed')
    result.update(classification='source-bound fitted clock engineering observations',
        source_revision=record['revision'], bindings=before, tools_sha256=identities,
        command=command, stock_audio=stock, vendor_hdmi_reference=reference,
        reports_sha256={p.name: async_inputs.sha(p) for p in sorted(reports.iterdir())},
        budget_excludes=['PCB differential trace delay', 'unaccounted master-clock jitter and uncertainty',
                        'signal-integrity behavior'],
        receiver_reference='https://www.analog.com/media/en/technical-documentation/user-guides/adv7513_hardware_user_guide.pdf#page=12',
        package_timing_admission=False, independent_pcb_timing_qualified=False, hardware_acceptance=False)
    (output / 'receipt.json').write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
    return output


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--quartus-sta', type=Path, required=True)
    parser.add_argument('--vendor-archive', type=Path, required=True)
    args = parser.parse_args()
    try:
        print(collect(args.candidate, args.output, args.quartus_sta, args.vendor_archive))
    except (BuildError, OSError, ValueError, subprocess.SubprocessError) as error:
        parser.exit(1, str(error) + '\n')
