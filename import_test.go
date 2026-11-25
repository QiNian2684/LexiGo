package main

import (
	"encoding/csv"
	"strings"
	"testing"
)

func TestPastedAndDeclaredCSV(t *testing.T) {
	text := "\ufeff单词,释义,词性,英文例句,例句中文翻译,词根和词缀,记忆联想,常见搭配,我的备注\r\n" +
		"rewrite,重写,v.,\"After feedback, she said \"\"rewrite it\"\".\",收到反馈后她说要重写。,re-（再次）+ write（写）,想象重写文章,rewrite the report,\"笔记第一行\r\n第二行\""
	for _, format := range []string{"", "words", "csv"} {
		items, err := parseImport(importInput{Text: text, Format: format})
		if err != nil || len(items) != 1 {
			t.Fatalf("format %q: %+v, %v", format, items, err)
		}
		got := items[0]
		if got.Word != "rewrite" || got.Meaning != "重写" || got.POS != "v." ||
			got.Examples != "After feedback, she said \"rewrite it\"." || got.ExampleTranslation != "收到反馈后她说要重写。" ||
			got.Etymology != "re-（再次）+ write（写）" || got.Note != "想象重写文章" ||
			got.Collocations != "rewrite the report" || got.PersonalNote != "笔记第一行\n第二行" {
			t.Fatalf("format %q: unexpected fields %+v", format, got)
		}
	}
}

func TestImportRejectsBrokenCSVWithoutTurningCellsIntoWords(t *testing.T) {
	for _, test := range []struct{ text, message string }{
		{"term,meaning\napple,fruit", "第一列"},
		{"unknown,word\nfruit,apple", "第一列"},
		{"word,meaning,释义\napple,水果,苹果", "重复"},
		{"word；meaning\napple；苹果", "半角逗号"},
		{"word,meaning\napple,苹果,fruit", "第 2 行"},
		{"word,examples\napple,\"Apple", "双引号"},
		{"word,meaning", "只有表头"},
		{"word\n" + strings.Repeat("apple\n", 1001), "1,000"},
		{strings.Repeat("a", 4<<20+1), "4 MiB"},
	} {
		for _, format := range []string{"", "csv"} {
			items, err := parseImport(importInput{Text: test.text, Format: format})
			if items != nil || err == nil || !strings.Contains(err.Error(), test.message) {
				t.Errorf("format %q: expected %q, got %d items, %v", format, test.message, len(items), err)
			}
		}
	}
}

func TestImportStillAcceptsSimpleWordLists(t *testing.T) {
	for _, test := range []struct {
		text, format string
		count        int
	}{
		{"word", "", 1},
		{"word, apple", "", 2},
		{"crucial, note\nsophisticated, abundance", "", 4},
		{"crucial, abundance\nsophisticated", "csv", 3},
		{"\ufeffcrucial，abundance\nsophisticated", "words", 3},
		{`[{"word":"crucial","note":"记住关键的"}]`, "", 1},
	} {
		items, err := parseImport(importInput{Text: test.text, Format: test.format})
		if err != nil || len(items) != test.count {
			t.Errorf("%q, format %q: got %+v, %v", test.text, test.format, items, err)
		}
	}
}

func TestBundledCSVTemplates(t *testing.T) {
	for _, template := range []struct {
		data         []byte
		fields, rows int
	}{
		{commonCSVTemplate, 7, 3},
		{fullCSVTemplate, 13, 1},
	} {
		text := string(template.data)
		if !strings.HasPrefix(text, "\ufeff") {
			t.Error("template is missing the UTF-8 BOM")
		}
		records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, "\ufeff"))).ReadAll()
		if err != nil || len(records) != template.rows+1 || len(records[0]) != template.fields {
			t.Fatalf("invalid template: %+v, %v", records, err)
		}
		words, err := parseImport(importInput{Text: text})
		if err != nil || len(words) != template.rows || words[0].Etymology == "" || words[0].Note == "" {
			t.Fatalf("template did not import: %+v, %v", words, err)
		}
		if template.fields == 13 && (words[0].IPA == "" || words[0].IPAUK == "" || words[0].IPAUS == "" ||
			words[0].PersonalNote == "" || words[0].Collocations == "") {
			t.Fatalf("full template is missing fields: %+v", words[0])
		}
	}
}
