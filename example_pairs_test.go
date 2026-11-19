package main

import "testing"

func TestWiktionaryAddsOnlyTranslationsOfTheirOwnExamples(t *testing.T) {
	source := `<h2 id="English">English</h2><h3>Noun</h3>
	<div class="h-usage-example"><i class="e-example" lang="en">An unrelated English example.</i>
	<span class="e-translation" lang="fr">Un exemple français.</span></div>
	<div class="h-usage-example"><i class="e-example" lang="en">They waited at the library.</i>
	<span class="e-translation"><span lang="cmn-Hant">他們在圖書館等候。</span></span></div>
	<h4>Translations</h4><span class="Hans" lang="cmn">图书馆</span>`
	entry := parseWiktionary(source)
	if entry.Examples != "They waited at the library." || entry.ExampleTranslation != "他們在圖書館等候。" {
		t.Fatalf("source pair mismatch: %+v", entry)
	}

	existing := cardDetails{Examples: entry.Examples}
	if !existing.fill(entry) || existing.ExampleTranslation != "他们在图书馆等候。" {
		t.Fatalf("existing example did not gain the matching translation: %+v", existing)
	}
	other := cardDetails{Examples: "They visited the museum."}
	other.fill(entry)
	if other.ExampleTranslation != "" {
		t.Fatalf("translation was attached to a different sentence: %+v", other)
	}
	withMultiple := cardDetails{
		Examples:           "It was late.\n\n" + entry.Examples,
		ExampleTranslation: "時間不早了。\n\n" + entry.ExampleTranslation,
	}
	if got := matchingExampleTranslation(entry.Examples, withMultiple); got != entry.ExampleTranslation {
		t.Fatalf("wrong translation selected from multiple sentences: %q", got)
	}
}

func TestOfflinePairsBackfillOnlyUnmodifiedOldExamples(t *testing.T) {
	s, path := newTestStore(t)
	importTest(t, s, "2026-11-19", "perception,probability,disruptive,incentive,tuck")
	original, err := bundledExamples()
	if err != nil {
		t.Fatal(err)
	}
	paired, err := bundledExamplePairs()
	if err != nil {
		t.Fatal(err)
	}
	card := s.db.Words["perception"]
	pair := paired["perception"]
	if card.Examples != pair.Examples || card.ExampleTranslation == "" || card.ExampleBy != pair.By {
		t.Fatalf("offline import did not choose the bilingual pair: %+v", card)
	}
	card.Examples = original["perception"].Examples
	card.ExampleBy, card.ExampleTranslation, card.PersonalNote = "simple", "", "保留此备注"
	edited := s.db.Words["probability"]
	edited.ExampleBy, edited.ExampleTranslation = "simple", ""
	edited.Examples = "The probability is hard to estimate."
	machine := s.db.Words["disruptive"]
	if machine.ExampleBy != "simple-mt" || machine.Examples != original["disruptive"].Examples ||
		machine.ExampleTranslation == "" {
		t.Fatalf("new card did not get the translation of its original sentence: %+v", machine)
	}
	expectedTranslation := machine.ExampleTranslation
	machine.ExampleBy, machine.ExampleTranslation, machine.PersonalNote = "simple", "", "保留旧备注"
	manual := s.db.Words["incentive"]
	manual.ExampleBy, manual.ExampleTranslation = "simple", "我自己的譯文"
	cleared := s.db.Words["tuck"]
	cleared.ExampleBy, cleared.ExampleTranslation = "simple-edited", ""
	if err := s.save([]string{card.Key, edited.Key, machine.Key, manual.Key, cleared.Key}, len(s.db.Events)); err != nil {
		t.Fatal(err)
	}
	s.io.Close()

	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	card = reopened.db.Words["perception"]
	if card.Examples != pair.Examples || card.ExampleTranslation == "" || card.ExampleBy != pair.By || card.PersonalNote != "保留此备注" {
		t.Fatalf("backfill was not persisted correctly: %+v", card)
	}
	if edited = reopened.db.Words["probability"]; edited.Examples != "The probability is hard to estimate." || edited.ExampleTranslation != "" {
		t.Fatalf("hand-edited card was overwritten: %+v", edited)
	}
	machine = reopened.db.Words["disruptive"]
	if machine.ExampleBy != "simple-mt" || machine.Examples != original["disruptive"].Examples ||
		machine.ExampleTranslation != expectedTranslation || machine.PersonalNote != "保留旧备注" {
		t.Fatalf("translation-only backfill was not persisted correctly: %+v", machine)
	}
	if manual = reopened.db.Words["incentive"]; manual.ExampleTranslation != "我自己的譯文" {
		t.Fatalf("manual translation was overwritten: %+v", manual)
	}
	if cleared = reopened.db.Words["tuck"]; cleared.ExampleTranslation != "" {
		t.Fatalf("a translation the user cleared was restored: %+v", cleared)
	}
}

func TestBundledBilingualExampleCoverage(t *testing.T) {
	var dict dictionary
	dict.load()
	if dict.err != nil {
		t.Fatal(dict.err)
	}
	keys := make(map[string]bool)
	for key := range dict.examples {
		keys[key] = true
	}
	for key := range dict.notes {
		keys[key] = true
	}
	var total, bilingual, machine int
	for key := range keys {
		entry, err := dict.lookup(key)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Examples == "" {
			continue
		}
		total++
		if entry.ExampleTranslation != "" {
			bilingual++
		}
		if entry.ExampleBy == "simple-mt" {
			machine++
			original := dict.examples[key]
			if entry.Examples != original.Examples || original.ExampleTranslation != "" ||
				entry.ExampleTranslation == "" {
				t.Errorf("machine translation displaced an original pair or lost its sentence for %q", key)
			}
		}
	}
	t.Logf("%d/%d bilingual, including %d machine-assisted translations", bilingual, total, machine)
	if total < 17876 || bilingual*100 < total*90 || machine == 0 {
		t.Fatalf("below 90%% bilingual coverage: %d/%d (%d machine-assisted)", bilingual, total, machine)
	}
}
