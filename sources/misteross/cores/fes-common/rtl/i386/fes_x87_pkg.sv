// SPDX-License-Identifier: MIT
package fes_x87_pkg;
    // Decoded arithmetic interface, not ESC opcodes. Architectural state,
    // loads/stores, exception masks and retirement belong to the CPU adapter.
    typedef enum logic [2:0] {
        OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_SQRT, OP_CMP, OP_UCMP
    } op_t;
    typedef struct packed {
        logic sign;
        logic signed [17:0] exponent; // biased; normal minimum is 1
        logic [63:0] significand;     // normalized except for zero
    } unpack_t;
    typedef struct packed {
        op_t op;
        logic [79:0] a, b;
        logic [1:0] pc, rc; // x87 CW bits 9:8 and 11:10
        logic [3:0] slot;
        unpack_t ua, ub; // shared registered front-end normalization
    } request_t;
    typedef struct packed {
        logic [3:0] slot;
        logic [1:0] pc, rc;
        logic sign;
        logic signed [17:0] exponent; // biased; bit 66 is the integer bit
        logic [66:0] significand;     // 64 retained bits, guard/round/sticky
        logic special;
        logic [79:0] value;
        logic [5:0] flags;            // PE UE OE ZE DE IE
        logic [2:0] cc;               // C3 C2 C0; meaningful for compare only
    } raw_t;
    typedef struct packed {
        logic [3:0] slot;
        logic [79:0] value;
        logic [5:0] flags;
        logic [2:0] cc;
        logic incremented;            // C1: magnitude rounded upward
    } result_t;

    function automatic logic is_zero(input logic [79:0] x);
        return x[78:64] == 0 && x[63:0] == 0;
    endfunction
    function automatic logic is_denormal(input logic [79:0] x);
        return x[78:64] == 0 && x[63:0] != 0;
    endfunction
    function automatic logic is_inf(input logic [79:0] x);
        return x[78:64] == 15'h7fff && x[63:0] == 64'h8000000000000000;
    endfunction
    function automatic logic is_nan(input logic [79:0] x);
        return x[78:64] == 15'h7fff && x[63] && x[62:0] != 0;
    endfunction
    function automatic logic is_snan(input logic [79:0] x);
        return is_nan(x) && !x[62];
    endfunction
    function automatic logic is_unsupported(input logic [79:0] x);
        return x[78:64] != 0 && !x[63];
    endfunction
    // Balanced leading-zero tree; barrel shifting is a separate register
    // stage in the front end. Zero needs no shift.
    function automatic logic [5:0] normalization_shift(input logic [63:0] x);
        logic [31:0] v32;
        logic [15:0] v16;
        logic [7:0] v8;
        logic [3:0] v4;
        logic [1:0] v2;
        logic [5:0] n;
        n[5] = ~|x[63:32];
        v32 = n[5] ? x[31:0] : x[63:32];
        n[4] = ~|v32[31:16];
        v16 = n[4] ? v32[15:0] : v32[31:16];
        n[3] = ~|v16[15:8];
        v8 = n[3] ? v16[7:0] : v16[15:8];
        n[2] = ~|v8[7:4];
        v4 = n[2] ? v8[3:0] : v8[7:4];
        n[1] = ~|v4[3:2];
        v2 = n[1] ? v4[1:0] : v4[3:2];
        n[0] = ~v2[1];
        return x == 0 ? 6'd0 : n;
    endfunction
    function automatic logic [66:0] shr_sticky(input logic [66:0] x, input logic [17:0] n);
        logic [66:0] y;
        if (n >= 67) return {66'd0,|x};
        y = x >> n;
        for (int i = 0; i < 67; i++)
            if (18'(i) < n) y[0] = y[0] | x[i];
        return y;
    endfunction
    // x87 NaN propagation: quiet before signalling, then greatest payload;
    // equal payloads prefer the positive sign. Unsupported encodings take
    // precedence and produce the architectural indefinite.
    function automatic logic [79:0] nan_value(input logic [79:0] a, b);
        logic [79:0] n;
        if (is_unsupported(a) || is_unsupported(b)) return 80'hffffc000000000000000;
        if (!is_nan(a)) n = b;
        else if (!is_nan(b)) n = a;
        else if (is_snan(a) != is_snan(b)) n = is_snan(a) ? b : a;
        else if (a[62:0] > b[62:0]) n = a;
        else if (a[62:0] < b[62:0]) n = b;
        else n = a[79] ? b : a;
        n[62] = 1;
        return n;
    endfunction
endpackage
