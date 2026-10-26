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

// WB: a shift's CF and OF, from the merged result in the WB register and
// the bits EX kept beside it (fes_i386_ex): the bit out at the right
// (out[0]), the one out at the left by size (out[n+1]), and an 8-bit SHL or
// SHR's CF at a count of 16 or 24 (wide).
//
// The flags follow the 386EX where the manual leaves them undefined (Phase 0
// divergences shift-rotate-of and shift-cf-wide-count): OF is the result's
// top bit XOR CF after a left shift or rotate, and its top two bits XORed
// after a right one, at every count; an 8-bit SHL or SHR by 16 or 24 sets CF
// to the operand's bit 0 or bit 7. AF, SF, ZF and PF are the caller's.
module fes_i386_shflags (
    input  fes_i386_pkg::shctl_t c,
    input  logic        hi8,     // the destination is a high byte: the result is in bits 15:8
    input  logic [31:0] result,
    input  logic        out0,
    input  logic        outn,
    input  logic        cfw,     // the wide count's CF ...
    input  logic        wide,    // ... and whether it applies
    output logic        cf,
    output logic        of
);
    logic h, lsb, msb, msb2;
    assign h = c.z8 && hi8;
    assign lsb = h ? result[8] : result[0];
    assign msb = c.z8 ? (h ? result[15] : result[7]) : c.z16 ? result[15] : result[31];
    assign msb2 = c.z8 ? (h ? result[14] : result[6]) : c.z16 ? result[14] : result[30];
    assign cf = c.cf_rol ? lsb : c.cf_ror ? msb : wide ? cfw : c.left ? outn : out0;
    assign of = c.left ? msb ^ cf : msb ^ msb2;
endmodule
