// Allwinner H616 memory layout
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !linkramstart

package h616

import (
	_ "unsafe"
)

// BL31 runs FROM DRAM on the H616 (SRAM A2 is too small): the boot
// chain loads TF-A at DDR_BASE (0x40000000) and it stays resident to
// serve SMCs. The OS must not touch that region — doing so overwrites
// live EL3 code and the next SMC executes garbage (found the hard way:
// vector tables placed at DDR_BASE trampled BL31).
//
// The reservation is 2 MB because RamStart must be aligned to the MMU
// L2 section size: the arm64 package's table builder classifies whole
// 2 MB sections, and a section straddling RamStart is mapped Device+XN
// (which is fatal once the image's text lives in it).
//go:linkname ramStart runtime/goos.RamStart
var ramStart uint64 = DDR_BASE + 0x200000
