// Allwinner H616 random number generator
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package h616

import (
	_ "unsafe"

	"github.com/usbarmory/tamago/internal/rng"
)

// The H616 Crypto Engine has a TRNG, but it sits behind the CE task
// queue interface (DMA descriptors) — real work, deferred until a
// networking milestone needs cryptographic quality. Until then the DRBG
// is seeded from generic-timer sampling jitter: successive counter
// reads interleaved with integer mixing have data-dependent spacing on
// an out-of-order-fetch, DRAM-backed early boot. DEGRADED entropy —
// fine for runtime map hashing and bring-up, not for key material.

// splitmix64 is the SplitMix64 mixing function.
func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb

	return x ^ (x >> 31)
}

//go:linkname initRNG runtime/goos.InitRNG
func initRNG() {
	drbg := &rng.DRBG{}

	state := ARM64.Counter()

	for i := 0; i < len(drbg.Seed); i += 4 {
		state = splitmix64(state ^ ARM64.Counter())
		rng.Fill(drbg.Seed[:], i, uint32(state))
	}

	rng.GetRandomDataFn = drbg.GetRandomData
}
