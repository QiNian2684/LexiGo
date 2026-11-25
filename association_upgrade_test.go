package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSavedAssociationSources(t *testing.T) {
	s, path := newTestStore(t)
	day := localDay()
	importTest(t, s, day, "rewrite, unfold, crucial")
	explicit, ambiguous, bundled := s.db.Words["rewrite"], s.db.Words["unfold"], s.db.Words["crucial"]
	explicit.Etymology = "A user-authored breakdown written in English."
	explicit.Note = "自己写的技巧"
	ambiguous.Etymology = "Even short Chinese text can be an ambiguous reference. 词源"
	ambiguous.WikiSource = wikiSource(12345)
	notes, err := bundledLearningNotes()
	if err != nil {
		t.Fatal(err)
	}
	bundled.Note = notes["crucial"].Note
	bundled.PersonalNote = "个人笔记继续保留"
	mustSave(t, s, "rewrite")
	mustSave(t, s, "unfold")
	mustSave(t, s, "crucial")

	legacy, err := s.transferBytes(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	old, err := parseTransfer(legacy)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := newTestStore(t)
	if err := other.restore(old.Database); err != nil {
		t.Fatal(err)
	}
	if err := s.io.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()

	for _, current := range []*store{reopened, other} {
		manual := presentCard(*current.db.Words["rewrite"])
		if manual.Etymology != explicit.Etymology || manual.Note != explicit.Note || manual.MemoryHint != "" {
			t.Fatalf("legacy user details were lost: %+v", manual)
		}
		lookup := presentCard(*current.db.Words["unfold"])
		if lookup.RawEtymology != ambiguous.Etymology || lookup.WikiSource != ambiguous.WikiSource ||
			lookup.Etymology != "" || lookup.MemoryHint == "" {
			t.Fatalf("ambiguous text must remain a reference: %+v", lookup)
		}
		tip := presentCard(*current.db.Words["crucial"])
		if tip.Note != "" || tip.ReferenceTip != bundled.Note || tip.PersonalNote != bundled.PersonalNote {
			t.Fatalf("legacy bundled tip or personal note changed: %+v", tip)
		}
	}
	disk, err := readDatabase(reopened.io.DB)
	if err != nil || disk.Words["rewrite"].Construction == "" || disk.Words["rewrite"].Etymology != "" {
		t.Fatalf("upgrade was not persisted: %v", err)
	}
}

func TestAssociationCSVAndTransferRoundTrip(t *testing.T) {
	s, _ := newTestStore(t)
	day := localDay()
	words, err := parseImport(importInput{Text: "word,etymology,note,personalNote,rawEtymology\nrewrite,-re 拆词,自填的记法,个人备注,Source reference"})
	if err != nil {
		t.Fatal(err)
	}
	preview, _ := s.preview(words)
	if _, err := s.add(day, words, preview.Token, &testDictionary); err != nil {
		t.Fatal(err)
	}
	card := s.db.Words["rewrite"]
	card.fillFromDictionary(cardDetails{Etymology: "other raw text", Note: "dictionary tip"})
	if card.RawEtymology != "Source reference" || card.Construction != "-re 拆词" || card.MemoryTip != "自填的记法" {
		t.Fatalf("dictionary replaced user fields: %+v", card)
	}
	export, err := asCSV([]*wordCard{card, {Word: "unfold", RawEtymology: "'Source without user construction"}})
	if err != nil || !bytes.Contains(export, []byte("'-re")) {
		t.Fatalf("export missing spreadsheet protection: %s (%v)", export, err)
	}
	decoded, err := parseImport(importInput{Text: string(export), Format: "csv"})
	if err != nil || len(decoded) != 2 || decoded[0].Etymology != card.Construction || decoded[0].Note != card.MemoryTip ||
		decoded[0].RawEtymology != card.RawEtymology || decoded[0].PersonalNote != card.PersonalNote ||
		decoded[1].Etymology != "" || decoded[1].RawEtymology != "'Source without user construction" {
		t.Fatalf("CSV round-trip mixed the fields: %+v (%v)", decoded, err)
	}
	card.Construction = ""
	if hint := presentCard(*card); hint.MemoryHint == "" || hint.Note != card.MemoryTip {
		t.Fatalf("clearing construction should restore the algorithm and retain the memory tip: %+v", hint)
	}
	file, err := s.transferBytes(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var envelope transferEnvelope
	if err := json.Unmarshal(file, &envelope); err != nil || envelope.Version != 7 {
		t.Fatalf("association fields require the new transfer version: %d (%v)", envelope.Version, err)
	}
	data, err := parseTransfer(file)
	if err != nil || data.Database.Words["rewrite"].RawEtymology != card.RawEtymology ||
		data.Database.Words["rewrite"].MemoryTip != card.MemoryTip {
		t.Fatalf("transfer lost either field: %v", err)
	}
}

func TestLegacyLibraryCSVEtymologyStaysAReference(t *testing.T) {
	var buffer strings.Builder
	rows := csv.NewWriter(&buffer)
	headers := libraryCSVColumns[:len(libraryCSVColumns)-1]
	if err := rows.Write(headers); err != nil {
		t.Fatal(err)
	}
	line := make([]string, len(headers))
	line[0], line[8] = "rewrite", "旧词库导出的混合词源"
	if err := rows.Write(line); err != nil {
		t.Fatal(err)
	}
	rows.Flush()
	words, err := parseImport(importInput{Text: buffer.String()})
	if err != nil || len(words) != 1 || words[0].Etymology != "" || words[0].RawEtymology != line[8] {
		t.Fatalf("legacy CSV source was promoted: %+v (%v)", words, err)
	}
}
