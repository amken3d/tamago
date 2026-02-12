// BCM2835 SoC support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build arm

package bcm2835

import "github.com/usbarmory/tamago/arm"

func busyloop(count int32) {
	arm.Busyloop(uint32(count))
}
