//
// KF8253_Counter
// Counter Logic
//
// Written by Kitune-san
//

`include "KF8253_Definitions.svh"

module KF8253_Counter #(
    parameter READ_BACK = 0
) (
    // Bus
    input   logic           clock,
    input   logic           reset,

    input   logic   [7:0]   internal_data_bus,
    input   logic           write_control,
    input   logic           write_counter,
    input   logic           read_counter,
    input   logic           read_back_count,
    input   logic           read_back_status,

    output  logic   [7:0]   read_counter_data,

    // I/O
    input   logic           counter_clock,
    input   logic           counter_gate,
    output  logic           counter_out
);

    //
    // Internal Signals
    //
    logic   [1:0]   select_read_write;
    logic           update_counter_config;
    logic           update_select_read_write;
    logic           count_latched_flag;
    logic   [2:0]   select_mode;
    logic           select_bcd;

    logic           write_count_step;
    logic   [7:0]   count_preset_lsb;
    logic   [15:0]  count_preset;
    logic   [16:0]  count_preset_load;

    logic           read_negedge;
    logic           read_count_step;
    logic           prev_read_counter;

    logic           prev_counter_clock;
    logic           count_edge;

    logic           prev_counter_gate;
    logic           gate_edge;

    logic           load_edge;

    logic           start_counting;

    logic           first_count_edge;

    logic   [16:0]  count_next;
    logic           count_period;
    logic           prev_count_period;

    logic   [16:0]  count;
    logic   [16:0]  count_latched;

    logic           count_read_negedge;
    logic           status_latched_flag;
    logic   [7:0]   status_latched;
    logic           null_count;
    logic           preset_loading;


    //
    // Mode control word
    //
    // READ/LOAD
    always_ff @(posedge clock, posedge reset) begin
        if (reset) begin
            select_read_write  <= `RL_SELECT_MSB;
        end
        else if (write_control) begin
            case (internal_data_bus[5:4])
                `RL_SELECT_MSB     : begin
                    select_read_write  <= internal_data_bus[5:4];
                end
                `RL_SELECT_LSB     : begin
                    select_read_write  <= internal_data_bus[5:4];
                end
                `RL_SELECT_LSB_MSB : begin
                    select_read_write  <= internal_data_bus[5:4];
                end
                default         : begin
                    select_read_write  <= select_read_write;
                end
            endcase
        end
        else begin
            select_read_write  <= select_read_write;
        end
    end

    assign update_counter_config = (internal_data_bus[5:4] != `RL_COUNTER_LATCH) & write_control;
    assign update_select_read_write = (select_read_write != internal_data_bus[5:4]) & update_counter_config;

    // Read latch
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            count_latched_flag <= 1'b0;
        else if (((write_control) && (internal_data_bus[5:4] == `RL_COUNTER_LATCH)) || ((READ_BACK != 0) && read_back_count))
            count_latched_flag <= 1'b1;
        else if ((count_latched_flag == 1'b1) && (count_read_negedge)) begin
            if (select_read_write != `RL_SELECT_LSB_MSB)
                count_latched_flag <= 1'b0;
            else
                count_latched_flag <= (read_count_step == 1'b0) ? 1'b0 : 1'b1;
        end
        else
            count_latched_flag <= count_latched_flag;
    end

    // MODE
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            select_mode <= `KF8253_CONTROL_MODE_0;
        else if (update_counter_config)
            select_mode <= internal_data_bus[3:1];
        else
            select_mode <= select_mode;
    end

    // BCD
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            select_bcd <= 1'b0;
        else if (update_counter_config)
            select_bcd <= internal_data_bus[0];
        else
            select_bcd <= select_bcd;
    end


    //
    // Write count value (preset)
    //
    // With LSB/MSB access the LSB waits in count_preset_lsb and the preset
    // only changes when the MSB completes the count, as on the 8253: a
    // counter that reloads between the two writes (modes 2 and 3) must reload
    // the old count, not a mix of the new LSB and the old MSB.
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            count_preset_lsb <= 8'h00;
        else if ((write_counter) && (write_count_step == 1'b1))
            count_preset_lsb <= internal_data_bus;
        else
            count_preset_lsb <= count_preset_lsb;
    end

    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            count_preset[15:0] <= 16'h0000;
        else if (write_counter)
            if (write_count_step == 1'b0)
                count_preset[15:0] <= {internal_data_bus,
                                       (select_read_write == `RL_SELECT_LSB_MSB) ? count_preset_lsb : count_preset[7:0]};
            else if (select_read_write != `RL_SELECT_LSB_MSB)
                count_preset[15:0] <= {count_preset[15:8], internal_data_bus};
            else
                count_preset[15:0] <= count_preset[15:0];
        else
            count_preset[15:0] <= count_preset[15:0];
    end

    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            write_count_step <= 1'b0;
        else if (update_select_read_write)
            case (internal_data_bus[5:4])
                `RL_SELECT_MSB      : write_count_step <= 1'b0;
                `RL_SELECT_LSB      : write_count_step <= 1'b1;
                `RL_SELECT_LSB_MSB  : write_count_step <= 1'b1;
                default             : write_count_step <= write_count_step;
            endcase
        else if ((write_counter) && (select_read_write == `RL_SELECT_LSB_MSB))
            write_count_step <= (write_count_step) ? 1'b0 : 1'b1;
        else
            write_count_step <= write_count_step;
    end

    always_comb begin
        count_preset_load[15:0] = (select_read_write == `RL_SELECT_MSB) ? {count_preset[15:8], 8'h00} :
                                  (select_read_write == `RL_SELECT_LSB) ? {8'h00, count_preset[ 7:0]} :
                                                                          count_preset[15:0];
        count_preset_load[16]   = (count_preset_load[15:0] == 16'h0000) ? 1'b1 : 1'b0;
    end


    //
    // Read count value (latched)
    //
    always_comb begin
        if (status_latched_flag)
            read_counter_data <= status_latched;
        else if (read_count_step == 1'b0)
            read_counter_data <= count_latched[15:8];
        else
            read_counter_data <= count_latched[7:0];
    end

    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            prev_read_counter <= 1'b0;
        else
            prev_read_counter <= read_counter;
    end

    assign read_negedge = ((prev_read_counter != read_counter) & read_counter == 1'b0);
    // A latched status byte is read first and does not advance the count read.
    assign count_read_negedge = read_negedge & ~status_latched_flag;

    // 8254 read-back status: OUT, null count, then the control word's RW, mode
    // and BCD fields. A second status latch before the read is ignored. The
    // 8253 (READ_BACK = 0) has none of this state.
    generate
        if (READ_BACK != 0) begin : g_read_back_status
            always_ff @(posedge clock, posedge reset) begin
                if (reset) begin
                    status_latched_flag <= 1'b0;
                    status_latched      <= 8'h00;
                end
                else if (read_back_status && !status_latched_flag) begin
                    status_latched_flag <= 1'b1;
                    status_latched      <= {counter_out, null_count, select_read_write, select_mode, select_bcd};
                end
                else if (status_latched_flag && read_negedge)
                    status_latched_flag <= 1'b0;
                else
                    status_latched_flag <= status_latched_flag;
            end
        end
        else begin : g_no_read_back_status
            assign status_latched_flag = 1'b0;
            assign status_latched      = 8'h00;
        end
    endgenerate

    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            read_count_step <= 1'b0;
        else if (update_select_read_write)
            case (internal_data_bus[5:4])
                `RL_SELECT_MSB      : read_count_step <= 1'b0;
                `RL_SELECT_LSB      : read_count_step <= 1'b1;
                `RL_SELECT_LSB_MSB  : read_count_step <= 1'b1;
                default             : read_count_step <= read_count_step;
            endcase
        else if ((count_read_negedge) && (select_read_write == `RL_SELECT_LSB_MSB))
            read_count_step <= (read_count_step) ? 1'b0 : 1'b1;
        else
            read_count_step <= read_count_step;
    end


    //
    // Count Counter
    //
    // Count trigger
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            prev_counter_clock <= 1'b0;
        else
            prev_counter_clock <= counter_clock;
    end

    assign count_edge = (prev_counter_clock != counter_clock) & (counter_clock == 1'b0);

    // Gate trigger
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            prev_counter_gate <= 1'b0;
        else if (count_edge)
            prev_counter_gate <= counter_gate;
        else
            if (prev_counter_gate != 1'b0)
                prev_counter_gate <= counter_gate;
            else
                prev_counter_gate <= prev_counter_gate;
    end

    assign gate_edge = (prev_counter_gate != counter_gate) & (counter_gate == 1'b1);

    // Load trigger
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            load_edge <= 1'b0;
        else if (write_counter)
            load_edge <= 1'b1;
        else if (count_edge)
            load_edge <= 1'b0;
        else
            load_edge <= load_edge;
    end

    // Count start/end
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            start_counting <= 1'b0;
        else if (update_counter_config)
            start_counting <= 1'b0;
        else if (write_counter) begin
            if (select_read_write != `RL_SELECT_LSB_MSB)
                start_counting <= 1'b1;
            else begin
                casez (select_mode)
                    `KF8253_CONTROL_MODE_0: begin
                        if (write_count_step == 1'b0)
                            start_counting <= 1'b1;
                        else
                            start_counting <= 1'b0;
                    end
                    `KF8253_CONTROL_MODE_4: begin
                        if (write_count_step == 1'b0)
                            start_counting <= 1'b1;
                        else
                            start_counting <= 1'b0;
                    end
                    default        : begin
                        if (start_counting == 1'b1)
                            start_counting <= 1'b1;
                        else begin
                            if (write_count_step == 1'b0)
                                start_counting <= 1'b1;
                            else
                                start_counting <= 1'b0;
                        end
                    end
                endcase
            end
        end
        else
            start_counting <= start_counting;
    end

    // First counte edge signal
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            first_count_edge    <= 1'b0;
        else if (start_counting==1'b0)
            first_count_edge    <= 1'b1;
        else if (count_edge == 1'b1)
            first_count_edge    <= 1'b0;
        else
            first_count_edge    <= first_count_edge;
    end

    // Saturating decrement by one or two, with independent BCD borrow
    // prefixes. Mode 3 must not place two complete decrementers in series.
    // The extra high bit represents 65536 (binary) or 10000 (BCD).
    function automatic logic [16:0] decrement(input [16:0] count, input is_bcd, input twice);
        reg [16:0] result;
        integer step, i;
        begin
            step = twice ? 2 : 1;
            if (!is_bcd)
                result = count - 17'(step);
            else begin
                result[3:0] = count[3:0] < 4'(step) ?
                    count[3:0] + 4'(10-step) : count[3:0] - 4'(step);
                for (i = 1; i < 4; i = i + 1) begin
                    if ((count & 17'((1 << (4*i))-1)) < 17'(step))
                        result[4*i +: 4] = count[4*i +: 4] == 0 ? 4'd9 : count[4*i +: 4] - 4'd1;
                    else result[4*i +: 4] = count[4*i +: 4];
                end
                result[16] = count[16] && count[15:0] >= 16'(step);
            end
            decrement = count < 17'(step) ? 17'b0 : result;
        end
    endfunction

    // Precompute both encodings between count edges. Falling input-clock
    // edges cannot occur on consecutive fabric clocks. Count can therefore
    // change at an edge, refresh these candidates on the following clock,
    // and consume them at the next edge with the original visible timing.
    // Keep binary and BCD candidates separate so a control write can select
    // either encoding immediately. Clearing an inactive counter also clears
    // the candidates, covering restart on the very next fabric clock.
    logic [16:0] dec_binary_q, dec2_binary_q, dec_bcd_q, dec2_bcd_q;
    logic count_zero_q, count_le_one_q, count_le_two_q, count_le_three_q, count_two_q;
    always_ff @(posedge clock, posedge reset) begin
        if (reset) begin
            dec_binary_q <= 0; dec2_binary_q <= 0;
            dec_bcd_q <= 0; dec2_bcd_q <= 0;
            count_zero_q <= 1; count_le_one_q <= 1; count_le_two_q <= 1;
            count_le_three_q <= 1; count_two_q <= 0;
        end else if (!start_counting) begin
            dec_binary_q <= 0; dec2_binary_q <= 0;
            dec_bcd_q <= 0; dec2_bcd_q <= 0;
            count_zero_q <= 1; count_le_one_q <= 1; count_le_two_q <= 1;
            count_le_three_q <= 1; count_two_q <= 0;
        end else begin
            dec_binary_q <= decrement(count, 1'b0, 1'b0);
            dec2_binary_q <= decrement(count, 1'b0, 1'b1);
            dec_bcd_q <= decrement(count, 1'b1, 1'b0);
            dec2_bcd_q <= decrement(count, 1'b1, 1'b1);
            count_zero_q <= count == 0; count_le_one_q <= count <= 1;
            count_le_two_q <= count <= 2; count_le_three_q <= count <= 3;
            count_two_q <= count == 2;
        end
    end
    logic [16:0] dec_count, dec2_count;
    assign dec_count = select_bcd ? dec_bcd_q : dec_binary_q;
    assign dec2_count = select_bcd ? dec2_bcd_q : dec2_binary_q;

    // These scalar flags are equivalent to comparing the selected 17-bit
    // next count, but avoid putting that wide data mux before output control.
    wire preset_zero = count_preset_load == 0;
    wire preset_one = count_preset_load == 1;
    wire mode2_zero = counter_gate ? count_le_one_q : count_zero_q;
    wire mode3_zero = counter_gate ? (count[0] ?
        (counter_out ? count_le_one_q : count_le_three_q) : count_le_two_q) : count_zero_q;
    wire mode2_next_one = (gate_edge || count_le_one_q) ? preset_one : count_two_q;
    always_comb begin
        count_next = dec_count;
        count_period = count_le_one_q;

        casez (select_mode)
            `KF8253_CONTROL_MODE_0: begin
                count_period = load_edge ? preset_zero : (counter_gate ? count_le_one_q : count_zero_q);
                if (counter_gate == 1'b0)
                    count_next = count;

                if (load_edge)
                    count_next = count_preset_load;
            end

            `KF8253_CONTROL_MODE_1: begin
                count_period = gate_edge ? preset_zero : count_le_one_q;
                if (gate_edge)
                    count_next = count_preset_load;
            end

            `KF8253_CONTROL_MODE_2: begin
                count_period = (gate_edge || mode2_zero) && preset_zero;
                if (counter_gate == 1'b0)
                    count_next = count;

                if (mode2_zero)
                    count_next = count_preset_load;

                if (gate_edge)
                    count_next = count_preset_load;
            end

            `KF8253_CONTROL_MODE_3: begin
                count_period = (mode3_zero && !first_count_edge) ||
                               ((gate_edge || mode3_zero) && preset_zero);
                if (count[0] == 1'b1) begin
                    if (counter_out == 1'b0)
                        count_next = {dec2_count[16:1], 1'b0};
                end
                else
                    count_next = dec2_count;

                if (counter_gate == 1'b0)
                    count_next = count;

                if (mode3_zero) begin
                    count_next = count_preset_load;
                end

                if (gate_edge)
                    count_next = count_preset_load;
            end

            `KF8253_CONTROL_MODE_4: begin
                count_period = load_edge ? preset_zero : (counter_gate ? count_le_one_q : count_zero_q);
                if (counter_gate == 1'b0)
                    count_next = count;

                if (load_edge)
                    count_next = count_preset_load;
            end

            `KF8253_CONTROL_MODE_5: begin
                count_period = gate_edge ? preset_zero : count_le_one_q;
                if (gate_edge)
                    count_next = count_preset_load;
            end
            default: begin
            end
        endcase

    end

    // 8254 null count: set by a control word or count write, cleared when the
    // counting element takes the preset at a count edge.
    always_comb begin
        casez (select_mode)
            `KF8253_CONTROL_MODE_0: preset_loading = load_edge;
            `KF8253_CONTROL_MODE_1: preset_loading = gate_edge;
            `KF8253_CONTROL_MODE_2: preset_loading = gate_edge || mode2_zero;
            `KF8253_CONTROL_MODE_3: preset_loading = gate_edge || mode3_zero;
            `KF8253_CONTROL_MODE_4: preset_loading = load_edge;
            `KF8253_CONTROL_MODE_5: preset_loading = gate_edge;
            default:                preset_loading = 1'b0;
        endcase
    end

    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            null_count <= 1'b0;
        else if (update_counter_config || write_counter)
            null_count <= 1'b1;
        else if (start_counting && count_edge && preset_loading)
            null_count <= 1'b0;
        else
            null_count <= null_count;
    end

    // Update Count
    always_ff @(posedge clock, posedge reset) begin
        if (reset) begin
            count <= 17'b0_0000_0000_0000_0000;
            count_latched <= 17'b0_0000_0000_0000_0000;
        end
        else if (start_counting == 1'b0) begin
            count <= 17'b0_0000_0000_0000_0000;
            if (count_latched_flag == 1'b0)
                count_latched <= 17'b0_0000_0000_0000_0000;
            else
                count_latched <= count_latched;
        end
        else if (start_counting & count_edge) begin
            count <= count_next;
            if (count_latched_flag == 1'b0)
                count_latched <= count_next;
            else
                count_latched <= count_latched;
        end
        else begin
            count <= count;
            if (count_latched_flag == 1'b0)
                count_latched <= count;
            else
                count_latched <= count_latched;
        end
    end

    // Period
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            prev_count_period <= 1'b1;
        else if (start_counting == 1'b0)
            prev_count_period <= 1'b1;
        else if (count_edge)
            prev_count_period <= count_period;
        else
            prev_count_period <= prev_count_period;
    end

    // Output
    always_ff @(posedge clock, posedge reset) begin
        if (reset)
            counter_out <= 1'b0;
        else if (start_counting == 1'b0) begin
            casez (select_mode)
                `KF8253_CONTROL_MODE_0: counter_out <= 1'b0;
                `KF8253_CONTROL_MODE_1: counter_out <= 1'b1;
                `KF8253_CONTROL_MODE_2: counter_out <= 1'b1;
                `KF8253_CONTROL_MODE_3: counter_out <= 1'b1;
                `KF8253_CONTROL_MODE_4: counter_out <= 1'b1;
                `KF8253_CONTROL_MODE_5: counter_out <= 1'b1;
                default        : counter_out <= 1'b0;
            endcase
        end
        else if (count_edge) begin
            casez (select_mode)
                `KF8253_CONTROL_MODE_0: begin
                    if (count_period)
                        counter_out <= 1'b1;
                    else
                        counter_out <= 1'b0;
                end
                `KF8253_CONTROL_MODE_1: begin
                    if (count_period)
                        counter_out <= 1'b1;
                    else
                        counter_out <= 1'b0;
                end
                `KF8253_CONTROL_MODE_2: begin
                    if (counter_gate == 1'b0)
                        counter_out <= 1'b1;
                    else if (mode2_next_one)
                        counter_out <= 1'b0;
                    else
                        counter_out <= 1'b1;
                end
                `KF8253_CONTROL_MODE_3: begin
                    if (counter_gate == 1'b0)
                        counter_out <= 1'b1;
                    else if (count_period)
                        counter_out <= ~counter_out;
                    else
                        counter_out <= counter_out;
                end
                `KF8253_CONTROL_MODE_4: begin
                    if ((count_period) && (prev_count_period == 1'b0))
                        counter_out <= 1'b0;
                    else
                        counter_out <= 1'b1;
                end
                `KF8253_CONTROL_MODE_5: begin
                    if ((count_period) && (prev_count_period == 1'b0))
                        counter_out <= 1'b0;
                    else
                        counter_out <= 1'b1;
                end
                default        : begin
                    counter_out <= counter_out;
                end
            endcase
        end
        else
            casez (select_mode)
                `KF8253_CONTROL_MODE_2: begin
                    if ((counter_gate == 1'b0) || (prev_counter_gate == 1'b0))
                        counter_out <= 1'b1;
                    else
                        counter_out <= counter_out;
                end
                `KF8253_CONTROL_MODE_3: begin
                    if ((counter_gate == 1'b0) || (prev_counter_gate == 1'b0))
                        counter_out <= 1'b1;
                    else
                        counter_out <= counter_out;
                end
                default:
                    counter_out <= counter_out;
            endcase
    end
endmodule

