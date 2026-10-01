# Guess the number - original FES homebrew, copyright 2026 FES contributors.
# SPDX-License-Identifier: MIT. See ../licenses/guess-number-MIT.txt.
10 RAND
20 LET N=INT (RND*20)+1
30 CLS
40 PRINT "GUESS 1 TO 20"
50 INPUT G
60 CLS
70 IF G<N THEN PRINT "HIGHER"
80 IF G>N THEN PRINT "LOWER"
90 IF G<>N THEN GOTO 50
100 PRINT "RIGHT - RUN TO PLAY AGAIN"
110 STOP
