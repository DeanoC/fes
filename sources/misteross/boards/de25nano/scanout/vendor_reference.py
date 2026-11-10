"""Bind the user-selected Terasic HDMI board profile to an actual fitted design.

This is reference-design acceptance, not an independently derived PCB timing
model. Raw missing-output-delay findings remain in the compiler evidence.
"""
import hashlib
import json
from pathlib import Path
import re
import zipfile

from boards.de25nano.display import build as display
from boards.de25nano.scanout import hdmi_inputs
from scripts.fes_build_common import BuildError

POLICY = 'de25-terasic-hdmi-reference-v1'
ARCHIVE_SHA256 = 'f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66'
PREFIX = 'Demonstration/FPGA/HDMI_ASx4/'
MEMBERS = {
    'Demonstration/FPGA/Golden_top/golden_top.qsf': 'de1431e72627743c7fa3ba7d6b8a833c54a28b9a3c0bfa76292126175bcea105',
    PREFIX + 'golden_top.qsf': '73dac113e3914d4b34fb1642525bbf8467e4e339cedcd1e4fcec2b5eed0eda45',
    PREFIX + 'sys_pll.ip': 'ca35f6ecd0034b3e88f51425433d2c6c02a288c863247ac02ad58b69c794df38',
    PREFIX + 'HDMI_I2C/I2C_HDMI_Config.v': 'b9b7c477173fee9b6cb5e3d6a6ba1122c7a0b539f205d807b1d61b22048fd391',
    PREFIX + 'HDMI_I2C/I2C_Controller.v': '4ac2feae20095dc7287dedec70bdeb8949b086ea6ce3c3a33583063e978003ab',
}
# Quartus classifies the forwarded clock separately. The remaining raw output
# findings are 29 HDMI data/control pins and the asynchronous board status LED.
OUTPUTS = {'HDMI_TX_DE', 'HDMI_TX_HS', 'HDMI_TX_VS', 'HDMI_I2C_SCL',
           'HDMI_I2C_SDA', 'LED[7]', *[f'HDMI_TX_D[{n}]' for n in range(24)]}


def require(condition, message):
    if not condition:
        raise BuildError('DE25 Terasic HDMI reference: ' + message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def pins(text):
    result = {}
    # Keep the existing complete HDMI selector and bind the status LED from
    # the same golden board. An LED has no external synchronous receiver.
    selected = display.pin_assignments(text).splitlines()
    led = [line for line in text.splitlines() if re.fullmatch(
        r'(set_location_assignment\s+PIN_\w+|set_instance_assignment\s+-name IO_STANDARD\s+"[^"]+")\s+-to\s+LED\[7\]\s*', line)]
    require(len(led) == 2 and len({line.split()[0] for line in led}) == 2,
            'status LED pin/standard assignments changed')
    for line in selected + led:
        port = line.rsplit(' ', 1)[1]
        entry = result.setdefault(port, {})
        if line.startswith('set_location_assignment '):
            entry['pin'] = line.split()[1].removeprefix('PIN_')
        else:
            entry['standard'] = line.split('"')[1]
    return result


def lut(text):
    rows = re.findall(r"^\s*(\d+)\s*:\s*LUT_DATA\s*<=\s*16'h([0-9a-fA-F]{4})", text, re.M)
    require(len(rows) == 33 and {int(n) for n, _ in rows} == set(range(33)),
            'expected all 33 transmitter register writes')
    return [v.lower() for _, v in sorted(rows, key=lambda r: int(r[0]))]


def profile(members):
    require(set(members) == set(MEMBERS) and
            all(sha(members[n]) == digest for n, digest in MEMBERS.items()),
            'selected vendor files differ from the locked resource package')
    expected = pins(members[PREFIX + 'golden_top.qsf'].decode())
    require(expected == pins(members['Demonstration/FPGA/Golden_top/golden_top.qsf'].decode()),
            'HDMI example differs from the SCS golden pin assignments')
    pll, _ = display.retarget_pll(members[PREFIX + 'sys_pll.ip'])
    derived, _ = hdmi_inputs.derive(members[PREFIX + 'HDMI_I2C/I2C_HDMI_Config.v'],
                                    members[PREFIX + 'HDMI_I2C/I2C_Controller.v'])
    return {'pins': expected, 'pll_parameters': display.pll_parameters(pll),
            'transmitter_lut': lut(hdmi_inputs.clear_first(members[PREFIX + 'HDMI_I2C/I2C_HDMI_Config.v'].decode())),
            'transmitter_order': 'reviewed clear-first permutation; all stock values retained',
            'derived_hdmi_sha256': {n: sha(data) for n, data in derived.items()}}


def validate_project(project, expected, core_id, *, standard_core=False):
    project = Path(project)
    require(pins((project / 'golden_top.qsf').read_text()) == expected['pins'],
            'fitted project changed a vendor pin or I/O standard')
    actual_pins = {}
    normalize = lambda value: {'3.3v': '3.3vlvcmos'}.get(
        re.sub(r'[ -]', '', value).lower(), re.sub(r'[ -]', '', value).lower())
    for line in (project / 'output_files/golden_top.pin').read_text().splitlines():
        fields = [v.strip() for v in line.split(':')]
        if len(fields) >= 6 and fields[0] in expected['pins']:
            port = fields[0]
            require(port not in actual_pins, 'duplicate fitted pin ' + port)
            actual_pins[port] = {'pin': fields[1], 'standard': normalize(fields[3])}
    require(actual_pins == {n: {'pin': v['pin'], 'standard': normalize(v['standard'])}
                            for n, v in expected['pins'].items()},
            'actual fitted pin locations/I/O standards differ from Terasic')
    require(type(standard_core) is bool and (not standard_core or core_id == 'fes.zx81'),
            'unsupported standard-core reference selection')
    board_name, pixel, pll_name = {
        'fes.menu': ('fes_de25_scanout_board.v', 'pixel_clk', 'fes_combined/sys_pll.ip'),
        'fes.zx81': ('board.v', 'pixel_clk', 'sys_pll.ip'),
    }['fes.menu' if standard_core else core_id]
    assigned = re.findall(r'^\s*set_global_assignment\s+-name IP_FILE\s+(\S+)\s*$',
                          (project / 'golden_top.qsf').read_text(), re.M)
    require([name.strip('"') for name in assigned
             if Path(name.strip('"')).name == 'sys_pll.ip'] == [pll_name],
            'project does not select the checked video PLL')
    pll = project / pll_name
    require(display.pll_parameters(pll.read_bytes()) == expected['pll_parameters'],
            'vendor pixel PLL parameters changed')
    board = project / 'fes_mailbox' / board_name
    text = board.read_text()
    require(len(re.findall(r'assign\s+HDMI_TX_CLK\s*=\s*' + pixel + r'\s*;', text)) == 1 and
            len(re.findall(r'\.refclk\s*\(CLOCK0_50\)', text)) == 1 and
            len(re.findall(r'\.outclk_0\s*\(' + pixel + r'\)', text)) == 1,
            'forwarded clock/source differs from the selected vendor profile')
    config = project / 'fes_mailbox/fes_de25_scanout_hdmi_config.v'
    require(lut(config.read_text()) == expected['transmitter_lut'],
            'transmitter values/reviewed clear-first order differ')
    require({name: sha((project / 'fes_mailbox' / name).read_bytes())
             for name in expected['derived_hdmi_sha256']} == expected['derived_hdmi_sha256'],
            'compiled HDMI controller/configuration differs from the checked vendor derivation')
    paths = [project / 'golden_top.qsf', project / 'output_files/golden_top.pin', pll, board, config]
    paths += [project / 'fes_mailbox' / name for name in expected['derived_hdmi_sha256']
              if name != config.name]
    return {'policy': POLICY, 'archive_sha256': ARCHIVE_SHA256,
            'reference': 'Terasic rev-B resource package HDMI_ASx4 and SCS Golden_top',
            'pins': expected['pins'], 'pll_parameters': expected['pll_parameters'],
            'transmitter_lut': expected['transmitter_lut'],
            'transmitter_order': expected['transmitter_order'],
            'derived_hdmi_sha256': expected['derived_hdmi_sha256'],
            'compiled_files_sha256': {p.relative_to(project).as_posix(): sha(p.read_bytes()) for p in paths},
            'independent_pcb_timing_qualified': False,
            'acceptance_basis': 'User-selected stock Terasic Linux/resources board setup; pins/PLL/values retained; functional clear-first order checked',
            'hardware_acceptance': False}


def output_findings(drc):
    sections = [s for s in re.split(r'(?m)^Status:\s*', drc)[1:]
                if re.search(r'^; TMC-20012 - [^;]+;', s, re.M)]
    require(len(sections) == 1, 'missing/ambiguous output finding detail')
    rows = [[c.strip() for c in line.split(';')[1:-1]] for line in sections[0].splitlines() if line.startswith(';')]
    require(['Port', 'Reason', 'Waived'] in rows, 'missing output finding columns')
    rows = [r for r in rows if len(r) == 3 and r[0] != 'Port']
    require(len(rows) == 30 and {r[0] for r in rows} == OUTPUTS and
            all(r[1] == 'No output delay was set on the output port.' and not r[2] for r in rows),
            'only the 29 vendor HDMI outputs and asynchronous status LED are covered')
    return sorted(OUTPUTS)


def collect(directory, destination, archive):
    destination = Path(destination)
    require(not destination.exists(), 'review destination already exists')
    archive = Path(archive)
    with archive.open('rb') as stream:
        require(hashlib.file_digest(stream, 'sha256').hexdigest() == ARCHIVE_SHA256, 'vendor archive changed')
    with zipfile.ZipFile(archive) as package:
        require(all(package.namelist().count(n) == 1 for n in MEMBERS), 'missing/duplicate reference member')
        members = {n: package.read(n) for n in MEMBERS}
    expected = profile(members)
    record = json.loads((Path(directory).parent / 'build-inputs.json').read_text())
    require(record['parameters'].get('hdmi_output_policy') == POLICY, 'build did not select the vendor board profile')
    result = validate_project(Path(directory) / 'combined/hps/project', expected, record['parameters']['core_id'], standard_core=record['parameters'].get('standard_core', False))
    destination.mkdir()
    for name, data in members.items():
        path = destination / 'vendor' / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    (destination / 'receipt.json').write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
    return result


def verify(directory, destination):
    destination = Path(destination)
    members = {n: (destination / 'vendor' / n).read_bytes() for n in MEMBERS}
    record = json.loads((Path(directory).parent / 'build-inputs.json').read_text())
    require(record['parameters'].get('hdmi_output_policy') == POLICY, 'build did not select the vendor board profile')
    result = validate_project(Path(directory) / 'combined/hps/project', profile(members), record['parameters']['core_id'], standard_core=record['parameters'].get('standard_core', False))
    require(result == json.loads((destination / 'receipt.json').read_text()), 'reference or fitted artifact changed')
    return result
