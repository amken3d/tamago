// BCM2835 SoC support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// func busyloop(count int32)
TEXT ·busyloop(SB),$0-4
	MOVW	count+0(FP), R0
loop:
	SUBS	$1, R0, R0
	BNE	loop
	RET
