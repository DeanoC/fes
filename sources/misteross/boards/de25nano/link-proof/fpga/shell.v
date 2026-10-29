// One clock, synchronous registered slot; no HPS or physical deployment backend.
module fes_link_shell (
    input wire clk,
    input wire reset_n,
    input wire freeze_req,
    output reg req,
    output reg [7:0] address,
    output wire slot_reset,
    input wire rsp_valid,
    input wire [31:0] rsp_data,
    output wire freeze_ack,
    output wire [31:0] isolated_data,
    output reg [31:0] static_count,
    output reg [31:0] retired,
    output reg [31:0] last_data
);
    (* preserve, dont_merge *) reg [1:0] freeze_sync;
    reg frozen;
    reg [2:0] outstanding;
    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) freeze_sync <= 2'b11;
        else freeze_sync <= {freeze_sync[0], freeze_req};
    end
    assign freeze_ack = frozen;
    assign slot_reset = ~reset_n | frozen;
    assign isolated_data = frozen ? 32'b0 : rsp_data;
    always @(posedge clk) begin
        if (!reset_n) begin
            static_count <= 0;
            retired <= 0;
            last_data <= 0;
            address <= 0;
            req <= 0;
            frozen <= 1;
            outstanding <= 0;
        end else begin
            static_count <= static_count + 32'd1;
            req <= !freeze_sync[1] && !frozen && static_count[2:0] == 0;
            if (!freeze_sync[1] && !frozen && static_count[2:0] == 0)
                address <= address + 8'd1;
            if (!frozen) begin
                case ({req, rsp_valid})
                    2'b10: outstanding <= outstanding + 3'd1;
                    2'b01: outstanding <= outstanding - 3'd1;
                    default: outstanding <= outstanding;
                endcase
                if (rsp_valid) begin
                    retired <= retired + 32'd1;
                    last_data <= isolated_data;
                end
                if (freeze_sync[1] && outstanding == 0 && !req && !rsp_valid)
                    frozen <= 1;
            end else if (!freeze_sync[1]) begin
                frozen <= 0;
            end
        end
    end
endmodule
