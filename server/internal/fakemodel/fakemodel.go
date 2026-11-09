// Package fakemodel holds the deterministic text embedding used by the fake model servers and by the tools that
// make test data: texts that share words share dimensions, so the vector search finds them without a real model.
package fakemodel

import (
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// Dimension is the size of bge-m3 vectors, which the vector index is built for.
const Dimension = 1024

// Words splits text into lowercase words of letters and digits.
func Words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// Embed hashes each word to a dimension and a sign, then normalizes. Texts that share words share dimensions.
func Embed(text string) []float32 {
	v := make([]float32, Dimension)
	for _, w := range Words(text) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		sum := h.Sum32()
		sign := float32(1)
		if sum&1 == 1 {
			sign = -1
		}
		v[(sum>>1)%Dimension] += sign
	}
	return normalize(v)
}

func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		v[0] = 1
		return v
	}
	norm := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= norm
	}
	return v
}
