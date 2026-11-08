// SPDX-License-Identifier: MIT
#pragma once

#include <string>

namespace pc386 {

// FreeDOS's PROMPT $P$G includes its current directory, so a returned game
// can leave a row such as C:\WOLF3D>. Only game runs accept that form;
// other machine tests keep matching their configured prompt exactly.
inline bool is_game_prompt_row(const std::string &row) {
    if (row.size() < 4 || row[1] != ':' || row[2] != '\\' || row.back() != '>') return false;
    const char drive = row[0];
    if (!((drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z'))) return false;
    return row.find('>', 3) == row.size() - 1;
}

inline bool prompt_matches(const std::string &row, const std::string &expected, bool game_launch) {
    return row == expected || (game_launch && is_game_prompt_row(row));
}

}  // namespace pc386
