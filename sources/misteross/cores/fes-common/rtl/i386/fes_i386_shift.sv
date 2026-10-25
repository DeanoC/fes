// SPDX-License-Identifier: MIT
// EX: the 64-bit funnel shifter (docs/i386-cpu-design.md, "Execute") for the
// group-2 shifts and rotates and SHLD/SHRD, with the count masked to five
// bits as on the 386. One right shift of a 64-bit arrangement x by s, five
// mux levels, serves every operation:
//   right (ROR RCR SHR SAR SHRD)  x[n:1] = a, the fill above it, s = count;
//                                 the result is out[n:1] and CF is out[0]
//   left  (ROL RCL SHL SAL SHLD)  x[32+n-1:32] = a, the fill below it,
//                                 s = 31 - count; the result is out[n:1] and
//                                 CF is out[n+1]
// The fill is zeros, the sign, a repeated every n bits (ROL, ROR), a and CF
// repeated every n+1 bits (RCL, RCR, so no count needs reducing modulo 9 or
// 17), or the source (SHLD, SHRD). A 16-bit SHLD or SHRD by more than 16
// shifts dst:src:src or src:src:dst, which is what the 386EX does (Phase 0
// divergence double-shift-wide-count).
//
// The flags follow the 386EX where the manual leaves them undefined (Phase 0
// divergences shift-rotate-of and shift-cf-wide-count): OF is the result's
// top bit XOR CF after a left shift or rotate, and its top two bits XORed
// after a right one, at every count; an 8-bit SHL or SHR by 16 or 24 sets CF
// to the operand's bit 0 or bit 7. AF, SF, ZF and PF are the caller's.
module fes_i386_shift (
    input  logic [3:0]  op,      // fes_i386_pkg::shift_op_e
    input  logic [1:0]  size,    // fes_i386_pkg::size_e
    input  logic [4:0]  count,
    input  logic [31:0] a,       // the destination, zero-extended
    input  logic [31:0] b,       // SHLD and SHRD's source, zero-extended
    input  logic        cf_in,
    output logic [31:0] r,
    output logic        cf,
    output logic        of
);
    localparam logic [3:0] ROL = 4'd0, ROR = 4'd1, RCL = 4'd2, RCR = 4'd3, SHL = 4'd4, SHR = 4'd5,
                           SAL = 4'd6, SAR = 4'd7, SHLD = 4'd8, SHRD = 4'd9;

    logic left;
    assign left = op == ROL || op == RCL || op == SHL || op == SAL || op == SHLD;

    // ---- The arrangement, per size --------------------------------------------
    logic [2:0][63:0] xs;  // 8-, 16- and 32-bit
    logic [2:0]       sign;
    assign sign = {a[31], a[15], a[7]};

    for (genvar z = 0; z < 3; z++) begin : g_size
        localparam int N = 8 << z;
        always_comb begin
            xs[z] = 64'd0;
            if (left) begin
                for (int i = 0; i < N; i++) xs[z][32 + i] = a[i];
                for (int i = 0; i < 32; i++) begin
                    case (op)
                        ROL: xs[z][i] = a[i % N];
                        RCL: xs[z][31 - i] = (i % (N + 1)) == 0 ? cf_in : a[N - (i % (N + 1))];
                        SHLD: xs[z][i] = b[i % 16 + (N == 32 ? i - i % 16 : 0)];
                        default: xs[z][i] = 1'b0;
                    endcase
                end
            end else begin
                for (int i = 0; i < N; i++) xs[z][1 + i] = a[i];
                for (int k = N; k < 63; k++) begin
                    case (op)
                        ROR: xs[z][1 + k] = a[k % N];
                        RCR: xs[z][1 + k] = (k % (N + 1)) == N ? cf_in : a[k % (N + 1)];
                        SAR: xs[z][1 + k] = sign[z];
                        SHRD: xs[z][1 + k] = N == 32 ? b[(k - 32) & 31] : k < 48 ? b[(k + 16) % 16] : 1'b0;
                        default: xs[z][1 + k] = 1'b0;
                    endcase
                end
            end
        end
    end

    // ---- The shift ------------------------------------------------------------
    logic [63:0] x;
    logic [4:0]  s;
    logic [33:0] out;
    assign x = xs[size];
    assign s = left ? ~count : count;
    assign out = 34'(x >> s);

    // ---- Result and flags -----------------------------------------------------
    logic [5:0] n;
    logic       msb, msb2, cf_out, wide8;
    always_comb begin
        case (size)
            2'd0: begin r = {24'b0, out[8:1]}; msb = out[8]; msb2 = out[7]; end
            2'd1: begin r = {16'b0, out[16:1]}; msb = out[16]; msb2 = out[15]; end
            default: begin r = out[32:1]; msb = out[32]; msb2 = out[31]; end
        endcase
    end
    assign n = size == 2'd0 ? 6'd8 : size == 2'd1 ? 6'd16 : 6'd32;
    assign cf_out = left ? out[n + 6'd1] : out[0];
    assign wide8 = size == 2'd0 && (count == 5'd16 || count == 5'd24);
    always_comb begin
        case (op)
            ROL: cf = r[0];
            ROR: cf = msb;
            SHL, SAL: cf = wide8 ? a[0] : cf_out;
            SHR: cf = wide8 ? a[7] : cf_out;
            default: cf = cf_out;
        endcase
    end
    assign of = left ? msb ^ cf : msb ^ msb2;
endmodule
