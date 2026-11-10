// Check mechanism (c): a failed check includes a file that does not exist; the name is the message.
`ifndef CENSUS_FRONT_END
`include "FES_TOOLCHAIN_ERROR_no_front_end.svh"
`endif
module f12a_check_include (output logic ok);
    assign ok = 1'b1;
endmodule
