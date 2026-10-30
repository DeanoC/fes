// SPDX-License-Identifier: GPL-2.0-or-later
// Quartus transport for the board's two OSS boundary wrappers. These do not
// replace machine logic, change the clock phases or add a register stage.
module MISTRAL_IO(input I, OE, output O, inout PAD);
    assign PAD = OE ? I : 1'bz;
    assign O = PAD;
endmodule

module MISTRAL_CLKBUF(input A, output Q);
    cyclonev_clkena #(
        .clock_type("global clock"), .ena_register_mode("always enabled")
    ) buffer (.inclk(A), .ena(1'b1), .outclk(Q));
endmodule

// Yosys elaborates the board while Quartus implements this native atom.
(* blackbox *) module cyclonev_clkena(input inclk, ena, output outclk, enaout);
    parameter clock_type = "auto", ena_register_mode = "always enabled";
endmodule
