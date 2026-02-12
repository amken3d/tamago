// BCM2835 SoC support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build arm64

package bcm2835

// defined in busyloop_arm64.s
func busyloop(count int32)
