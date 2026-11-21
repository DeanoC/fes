"""Reviewed DE25 signoff findings for a machine compiled from a shared netlist.

pc386's machine is the DE10's pc386_system, compiled from the pc386 lock's
Yosys netlist, and Quartus Pro's design assistant does not recognise every
crossing in it. Deano chose (21 November 2026, #2029) to admit those findings
by reviewed bases, as the Spectrum's are admitted, without changing the
shared RTL and without widening any rule.

A basis admits rows of one rule. It names:

- the exact rows, by a node pattern for each column it binds;
- the constraint in the compiled pc386-clocks.sdc that bounds them, quoted
  verbatim from the file, and that file's digest;
- the synchroniser or handshake that qualifies them, with the source lines
  that show it.

Every row of a reviewed rule must match exactly one basis. A crossing
basis's row count is exact, and a crossing basis that matches nothing is
stale and refused. The advisory rules bind every row by hierarchy and record
the count, which moves from fit to fit. The gate itself is unchanged:
build_menu_package.timing_evidence still requires every failed rule to be
reviewed, and each rule once.
"""
import hashlib
import json
from pathlib import Path
import re

from scripts.fes_build_common import BuildError

POLICY = 'de25-pc386-reviewed-signoff-findings-v1'
CORE = 'fes.pc386'
SDC = 'combined/hps/project/fes_mailbox/machine-clocks.sdc'
QSF = 'combined/hps/project/golden_top.qsf'
E = re.escape('soc_inst|subsys_periph|fes_scanout|fes_scanout|')
S = E + re.escape('system|')
MACHINE = 'fes_machine_pll|machine_pll_outclk0'
PIXEL = 'fes_video_inst|video_clock|iopll_0_outclk0'
CLOCKS = {'machine-to-pixel': (MACHINE, PIXEL), 'pixel-to-machine': (PIXEL, MACHINE)}


def require(condition, message):
    if not condition:
        raise BuildError('DE25 pc386 reviewed signoff findings: ' + message)


def nodes(pattern):
    """A pattern for one cell: one or more space-separated nodes, each matching pattern."""
    return r'(?:%s)(?:\s+(?:%s))*' % (pattern, pattern)


def bus(*names):
    return S + '(?:' + '|'.join(re.escape(n) for n in names) + r')(?:\[(?:\d+|\*)\])?'


def crossing(rule, name, sources, sinks, direction, count, sdc, qualifier):
    return {'rule': rule, 'name': name, 'count': count, 'sdc': sdc, 'qualifier': qualifier,
            'match': {0: nodes(sources), 1: nodes(sinks), 2: re.escape(CLOCKS[direction][0]),
                      3: re.escape(CLOCKS[direction][1])}}


def advisory(rule, name, column, pattern, qualifier):
    return {'rule': rule, 'name': name, 'count': None, 'sdc': (), 'qualifier': qualifier,
            'match': {column: pattern}}


def lint(name, driver, qualifier):
    # The driver, or a fitter duplicate of it: duplication varies from fit to fit.
    return {'rule': 'LNT-30010', 'name': name, 'count': None, 'sdc': (), 'qualifier': qualifier,
            'match': {0: S + re.escape(driver) + r'(?:~_Duplicate_\d+)?'}}


# The constraints each crossing basis quotes, verbatim from pc386-clocks.sdc.
# The handshakes are in cores/fes-pc386/rtl/pc386_presenter.sv (capture,
# fetch, loaded), pc386_vga_scanout.sv (the capture bundle's source),
# pc386_snapshot.sv (counts) and pc386_machine.sv (the pixel reset).
BUNDLE_SDC = ('    set_net_delay -max 10.000 -from $launches -to $captures',
              '    set_data_delay 10.000 -from $launches -to $captures',
              '    set_max_skew 10.000 -from $launches -to $captures',
              '    set_max_delay 10.000 -from $launches -to $captures',
              '    set_min_delay 0.000 -from $launches -to $captures')
CAPTURE_SDC = (
    '    capture {system|frame_buffer[*] system|frame_width[*] system|frame_height[*]}',
    '            {system|machine_presenter_latest_buffer[*] system|machine_presenter_latest_width[*] '
    'system|machine_presenter_latest_height[*]}',
    '    frame_sequence {system|machine_frame_sequence[0]} {system|machine_presenter_sequence_sync[0]}',
    *BUNDLE_SDC)
FETCH_SDC = (
    '    fetch {system|machine_presenter_epoch[*] system|machine_presenter_width[*] '
    'system|machine_presenter_height[*] system|machine_presenter_request_limit[*] '
    'system|machine_presenter_shown_buffer[*]}',
    '    fetch_request {system|machine_presenter_request_toggle} {system|machine_presenter_request_sync[0]}',
    *BUNDLE_SDC)
LOADED_SDC = (
    '    loaded {system|machine_presenter_loaded_in[*] system|machine_presenter_loaded_epoch_in[*]}',
    '           {system|machine_presenter_loaded[*] system|machine_presenter_loaded_epoch[*]}',
    '    fetch_loaded {system|machine_presenter_loaded_flag} {system|machine_presenter_loaded_sync[0]}',
    *BUNDLE_SDC)
COUNTS_SDC = (
    '    counts {system|pixel_counts_copy[*]} {system|pixel_counts_snapshot[*]}',
    '    counts_ask {system|pixel_counts_ask} {system|pixel_counts_ask_sync[0]}',
    '    counts_answer {system|pixel_counts_answer} {system|pixel_counts_answer_sync[0]}',
    *BUNDLE_SDC)
TOGGLE_SDC = (
    '    frame_sequence {system|machine_frame_sequence[0]} {system|machine_presenter_sequence_sync[0]}',
    '    fetch_request {system|machine_presenter_request_toggle} {system|machine_presenter_request_sync[0]}',
    '    fetch_loaded {system|machine_presenter_loaded_flag} {system|machine_presenter_loaded_sync[0]}',
    '    set first_d [get_pins -compatibility_mode -nocase "${base}|${head}|d"]',
    '    set_false_path -from $launches -to $first_d')
TOGGLE_ASSIGNMENT = ('machine_presenter_sequence_sync[*]', 'machine_presenter_request_sync[*]',
                     'machine_presenter_loaded_sync[*]')
RESET_SDC = (
    'set launches [get_registers "${base}|system|reset"]',
    'set first_d [get_pins -compatibility_mode -nocase "${base}|system|machine_pixel_reset_sync[0]|d"]',
    'set_false_path -from $launches -to $first_d')

CAPTURE = ('Machine to pixel: the newest capture {buffer, width, height}. pc386_vga_scanout.sv assigns '
           'frame_buffer, frame_width and frame_height on the edge that increments frame_sequence and '
           'changes them only at the next completed capture, a VGA frame later. Bit 0 of the sequence is '
           'a toggle (machine_frame_sequence[0]) into a three-stage pixel-clock synchroniser '
           '(machine_presenter_sequence_sync); pc386_presenter.sv copies the bundle only when stages 2 '
           'and 1 differ, so the bundle has been still since the toggle left and stays still for a VGA '
           'frame after the copy. The bundle paths are bounded to 10 ns, with skew, net and data delay '
           'bounds, less than the two synchroniser stages (32.6 ns at 61.4 MHz) before the copy.')
FETCH = ('Pixel to machine: the fetch window {epoch, width, height, request limit, shown buffer}. '
         'pc386_presenter.sv assigns the window on the pixel edge that flips '
         'machine_presenter_request_toggle (frame setup, or a row setup that moves the limit), or '
         'earlier, and holds it until the next flip, at least one raster line (1,650 pixel clocks) '
         'later. The toggle crosses a three-stage machine-clock synchroniser '
         '(machine_presenter_request_sync); the machine side copies the window only when stages 2 and '
         '1 differ. The bundle paths are bounded to 10 ns, less than two machine clocks (32.6 ns).')
LOADED = ('Machine to pixel: the loaded-row report {row, epoch}. pc386_presenter.sv assigns loaded_out '
          'and loaded_epoch_out on the machine edge that flips machine_presenter_loaded_flag at the end '
          'of a row fetch, and holds them until the next row fetch ends, at least one DDR read burst '
          'later. The flag crosses a three-stage pixel-clock synchroniser (machine_presenter_loaded_sync); '
          'the pixel side copies the report only when stages 2 and 1 differ. The bundle paths are '
          'bounded to 10 ns, less than two pixel clocks (26.9 ns).')
COUNTS = ('Pixel to machine: the 96-bit status counters through pc386_snapshot.sv. The machine asks with '
          'a toggle (pixel_counts_ask, two-stage pixel synchroniser); the pixel side copies the value '
          'into pixel_counts_copy and flips pixel_counts_answer, which crosses a two-stage machine-clock '
          'synchroniser (pixel_counts_answer_sync); the machine takes the copy only once that answer has '
          'crossed, and the copy does not change until the next ask. The design assistant reports the '
          'copy as a CE-type bus followed by a synchroniser. The bundle paths are bounded to 10 ns.')
RESET = ('Machine to pixel: pc386_machine.sv synchronises (reset || !pixel_locked) into the pixel clock with '
         'a two-stage synchroniser (machine_pixel_reset_sync, power-up asserted). The launch from the '
         'machine reset register into stage 0 is a named false path; the synchronised reset\'s loads are '
         'timed in the pixel clock. The stage-0 input is that OR with the pixel PLL\'s lock, so the design '
         'assistant finds the false path insufficient for the reset alone.')
TOGGLES = ('The presenter\'s three handshake toggles, each into its own three-stage synchroniser in '
           'pc386_presenter.sv: the capture sequence (machine_frame_sequence[0] into '
           'machine_presenter_sequence_sync) and the loaded-row flag (machine_presenter_loaded_flag into '
           'machine_presenter_loaded_sync), machine to pixel; the fetch request '
           '(machine_presenter_request_toggle into machine_presenter_request_sync), pixel to machine. Each '
           'stage 0 drives only stage 1. The launch into each stage 0 is a named false path, and the board '
           'assigns SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS to the three chains (pc386/build.py '
           'ASSIGNMENTS), so the design assistant finds them as synchronisers. Each stage 0 also has a '
           'synchronous reset in its receiving clock ahead of D (pixel_reset or the machine reset), so the '
           'design assistant reports the false path as insufficient; that reset\'s path is timed in the '
           'receiving clock.')
RAM = ('Area advisory: a small logical RAM placed alone in a physical M20K. These are the machine '
       'netlist\'s cache tags and probes, VGA, keyboard-controller, RTC, CMOS and FDC tables and OPL2 '
       'banks; the window guards\' two-bank command stores (fes_hps_ddr_guard) and the arbiter\'s unpack '
       'FIFOs; and the window bridge\'s four-deep command FIFO (Platform Designer mm_ccb). Area is '
       'bounded by the fit\'s resource report, not by this rule.')
TENSION = ('Placement advisory: fes_i386_cpu control registers in the machine netlist (stall, drain, wait, '
           'reset and fault flags, and the synthesis netlist\'s auto-named copies of them) whose many '
           'endpoints spread across the CPU. Every clock meets setup, hold, recovery and removal at every '
           'corner, which the gate requires separately.')
BIU_RESET = ('fes_i386_cpu\'s BIU reset, a machine-clock register: it drives synchronous clears, and the '
             'synthesis netlist also uses it in clock enables (fetch prediction). It is not an '
             'asynchronous reset; its paths are timed in the machine clock.')
MACHINE_RESET = ('pc386_system\'s machine reset, a machine-clock register fed by the board\'s reset '
                 'synchroniser: it drives asynchronous clears (8259 masks), synchronous clears and, '
                 'through the netlist, RTC clock enables. Its recovery and removal are timed in the '
                 'machine clock.')

BASES = (
    crossing('CDC-50001', 'presenter-capture', bus('frame_width', 'frame_height'),
             bus('machine_presenter_latest_width', 'machine_presenter_latest_height'),
             'machine-to-pixel', 20, CAPTURE_SDC, CAPTURE),
    crossing('CDC-50002', 'presenter-capture-buffer', bus('frame_buffer'),
             bus('machine_presenter_latest_buffer'), 'machine-to-pixel', 2, CAPTURE_SDC, CAPTURE),
    crossing('CDC-50001', 'presenter-fetch',
             bus('machine_presenter_epoch', 'machine_presenter_width', 'machine_presenter_height',
                 'machine_presenter_request_limit', 'machine_presenter_shown_buffer'),
             bus('machine_presenter_f_epoch', 'machine_presenter_f_new_epoch', 'machine_presenter_f_width',
                 'machine_presenter_f_height', 'machine_presenter_f_limit', 'machine_present_buffer',
                 'machine_presenter_f_buffer'),
             'pixel-to-machine', 35, FETCH_SDC, FETCH),
    crossing('CDC-50001', 'presenter-loaded',
             bus('machine_presenter_loaded_in', 'machine_presenter_loaded_epoch_in'),
             bus('machine_presenter_loaded', 'machine_presenter_loaded_epoch'),
             'machine-to-pixel', 12, LOADED_SDC, LOADED),
    crossing('CDC-50102', 'status-counts', bus('pixel_counts_copy'), bus('pixel_counts_snapshot'),
             'pixel-to-machine', 1, COUNTS_SDC, COUNTS),
    crossing('CDC-50002', 'presenter-toggles-to-pixel',
             bus('machine_frame_sequence', 'machine_presenter_loaded_flag'),
             bus('machine_presenter_sequence_sync', 'machine_presenter_loaded_sync'),
             'machine-to-pixel', 2, TOGGLE_SDC, TOGGLES),
    crossing('CDC-50002', 'presenter-toggle-to-machine', bus('machine_presenter_request_toggle'),
             bus('machine_presenter_request_sync'), 'pixel-to-machine', 1, TOGGLE_SDC, TOGGLES),
    crossing('CDC-50002', 'pixel-reset', bus('reset'), bus('machine_pixel_reset_sync'),
             'machine-to-pixel', 1, RESET_SDC, RESET),
    lint('biu-reset', 'cpu_biu_reset', BIU_RESET),
    lint('machine-reset', 'reset', MACHINE_RESET),
    advisory('FLP-10000', 'small-rams', 3, nodes(
        '(?:' + S + r'[^|]+|' + E + r'window\|port[0-2]\|command_[a-z]+_rtl_\d+|' + E + r'arbiter\|unpack[12]\|[^ ]+|'
        + re.escape('soc_inst|subsys_periph|fes_win_cdc|fes_win_cdc|cmd_fifo|') + r'[^ ]+)(?:\|[^ ]+)?'), RAM),
    advisory('TMC-20602', 'cpu-control-tension', 0, S + r'(?:cpu_[a-z0-9_]+|reset|_\d+_)(?:\[\d+\])?(?:~DUPLICATE)?',
             TENSION),
)


def selected(directory):
    # Without a producer record nothing is reviewed, so every finding stays
    # unreviewed and the gate refuses: the absence can only make it stricter.
    path = Path(directory).parent / 'build-inputs.json'
    if not path.exists():
        return False
    record = json.loads(path.read_text())
    policy = record['parameters'].get('signoff_findings_review_policy')
    require(policy in (None, POLICY), 'unknown policy ' + str(policy))
    require(policy is None or record['parameters']['core_id'] == CORE, 'policy selected for another core')
    return policy == POLICY


def table(drc, rule):
    """The header and data rows of one rule's detail table."""
    sections = [s for s in re.split(r'(?m)^Status:\s*', drc)[1:]
                if re.search('^; ' + re.escape(rule) + r' - [^;]+;', s, re.M)]
    require(len(sections) == 1, 'missing/ambiguous detail for ' + rule)
    lines = [[c.strip() for c in line.split(';')[1:-1]] for line in sections[0].splitlines()
             if line.startswith(';') and not line.startswith('; ' + rule + ' - ')]
    require(lines and lines[0] and lines[0][-1] == 'Waived', rule + ' detail columns changed')
    return lines[0], lines[1:]


def review(drc, sdc_text, bases=None):
    """Match every row of each rule the bases name; return one reviewed entry per failed rule."""
    bases = BASES if bases is None else bases
    summary = {r[0]: int(r[1]) for r in re.findall(
        r'^; ([A-Z]+-\d+) - [^;]+; (?:Critical|High|Medium|Low|Info)\s*;\s*(\d+)\s*;', drc, re.M)}
    sdc_lines = set(sdc_text.splitlines())
    entries = []
    for rule in sorted({b['rule'] for b in bases}):
        if not summary.get(rule):
            continue
        header, rows = table(drc, rule)
        require(len(rows) == summary[rule], rule + ' detail rows differ from its violation count')
        selected_bases = [b for b in bases if b['rule'] == rule]
        matched = {b['name']: [] for b in selected_bases}
        for row in rows:
            require(len(row) == len(header) and not row[-1], rule + ' row changed shape or is waived')
            hits = [b for b in selected_bases
                    if all(re.fullmatch(pattern, row[column]) for column, pattern in b['match'].items())]
            require(len(hits) == 1, rule + ' row matches %d bases: %s' % (len(hits), ' ; '.join(row)[:300]))
            matched[hits[0]['name']].append(row)
        groups = []
        for basis in selected_bases:
            found = matched[basis['name']]
            if basis['count'] is not None:
                require(len(found) == basis['count'],
                        '%s %s covers %d rows, reviewed %d' % (rule, basis['name'], len(found), basis['count']))
            missing = [line for line in basis['sdc'] if line not in sdc_lines]
            require(not missing, '%s %s constraint not in the compiled SDC: %s' % (rule, basis['name'], missing))
            groups.append({'name': basis['name'], 'rows': len(found), 'constraint': list(basis['sdc']),
                           'qualifier': basis['qualifier']})
        entries.append({'id': rule, 'violations': summary[rule], 'basis': POLICY, 'columns': header,
                        'groups': groups})
    return entries


def assignments(qsf_text):
    """The toggle basis relies on the board naming the three chains synchronisers."""
    lines = {' '.join(line.split()) for line in qsf_text.splitlines()}
    missing = [name for name in TOGGLE_ASSIGNMENT if
               'set_instance_assignment -name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS -to "%ssystem|%s"'
               % ('soc_inst|subsys_periph|fes_scanout|fes_scanout|', name) not in lines]
    require(not missing, 'compiled project lacks the synchroniser assignments: ' + ', '.join(missing))


def findings(directory, drc):
    directory = Path(directory)
    sdc, qsf = directory / SDC, directory / QSF
    require(all(p.is_file() and not p.is_symlink() for p in (sdc, qsf)), 'missing/linked compiled SDC or QSF')
    assignments(qsf.read_text())
    entries = review(drc, sdc.read_text())
    digest = hashlib.sha256(sdc.read_bytes()).hexdigest()
    return [dict(entry, sdc_sha256=digest) for entry in entries]
