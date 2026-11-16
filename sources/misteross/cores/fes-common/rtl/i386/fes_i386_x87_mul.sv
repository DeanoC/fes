// SPDX-License-Identifier: MIT
// The 64x64 significand multiply: nine 22x22 products in DSP blocks, summed
// over four clocks. `done` pulses with `p`. Slice 2 uses it for the shifts;
// slice 3's FMUL takes the same block.
module fes_i386_x87_mul (
    input  logic         clk,
    input  logic         reset,
    input  logic         start,
    input  logic [63:0]  a,
    input  logic [63:0]  b,
    output logic         done,
    output logic [127:0] p
);
    logic [63:0] ar, br;
    logic [43:0] p00, p01, p02, p10, p11, p12, p20, p21, p22;
    logic [87:0] r0, r1, r2;
    logic [3:0]  v;
    always_ff @(posedge clk) begin
        v <= reset ? 4'd0 : {v[2:0], start};
        ar <= a;
        br <= b;
        p00 <= ar[21:0] * br[21:0];
        p01 <= ar[21:0] * br[43:22];
        p02 <= ar[21:0] * {2'b0, br[63:44]};
        p10 <= ar[43:22] * br[21:0];
        p11 <= ar[43:22] * br[43:22];
        p12 <= ar[43:22] * {2'b0, br[63:44]};
        p20 <= {2'b0, ar[63:44]} * br[21:0];
        p21 <= {2'b0, ar[63:44]} * br[43:22];
        p22 <= {2'b0, ar[63:44]} * {2'b0, br[63:44]};
        r0 <= {44'd0, p00} + {22'd0, p01, 22'd0} + {p02, 44'd0};
        r1 <= {44'd0, p10} + {22'd0, p11, 22'd0} + {p12, 44'd0};
        r2 <= {44'd0, p20} + {22'd0, p21, 22'd0} + {p22, 44'd0};
        p <= {40'd0, r0} + {18'd0, r1, 22'd0} + {r2[83:0], 44'd0};
    end
    assign done = v[3];
endmodule
