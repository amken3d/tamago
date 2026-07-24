// Qualcomm QCM2290 memory layout
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !linkramstart

package qcm2290

import (
	_ "unsafe"
)

//go:linkname ramStart runtime/goos.RamStart
var ramStart uint64 = DDR_BASE
