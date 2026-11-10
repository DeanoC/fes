// IW'(k) inside a for loop (fes_i386_tlb.sv:58). v is a constant, so slot folds to a constant:
// the lowest clear bit of 8'b1110_1101 is bit 1, so slot = 3'd1 and ok = 1.
module f06a_size_cast #(parameter int N = 8, parameter int IW = 3) (output logic [IW-1:0] slot, output logic ok);
    localparam logic [N-1:0] V = 8'b1110_1101;
    always_comb begin
        slot = '0;
        for (int k = N - 1; k >= 0; k--) if (!V[k]) slot = IW'(k);
    end
    assign ok = (slot == 3'd1);
endmodule
