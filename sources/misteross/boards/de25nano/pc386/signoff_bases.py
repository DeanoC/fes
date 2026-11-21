"""pc386's reviewed signoff bases (scanout/machine_findings.py is the engine).

Each crossing basis names its rows, the pc386-clocks.sdc lines that bound
them, and the synchroniser or handshake that qualifies them. SOURCES pins
the shared RTL those arguments rest on, as the record captured it, so a
change to a handshake forces the bases to be reviewed again. This file is
in pc386's source closure only.
"""
import re

from boards.de25nano.scanout.machine_findings import S, E, nodes, bus, crossing, advisory, lint

TABLE = 'boards/de25nano/pc386/signoff_bases.py'
SOURCES = {
    'cores/fes-pc386/rtl/pc386_presenter.sv': 'aed6df2bcb71cf1d975d5591d20d3c35258a25839743f088f9a29bc028a251b3',
    'cores/fes-pc386/rtl/pc386_vga_scanout.sv': '281fe4b87ff2016fdab187a83df17b02443b2b8c44729d940494c9a2b994d0a3',
    'cores/fes-pc386/rtl/pc386_snapshot.sv': '795254ffa671badf5c158abdc4b660461ad239683030546f4c5a9fd7bc5237c4',
    'cores/fes-pc386/rtl/pc386_machine.sv': 'c1aa79dbefc5c1632601b6097765249c68ed01f0d172ef7d6087e1f71893dabc',
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
           'meets setup, hold, recovery and removal at every corner, which the gate requires separately.')
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
