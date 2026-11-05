"""The two audio-clock exceptions approved on 2026-11-04.

Only the reviewed standard internal16K/constant-PCM base is eligible. This is
a stock-reference decision, not independent PCB timing or hardware acceptance.
"""
import json
from pathlib import Path
import re

from boards.de25nano.scanout import async_inputs, vendor_reference
from scripts import review_de25_audio_clocks as fitted
from scripts.fes_build_common import BuildError

POLICY = 'de25-standard-stock-audio-clocks-v1'
PORTS = {'HDMI_SCLK', 'HDMI_LRCLK'}
REPORTS = {'clocks.tsv', 'paths.tsv'} | {
    f'{corner}_{clock}_{path}_{bound}.rpt'
    for corner in ('4_slow_fix4_0c', '4_slow_fix4_100c', '4_slow_fix4a_0c', 'MIN_fast_fix4_0c')
    for clock in ('bit_clock', 'word_clock') for path in ('network', 'output') for bound in ('min', 'max')}
# Frozen reviewed implementation. A serializer, wrapper, integration or clock
# change requires another review; source hashes cannot silently follow HEAD.
# The reviewed full-reset export adds only an unused standard-base output and
# its system-reset conduit metadata; audio logic and wiring remain identical.
# The explicit PR recipe adds an opt-in expansion selection to the builder;
# the default internal16K audio sources, wiring and timing source are unchanged.
# PR mode remains ineligible, including when it uses that same builder.
SOURCES = {
    'boards/de25nano/zx81-hps/fes_de25_audio.v': '9f2d1436b75ef1fe489a55ea4139b2f5dcd922072528b6f68d344d851cca6f33',
    'cores/fes-common/rtl/fes_audio_output.v': 'e686a035484907d1690d7e0cd8e91d6ace86146b38bf4d85d4f500690ddfb0dd',
    'cores/fes-common/rtl/fes_audio_i2s.v': '14c24da8c166a623c41bc0ae57fc26c345ed6c419f825f2ba1f4f5057c4435f4',
    'cores/fes-zx81/rtl/fes_zx81_core.v': '79c070b837a6b455a4a4a64fcbef4d76ac3ca7c467ab692a16af7610bed42970',
    'boards/de25nano/zx81-hps/fes_de25_zx81_standard.v': 'c6ac81675d1dd07a50bbc2660049ba62e0e79e34fb75a5aac0532340adfe1c97',
    'boards/de25nano/zx81-hps/fes_de25_zx81_standard_hw.tcl': 'c08fc345f9196b85e9bb1c310759cebc79d85395631c8428717d95dbad0415a9',
    'boards/de25nano/zx81-hps/standard-clocks.sdc': 'b7a528ac1a1bf0b147c269e83078a405b523698cfd16c1fe62cf028858b431d0',
    'boards/de25nano/zx81-hps/build.py': 'e02095ca606c32d426c5e5ca5788297c5b34362908ebab8c77337595db6c31cf',
}


def require(condition, message):
    if not condition:
        raise BuildError('DE25 approved audio-clock reference: ' + message)


def eligible(directory):
    directory = Path(directory)
    record = json.loads((directory.parent / 'build-inputs.json').read_text())
    params = record['parameters']
    expected = {'core_id': 'fes.zx81', 'programming_profile': 'de25-linux-computer-v1',
                'standard_core': True, 'expansion_socket': False, 'internal_ram_kib': 16,
                'audio_clock_reference_policy': POLICY}
    require(all(type(params.get(k)) is type(v) and params[k] == v for k, v in expected.items()),
            'requires the selected standard internal16K reference profile')
    require(all(record['source_inputs'].get(name) == sha for name, sha in SOURCES.items()),
            'reviewed audio implementation/source changed')
    project = directory / 'combined/hps/project'
    files = {project / 'fes_mailbox' / Path(name).name: digest for name, digest in SOURCES.items()
             if not name.endswith(('/build.py', '_hw.tcl'))}
    require(all(not p.is_symlink() and async_inputs.sha(p) == digest for p, digest in files.items()),
            'compiled audio implementation differs from the reviewed source')
    top = project / 'golden_top.v'
    require(not top.is_symlink(), 'linked compiled audio wrapper')
    text = ' '.join(top.read_text().split())
    wiring = '''fes_de25_audio fes_audio_inst (
      .CLOCK1_50(CLOCK1_50), .source_clk(system_clk_100_internal),
      .reset_n(top_reset_n), .hold(standard_exec_reset || scanout_clock_fault),
      .left_sample(standard_audio_sample), .right_sample(standard_audio_sample), .locked(),
      .HDMI_MCLK(HDMI_MCLK), .HDMI_SCLK(HDMI_SCLK),
      .HDMI_LRCLK(HDMI_LRCLK), .HDMI_I2S(HDMI_I2S) );'''
    require(text.count(' '.join(wiring.split())) == 1 and
            len(re.findall(r'\bstandard_audio_sample\b', text)) == 4,
            'compiled audio wrapper/constant-PCM wiring changed')
    for connection in ('.fes_scanout_audio_sample(standard_audio_sample)',
                       ".fes_scanout_expansion_expansion_running(1'b1)",
                       ".fes_scanout_expansion_bus_peek_data(8'b0)",
                       ".fes_scanout_expansion_bus_ram_present(1'b0)"):
        require(text.count(connection) == 1, 'compiled standard base connection changed')
    files[top] = async_inputs.sha(top)
    return {p.relative_to(project).as_posix(): async_inputs.sha(p) for p in files}


def output_findings(drc):
    sections = [s for s in re.split(r'(?m)^Status:\s*', drc)[1:]
                if re.search(r'^; TMC-20012 - [^;]+;', s, re.M)]
    require(len(sections) == 1, 'missing/ambiguous output finding detail')
    rows = [[c.strip() for c in line.split(';')[1:-1]]
            for line in sections[0].splitlines() if line.startswith(';')]
    require(['Port', 'Reason', 'Waived'] in rows, 'missing output finding columns')
    rows = [r for r in rows if len(r) == 3 and r[0] != 'Port']
    expected = vendor_reference.OUTPUTS | PORTS
    require(len(rows) == 32 and {r[0] for r in rows} == expected and
            all(r[1] == 'No output delay was set on the output port.' and not r[2] for r in rows),
            'only the existing30 outputs and the two approved audio clocks are covered')
    return sorted(expected)


def collect(directory, quartus_sta, archive):
    directory = Path(directory).resolve()
    before = eligible(directory)
    output = directory.with_name(directory.name + '-audio-clock-review')
    fitted.collect_selected(directory, output, quartus_sta, archive)
    require(eligible(directory) == before, 'compiled audio sources changed during collection')
    selected = {'policy': POLICY, 'compiled_files_sha256': before,
                'archive': str(Path(archive).resolve())}
    (output / 'approved-reference.json').write_text(json.dumps(selected, indent=2, sort_keys=True) + '\n')
    verify(directory, output)
    return output


def verify(directory, review):
    directory, review = Path(directory), Path(review)
    selected = json.loads((review / 'approved-reference.json').read_text())
    require(selected['policy'] == POLICY and selected['compiled_files_sha256'] == eligible(directory),
            'selected policy or compiled audio sources changed')
    receipt = json.loads((review / 'receipt.json').read_text())
    require(receipt['bindings'] == async_inputs.bindings(directory), 'fit/RBF/database binding changed')
    require(receipt['source_revision'] == json.loads((directory.parent / 'build-inputs.json').read_text())['revision'],
            'source revision changed')
    command = receipt['command']
    require(len(command) == 5 and Path(command[0]).name == 'quartus_sta' and command[1:] == [
                '-t', str(fitted.TCL.resolve()),
                str(review.resolve() / 'project-inputs/combined/hps/project'),
                str(review.resolve() / 'reports')] and set(receipt['tools_sha256']) == {
                    command[0], str(fitted.TCL.resolve()), str(Path(fitted.__file__).resolve())},
            'missing or changed fitted-observation invocation/tool inventory')
    require(all(async_inputs.sha(p) == digest for p, digest in receipt['tools_sha256'].items()),
            'observation tool/source changed')
    reports = review / 'reports'
    require({p.name for p in reports.iterdir()} == REPORTS,
            'missing or unexpected fitted report')
    require(receipt['reports_sha256'] == {p.name: async_inputs.sha(p) for p in sorted(reports.iterdir())},
            'fitted clock/path reports changed')
    observations = fitted.observations(reports)
    require(all(receipt[k] == value for k, value in observations.items()), 'clock/path observations changed')
    require(all(fitted.number(row[k]) > 0 for row in observations['conditional_pin_budgets']
                for k in ('setup_budget_before_unmodeled_skew_and_jitter_ns',
                          'hold_budget_before_unmodeled_skew_and_jitter_ns')),
            'nonpositive conditional receiver budget')
    project = directory / 'combined/hps/project'
    require(receipt['stock_audio'] == fitted.stock_audio(project, selected['archive']),
            'stock pins, voltage or complete PLL changed')
    require(receipt['vendor_hdmi_reference'] == vendor_reference.verify(directory, review / 'vendor-reference'),
            'receiver mode or stock reference changed')
    ports = output_findings((project / 'output_files/golden_top.tq.drc.signoff.rpt').read_text())
    return {'policy': POLICY, 'ports': ports, 'receipt_sha256': async_inputs.sha(review / 'receipt.json'),
            'approved_reference_sha256': async_inputs.sha(review / 'approved-reference.json'),
            'conditional_pin_budgets': observations['conditional_pin_budgets'],
            'fitted_corner_count': observations['fitted_corner_count'],
            'fitted_path_count': observations['fitted_path_count'],
            'independent_pcb_timing_qualified': False, 'hardware_acceptance': False}
