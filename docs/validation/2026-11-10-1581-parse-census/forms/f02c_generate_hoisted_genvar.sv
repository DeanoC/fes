// The rewrite the common-subset rule prescribes: genvar declared before the loop, loop inside
// generate/endgenerate.
module f02c_generate_hoisted_genvar (input logic [7:0] a, output logic [7:0] y, output logic ok);
    genvar i;
    generate
        for (i = 0; i < 8; i++) begin : g
            assign y[i] = a[7 - i];
        end
    endgenerate
    assign ok = (y == {a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7]});
endmodule
