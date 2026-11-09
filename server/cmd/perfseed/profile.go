package main

import (
	"fmt"
	"sort"
)

// profile is the shape of a synthetic corpus.
type profile struct {
	Documents    int
	ChunksPerDoc int
	Sources      int // documents are spread over this many sources
	Restricted   int // how many of those sources only the reader may read
	HugeDocs     int // documents of HugeChunks passages each, on top of Documents
	HugeChunks   int
}

// Topics and words are what the corpus is made of: a document is about one topic, its text mixes that topic's
// words with common ones, so word search matches many passages (as real text does) and the topic decides the vector.
const (
	topics         = 200
	topicWords     = 40
	commonWords    = 300
	wordsPerChunk  = 52
	topicShare     = 12 // of wordsPerChunk, words drawn from the topic
	questionTopics = 3  // topic words in a generated question
	questionCommon = 2
	chunkPages     = 20 // a document's passages are spread over this many pages
)

var profiles = map[string]profile{
	"small":     {Documents: 1000, ChunksPerDoc: 20, Sources: 4},
	"pilot":     {Documents: 10000, ChunksPerDoc: 30, Sources: 8},
	"skew":      {Documents: 2000, ChunksPerDoc: 8, Sources: 4, HugeDocs: 20, HugeChunks: 1500},
	"multi-acl": {Documents: 5000, ChunksPerDoc: 30, Sources: 20, Restricted: 15},
}

func profileNamed(name string) (profile, error) {
	p, ok := profiles[name]
	if ok {
		return p, nil
	}
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return profile{}, fmt.Errorf("unknown profile %q; choose one of %v", name, names)
}

func (p profile) totalChunks() int { return p.Documents*p.ChunksPerDoc + p.HugeDocs*p.HugeChunks }
