// Simulation only: nextpnr MISTRAL_FF with inactive ACLR when tied high.
module MISTRAL_FF (
    input wire DATAIN,
    input wire CLK,
    input wire ACLR,
    input wire ENA,
    input wire SCLR,
    input wire SLOAD,
    input wire SDATA,
    output wire Q
);
    reg q;
    assign Q = q;
    always @(posedge CLK or negedge ACLR) begin
        if (!ACLR)
            q <= 1'b0;
        else if (ENA) begin
            if (SLOAD)
                q <= SDATA;
            else if (SCLR)
                q <= 1'b0;
            else
                q <= DATAIN;
        end
    end
endmodule
