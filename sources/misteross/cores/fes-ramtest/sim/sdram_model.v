// SPDX-License-Identifier: GPL-2.0-or-later
// Behavioral 16-bit SDRAM. Mode-register CAS latency, burst length 1.
module sdram_model (
    input wire [2:0] mask_fault,
    input wire clk,
    input wire cke,
    input wire ncs,
    input wire nras,
    input wire ncas,
    input wire nwe,
    input wire [1:0] ba,
    input wire [12:0] a,
    input wire dqml,
    input wire dqmh,
    inout wire [15:0] dq
);
    // Cover the full scan simulation and both selected high columns without aliasing.
    reg [15:0] mem [0:262143];
    reg [1:0] open_ba = 2'd0;
    reg [12:0] open_row = 13'd0;
    reg [15:0] beat = 16'h0000;
    reg [2:0] read_delay = 3'd0;
    reg reading = 1'b0;
    reg [15:0] read_data = 16'h0000;
    reg [1:0] cycles_after_activate = 2'd3;
    integer index;
    reg [2:0] cas_latency = 3'd2;
    reg [1:0] lower_history = 2'b00, upper_history = 2'b00;
    reg hold_write_mask = 1'b0;
    reg last_lower = 1'b0, last_upper = 1'b0;
    reg [3:0] banks_seen = 4'd0;
    reg [3:0] low_columns_seen = 4'd0;
    reg [1:0] rows_seen = 2'd0, high_columns_seen = 2'd0;
    reg [31:0] refreshes = 32'd0, masked_writes = 32'd0, no_writes = 32'd0;
    reg [31:0] refresh_masked_writes = 32'd0;
    reg refreshed = 1'b0;
    reg effective_lower, effective_upper;
    always @* begin
        effective_lower = dqml;
        effective_upper = dqmh;
        case (mask_fault)
            3'd1: begin effective_lower = 1'b0; effective_upper = 1'b0; end
            3'd2: begin effective_lower = dqmh; effective_upper = dqml; end
            3'd3: effective_lower = 1'b1;
            3'd4: effective_upper = 1'b1;
            3'd5: begin effective_lower = 1'b1; effective_upper = 1'b1; end
            default: begin end
        endcase
    end

    initial begin
        for (index = 0; index < 262144; index = index + 1)
            mem[index] = 16'h0000;
    end

    wire [3:0] command = {ncs, nras, ncas, nwe};
    // A10 is auto-precharge. The MiSTer tester's halfword is
    // {chip select, column[9:2], row, bank, column[1:0]}.
    wire [25:0] word_addr = {ncs, a[9:2], open_row, open_ba, a[1:0]};

    // Read masks take effect two chip clocks later. A masked byte is not
    // driven; the tester must clear both masks before every read.
    assign dq[7:0] = reading && !lower_history[1] ? read_data[7:0] : 8'hzz;
    assign dq[15:8] = reading && !upper_history[1] ? read_data[15:8] : 8'hzz;

    always @(posedge clk) begin
        lower_history <= {lower_history[0], dqml};
        upper_history <= {upper_history[0], dqmh};
        if (hold_write_mask && (dqml != last_lower || dqmh != last_upper))
            $fatal(1, "SDRAM write masks not held past WRITE");
        hold_write_mask <= 1'b0;

        if (cycles_after_activate != 2'd3)
            cycles_after_activate <= cycles_after_activate + 2'd1;
        if (reading)
            reading <= 1'b0;
        if (cke && read_delay != 3'd0) begin
            if (read_delay == 3'd1) begin
                reading <= 1'b1;
                read_delay <= 3'd0;
            end else begin
                read_delay <= read_delay - 3'd1;
            end
        end
        if (cke && !ncs) begin
            case (command)
                4'b0000: cas_latency <= a[6:4];
                4'b0001: begin
                    refreshes <= refreshes + 32'd1;
                    refreshed <= 1'b1;
                end
                4'b0011: begin
                    open_ba <= ba;
                    open_row <= a;
                    cycles_after_activate <= 2'd0;
                end
                4'b0101: begin
                    if (cycles_after_activate < 2'd2)
                        $fatal(1, "SDRAM READ violates ACTIVATE-to-CAS timing");
                    if (dqml || dqmh)
                        $fatal(1, "SDRAM READ masks must be clear");
                    read_delay <= cas_latency;
                    read_data <= mem[word_addr[17:0]];
                end
                4'b0100: begin
                    if (cycles_after_activate < 2'd2)
                        $fatal(1, "SDRAM WRITE violates ACTIVATE-to-CAS timing");
                    if (lower_history != {2{dqml}} || upper_history != {2{dqmh}})
                        $fatal(1, "SDRAM write masks lack two-clock setup");
                    hold_write_mask <= 1'b1;
                    last_lower <= dqml;
                    last_upper <= dqmh;
                    banks_seen[open_ba] <= 1'b1;
                    low_columns_seen[a[1:0]] <= 1'b1;
                    rows_seen[open_row[0]] <= 1'b1;
                    high_columns_seen[a[2]] <= 1'b1;
                    if (dqml != dqmh) begin
                        masked_writes <= masked_writes + 32'd1;
                        if (refreshed) refresh_masked_writes <= refresh_masked_writes + 32'd1;
                    end
                    if (dqml && dqmh) no_writes <= no_writes + 32'd1;
                    refreshed <= 1'b0;
                    beat = mem[word_addr[17:0]];
                    if (!effective_lower)
                        beat[7:0] = dq[7:0];
                    if (!effective_upper)
                        beat[15:8] = dq[15:8];
                    mem[word_addr[17:0]] <= beat;
                end
                default: begin
                end
            endcase
        end
    end
endmodule
