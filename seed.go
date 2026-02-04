package main

import "math/rand"

type SeedProvider struct {
	rand *rand.Rand
}

func NewSeedProvider(seed int64) *SeedProvider {
	return &SeedProvider{
		rand: rand.New(rand.NewSource(seed)),
	}
}

func (sp *SeedProvider) Next() int64 {
	return sp.rand.Int63()
}
