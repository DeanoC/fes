module tb_f04;
    logic [7:0] pins; logic [2:0] v; logic bit_o, ok; int bad = 0;
    f04_struct_shadow dut (.*);
    initial begin
        for (int p = 0; p < 256; p += 37) for (int i = 0; i < 8; i++) begin
            pins = 8'(p); v = 3'(i); #1; if (!ok) bad++;
        end
        $display("CENSUS f04 bad=%0d", bad); $finish;
    end
endmodule
