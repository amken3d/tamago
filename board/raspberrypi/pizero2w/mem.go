// Raspberry Pi Zero 2W support for tamago/arm64
// https://github.com/usbarmory/tamago
//
// Copyright (c) the pizero2w package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !linkramsize

package pizero2w

import (
	_ "unsafe"
)

// The Pi Zero 2W has 512 MB LPDDR2 RAM. The VideoCore GPU reserves 64 MB by
// default (gpu_mem=64 in config.txt), leaving 448 MB for the ARM.

//go:linkname ramSize runtime/goos.RamSize
var ramSize uint64 = 0x20000000 - 0x04000000 // 512 MB - 64 MB (VideoCore)
