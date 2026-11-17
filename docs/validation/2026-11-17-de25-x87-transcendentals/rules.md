# The 387's rules for the ten instructions, as read

Written 17 Nov 2026, before any RTL for these instructions. Each rule is
**read** from Intel's documents, not recalled, and carries its page. Where the
documents are silent, contradict themselves or say "undefined", that is said,
and the rule the RTL follows is marked **measured** (on the two build hosts'
x87 units and the Bochs oracle) with what it rests on. A measured rule is a
modern part's behaviour, not a 387's: no 387 has been consulted.

Sources, and how each was read:

- **PRM**: Intel 80387 Programmer's Reference Manual 231917-001 (1987). The
  public PDF is an image scan. Prose is from archive.org's OCR text; **every
  table and every inequality quoted here was checked on the page image**.
- **DS**: Intel387 DX datasheet 240448-005 (1992), from its text layer.

Rule numbers (`G1`, `SIN3`, ...) are what the directed cases cite; the
generator lists the cases against each rule and prints a rule with none as
uncovered.

## General rules

| Rule | The rule | Source |
| --- | --- | --- |
| G1 | The relative error of a transcendental result is **less than 2^-62**. | PRM 4.6 p.4-16: "The absolute value of the relative error of the transcendental instructions is guaranteed to be less than 2^-62." |
| G2 | Trigonometric operands are reduced by an internally stored **pi/4 with 67 significant bits**, so the function computed has a rounded period, not a rounded argument. | PRM 4.4.9 p.4-10: "an internally stored pi/4 divisor that has 67 significant bits"; "does not create the effect of a rounded argument, but of a rounded period." |
| G3 | **Precision control does not apply.** The result is rounded to extended precision whatever PC says. | PRM 2.1.3 p.2-4: "No other operations are affected by PC."; DS 2.3.5 p.14. |
| G4 | **Rounding control applies** to all ten: they are arithmetic instructions that round at the end. How RC combines with G1's bound is not stated. | PRM Table 3-1 p.3-2; DS 2.3.5 p.14. |
| G5 | **C1** is the round-up bit when PE is set, and the overflow (1) or underflow (0) direction on a stack fault. C0, C2 and C3 are **undefined** except C2 on the four trigonometric forms. | PRM Table 2-1 p.2-5 and its footnotes. |
| G6 | "All the transcendental instructions are inexact by definition; they always cause the inexact exception." The manual does not say whether that holds for the exact special cases its own tables give (F2XM1 of zero, FCOS of zero, FPATAN of a zero). | PRM 3.2.6 p.3-25. **See M1.** |
| G7 | An unsupported format (pseudo-zero, pseudo-NaN, pseudo-infinity, unnormal) as an operand raises invalid; masked, the result is the real indefinite. | PRM 3.1.7 p.3-13; Table 3-10 p.3-21. |
| G8 | A signalling NaN raises invalid and is quieted; a quiet NaN raises nothing (FBSTP excepted). Two NaNs deliver the one with the larger significand. | PRM Table 3-5 p.3-12; C.2 p.C-1. |
| G9 | A denormal operand raises the denormal exception in a transcendental; masked, the operation proceeds on the normalised value. A pseudo-denormal is used with its exponent read as 1 and raises it too. | PRM C.2 p.C-2; 3.1.7. |
| G10 | Exception priority: stack underflow, stack overflow, unsupported format, SNaN, QNaN, other invalid or zero divide, denormal, overflow and underflow, precision. The manual has no "pre-operation / post-operation" terms. | PRM 3.2.7 p.3-26. |
| G11 | An unmasked invalid, zero divide or denormal leaves the operands and TOP as they were. | PRM 3.2.2.1 p.3-20, 3.2.2.2, 3.2.3, 3.2.4. |
| G12 | Underflow can occur in all eight transcendentals: masked, it is flagged when the result is tiny **and** inexact; unmasked, whenever it is tiny, and the result is delivered scaled by 2^24576. | PRM 3.2.5.2 p.3-24. |
| G13 | A stack fault sets IE and SF; masked, "the 80387 returns the QNaN indefinite. This value overwrites the destination register"; unmasked, TOP and the sources are unchanged. | PRM 3.2.2.1 p.3-20. |
| G14 | Which exceptions each instruction can raise (IS stack, I, D, Z, O, U, P): F2XM1, FCOS, FSIN, FSINCOS, FPTAN, FPATAN, FYL2XP1: IS I D U P. FYL2X: IS I D Z O U P. FBLD: IS. FBSTP: IS I P. | PRM Appendix B pp.B-1/B-2. |

The manual contradicts itself in two places, recorded rather than resolved by
preference: section 3.2.5.1 lists FYL2XP1 among the instructions that can
overflow while Appendix B gives it no O; and Appendix C says FBSTP "can
underflow" on a denormal while Appendix B gives it neither U nor D. **See
M5 and M6.**

## FSIN, FCOS, FSINCOS, FPTAN

| Rule | The rule | Source |
| --- | --- | --- |
| TRIG1 | The operand must satisfy \|ST\| < 2^63. In range, C2 is cleared and the result produced. | PRM 4.6.1 p.4-17. |
| TRIG2 | At \|ST\| >= 2^63, **C2 is set and ST is left intact**: "no reduction of the operand is performed". C1 is undefined then. | PRM 4.6.1 p.4-17; Table 2-1 footnote: "the original operand remains at the top of the stack." |
| TRIG3 | For FSINCOS and FPTAN out of range, that nothing is pushed is **implied** by TRIG2's wording and nowhere stated. | **See M2.** |
| TRIG4 | An infinite operand is invalid: masked, "Return the QNaN indefinite; set C2." What FSINCOS and FPTAN push then is not stated. | PRM Table 3-10 p.3-21. **See M3.** |
| SIN1 | FSIN replaces ST with sin(ST). FSIN of a zero is that zero, sign kept. | PRM 4.6.2; Table 3-3. |
| COS1 | FCOS replaces ST with cos(ST). FCOS of either zero is +1. | PRM 4.6.1; Table 3-3. |
| SC1 | FSINCOS replaces ST with sin(ST) and then **pushes cos(ST)**: afterwards ST(0) is the cosine and ST(1) the sine. ST(7) must be empty. | PRM 4.6.3 p.4-17. |
| TAN1 | FPTAN replaces ST with tan(ST) and then **pushes 1.0**: afterwards ST(0) is 1 and ST(1) the tangent, "a true tangent result in ST(1), and (always) a floating point 1 in ST". ST(7) must be empty. FPTAN of a zero is that zero. | PRM 4.6.4 p.4-17; DS 2.7.1 p.16; Table 3-3. |
| TAN2 | FPTAN's masked stack overflow leaves quiet NaNs in **both** ST and ST(1), so the push happens. The same is not stated for FSINCOS. | PRM C.4 p.C-5; DS 2.7.2 item 13. **See M4.** |

## FPATAN

| Rule | The rule | Source |
| --- | --- | --- |
| ATAN1 | Computes arctan(ST(1)/ST(0)), pops, and leaves the result in the new ST. Operands are unrestricted. | PRM 4.6.5 p.4-18. |
| ATAN2 | The result's octant follows the two signs and which magnitude is larger, from -pi to +pi. | PRM Table 4-10 p.4-18. |
| ATAN3 | Zeros: a zero Y over a positive X is that zero; over a negative X, pi with Y's sign; any non-zero Y over a zero X is pi/2 with Y's sign; zero over +0 is the zero, over -0 pi with its sign. | PRM Table 3-3 p.3-8. |
| ATAN4 | Infinities: infinite Y over finite X is pi/2 with Y's sign; finite Y over +infinity is a zero with Y's sign, over -infinity pi with Y's sign; infinity over +infinity is pi/4, over -infinity 3pi/4, with Y's sign. **No operand pair is invalid**; only SNaNs and unsupported formats are. | PRM Table 3-4 p.3-10. |

## F2XM1

| Rule | The rule | Source |
| --- | --- | --- |
| EXP1 | Computes 2^ST - 1 in place, for **-1 <= ST <= +1**. "If the argument is out of range, the results are undefined." | PRM 4.6.6 p.4-18; DS note l. |
| EXP2 | Either zero gives that zero. +infinity gives +infinity and -infinity gives -1, which the manual defines although EXP1 calls them undefined. | PRM Table 3-3, Table 3-4. |
| EXP3 | "For values of the argument very close in magnitude to 1, a larger error will be incurred" than G1's. | PRM 4.6.6 p.4-18. |

## FYL2X and FYL2XP1

| Rule | The rule | Source |
| --- | --- | --- |
| LOG1 | FYL2X computes ST(1) * log2(ST), pops, and leaves the result in the new ST, for 0 <= ST < +infinity and any ST(1). | PRM 4.6.7 p.4-19. |
| LOG2 | A negative ST is invalid (indefinite when masked), "except FYL2X(-0) = -infinity". | PRM Table 3-10 p.3-21. |
| LOG3 | A zero ST with a non-zero ST(1) is a **zero divide**: masked, an infinity "signed with the exclusive OR of the signs of the operands". A zero ST with a zero ST(1) is invalid. | PRM Table 3-3 p.3-8; 3.2.3 p.3-22. |
| LOG4 | Infinities: infinite ST(1) with ST = 1 is invalid; with ST > 1 the infinity keeps its sign and with 0 < ST < 1 it is negated; ST = +infinity with a non-zero ST(1) is an infinity with ST(1)'s sign, with a zero ST(1) invalid; ST = -infinity is invalid. | PRM Table 3-4 p.3-10. |
| LOG5 | FYL2XP1 computes ST(1) * log2(ST + 1) the same way, for **\|ST\| < 1 - sqrt(2)/2**. "If the argument is out of range, the results are undefined", but an operand more negative than -1 is invalid. | PRM 4.6.8 p.4-19; Table 3-10; DS note n. |
| LOG6 | FYL2XP1 of a zero ST is a zero whose sign is the zero's for positive ST(1) and the opposite for negative. | PRM Table 3-3. |

## FBLD and FBSTP

| Rule | The rule | Source |
| --- | --- | --- |
| BCD1 | The format: bit 79 the sign, bits 78 to 72 without significance ("80387 ignores when loading, zeros when storing"), eighteen digits in bits 71 to 0. | PRM Fig. 2-10 p.2-12, note 3. |
| BLD1 | FBLD converts and pushes; it is exact; "the sign of the source is preserved, including the case where the value is negative zero." | PRM 4.3.8 p.4-4. |
| BLD2 | "The instruction does not check for invalid digits (A-FH), and the result of attempting to load an invalid encoding is undefined." | PRM 4.3.8. **See M7.** |
| BLD3 | Its only exception is the stack fault; C1 is zero otherwise. | PRM Appendix B; Table 2-1. |
| BST1 | FBSTP converts ST to a packed decimal integer, stores it and pops, rounding "according to the RC field". PE and the round-up bit record an inexact conversion. | PRM 4.3.9 p.4-5; Table 2-1. |
| BST2 | It is invalid when the source "is empty, a NaN, infinity, or exceeds 18 decimal digits": masked, it stores the packed decimal indefinite. A **quiet** NaN is invalid here too. | PRM Table 3-10 p.3-21; C.2 p.C-1. |
| BST3 | The packed decimal indefinite is defined **only in its top two bytes, FFh FFh**; the sixteen low digits are printed as undefined. | PRM Table 3-7 p.3-15. **See M8.** |
| BST4 | Either zero stores as a zero with that sign. A denormal operand is supported. | PRM Table 3-3 p.3-7; C.2 p.C-2. |

## Where the manual is silent: measured rules

Each is filled in from a measurement on the references and says which. They
are the hosts' and the oracle's behaviour, adopted where the references agree
and the manual does not contradict them; none is evidence about a 387.

| Rule | The question the manual leaves open | What is adopted, and from what |
| --- | --- | --- |
| M1 | Is PE set on the exact special cases (G6)? | **Not for a zero operand; always for a finite non-zero one.** Both hosts and the oracle leave PE clear for F2XM1, FSIN and FPTAN of a zero, FCOS of a zero (+1), FPATAN's zero results and FYL2X of exactly 1. They set it for every other finite operand, including results that are exactly representable: F2XM1 of +1 and -1, FYL2X of a power of two, FPATAN's multiples of pi/4. |
| M2 | Do FSINCOS and FPTAN push when C2 is set (TRIG3)? | **No.** At and above 2^63 all three references set C2 and nothing else: no push, the operand unchanged, no flag, C1 clear. |
| M3 | What do FSINCOS and FPTAN leave for an infinite or NaN operand, masked (TRIG4)? | **Both results, pushed:** the indefinite in ST(0) and ST(1) for an infinity or an unsupported format, the quieted NaN in both for a NaN. All three references. The references leave C2 clear for the infinity; the unit sets it, by TRIG4, and that is a listed divergence. |
| M4 | FSINCOS's masked stack overflow (TAN2). | **As FPTAN's:** the push is made, both registers hold the indefinite, C1 is set, and nothing else is flagged, not even PE. All three references. |
| M5 | Can FYL2XP1 overflow? | **Yes.** The hosts deliver an overflowed result for a large ST(1); the unit's last multiply overflows through A_RND as theirs does. |
| M6 | Does FBSTP of a denormal raise D or U? | **Neither.** PE only, on the host and the oracle, including with DE unmasked. |
| M7 | FBLD of digits A to F (BLD2). | **`acc * 10 + nibble`**, on the host and the oracle, over 4,000 random encodings. The unit does the same. |
| M8 | The packed decimal indefinite's low bytes (BST3), FBSTP's pop on a masked invalid, and the sign of a zero a small value rounds to. | **`FFFF C000 0000 0000 0000`**, whose low eight bytes are the references' and not the manual's; **the pop is made**; **the source's sign** is stored. Host and oracle. |
| M9 | A masked stack underflow in FPATAN, FYL2X and FYL2XP1: is the pop still made? | **Yes.** The indefinite goes to ST(1) and ST(0) is popped; C1 is clear. All three references. |
| M10 | F2XM1 and FYL2XP1 outside their ranges. | **F2XM1 beyond 1: the operand comes back unchanged, with PE**, on all three references, and the unit does the same. **FYL2XP1 above the range: the hosts compute y log2(1 + x)** and so does the unit (its model is within 1 of the host there). **At and below -1 the references return a finite value**; the unit follows the manual below -1 (invalid) and treats -1 as the logarithm of zero, and neither is compared with a reference. |
| M11 | A denormal operand where the answer comes from the operand classes (a zero, an infinity, a multiple of pi/4). | **DE is raised there too**, unless an invalid or a zero divide is the only thing reported. Both hosts. |
| M12 | A tiny result. | **UE with PE whenever the result is a denormal**, the instruction being inexact by definition (G6, G12). Both hosts. The oracle leaves UE clear on some of these (FPATAN, FYL2X and FYL2XP1 with a denormal ST(1)), which is a listed divergence. |
| M13 | The sign of FYL2X's zero-divide infinity (LOG3). | **The opposite of ST(1)'s**, whatever the zero's sign: the logarithm of zero is minus infinity. All three references. The manual's "exclusive OR of the signs" contradicts its own "FYL2X(-0) = -infinity" and is not followed. |

Where each answer comes from: FBLD and FBSTP (M6 to M8) were measured by the
unit bench's own packed decimal cases against the host and by the lockstep
against the oracle. Everything else is the special-operand probe: 4,460
cases on the Intel host, the AMD host and the oracle, kept under
[evidence/](evidence/) as `special-host.txt.gz`, `special-other-host.txt.gz`
and `special-oracle.txt.gz`, with their comparison in
`special-comparison.json`. The two hosts agree on all 4,460 (a value within
one, C1 apart). The oracle differs from the host in flags, TOP or tags on 82:
the unmasked overflow and underflow results, which a host delivers scaled as
G12 says and the oracle does not, and M12's underflow flag.

## What the modern references are known not to share with a 387

Stated so the measured rules are not read for more than they are.

- **The reduction constant.** G2 gives a 67-bit pi/4. The two hosts agree with
  each other bit for bit beside multiples of pi/2
  ([evidence/host-comparison.json](evidence/host-comparison.json)), so they
  share a constant; whether it is G2's is settled by the model, which
  reduces by G2's constant, and reported as a measurement of the hosts
  against the manual, not assumed.
- **The algorithm.** The 387 uses a modified CORDIC whose microcode is not
  published; the hosts use something else. Results are comparable only
  through G1's bound.
- **Rounding control.** G4 says RC is in effect. What a 387 returns under a
  directed rounding mode is known only to within G1's bound.
