// SPDX-License-Identifier: GPL-2.0-or-later
// Build-time machine model. The producer defines one of SPECTRUM_MODEL_128K,
// SPECTRUM_MODEL_PLUS3 or SPECTRUM_MODEL_PENTAGON; none is the 48K. The
// define, not a parameter, selects the linked ROM lanes so their routed cell
// names stay flat.
`ifndef SPECTRUM_MODEL_VH
`define SPECTRUM_MODEL_VH
`ifdef SPECTRUM_MODEL_128K
`define SPECTRUM_MODEL 1
`define SPECTRUM_ROM_32K
`elsif SPECTRUM_MODEL_PLUS3
`define SPECTRUM_MODEL 2
`define SPECTRUM_ROM_32K
`define SPECTRUM_ROM_48K
`define SPECTRUM_ROM_64K
`elsif SPECTRUM_MODEL_PENTAGON
`define SPECTRUM_MODEL 3
`define SPECTRUM_ROM_32K
`define SPECTRUM_ROM_48K
`else
`define SPECTRUM_MODEL 0
`endif
`endif
