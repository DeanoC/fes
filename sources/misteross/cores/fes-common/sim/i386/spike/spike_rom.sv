// SPDX-License-Identifier: MIT
// Timing spike (d): a registered case-table ROM, synthesized without
// proc -norom so it maps to an M10K. That needs CLK1 connected to place and
// route (DeanoC/yosys#40); the i386 RTL keeps its own tables in logic by
// choice, so this spike is what checks the fix.
module spike_rom (
    input  wire  FPGA_CLK1_50,
    input  wire  KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic [7:0] a = 8'd1;
    logic [3:0] y, q;
    always_comb begin
        case (a)
            8'h00: y = 4'd4;
            8'h03: y = 4'd2;
            8'h06: y = 4'd8;
            8'h09: y = 4'd3;
            8'h0c: y = 4'd15;
            8'h0f: y = 4'd14;
            8'h12: y = 4'd15;
            8'h15: y = 4'd12;
            8'h18: y = 4'd6;
            8'h1b: y = 4'd3;
            8'h1e: y = 4'd15;
            8'h21: y = 4'd0;
            8'h24: y = 4'd12;
            8'h27: y = 4'd13;
            8'h2a: y = 4'd0;
            8'h2d: y = 4'd14;
            8'h30: y = 4'd8;
            8'h33: y = 4'd7;
            8'h36: y = 4'd3;
            8'h39: y = 4'd10;
            8'h3c: y = 4'd0;
            8'h3f: y = 4'd0;
            8'h42: y = 4'd0;
            8'h45: y = 4'd0;
            8'h48: y = 4'd12;
            8'h4b: y = 4'd6;
            8'h4e: y = 4'd13;
            8'h51: y = 4'd0;
            8'h54: y = 4'd7;
            8'h57: y = 4'd14;
            8'h5a: y = 4'd15;
            8'h5d: y = 4'd7;
            8'h60: y = 4'd11;
            8'h63: y = 4'd7;
            8'h66: y = 4'd7;
            8'h69: y = 4'd14;
            8'h6c: y = 4'd9;
            8'h6f: y = 4'd0;
            8'h72: y = 4'd13;
            8'h75: y = 4'd3;
            8'h78: y = 4'd5;
            8'h7b: y = 4'd9;
            8'h7e: y = 4'd3;
            8'h81: y = 4'd10;
            8'h84: y = 4'd13;
            8'h87: y = 4'd6;
            8'h8a: y = 4'd9;
            8'h8d: y = 4'd9;
            8'h90: y = 4'd15;
            8'h93: y = 4'd12;
            8'h96: y = 4'd1;
            8'h99: y = 4'd15;
            8'h9c: y = 4'd7;
            8'h9f: y = 4'd12;
            8'ha2: y = 4'd13;
            8'ha5: y = 4'd5;
            8'ha8: y = 4'd11;
            8'hab: y = 4'd11;
            8'hae: y = 4'd2;
            8'hb1: y = 4'd14;
            8'hb4: y = 4'd3;
            8'hb7: y = 4'd5;
            8'hba: y = 4'd12;
            8'hbd: y = 4'd11;
            8'hc0: y = 4'd15;
            8'hc3: y = 4'd0;
            8'hc6: y = 4'd15;
            8'hc9: y = 4'd1;
            8'hcc: y = 4'd9;
            8'hcf: y = 4'd12;
            8'hd2: y = 4'd5;
            8'hd5: y = 4'd5;
            8'hd8: y = 4'd7;
            8'hdb: y = 4'd0;
            8'hde: y = 4'd6;
            8'he1: y = 4'd7;
            8'he4: y = 4'd12;
            8'he7: y = 4'd11;
            8'hea: y = 4'd11;
            8'hed: y = 4'd14;
            8'hf0: y = 4'd8;
            8'hf3: y = 4'd0;
            8'hf6: y = 4'd12;
            8'hf9: y = 4'd4;
            8'hfc: y = 4'd6;
            8'hff: y = 4'd13;
            default: y = 4'd0;
        endcase
    end
    always_ff @(posedge clk) begin
        a <= a + {7'd0, KEY0_N};
        q <= y;
        LED0 <= ^q;
    end
endmodule
