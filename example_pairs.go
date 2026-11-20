package main

import "strings"

func bundledMachineTranslation(source string) bool {
	return source == "simple-mt" || strings.HasPrefix(source, "tatoeba-mt|") ||
		strings.HasPrefix(source, "wiktionary-data|") || strings.HasPrefix(source, "simple-wikipedia|")
}

func updateSavedExamples(words map[string]*wordCard) ([]string, error) {
	keys, err := simplifySavedExamples(words)
	if err != nil {
		return nil, err
	}
	var sources, pairs map[string]sourcedExample
	var expanded map[string]sourcedExample
	for key, card := range words {
		if card.ExampleTranslation != "" || card.ExampleTranslationBy == "manual" ||
			(card.Examples != "" && card.ExampleBy != "simple") {
			continue
		}
		if pairs == nil {
			pairs, err = bundledExamplePairs()
			if err != nil {
				return nil, err
			}
			sources, err = bundledExamples()
			if err != nil {
				return nil, err
			}
			expanded, err = expandedExamples()
			if err != nil {
				return nil, err
			}
		}
		// Source markers on older cards could survive editing. Backfill
		// only when the saved text still matches the original English.
		if card.Examples != "" && (sources[key].By != "simple" || sources[key].Examples != card.Examples) {
			continue
		}
		pair, ok := pairs[key]
		if !ok {
			pair, ok = expanded[key]
		}
		if !ok {
			continue
		}
		if card.Examples != "" && pair.ExampleTranslation == "" {
			continue
		}
		card.Examples = pair.Examples
		card.ExampleTranslation = simplifyExampleTranslation(pair.ExampleTranslation)
		card.ExampleBy = pair.By
		keys = append(keys, key)
	}
	return keys, nil
}
