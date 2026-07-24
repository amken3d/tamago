// Qualcomm QCM2290 random number generator
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package qcm2290

import (
	_ "unsafe"

	"github.com/usbarmory/tamago/internal/reg"
	"github.com/usbarmory/tamago/internal/rng"
)

// PRNG registers (qcom,prng-ee: the TrustZone-configured execution
// environment interface, drivers/char/hw_random/qcom-rng.c)
const (
	PRNG_BASE = 0x04453000

	PRNG_DATA_OUT = PRNG_BASE + 0x0
	PRNG_STATUS   = PRNG_BASE + 0x4

	PRNG_STATUS_DATA_AVAIL = 1 << 0
)

// prngWord returns one word of hardware entropy, or a degraded counter
// mix if the PRNG never signals availability (bounded spin: this runs
// before timers exist).
func prngWord() uint32 {
	for i := 0; i < 100000; i++ {
		if reg.Read(PRNG_STATUS)&PRNG_STATUS_DATA_AVAIL != 0 {
			return reg.Read(PRNG_DATA_OUT)
		}
	}

	return reg.Read(PRNG_DATA_OUT) // degraded: whatever the FIFO holds
}

//go:linkname initRNG runtime/goos.InitRNG
func initRNG() {
	// seed an AES-CTR DRBG from the TrustZone-maintained PRNG
	drbg := &rng.DRBG{}

	for i := 0; i < len(drbg.Seed); i += 4 {
		rng.Fill(drbg.Seed[:], i, prngWord())
	}

	rng.GetRandomDataFn = drbg.GetRandomData
}
