// SPDX-License-Identifier: MIT
#include "dos_prompt.h"

#include <iostream>
#include <string>

int main() {
    struct Case {
        std::string row;
        std::string expected;
        bool game_launch;
        bool matches;
    };
    const Case cases[] = {
        {"C:\\>", "C:\\>", false, true},
        {"C:\\WOLF3D>", "C:\\>", true, true},
        {"D:\\GAMES\\DOOM>", "C:\\>", true, true},
        {"C:\\WOLF3D>", "C:\\>", false, false},
        {"C:\\>WOLF3D", "C:\\>", true, false},
        {"C:\\WOLF3D>READY>", "C:\\>", true, false},
        {"WOLF3D>", "C:\\>", true, false},
    };
    for (const auto &test : cases) {
        const bool actual = pc386::prompt_matches(test.row, test.expected, test.game_launch);
        if (actual != test.matches) {
            std::cerr << "prompt match for '" << test.row << "' was " << actual
                      << ", expected " << test.matches << '\n';
            return 1;
        }
    }
    std::cout << "PASS pc386 game prompt recognition\n";
}
