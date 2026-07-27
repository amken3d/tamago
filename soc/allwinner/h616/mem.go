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

//go:linkname ramStart runtime/goos.RamStart
var ramStart uint64 = DDR_BASE
