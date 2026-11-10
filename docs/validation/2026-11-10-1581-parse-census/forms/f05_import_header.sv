// module X import pkg::*; #(...) (...)
module f05_import_header import census_pkg::*; #(parameter int W = 4) (input logic [W-1:0] a, output rec_t r, output logic ok);
    assign r = '{data: 8'(a), tag: 4'(a)};
    assign ok = (r.tag == 4'(a));
endmodule
