// BCM2835 SoC support
// https://github.com/usbarmory/tamago
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// func read_systimer() int64
//
// The 64-bit counter is exposed as two 32-bit registers, so a naive lower-then-
// upper read is torn by the carry: sample the low word at 0xffffffff, take the
// carry, then sample the high word, and the two halves describe different
// instants -- the result is ~4295 seconds (2^32 microseconds) in the FUTURE.
// It happens once per low-word wrap, which is to say every 71 minutes there is
// a window a few cycles wide in which time can jump forward by an hour.
//
// Read the high word, the low word, then the high word again, and retry if the
// carry landed in between.
TEXT ·read_systimer(SB),$0-8
	MOVW	·peripheralBase(SB), R2
	ADD	$0x00003000, R2 // timer peripheral offset

again:
	MOVW	8(R2), R1 // upper 32-bits
	MOVW	4(R2), R0 // lower 32-bits
	MOVW	8(R2), R3 // upper 32-bits again

	CMP	R1, R3
	BNE	again

	MOVW	R0, ret_lo+0(FP)
	MOVW	R1, ret_hi+4(FP)

	RET
