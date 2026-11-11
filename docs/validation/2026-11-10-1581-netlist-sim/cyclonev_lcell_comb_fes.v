// S1c trial model, not production code: a two-state transliteration of Intel's
// cyclonev_lcell_comb (cyclonev_atoms.v:567) as continuous assignments. Intel's model
// works under Verilator 5.032 but is one level-sensitive always block per cell that
// infers a latch, and the netlist then simulates about a hundred times slower than the
// RTL. This cell is optional and is here to measure that cost. It has no X propagation.
`timescale 1 ps / 1 ps
module cyclonev_lcell_comb (
    input dataa, datab, datac, datad, datae, dataf, datag, cin, sharein,
    output combout, sumout, cout, shareout
);
    parameter [63:0] lut_mask = 64'hFFFFFFFFFFFFFFFF;
    parameter shared_arith = "off";
    parameter extended_lut = "off";
    parameter dont_touch = "off";
    parameter lpm_type = "cyclonev_lcell_comb";

    localparam EXTENDED = extended_lut == "on";
    localparam SHARED = shared_arith == "on";

    wire in3 = EXTENDED ? datag : datac;
    wire f0 = lut_mask[{2'b00, datad, datac, datab, dataa}];
    wire f1 = lut_mask[{2'b01, datad, in3, datab, dataa}];
    wire f2 = lut_mask[{2'b10, datad, datac, datab, dataa}];
    wire f3 = lut_mask[{2'b11, datad, in3, datab, dataa}];
    wire f2_f = lut_mask[{2'b10, dataf, datac, datab, dataa}];
    wire addend = SHARED ? sharein : !f2_f;

    assign combout = EXTENDED ? (dataf ? (datae ? f3 : f2) : (datae ? f1 : f0))
                              : lut_mask[{dataf, datae, datad, datac, datab, dataa}];
    assign sumout = cin ^ f0 ^ addend;
    assign cout = (cin & f0) | (cin & addend) | (f0 & addend);
    assign shareout = f2;
endmodule
