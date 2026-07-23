// ARM processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build arm.6 && !linkhwinit0

package arm

import (
	_ "unsafe"
)

// Init takes care of the lower level initialization triggered before runtime
// setup (pre World start).
//
// Applications requiring different pre-runtime initialization (e.g. SoCs
// that must bring up the MMU and caches before runtime setup, see
// soc/bcm2835.Init0) can exclude this hook with the `linkhwinit0` build tag
// and provide their own runtime/goos.Hwinit0.
//
//go:linkname Init runtime/goos.Hwinit0
func Init() {
	if int(read_cpsr()&0x1f) != SYS_MODE {
		// initialization required only when in PL1
		return
	}

	vfp_enable()
}
