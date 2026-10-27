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
// M2 decodes the operation into c (fes_i386_exctl): each bit of x is an OR
// of data bits, each gated by one registered select for its direction, size
// and fill, so no opcode compare sits between the operands and the shift. At
// a count of zero the shift returns a unchanged.
//
// The shifter hands out its whole output; fes_i386_shflags, at WB, takes
// the flags from it.
module fes_i386_shift (
    input  fes_i386_pkg::shctl_t c,
    input  logic [4:0]  s,       // count, or 31 - count for a left shift
    input  logic [31:0] a,       // the destination, zero-extended
    input  logic [31:0] b,       // SHLD and SHRD's source, zero-extended
    input  logic        cf_in,
    output logic [33:0] out      // the result is out[n:1]
);
    localparam int DATA = 0, ROT = 1, RC = 2, SAR = 3, SHD = 4;

    // ---- The arrangement ---------------------------------------------------------
    // One 64-bit candidate per direction (0 right, 1 left), size and fill; x
    // ORs them, each gated by its select.
    logic [1:0][2:0][4:0][63:0] cand;
    logic [2:0] sign;
    assign sign = {a[31], a[15], a[7]};

    for (genvar z = 0; z < 3; z++) begin : g_size
        localparam int N = 8 << z;
        always_comb begin
            cand[1][z] = '0;
            cand[0][z] = '0;
            for (int i = 0; i < N; i++) cand[1][z][DATA][32 + i] = a[i];
            for (int i = 0; i < 32; i++) begin
                cand[1][z][ROT][i] = a[i % N];
                cand[1][z][RC][31 - i] = (i % (N + 1)) == 0 ? cf_in : a[N - (i % (N + 1))];
                cand[1][z][SHD][i] = b[i % 16 + (N == 32 ? i - i % 16 : 0)];
            end
            for (int i = 0; i < N; i++) cand[0][z][DATA][1 + i] = a[i];
            for (int k = N; k < 63; k++) begin
                cand[0][z][ROT][1 + k] = a[k % N];
                cand[0][z][RC][1 + k] = (k % (N + 1)) == N ? cf_in : a[k % (N + 1)];
                cand[0][z][SAR][1 + k] = sign[z];
                cand[0][z][SHD][1 + k] = N == 32 ? b[(k - 32) & 31] : k < 48 ? b[(k + 16) % 16] : 1'b0;
            end
        end
    end

    logic [63:0] x;
    always_comb begin
        x = '0;
        for (int d = 0; d < 2; d++)
            for (int z = 0; z < 3; z++)
                for (int k = 0; k < 5; k++)
                    x = x | ({64{c.sel[d * 15 + z * 5 + k]}} & cand[d][z][k]);
    end

    // ---- The shift ----------------------------------------------------------------
    assign out = 34'(x >> s);
endmodule
