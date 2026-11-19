package main

func updateSavedExamples(words map[string]*wordCard) ([]string, error) {
	keys, err := simplifySavedExamples(words)
	if err != nil {
		return nil, err
	}
	var sources, pairs map[string]sourcedExample
	for key, card := range words {
		if card.ExampleBy != "simple" || card.Examples == "" || card.ExampleTranslation != "" {
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
		}
		pair, ok := pairs[key]
		original := sources[key]
		// Source markers on older cards could survive editing. Backfill
		// only when the saved text still matches the original English.
		if !ok || original.By != "simple" || original.Examples != card.Examples {
			continue
		}
		card.Examples = pair.Examples
		card.ExampleTranslation = simplifyExampleTranslation(pair.ExampleTranslation)
		card.ExampleBy = pair.By
		keys = append(keys, key)
	}
	return keys, nil
}
