package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// vocabulary builds words that no real language shares, so a question about one topic can only be answered by
// the documents of that topic.
type vocabulary struct {
	rng *rand.Rand
}

func topicWord(topic, i int) string { return fmt.Sprintf("tema%03dp%02d", topic, i) }

func commonWord(i int) string { return fmt.Sprintf("comun%03d", i) }

// pickCommon draws common words with a skew: the first ones are in almost every passage, like "de" or "the".
func (v vocabulary) pickCommon() string {
	r := v.rng.Float64()
	return commonWord(int(r * r * r * commonWords))
}

// passage writes the text of one passage about a topic.
func (v vocabulary) passage(topic int) string {
	words := make([]string, 0, wordsPerChunk)
	for range topicShare {
		words = append(words, topicWord(topic, v.rng.IntN(topicWords)))
	}
	for len(words) < wordsPerChunk {
		words = append(words, v.pickCommon())
	}
	v.rng.Shuffle(len(words), func(i, j int) { words[i], words[j] = words[j], words[i] })
	return strings.Join(words, " ")
}

// question is what a person would ask about a topic: some of its words and a couple of common ones.
func (v vocabulary) question(topic int) string {
	words := make([]string, 0, questionTopics+questionCommon)
	for range questionTopics {
		words = append(words, topicWord(topic, v.rng.IntN(topicWords)))
	}
	for range questionCommon {
		words = append(words, v.pickCommon())
	}
	return strings.Join(words, " ")
}
