// A variable index into a packed-struct member whose name matches a module signal.
// rec.data is 8'hA5, set by concatenation so no assignment-pattern key names `data` (that clash
// is f04b). The module signal data is driven by input pins. ok is a constant 1 when rec.data[v]
// is read; if data[v] (the module signal) is read instead, ok depends on the pins.
module f04_struct_shadow import census_pkg::*; (input logic [7:0] pins, input logic [2:0] v, output logic bit_o, output logic ok);
    rec_t rec;
    logic [7:0] data;
    assign rec = {8'hA5, 4'h3};
    assign data = pins;
    assign bit_o = rec.data[v];
    assign ok = (bit_o == ((8'hA5 >> v) & 1'b1));
endmodule
