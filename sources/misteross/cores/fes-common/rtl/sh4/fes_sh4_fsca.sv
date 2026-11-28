// Quarter-wave sine with linear interpolation between phase samples.
// An angle of 0x10000 phase units is one revolution.
module fes_sh4_fsca (
    input  logic        clk,
    input  logic        reset,
    input  logic        start,
    input  logic        toward_zero,
    input  logic [15:0] angle,
    output logic        done,
    output logic [63:0] value
);
    typedef enum logic [3:0] {
        IDLE, FOLD, LOOKUP, PRODUCT_INPUT, PRODUCT, INTERPOLATE, NORMALIZE, PACK, DONE
    } state_t;
    state_t state;
    (* ramstyle = "M20K" *) logic [43:0] coefficients [0:1023];
    logic [43:0] coefficient;
    logic [15:0] phase;
    logic [14:0] pos;
    logic [16:0] slope;
    (* multstyle = "logic" *) logic [20:0] interpolation;
    logic [16:0] rounded_interpolation;
    logic [26:0] significand;
    logic [7:0] exponent;
    logic [24:0] rounded;
    logic [31:0] packed_value;
    logic negative, cosine;

    initial $readmemh("fes_sh4_fsca.hex", coefficients);

    assign rounded_interpolation = 17'((interpolation + 21'd8) >> 4);
    assign rounded = {1'b0, significand[26:3]} +
                     {24'b0, !toward_zero && significand[2] &&
                      ((|significand[1:0]) || significand[3])};
    assign packed_value = significand == 0 ? {negative, 31'b0} :
                          {negative, exponent + {7'b0, rounded[24]},
                           rounded[24] ? rounded[23:1] : rounded[22:0]};
    assign done = state == DONE;

    always_ff @(posedge clk) begin
        if (state == LOOKUP)
            coefficient <= coefficients[pos[13:4]];
        if (reset)
            state <= IDLE;
        else case (state)
            IDLE: if (start) begin
                cosine <= 0;
                phase <= angle;
                state <= FOLD;
            end
            FOLD: begin
                negative <= phase[15];
                pos <= phase[14] ? 15'd16384 - {1'b0, phase[13:0]} : {1'b0, phase[13:0]};
                state <= LOOKUP;
            end
            LOOKUP: state <= PRODUCT_INPUT;
            PRODUCT_INPUT: begin
                slope <= coefficient[16:0];
                state <= PRODUCT;
            end
            PRODUCT: begin
                interpolation <= slope * pos[3:0];
                state <= INTERPOLATE;
            end
            INTERPOLATE: begin
                significand <= pos[14] ? 27'h400_0000 :
                               coefficient[43:17] + {10'b0, rounded_interpolation};
                exponent <= 127;
                state <= NORMALIZE;
            end
            NORMALIZE: begin
                if (significand[26] || significand == 0)
                    state <= PACK;
                else if (significand[25:23] != 0) begin
                    significand <= significand << 1;
                    exponent <= exponent - 8'd1;
                end else begin
                    significand <= significand << 4;
                    exponent <= exponent - 8'd4;
                end
            end
            PACK: begin
                if (cosine) begin
                    value[31:0] <= packed_value;
                    state <= DONE;
                end else begin
                    value[63:32] <= packed_value;
                    cosine <= 1;
                    phase <= phase + 16'h4000;
                    state <= FOLD;
                end
            end
            DONE: state <= IDLE;
            default: state <= IDLE;
        endcase
    end
endmodule
