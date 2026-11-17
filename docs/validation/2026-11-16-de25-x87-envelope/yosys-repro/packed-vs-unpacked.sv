// Does the locked Yosys handle a PACKED array of packed structs, where it
// mishandles an unpacked one?
module t (input logic clk, input logic [7:0] in, output logic [7:0] u_out, output logic [7:0] p_out);
    typedef struct packed { logic [7:0] value; logic flag; } item_t;
    item_t u_stage [1:2];        // unpacked: known broken
    item_t [1:2] p_stage;        // packed: the candidate
    always_ff @(posedge clk) begin
        u_stage[1].value <= in;  u_stage[2] <= u_stage[1];  u_out <= u_stage[2].value;
        p_stage[1].value <= in;  p_stage[2] <= p_stage[1];  p_out <= p_stage[2].value;
    end
endmodule
