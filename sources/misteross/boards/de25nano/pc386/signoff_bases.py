"""pc386's reviewed signoff bases (scanout/machine_findings.py is the engine).

Each crossing basis names its rows, the pc386-clocks.sdc lines that bound
them, and the synchroniser or handshake that qualifies them. SOURCES pins
the shared RTL those arguments rest on, as the record captured it, so a
change to a handshake forces the bases to be reviewed again. This file is
in pc386's source closure only.
"""
import re

from boards.de25nano.scanout.machine_findings import S, E, MACHINE, nodes, crossing, advisory

# A fitter copy of a register keeps its name with a suffix. The recorded fits'
# reports and timing.tsv files spell it ~DUPLICATE, or ~_Duplicate with no
# number, a number (~_Duplicate1, ~_Duplicate0) or an underscored one
# (~_Duplicate_2), and the fitter stacks a further copy as a bare DUPLICATE
# (~_Duplicate1DUPLICATE, ~_Duplicate_1DUPLICATE, ~_DuplicateDUPLICATE). No
# recorded name mixes ~DUPLICATE with ~_Duplicate, so COPY takes one or the
# other. The fit picks which, so a basis that names a register admits each of
# those spellings of a copy of it, and no other name. Deano accepted fitter
# copies of the names the bases accept on 24 Nov 2026 (#2187), when a fit
# reported cpu_biu_reset~_Duplicate.
COPY = r'(?:~DUPLICATE|~_Duplicate(?:_?\d+)?(?:DUPLICATE)?)?'


def regs(*names):
    """system| registers, spelled as the SDC line that bounds them collects them: name[*] is any one
    bit of the bus (or the report's name[*] group of it), any other name is that register exactly.

    machine_findings.bus admits any index or none for every name, wider than the SDC collections;
    it is in every DE25 machine's closure, so the pc386 table builds its own rather than move
    their BUILD_IDs."""
    return S + '(?:' + '|'.join(re.escape(n[:-3]) + r'\[(?:\d+|\*)\]' if n.endswith('[*]') else re.escape(n)
                               for n in names) + ')'


def lint(name, driver, qualifier):
    # machine_findings.lint admits only ~_Duplicate_N; it is in every DE25 machine's closure, so the
    # pc386 table builds its own rather than move their BUILD_IDs.
    return {'rule': 'LNT-30010', 'name': name, 'count': None, 'sdc': (), 'qualifier': qualifier,
            'match': {0: S + re.escape(driver) + COPY}}

TABLE = 'boards/de25nano/pc386/signoff_bases.py'
SOURCES = {
    'cores/fes-pc386/rtl/pc386_presenter.sv': 'aed6df2bcb71cf1d975d5591d20d3c35258a25839743f088f9a29bc028a251b3',
    'cores/fes-pc386/rtl/pc386_vga_scanout.sv': '03d194d816290a6f6977cc84dfe5c64be374f1ca4eb3a297b1562495b0186fec',
    'cores/fes-pc386/rtl/pc386_snapshot.sv': '795254ffa671badf5c158abdc4b660461ad239683030546f4c5a9fd7bc5237c4',
    'cores/fes-pc386/rtl/pc386_machine.sv': 'c1aa79dbefc5c1632601b6097765249c68ed01f0d172ef7d6087e1f71893dabc',
    'cores/fes-pc386/rtl/pc386_io.sv': '4ae84cced9c83f8e13b8811a6fcd43130f23f10e38931214d9505b4f19296ece',
    'cores/fes-pc386/rtl/pc386_audio.sv': '3e3d1f4f7e872b9c8e99356099ed110f7040928c32c5ba91a981276cc2a4993a',
    'boards/de25nano/pc386/fes_de25_hps_window_control.v':
        'e908ff30afeec9e1ea0fc51a7c85afe8d8a572b81a1604c1dde09e859ed695bf',
}


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
           'bounds, less than the two pixel-clock synchroniser stages (26.9 ns at 74.25 MHz) before the copy.')
FETCH = ('Pixel to machine: the fetch window {epoch, width, height, request limit, shown buffer}. '
         'pc386_presenter.sv assigns the window on the pixel edge that flips '
         'machine_presenter_request_toggle (frame setup, or a row setup that moves the limit), or '
         'earlier, and holds it until the next flip, at least one raster line (1,650 pixel clocks) '
         'later. The toggle crosses a three-stage machine-clock synchroniser '
         '(machine_presenter_request_sync); the machine side copies the window only when stages 2 and '
         '1 differ. The bundle paths are bounded to 10 ns, less than two machine clocks (32.6 ns).')
LOADED = ('Machine to pixel: the loaded-row report {row, epoch}. pc386_presenter.sv assigns loaded_out '
          'and loaded_epoch_out on the machine edge that flips machine_presenter_loaded_flag, when a row '
          'fetch completes, and changes them only when the next one completes. That takes at least five '
          'machine clocks (81.4 ns at 61.437908 MHz): the next row starts (f_busy), issues its read '
          '(ddr_read), the read is taken, its one beat at the least returns (f_received), and the row '
          'completes. The flag crosses a three-stage pixel-clock synchroniser '
          '(machine_presenter_loaded_sync), and the pixel side copies the report when stages 2 and 1 '
          'differ: at most three pixel clocks (40.4 ns) after the flag reaches stage 0, plus the bundle '
          'paths\' 10 ns bound, 50.4 ns in all. The remaining 31 ns is what the flag\'s own route into '
          'stage 0 may take; that route is cut by the false path, not bounded.')
COUNTS = ('Pixel to machine: the 96-bit status counters through pc386_snapshot.sv. The machine asks with '
          'a toggle (pixel_counts_ask, two-stage pixel synchroniser); the pixel side copies the value '
          'into pixel_counts_copy and flips pixel_counts_answer, which crosses a two-stage machine-clock '
          'synchroniser (pixel_counts_answer_sync); the machine takes the copy only once that answer has '
          'crossed, and the copy does not change until the next ask. The design assistant reports the '
          'copy as a CE-type bus followed by a synchroniser. The bundle paths are bounded to 10 ns.')
RESET = ('Machine to pixel: pc386_machine.sv synchronises (reset || !pixel_locked) into the pixel clock with '
         'a two-stage synchroniser (machine_pixel_reset_sync, power-up asserted). The launch from the '
         'machine reset register into stage 0 is a named false path; the synchronised reset\'s loads are '
         'timed in the pixel clock. Observed in STA on the fit: the crossing path is cut, and the one timed '
         'path into stage 0 is the pixel PLL lock\'s, from its pixel-clock reset synchroniser '
         '(rst_controller_001). The design assistant still reports the chain.')
TOGGLES = ('The presenter\'s three handshake toggles, each into its own three-stage synchroniser in '
           'pc386_presenter.sv: the capture sequence (machine_frame_sequence[0] into '
           'machine_presenter_sequence_sync) and the loaded-row flag (machine_presenter_loaded_flag into '
           'machine_presenter_loaded_sync), machine to pixel; the fetch request '
           '(machine_presenter_request_toggle into machine_presenter_request_sync), pixel to machine. Each '
           'stage 0 drives only stage 1. The launch into each stage 0 is a named false path, and the board '
           'assigns SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS to the three chains (pc386/build.py '
           'ASSIGNMENTS), so the design assistant finds them as synchronisers. Observed in STA on the fit: each '
           'crossing path into stage 0 is cut, and the one timed path into each stage 0 is its synchronous '
           'reset in the receiving clock (pixel_reset or the machine reset). The design assistant still '
           'reports the three chains; pixel_counts\' toggles, whose stage 0 has no reset, it does not.')
RAM = ('Area advisory: a small logical RAM placed alone in a physical M20K. These are the machine '
       'netlist\'s cache tags and probes, VGA, keyboard-controller, RTC, CMOS and FDC tables and OPL2 '
       'banks; the window guards\' two-bank command stores (fes_hps_ddr_guard) and the arbiter\'s unpack '
       'FIFOs; and the window bridge\'s four-deep command FIFO (Platform Designer mm_ccb). Area is '
       'bounded by the fit\'s resource report, not by this rule.')
TENSION = ('Placement advisory: registers in the machine netlist with many endpoints spread across the '
           'CPU. The named ones are fes_i386_cpu control registers (stall, drain, wait, reset and fault '
           'flags) and the machine reset; the others are auto-named machine-netlist registers. Every clock '
           'meets setup, hold, recovery and removal at every corner, which the gate requires separately. '
           'A fitter copy of a named register (~_Duplicate_N) is the same register: Deano accepted that '
           'spelling on 23 Nov 2026 (#2036), when the fit at dfacd28e1 split cpu_biu_reset and reported '
           'cpu_biu_reset~_Duplicate_2 (6,484 endpoints) where the fit at 3401bb193 reported the undivided '
           'register (8,001 endpoints); the same copy is reviewed under LNT-30010 biu-reset. '
           'machine_at_decoder_state, _phase and _cycle_port are pc386_io.sv\'s I/O cycle state, phase counter '
           'and latched port (the AT core\'s port decoder), and audio_dsp_write is the register on pc386_audio.sv\'s '
           'Sound Blaster DSP (pc386_sb) write input; all are machine-clock registers. Deano admitted them on 24 Nov 2026 (#2187), when '
           'the i486DX slice-1 fit (seed 1) reported them and their ~DUPLICATE copies.')
# The window control block's held bundles (fes_de25_hps_window_control.v),
# between the board's peripheral clock (clk_sys, 100 MHz) and the machine
# clock. Their rows depend on the design and not on the seed: every seed of
# #2036's own fits reports neither, and every seed of some designs built on it
# reports the same nine base bits. So these two bases bind their rows by name
# and clock and record the count, as the advisories do, rather than pinning it.
SYS = 'pll_inst|iopll_0_outclk0'
WINDOW = E + re.escape('window_control|')
WINDOW_BASE_SDC = (
    '    window_base {window_control|base_lo[*] window_control|base_lo[*]~DUPLICATE*} '
    '{window_control|base_machine[*]} 20 64 20 32',
    '    window_granted {window_control|granted} {window_control|granted_sync[0]} 1',
    '    set first_d [get_pins -compatibility_mode -nocase "${base}|${head}|d"]',
    '    set_false_path -from $launches -to $first_d',
    *BUNDLE_SDC)
WINDOW_FAULT_SDC = (
    '    window_fault_record {window_control|fault_launch[*]} {window_control|machine_fault[*]} 5 5 5 5',
    '    window_fault_request {window_control|fault_request} {window_control|fault_request_sync[0]} 1',
    '    window_fault_ack {window_control|fault_ack} {window_control|fault_ack_sync[0]} 1',
    '    set first_d [get_pins -compatibility_mode -nocase "${base}|${head}|d"]',
    '    set_false_path -from $launches -to $first_d',
    *BUNDLE_SDC)
WINDOW_BASE = ('Peripheral to machine: the grant base. fes_de25_hps_window_control.v writes base_lo only while '
               'granted is clear (a write while granted answers SLVERR), and sets granted from a separate '
               'command write, so base_lo is still from at least one clk_sys clock (10 ns) before granted '
               'launches until granted is cleared. granted is a level into a two-stage machine-clock '
               'synchroniser (granted_sync, its launch into stage 0 a named false path); the machine side '
               'copies base_lo into base_machine only on the clock where granted_sync[1] is first seen set '
               '(granted_sync[1] && !grant_seen), at least two machine clocks (32.6 ns at 61.437908 MHz) after '
               'granted reaches stage 0. The bundle paths are bounded to 10 ns, with skew, net and data delay '
               'bounds, so the copy samples a settled base. A fitter copy of a base_lo bit (~DUPLICATE) is '
               'the same register, as the SDC collection says. clk_sys\'s reset clears base_lo and granted '
               'together. The machine-domain reset clears granted_sync and grant_seen even with its clock stopped, '
               'so the old base cannot authorize traffic after a fabric reset.')
WINDOW_FAULT = ('Machine to peripheral: the fault record (port bits and reason). fes_de25_hps_window_control.v '
                'launches it from fault_launch, which copies the held record every machine clock, on the same '
                'machine edge that launches its request toggle (fault_request); the held record changes only '
                'after clk_sys\'s acknowledge (fault_ack) has crossed back through fault_ack_sync. The request '
                'enters a two-stage clk_sys synchroniser (fault_request_sync, its launch into stage 0 a named '
                'false path), and clk_sys copies fault_launch into machine_fault only on the clock where '
                'fault_request_sync[1] differs from fault_request_seen, at least two clk_sys clocks (20 ns) after '
                'the request reaches stage 0. The bundle paths are bounded to 10 ns.')
BIU_RESET = ('fes_i386_cpu\'s BIU reset, a machine-clock register: it drives synchronous clears, and the '
             'synthesis netlist also uses it in clock enables (fetch prediction). It is not an '
             'asynchronous reset; its paths are timed in the machine clock.')
MACHINE_RESET = ('pc386_system\'s machine reset, a machine-clock register fed by the board\'s reset '
                 'synchroniser: it drives asynchronous clears (8259 masks), synchronous clears and, '
                 'through the netlist, RTC clock enables. Its recovery and removal are timed in the '
                 'machine clock.')

BASES = (
    crossing('CDC-50001', 'presenter-capture', regs('frame_width[*]', 'frame_height[*]'),
             regs('machine_presenter_latest_width[*]', 'machine_presenter_latest_height[*]'),
             'machine-to-pixel', 20, CAPTURE_SDC, CAPTURE),
    crossing('CDC-50002', 'presenter-capture-buffer', regs('frame_buffer[*]'),
             regs('machine_presenter_latest_buffer[*]'), 'machine-to-pixel', 2, CAPTURE_SDC, CAPTURE),
    crossing('CDC-50001', 'presenter-fetch',
             regs('machine_presenter_epoch[*]', 'machine_presenter_width[*]', 'machine_presenter_height[*]',
                  'machine_presenter_request_limit[*]', 'machine_presenter_shown_buffer[*]'),
             regs('machine_presenter_f_epoch[*]', 'machine_presenter_f_new_epoch', 'machine_presenter_f_width[*]',
                  'machine_presenter_f_height[*]', 'machine_presenter_f_limit[*]', 'machine_present_buffer[*]',
                  'machine_presenter_f_buffer[*]'),
             'pixel-to-machine', 35, FETCH_SDC, FETCH),
    crossing('CDC-50001', 'presenter-loaded',
             regs('machine_presenter_loaded_in[*]', 'machine_presenter_loaded_epoch_in[*]'),
             regs('machine_presenter_loaded[*]', 'machine_presenter_loaded_epoch[*]'),
             'machine-to-pixel', 12, LOADED_SDC, LOADED),
    crossing('CDC-50102', 'status-counts', regs('pixel_counts_copy[*]'), regs('pixel_counts_snapshot[*]'),
             'pixel-to-machine', 1, COUNTS_SDC, COUNTS),
    # Each toggle's SDC cuts its launch (and a ~DUPLICATE of it, which no recorded fit makes) into
    # the first stage's d pin only, so a basis admits the launch and that first stage alone, one
    # basis per toggle so that no launch is admitted into another toggle's synchroniser.
    crossing('CDC-50002', 'presenter-sequence-toggle', regs('machine_frame_sequence[0]'),
             regs('machine_presenter_sequence_sync[0]'), 'machine-to-pixel', 1, TOGGLE_SDC, TOGGLES),
    crossing('CDC-50002', 'presenter-loaded-toggle', regs('machine_presenter_loaded_flag'),
             regs('machine_presenter_loaded_sync[0]'), 'machine-to-pixel', 1, TOGGLE_SDC, TOGGLES),
    crossing('CDC-50002', 'presenter-toggle-to-machine', regs('machine_presenter_request_toggle'),
             regs('machine_presenter_request_sync[0]'), 'pixel-to-machine', 1, TOGGLE_SDC, TOGGLES),
    crossing('CDC-50002', 'pixel-reset', regs('reset'), regs('machine_pixel_reset_sync[0]'),
             'machine-to-pixel', 1, RESET_SDC, RESET),
    {'rule': 'CDC-50001', 'name': 'window-base', 'count': None, 'sdc': WINDOW_BASE_SDC,
     'qualifier': WINDOW_BASE,
     # Only the launches window_base's SDC collects (base_lo[*] and base_lo[*]~DUPLICATE*), and of
     # those only the spelling the recorded fits use; a ~_Duplicate copy would sit outside the bounds.
     'match': {0: nodes(WINDOW + r'base_lo\[\d+\](?:~DUPLICATE)?'), 1: nodes(WINDOW + r'base_machine\[\d+\]'),
               2: re.escape(SYS), 3: re.escape(MACHINE)}},
    {'rule': 'CDC-50001', 'name': 'window-fault-record', 'count': None, 'sdc': WINDOW_FAULT_SDC,
     'qualifier': WINDOW_FAULT,
     # window_fault_record's SDC collects fault_launch[*] alone, so no copy is admitted.
     'match': {0: nodes(WINDOW + r'fault_launch\[\d+\]'), 1: nodes(WINDOW + r'machine_fault\[\d+\]'),
               2: re.escape(MACHINE), 3: re.escape(SYS)}},
    lint('biu-reset', 'cpu_biu_reset', BIU_RESET),
    lint('machine-reset', 'reset', MACHINE_RESET),
    advisory('FLP-10000', 'small-rams', 3, nodes(
        '(?:' + S + r'[^|]+|' + E + r'window\|port[0-2]\|command_[a-z]+_rtl_\d+|' + E + r'arbiter\|unpack[12]\|[^ ]+|'
        + re.escape('soc_inst|subsys_periph|fes_win_cdc|fes_win_cdc|cmd_fifo|') + r'[^ ]+)(?:\|[^ ]+)?'), RAM),
    advisory('TMC-20602', 'cpu-control-tension', 0,
             S + r'(?:cpu_[a-z0-9_]+|reset|_\d+_|machine_at_decoder_(?:state|phase|cycle_port)|audio_dsp_write)(?:\[\d+\])?' + COPY, TENSION),
)
