// SPDX-License-Identifier: MIT
// pc386 OPL2 (docs/pc386-machine-design.md, "OPL2"): the opl2_fpga import
// behind an AdLib register port, with the status register and both timers.
//
// The chip samples at 3.579545 MHz / 72 = 49,715.9 Hz; SAMPLE_N/SAMPLE_D is
// that rate over the system clock (7/14080 at 100 MHz). Timer 1 counts every
// 4 samples (80.5 us) and timer 2 every 16 (321.8 us), as the chip's 288- and
// 1152-clock prescalers do.
//
// Register writes reach the import only while its operator pipeline is idle,
// between one sample's computation and the next, so every write lands on a
// sample boundary as in Nuked-OPL3, the reference it is checked against. A
// write that arrives during the computation waits in a four-entry queue;
// the pipeline is busy for under 100 clocks and each guest OUT takes 72.
//
// Ports: a0 = 0 writes the register index and reads status; a0 = 1 writes
// data and reads FFh. Status is {IRQ, timer 1 flag, timer 2 flag, 00110b},
// the low bits an OPL2 returns and an OPL3 does not. Register 04h: bit 7
// clears both flags and ignores the rest; otherwise bits 6 and 5 mask timers
// 1 and 2 and bits 0 and 1 start them, loading the preset from 02h or 03h.
module pc386_opl2
    import opl2_pkg::*;
#(
    parameter integer SAMPLE_N = 7, SAMPLE_D = 14080
) (
    input wire clk, reset,
    input wire access, write, a0,     // access: one clock per guest byte
    input wire [7:0] wdata,
    output wire [7:0] rdata,
    output wire sample_valid,         // one clock per sample
    output wire signed [15:0] sample
);
    // ---- Register port ------------------------------------------------------
    reg [7:0] index;
    reg [7:0] timer1_preset, timer2_preset;
    reg timer1_mask, timer2_mask, timer1_start, timer2_start, timer1_flag, timer2_flag;
    wire data_write = access && write && a0;
    assign rdata = a0 ? 8'hff : {timer1_flag || timer2_flag, timer1_flag, timer2_flag, 5'b00110};

    // ---- Sample enable and write queue --------------------------------------
    wire sample_tick;
    pc386_rate #(.N(SAMPLE_N), .D(SAMPLE_D)) sample_rate (.clk(clk), .reset(reset), .tick(sample_tick));

    reg busy;
    reg [15:0] queue [0:3];
    reg [1:0] head, tail;
    reg [2:0] count;
    wire release_write = count != 0 && !busy && !sample_tick;
    opl2_reg_wr_t opl2_reg_wr;
    always @* begin
        opl2_reg_wr.valid = release_write;
        opl2_reg_wr.address = queue[head][15:8];
        opl2_reg_wr.data = queue[head][7:0];
    end

    always @(posedge clk) begin
        if (reset) begin
            busy <= 0;
            head <= 0; tail <= 0; count <= 0;
        end else begin
            if (sample_tick) busy <= 1;
            else if (sample_valid) busy <= 0;
            if (data_write) begin
                queue[tail] <= {index, wdata};
                tail <= tail + 1'b1;
            end
            if (release_write) head <= head + 1'b1;
            count <= count + {2'b00, data_write} - {2'b00, release_write};
        end
    end

    channels opl2 (
        .clk(clk), .reset(reset), .clk_dac(clk),
        .opl2_reg_wr(opl2_reg_wr), .sample_clk_en(sample_tick),
        .sample_valid(sample_valid), .sample(sample)
    );

    // ---- Timers -----------------------------------------------------------------
    reg [3:0] prescale;
    reg [7:0] timer1, timer2;
    wire timer1_tick = sample_tick && prescale[1:0] == 2'b11;
    wire timer2_tick = sample_tick && prescale == 4'b1111;
    always @(posedge clk) begin
        if (reset) begin
            index <= 0;
            timer1_preset <= 0; timer2_preset <= 0; timer1 <= 0; timer2 <= 0;
            timer1_mask <= 0; timer2_mask <= 0; timer1_start <= 0; timer2_start <= 0;
            timer1_flag <= 0; timer2_flag <= 0;
            prescale <= 0;
        end else begin
            if (sample_tick) prescale <= prescale + 1'b1;
            if (timer1_start && timer1_tick) begin
                timer1 <= timer1 == 8'hff ? timer1_preset : timer1 + 1'b1;
                if (timer1 == 8'hff && !timer1_mask) timer1_flag <= 1;
            end
            if (timer2_start && timer2_tick) begin
                timer2 <= timer2 == 8'hff ? timer2_preset : timer2 + 1'b1;
                if (timer2 == 8'hff && !timer2_mask) timer2_flag <= 1;
            end
            if (access && write && !a0) index <= wdata;
            if (data_write) case (index)
                8'h02: timer1_preset <= wdata;
                8'h03: timer2_preset <= wdata;
                8'h04: if (wdata[7]) begin
                    timer1_flag <= 0;
                    timer2_flag <= 0;
                end else begin
                    timer1_mask <= wdata[6];
                    timer2_mask <= wdata[5];
                    timer1_start <= wdata[0];
                    timer2_start <= wdata[1];
                    if (wdata[0] && !timer1_start) timer1 <= timer1_preset;
                    if (wdata[1] && !timer2_start) timer2 <= timer2_preset;
                end
                default: ;
            endcase
        end
    end
    initial if (SAMPLE_N <= 0 || SAMPLE_N >= SAMPLE_D) $error("pc386_opl2 needs 0 < SAMPLE_N < SAMPLE_D");
endmodule
