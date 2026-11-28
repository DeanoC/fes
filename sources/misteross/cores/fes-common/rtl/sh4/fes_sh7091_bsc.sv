// SH7091 BSC registers and refresh timer. The board memory controller
// supplies the bus clock enable and handles external memory commands.
module fes_sh7091_bsc #(
    parameter logic [2:0] BCR1_STRAPS = 3'b110, // little-endian, master, non-MPX
    parameter logic [1:0] AREA0_WIDTH = 2'b10  // 16-bit ROM
) (
    input  logic        clk,
    input  logic        reset,
    input  logic        bus_tick,
    input  logic        request,
    input  logic [31:2] address,
    input  logic [3:0]  rmask, wstrb,
    input  logic [31:0] wdata,
    output logic        selected,
    output logic        rvalid,
    output logic [31:0] rdata,
    output logic [1:0]  refresh_irq,
    output logic [1:0]  sdram_command,
    output logic        sdram_mode_set,
    output logic [11:0] sdram_mode_address
);
    localparam logic [3:0] BCR1 = 0, BCR2 = 1, WCR1 = 2, WCR2 = 3, WCR3 = 4, MCR = 5,
                           PCR = 6, RTCSR = 7, RTCNT = 8, RTCOR = 9, RFCR = 10;
    logic [31:0] bcr1, wcr1, wcr2, wcr3, mcr;
    logic [15:0] bcr2, pcr;
    logic [7:0] rtcsr, rtcnt, rtcor;
    logic [9:0] rfcr;
    logic [11:0] prescaler, divisor;
    logic [3:0] select_reg;
    logic register_selected, mode_selected, mode_write, wide_reg;
    logic write_reg, read_reg, keyed_write, timer_tick, timer_match, overflow;
    logic [7:0] next_count;

    assign select_reg = address[5:2];
    assign register_selected = address[31:6] == 26'h3fe0000 && select_reg <= RFCR;
    assign mode_selected = address[31:12] inside {20'hff900, 20'hff940};
    assign selected = register_selected || mode_selected;
    assign wide_reg = select_reg != BCR2 && select_reg <= MCR;
    assign write_reg = request && register_selected && wstrb == (wide_reg ? 4'hf : 4'h3);
    assign read_reg = request && register_selected && wstrb == 0 && rmask == (wide_reg ? 4'hf : 4'h3);
    assign keyed_write = write_reg && wdata[15:8] == 8'ha5;
    assign mode_write = request && mode_selected && $onehot(wstrb);
    assign refresh_irq = {rtcsr[2] && rtcsr[1], rtcsr[7] && rtcsr[6]};

    always_comb begin
        case (rtcsr[5:3])
            1: divisor = 12'd3;
            2: divisor = 12'd15;
            3: divisor = 12'd63;
            4: divisor = 12'd255;
            5: divisor = 12'd1023;
            6: divisor = 12'd2047;
            default: divisor = 12'd4095;
        endcase
    end
    assign timer_tick = bus_tick && rtcsr[5:3] != 0 && prescaler == divisor;
    assign next_count = rtcnt + 8'd1;
    assign timer_match = timer_tick && next_count == rtcor;
    assign overflow = timer_match && (rtcsr[0] ? rfcr[8:0] == 9'd511 : rfcr == 10'd1023);

    always_ff @(posedge clk) begin
        if (reset) begin
            bcr1 <= {BCR1_STRAPS, 29'b0};
            bcr2 <= {AREA0_WIDTH, 14'h3ffc};
            wcr1 <= 32'h7777_7777;
            wcr2 <= 32'hfffe_efff;
            wcr3 <= 32'h0777_7777;
            mcr <= 0;
            pcr <= 0;
            rtcsr <= 0;
            rtcnt <= 0;
            rtcor <= 0;
            rfcr <= 0;
            prescaler <= 0;
            rvalid <= 0;
            sdram_command <= 0;
        end else begin
            rvalid <= request && selected && wstrb == 0;
            if (request && selected && wstrb == 0) begin
                rdata <= 0;
                if (read_reg) case (select_reg)
                    BCR1: rdata <= bcr1;
                    BCR2: rdata <= {16'b0, bcr2};
                    WCR1: rdata <= wcr1;
                    WCR2: rdata <= wcr2;
                    WCR3: rdata <= wcr3;
                    MCR: rdata <= mcr;
                    PCR: rdata <= {16'b0, pcr};
                    RTCSR: rdata <= {24'b0, rtcsr};
                    RTCNT: rdata <= {24'b0, rtcnt};
                    RTCOR: rdata <= {24'b0, rtcor};
                    RFCR: rdata <= {22'b0, rfcr};
                    default: ;
                endcase
            end
            sdram_command <= 0;
            if (mode_write) begin
                sdram_command <= address[18] ? 2'b10 : 2'b01;
                sdram_mode_set <= mcr[30];
                sdram_mode_address <= {address[11:2], wstrb[3] || wstrb[2], wstrb[3] || wstrb[1]};
            end
            if (write_reg) case (select_reg)
                BCR1: bcr1 <= {BCR1_STRAPS, 29'b0} | (wdata & 32'h033e_fffd);
                BCR2: bcr2 <= {AREA0_WIDTH, wdata[13:2], 1'b0, wdata[0]};
                WCR1: wcr1 <= wdata & 32'h7777_7777;
                WCR2: wcr2 <= wdata & 32'hfffe_efff;
                WCR3: wcr3 <= wdata & 32'h0777_7777;
                MCR: mcr <= wdata & 32'hf8bb_ffff;
                PCR: pcr <= wdata[15:0];
                RTCOR: if (keyed_write) rtcor <= wdata[7:0];
                default: ;
            endcase

            if (timer_tick || (keyed_write && select_reg == RTCSR && wdata[5:3] != rtcsr[5:3]))
                prescaler <= 0;
            else if (bus_tick && rtcsr[5:3] != 0)
                prescaler <= prescaler + 12'd1;
            if (keyed_write && select_reg == RTCNT)
                rtcnt <= wdata[7:0];
            else if (timer_tick)
                rtcnt <= timer_match ? 8'b0 : next_count;
            if (write_reg && select_reg == RFCR && wdata[15:10] == 6'b101001)
                rfcr <= wdata[9:0];
            else if (timer_match)
                rfcr <= overflow ? 10'b0 : rfcr + 10'd1;
            if (keyed_write && select_reg == RTCSR) begin
                rtcsr[6:3] <= wdata[6:3];
                rtcsr[1:0] <= wdata[1:0];
                rtcsr[7] <= rtcsr[7] && wdata[7];
                rtcsr[2] <= rtcsr[2] && wdata[2];
            end
            if (timer_match)
                rtcsr[7] <= 1;
            if (overflow)
                rtcsr[2] <= 1;
        end
    end
endmodule
