package main

import "testing"

func TestExampleTranslationUsesOfflinePhrasesAndCharacters(t *testing.T) {
	const traditional = "老師說：「請瞭解乾隆年間的歷史。」\n古代以樓臺瞭望；資料可用 Wi-Fi 傳送。"
	const simplified = "老师说：「请了解乾隆年间的历史。」\n古代以楼台瞭望；资料可用 Wi-Fi 传送。"
	if got := simplifyExampleTranslation(traditional); got != simplified {
		t.Fatalf("conversion = %q, want %q", got, simplified)
	}
	if got := simplifyExampleTranslation(simplified); got != simplified {
		t.Fatalf("simplified input changed: %q", got)
	}
	from := cardDetails{Examples: "Science-Fiction can be abbreviated as sci-fi.", ExampleTranslation: "科學幻想的簡稱是科幻。"}
	var automatic cardDetails
	automatic.fill(from)
	if automatic.Examples != from.Examples || automatic.ExampleTranslation != "科学幻想的简称是科幻。" {
		t.Fatalf("automatic example was not normalized: %+v", automatic)
	}
	manual := cardDetails{ExampleTranslation: "自訂譯文。"}
	manual.fill(from)
	if manual.ExampleTranslation != "自訂譯文。" {
		t.Fatalf("manual translation was changed: %+v", manual)
	}
}

func TestSavedAndRestoredSourcedExamplesConvertOnlyOriginalTranslations(t *testing.T) {
	s, path := newTestStore(t)
	importTest(t, s, "2026-11-19", "abbreviated,abhor")
	index, err := bundledExamples()
	if err != nil {
		t.Fatal(err)
	}
	original := index["abbreviated"]
	card := s.db.Words["abbreviated"]
	if card.ExampleBy != original.By || card.ExampleTranslation != "科学幻想的简称是科幻。" {
		t.Fatalf("new dictionary import did not convert automatic translation: %+v", card)
	}
	card.ExampleTranslation = original.ExampleTranslation
	card.PersonalNote = "繁體備註"
	s.db.Words["abhor"].ExampleTranslation = "我自己編輯的繁體譯文。"
	if err := s.save([]string{"abbreviated", "abhor"}, len(s.db.Events)); err != nil {
		t.Fatal(err)
	}
	older := s.db
	s.io.Close()

	verify := func(db database) {
		t.Helper()
		if card := db.Words["abbreviated"]; card.ExampleTranslation != "科学幻想的简称是科幻。" ||
			card.ExampleBy != original.By || card.PersonalNote != "繁體備註" || card.Examples != original.Examples {
			t.Fatalf("original example or its attribution was not preserved: %+v", card)
		}
		if got := db.Words["abhor"].ExampleTranslation; got != "我自己編輯的繁體譯文。" {
			t.Fatalf("hand-edited translation was changed: %q", got)
		}
	}
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	verify(reopened.db)
	stored, err := readDatabase(reopened.io.DB)
	if err != nil {
		t.Fatal(err)
	}
	verify(stored)
	restored, _ := newTestStore(t)
	if err := restored.restore(older); err != nil {
		t.Fatal(err)
	}
	verify(restored.db)
}
