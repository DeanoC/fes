// Compares architectural fetch and operand events at the user break registers.
module fes_sh7091_ubc (
    input  logic        clk, reset, power_reset,
    input  logic        request,
    input  logic [31:2] address,
    input  logic [3:0]  rmask, wstrb,
    input  logic [31:0] wdata,
    output logic        selected, rvalid,
    output logic [31:0] rdata,
    input  logic        observed, allow_pre, allow_post, operand, written, data_present,
    input  logic [2:0]  operand_size,
    input  logic [7:0]  observed_asid,
    input  logic [31:0] observed_address,
    input  logic [63:0] observed_data,
    input  logic [1:0]  retired_matches,
    output logic [1:0]  post_matches,
    output logic        enabled, pre_break, post_break, debug_vector
);
    localparam logic [3:0] BASRA = 9, BASRB = 10;
    logic [31:0] break_address [0:1];
    logic [7:0] break_asid [0:1];
    logic [3:0] break_mask [0:1];
    logic [6:0] break_bus [0:1];
    logic [31:0] break_data, data_mask;
    logic [1:0] matched;
    logic pc_after_a, pc_after_b, data_enable, sequence_enable, sequence_armed;
    logic [1:0] compare_address, match_channel, before_matches, reported, set_flag;
    logic reg_bank, asid_bank, byte_access, word_access, wide_access;
    logic [3:0] index, lanes;
    logic [31:0] align_bits;
    logic [31:0] data_care_mask, data_low;
    logic compare_data, write_control;

    function automatic logic [31:0] unmasked_address(input logic [2:0] mode);
        case (mode)
            0: return 32'hffff_ffff;
            1: return 32'hffff_fc00;
            2: return 32'hffff_f000;
            3: return 32'h0000_0000;
            4: return 32'hffff_0000;
            5: return 32'hfff0_0000;
            default: return 32'hffff_ffff;
        endcase
    endfunction

    assign index = asid_bank ? {3'b100, address[2]} : address[5:2];
    assign reg_bank = address[31:6] == 26'h3fc8000 && address[5:2] <= 8;
    assign asid_bank = address[31:6] == 26'h3fc0000 && address[5:2] inside {4'd5, 4'd6};
    assign selected = reg_bank || asid_bank;
    assign byte_access = asid_bank || (reg_bank && address[5:2] inside {4'd1, 4'd4});
    assign word_access = reg_bank && address[5:2] inside {4'd2, 4'd5, 4'd8};
    assign wide_access = reg_bank && !byte_access && !word_access;
    assign lanes = { {2{wide_access}}, wide_access || word_access, 1'b1};
    assign write_control = request && reg_bank && address[5:2] == 8 && wstrb == 4'h3;

    always_comb begin
        case (operand_size)
            2: align_bits = 32'hffff_fffe;
            3: align_bits = 32'hffff_fffc;
            4: align_bits = 32'hffff_fff8;
            default: align_bits = 32'hffff_ffff;
        endcase
        case (operand_size)
            1: begin
                data_low = {16'b0, observed_data[7:0], observed_data[7:0]};
                data_care_mask = ~data_mask & 32'h0000_ffff;
            end
            2: begin
                data_low = {16'b0, observed_data[15:0]};
                data_care_mask = ~data_mask & 32'h0000_ffff;
            end
            default: begin
                data_low = observed_data[31:0];
                data_care_mask = ~data_mask;
            end
        endcase
    end
    assign compare_data = data_present &&
                          (((data_low ^ break_data) & data_care_mask) == 0 ||
                           (operand_size == 4 && ((observed_data[63:32] ^ break_data) & data_care_mask) == 0));

    for (genvar channel = 0; channel < 2; channel++) begin : break_channel
        logic [31:0] address_bits;
        logic [2:0] size;
        assign size = {break_bus[channel][6], break_bus[channel][1:0]};
        assign address_bits = unmasked_address({break_mask[channel][3], break_mask[channel][1:0]}) &
                              (operand ? align_bits : 32'hffff_ffff);
        assign compare_address[channel] = ((observed_address ^ break_address[channel]) & address_bits) == 0;
        assign match_channel[channel] = compare_address[channel] &&
              (break_mask[channel][2] || observed_asid == break_asid[channel]) &&
              (operand ? break_bus[channel][5] : break_bus[channel][4] && !break_address[channel][0]) &&
              (written ? break_bus[channel][3] : break_bus[channel][2]) &&
              (size == 0 || size == operand_size) &&
              (channel == 0 || !operand || !data_enable || compare_data);
    end

    assign enabled = ((|break_bus[0][5:4]) && (|break_bus[0][3:2])) ||
                     ((|break_bus[1][5:4]) && (|break_bus[1][3:2]));
    // A post-execution match is a candidate until the CPU commits the
    // instruction. An exception or abandoned operation can discard it.
    assign before_matches = observed && allow_pre && !operand ? match_channel & ~{pc_after_b, pc_after_a} : 2'b00;
    assign post_matches = observed && allow_post ? match_channel & {operand || pc_after_b, operand || pc_after_a} : 2'b00;
    assign reported = before_matches | retired_matches;
    assign set_flag = {reported[1] && (!sequence_enable || sequence_armed),
                       reported[0] && !sequence_enable};
    assign pre_break = (before_matches[0] && !sequence_enable) ||
                       (before_matches[1] && (!sequence_enable || sequence_armed));
    assign post_break = (retired_matches[0] && !sequence_enable) ||
                        (retired_matches[1] && (!sequence_enable || sequence_armed));

    always_ff @(posedge clk) begin
        if (reset) begin
            rvalid <= 0;
            pc_after_a <= 0;
            pc_after_b <= 0;
            data_enable <= 0;
            sequence_enable <= 0;
            sequence_armed <= 0;
            if (power_reset) begin
                break_bus[0] <= 0;
                break_bus[1] <= 0;
                matched <= 0;
                debug_vector <= 0;
            end
        end else begin
            rvalid <= request && selected && wstrb == 0;
            if (request && selected && wstrb == 0) begin
                rdata <= 0;
                if (rmask == lanes) case (index)
                    0: rdata <= break_address[0];
                    1: rdata[3:0] <= break_mask[0];
                    2: rdata[6:0] <= break_bus[0];
                    3: rdata <= break_address[1];
                    4: rdata[3:0] <= break_mask[1];
                    5: rdata[6:0] <= break_bus[1];
                    6: rdata <= break_data;
                    7: rdata <= data_mask;
                    8: rdata[15:0] <= {matched[0], matched[1], 3'b0, pc_after_a, 2'b0, data_enable,
                                        pc_after_b, 2'b0, sequence_enable, 2'b0, debug_vector};
                    BASRA: rdata[7:0] <= break_asid[0];
                    BASRB: rdata[7:0] <= break_asid[1];
                    default: ;
                endcase
            end

            if (request && selected && wstrb == lanes) case (index)
                0: break_address[0] <= wdata;
                1: break_mask[0] <= wdata[3:0];
                2: break_bus[0] <= wdata[6:0];
                3: break_address[1] <= wdata;
                4: break_mask[1] <= wdata[3:0];
                5: break_bus[1] <= wdata[6:0];
                6: break_data <= wdata;
                7: data_mask <= wdata;
                BASRA: break_asid[0] <= wdata[7:0];
                BASRB: break_asid[1] <= wdata[7:0];
                default: ;
            endcase

            if (write_control) begin
                matched <= {wdata[14], wdata[15]};
                pc_after_a <= wdata[10];
                pc_after_b <= wdata[6];
                data_enable <= wdata[7];
                sequence_enable <= wdata[3];
                debug_vector <= wdata[0];
            end
            if (|reported)
                matched <= (write_control ? {wdata[14], wdata[15]} : matched) | set_flag;
            if (!sequence_enable || (write_control && !wdata[3]))
                sequence_armed <= 0;
            else if (|reported) begin
                if (set_flag[1])
                    sequence_armed <= 0;
                else if (reported[0])
                    sequence_armed <= 1;
            end
        end
    end
endmodule
