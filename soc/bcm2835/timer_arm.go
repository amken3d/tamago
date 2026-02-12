// BCM2835 SoC timer support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build arm

package bcm2835

// defined in timer_arm.s
func read_systimer() int64
