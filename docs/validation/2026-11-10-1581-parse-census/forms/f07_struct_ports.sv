// Struct-typed ports, across a sub-instance.
module f07_sub import census_pkg::*; (input rec_t i_rec, output rec_t o_rec);
    assign o_rec = '{data: ~i_rec.data, tag: i_rec.tag};
endmodule
module f07_struct_ports import census_pkg::*; (input rec_t a, output rec_t b, output logic ok);
    f07_sub u (.i_rec(a), .o_rec(b));
    assign ok = (b.data == ~a.data) && (b.tag == a.tag);
endmodule
