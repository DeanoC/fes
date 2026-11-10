// A named assignment pattern whose member key (data:) matches a module signal (data).
// ok is a constant 1 when rec.data is 8'hA5.
module f04b_pattern_key_shadow import census_pkg::*; (input logic [7:0] pins, output logic [7:0] d, output logic ok);
    rec_t rec;
    logic [7:0] data;
    assign rec = '{data: 8'hA5, tag: 4'h3};
    assign data = pins;
    assign d = rec.data;
    assign ok = (rec.data == 8'hA5);
endmodule
