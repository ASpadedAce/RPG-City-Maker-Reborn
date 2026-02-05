package main

import "math/rand"

// SeedProvider is a simple struct that provides a stream of random seeds
// from a single initial seed. This ensures that the entire map generation
// process is deterministic if the same initial seed is used.
type SeedProvider struct {
	rand *rand.Rand
}

// NewSeedProvider creates a new SeedProvider with the given initial seed.
func NewSeedProvider(seed int64) *SeedProvider {
	return &SeedProvider{
		rand: rand.New(rand.NewSource(seed)),
	}
}

// Next returns the next random seed in the sequence.
func (sp *SeedProvider) Next() int64 {
	return sp.rand.Int63()
}
