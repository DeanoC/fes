// S1c unit bench: Intel's cyclonev_lcell_comb against the FES trial cell (renamed to
// cyclonev_lcell_comb_fes by lcell_comb_eq.sh), N masks in each of the four
// extended_lut / shared_arith modes, all 512 input values.
module lcell_comb_eq #(parameter N = 64) (
    input [8:0] in,
    output [N*16-1:0] intel,
    output [N*16-1:0] fes
);
    function automatic [63:0] mask(input integer k);
        reg [63:0] x;
        begin
            x = 64'h9E3779B97F4A7C15 * (k + 1);
            x = x ^ (x >> 29);
            x = x * 64'hBF58476D1CE4E5B9;
            mask = x ^ (x >> 32);
        end
    endfunction
    genvar k;
    generate
        for (k = 0; k < N; k = k + 1) begin : g
`define S1C_PAIR(IDX, EXT, SH) \
            cyclonev_lcell_comb #(.lut_mask(mask(k)), .extended_lut(EXT), .shared_arith(SH)) i``IDX ( \
                .dataa(in[0]), .datab(in[1]), .datac(in[2]), .datad(in[3]), .datae(in[4]), \
                .dataf(in[5]), .datag(in[6]), .cin(in[7]), .sharein(in[8]), \
                .combout(intel[k*16+IDX*4]), .sumout(intel[k*16+IDX*4+1]), \
                .cout(intel[k*16+IDX*4+2]), .shareout(intel[k*16+IDX*4+3])); \
            cyclonev_lcell_comb_fes #(.lut_mask(mask(k)), .extended_lut(EXT), .shared_arith(SH)) f``IDX ( \
                .dataa(in[0]), .datab(in[1]), .datac(in[2]), .datad(in[3]), .datae(in[4]), \
                .dataf(in[5]), .datag(in[6]), .cin(in[7]), .sharein(in[8]), \
                .combout(fes[k*16+IDX*4]), .sumout(fes[k*16+IDX*4+1]), \
                .cout(fes[k*16+IDX*4+2]), .shareout(fes[k*16+IDX*4+3]));
            `S1C_PAIR(0, "off", "off")
            `S1C_PAIR(1, "on", "off")
            `S1C_PAIR(2, "off", "on")
            `S1C_PAIR(3, "on", "on")
        end
    endgenerate
endmodule
