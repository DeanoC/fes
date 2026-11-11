// Check mechanism (d): a failed check emits a token sequence no reader can parse at file scope.
`ifndef CENSUS_FRONT_END
FES_TOOLCHAIN_ERROR_no_front_end !;
`endif
module f12b_check_token (output logic ok);
    assign ok = 1'b1;
endmodule
