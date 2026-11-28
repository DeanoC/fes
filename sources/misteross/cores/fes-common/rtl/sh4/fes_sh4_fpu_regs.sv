// FR and XF are two banks of eight register pairs. The even-numbered
// register is the high word when the pair represents a double.
module fes_sh4_fpu_regs (
    input  logic        clk,
    input  logic        reset,
    input  logic [3:0]  read_number,
    input  logic        read_pair,
    output logic [63:0] read_value,
    input  logic        write_register,
    input  logic [3:0]  write_number,
    input  logic        write_pair,
    input  logic [63:0] write_value,
    input  logic        write_fpul,
    input  logic        write_fpscr,
    input  logic        change_bank,
    input  logic        change_size,
    input  logic [31:0] system_value,
    output logic [31:0] fpul,
    output logic [31:0] fpscr
);
    logic [31:0] fr_even [0:15], fr_odd [0:15];
    logic [3:0] read_index, write_index;
    logic bank;

    assign bank = fpscr[21];
    assign read_index = {bank ^ (read_pair && read_number[0]), read_number[3:1]};
    assign write_index = {bank ^ (write_pair && write_number[0]), write_number[3:1]};
    assign read_value[63:32] = read_pair ? fr_even[read_index] : 32'b0;
    assign read_value[31:0] = read_pair || read_number[0] ? fr_odd[read_index] : fr_even[read_index];

    always_ff @(posedge clk) begin
        if (reset) begin
            fpscr <= 32'h0004_0001;
        end else begin
            if (write_register) begin
                if (write_pair || !write_number[0])
                    fr_even[write_index] <= write_pair ? write_value[63:32] : write_value[31:0];
                if (write_pair || write_number[0])
                    fr_odd[write_index] <= write_value[31:0];
            end
            if (write_fpul)
                fpul <= system_value;
            if (write_fpscr)
                fpscr <= system_value & 32'h003f_ffff;
            else begin
                if (change_bank)
                    fpscr[21] <= !fpscr[21];
                if (change_size)
                    fpscr[20] <= !fpscr[20];
            end
        end
    end
endmodule
