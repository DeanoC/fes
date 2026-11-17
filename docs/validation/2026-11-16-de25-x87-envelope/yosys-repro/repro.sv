// DeanoC/yosys: reading a packed-struct MEMBER of an UNPACKED-ARRAY element
// is implicitly declared as a fresh, undriven identifier and reads as zero.
// Expected: `out` follows `in` through the pipeline. Observed in the netlist:
// `out` is constant zero, and the log warns
//   Warning: Identifier `\stage[2].value' is implicitly declared.
// A warning, not an error, so the netlist silently computes nonsense.
// Workaround: read the whole element into a wire first (see `ok_out`).
module repro (input logic clk, input logic [7:0] in, output logic [7:0] out, output logic [7:0] ok_out);
    typedef struct packed { logic [7:0] value; logic flag; } item_t;
    item_t stage [1:2];
    item_t copy2;
    assign copy2 = stage[2];
    always_ff @(posedge clk) begin
        stage[1].value <= in;
        stage[1].flag  <= 1'b1;
        stage[2] <= stage[1];
        out    <= stage[2].value;   // miscompiled: reads as zero
        ok_out <= copy2.value;      // correct
    end
endmodule
