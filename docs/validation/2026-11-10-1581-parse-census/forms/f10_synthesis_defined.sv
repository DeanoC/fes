// Is SYNTHESIS predefined? syn is 1 if it is (OPL2 control_operators.sv:444 relies on it).
module f10_synthesis_defined (output logic syn);
`ifdef SYNTHESIS
    assign syn = 1'b1;
`else
    assign syn = 1'b0;
`endif
endmodule
