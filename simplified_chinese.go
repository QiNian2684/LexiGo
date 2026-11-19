package main

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed assets/opencc-t2s.tsv
var traditionalToSimplified string

var exampleSimplifier = sync.OnceValue(func() *strings.Replacer {
	replacements := make([]string, 0, 2*strings.Count(traditionalToSimplified, "\n"))
	// The OpenCC-derived table lists longer phrases before their component
	// characters. This also preserves phrases such as 乾隆 and 瞭望.
	for line := range strings.Lines(traditionalToSimplified) {
		if word, simplified, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "\t"); ok {
			replacements = append(replacements, word, simplified)
		}
	}
	return strings.NewReplacer(replacements...)
})

func simplifyExampleTranslation(text string) string {
	if text == "" {
		return text
	}
	return exampleSimplifier().Replace(text)
}

// Older cards retained their source tag after editing. Match the actual
// example and translation to the bundled pair before changing saved text.
func simplifySavedExamples(words map[string]*wordCard) ([]string, error) {
	var candidates map[sourcedExample][]*wordCard
	for _, card := range words {
		if !strings.HasPrefix(card.ExampleBy, "tatoeba|") ||
			simplifyExampleTranslation(card.ExampleTranslation) == card.ExampleTranslation {
			continue
		}
		if candidates == nil {
			candidates = make(map[sourcedExample][]*wordCard)
		}
		example := sourcedExample{Examples: card.Examples, ExampleTranslation: card.ExampleTranslation, By: card.ExampleBy}
		candidates[example] = append(candidates[example], card)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sources, err := bundledExamples()
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, source := range sources {
		for _, card := range candidates[source] {
			card.ExampleTranslation = simplifyExampleTranslation(source.ExampleTranslation)
			keys = append(keys, card.Key)
		}
		delete(candidates, source)
	}
	return keys, nil
}
