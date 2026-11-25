package main

// Older cards used the same fields for manual and dictionary data. The built-in
// notes have never contained etymologies, and older online fills always recorded
// a Wiktionary revision. A revision can also come from another field, so it is
// evidence of ambiguity rather than proof that any particular text came from it.
func upgradeSavedAssociations(words map[string]*wordCard) ([]string, error) {
	bundled, err := bundledLearningNotes()
	if err != nil {
		return nil, err
	}
	referenceTips := make(map[string]bool, len(bundled))
	for _, entry := range bundled {
		referenceTips[entry.Note] = true
	}
	var changed []string
	for key, card := range words {
		updated := false
		if card.Etymology != "" && card.RawEtymology == "" {
			if card.Construction == "" && card.WikiSource == "" {
				card.Construction = card.Etymology
			} else {
				card.RawEtymology = card.Etymology
			}
			card.Etymology = ""
			updated = true
		}
		// Match the shipped text even for cards whose spelling was later edited.
		if card.Note != "" && card.MemoryTip == "" && !referenceTips[card.Note] {
			card.MemoryTip, card.Note = card.Note, ""
			updated = true
		}
		if updated {
			changed = append(changed, key)
		}
	}
	return changed, nil
}
