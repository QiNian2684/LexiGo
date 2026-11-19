package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var entryPattern = regexp.MustCompile(`^[A-Za-z]+(?:[ '-][A-Za-z]+)*$`)

func normalize(input string) (name, key string, valid bool) {
	name = strings.Join(strings.Fields(strings.NewReplacer("’", "'", "‐", "-", "‑", "-").Replace(input)), " ")
	return name, strings.ToLower(name), len(name) <= 96 && entryPattern.MatchString(name)
}

func splitWords(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n' || r == '\r'
	})
}

type importedWord struct {
	Word string `json:"word"`
	cardDetails
	PersonalNote string `json:"personalNote"`
}

type importInput struct {
	Text   string `json:"text"`
	Format string `json:"format"`
	Token  string `json:"token"`
}

func parseImport(input importInput) ([]importedWord, error) {
	text := strings.TrimPrefix(input.Text, "\ufeff")
	var words []importedWord
	switch input.Format {
	case "json":
		if err := json.Unmarshal([]byte(text), &words); err != nil || words == nil {
			return nil, errors.New("词表 JSON 应为词条数组，如 [{\"word\":\"crucial\",\"meaning\":\"关键的\"}]。完整备份请在设置中恢复。")
		}
	case "csv":
		reader := csv.NewReader(strings.NewReader(text))
		reader.FieldsPerRecord = -1
		first, err := reader.Read()
		if err != nil {
			return nil, errors.New("CSV 为空或格式无效。")
		}
		if len(first) == 0 {
			return nil, errors.New("CSV 中没有词条。")
		}
		if !strings.EqualFold(strings.TrimSpace(first[0]), "word") && strings.TrimSpace(first[0]) != "单词" {
			input.Format = "words"
			return parseImport(input)
		}
		headers := make([]string, len(first))
		for i, header := range first {
			headers[i] = normalizeHeader(header)
		}
		for {
			row, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, errors.New("CSV 有无法读取的行，请检查逗号和双引号。")
			}
			var item importedWord
			for i, value := range row {
				if i >= len(headers) {
					break
				}
				switch headers[i] {
				case "word":
					item.Word = value
				case "meaning":
					item.Meaning = value
				case "pos":
					item.POS = value
				case "ipa":
					item.IPA = value
				case "ipaUK":
					item.IPAUK = value
				case "ipaUS":
					item.IPAUS = value
				case "examples":
					item.Examples = value
				case "exampleTranslation":
					item.ExampleTranslation = value
				case "etymology":
					item.Etymology = value
				case "collocations":
					item.Collocations = value
				case "note":
					item.Note = value
				case "personalNote":
					item.PersonalNote = value
				}
			}
			words = append(words, item)
		}
	case "", "words":
		for _, word := range splitWords(text) {
			words = append(words, importedWord{Word: word})
		}
	default:
		return nil, errors.New("请使用 TXT、CSV 或 JSON 词表。")
	}
	if len(words) > 1000 {
		return nil, errors.New("每批最多 1,000 项，请分批导入。")
	}
	return words, nil
}

func normalizeHeader(header string) string {
	header = strings.TrimSpace(header)
	aliases := map[string]string{
		"单词": "word", "中文释义": "meaning", "词性": "pos", "音标": "ipa", "英式音标": "ipaUK",
		"美式音标": "ipaUS", "例句": "examples", "例句翻译": "exampleTranslation",
		"词根词缀": "etymology", "词源": "etymology", "搭配": "collocations", "记忆提示": "note",
		"备注": "personalNote", "个人备注": "personalNote",
	}
	if result := aliases[header]; result != "" {
		return result
	}
	if strings.EqualFold(header, "word") {
		return "word"
	}
	return header
}

type importPreview struct {
	Total      int      `json:"total"`
	New        int      `json:"new"`
	Reset      int      `json:"reset"`
	Duplicates int      `json:"duplicates"`
	Invalid    []string `json:"invalid"`
	ResetWords []string `json:"resetWords"`
	Token      string   `json:"token"`
}

func (s *store) preview(items []importedWord) (importPreview, []importedWord) {
	plan := importPreview{Total: len(items), Invalid: []string{}, ResetWords: []string{}}
	seen := make(map[string]bool)
	var good []importedWord
	digest := sha256.New()
	for _, item := range items {
		word, key, valid := normalize(item.Word)
		if !valid || utf8.RuneCountInString(item.PersonalNote) > 12000 || item.cardDetails.size() > 12000 {
			runes := []rune(word)
			plan.Invalid = append(plan.Invalid, string(runes[:min(96, len(runes))]))
			continue
		}
		if seen[key] {
			plan.Duplicates++
			continue
		}
		seen[key] = true
		item.Word = word
		good = append(good, item)
		if card := s.db.Words[key]; card != nil {
			plan.Reset++
			if len(plan.ResetWords) < 12 {
				plan.ResetWords = append(plan.ResetWords, card.Word)
			}
			fmt.Fprintf(digest, "%s\t%d\t%d\n", key, card.ID, card.Order)
		} else {
			plan.New++
			fmt.Fprintln(digest, key)
		}
	}
	if data, err := json.Marshal(good); err == nil {
		digest.Write(data)
	}
	plan.Token = hex.EncodeToString(digest.Sum(nil))
	return plan, good
}

var errImportChanged = errors.New("词库或输入已变化，请重新预览并确认。")

type importResult struct {
	importPreview
	Keys []string `json:"keys"`
}

func (s *store) add(day string, items []importedWord, token string, dict *dictionary) (importResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, entries := s.preview(items)
	if plan.Token != token {
		return importResult{}, errImportChanged
	}
	result := importResult{importPreview: plan, Keys: []string{}}
	updates := make([]*wordCard, 0, len(entries))
	old := make(map[string]*wordCard)
	for _, item := range entries {
		_, key, _ := normalize(item.Word)
		if current := s.db.Words[key]; current != nil {
			copy := *current
			old[key] = current
			updates = append(updates, &copy)
			continue
		}
		offline, err := dict.lookup(key)
		if err != nil {
			return importResult{}, err
		}
		details := item.cardDetails
		exampleBy := ""
		details.fill(offline.cardDetails)
		if details.Examples != item.Examples || details.ExampleTranslation != item.ExampleTranslation {
			exampleBy = offline.ExampleBy
		}
		card := &wordCard{Key: key, Word: item.Word, cardDetails: details,
			ECDICT: offline.ECDICT, ExampleBy: exampleBy, PersonalNote: item.PersonalNote,
			AddedDay: day, Lookup: "done"}
		if card.needsOnline() {
			card.Lookup = "pending"
		}
		updates = append(updates, card)
	}
	count, previousOrder := len(s.db.Events), s.db.NextOrder
	for _, card := range updates {
		if card.ID != 0 {
			s.reset(card, day, true)
			card.Reimports++
			s.record(day, "reimport", card)
		} else {
			s.advance(card)
			card.ID = card.Order
			s.record(day, "add", card)
		}
		s.db.Words[card.Key] = card
		result.Keys = append(result.Keys, card.Key)
	}
	if len(result.Keys) > 0 {
		if err := s.save(result.Keys, count); err != nil {
			for _, key := range result.Keys {
				if card := old[key]; card == nil {
					delete(s.db.Words, key)
				} else {
					s.db.Words[key] = card
				}
			}
			s.db.Events, s.db.NextOrder = s.db.Events[:count], previousOrder
			return importResult{}, err
		}
	}
	return result, nil
}

func asCSV(cards []*wordCard) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString("\ufeff")
	writer := csv.NewWriter(&buffer)
	writer.UseCRLF = true
	if err := writer.Write([]string{"word", "meaning", "pos", "ipa", "ipaUK", "ipaUS", "examples",
		"exampleTranslation", "etymology", "collocations", "note", "personalNote", "status", "streak", "lastReview"}); err != nil {
		return nil, err
	}
	for _, c := range cards {
		status := "待复习"
		if c.Mastered {
			status = "已掌握"
		}
		if c.Deleted {
			status = "回收站"
		}
		values := []string{c.Word, c.Meaning, c.POS, c.IPA, c.IPAUK, c.IPAUS, c.Examples,
			c.ExampleTranslation, c.Etymology, c.Collocations, c.Note, c.PersonalNote, status, strconv.Itoa(c.Streak), c.LastTry}
		for i, value := range values {
			if trimmed := strings.TrimLeft(value, " \r\n\t"); trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
				values[i] = "'" + value
			}
		}
		if err := writer.Write(values); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return buffer.Bytes(), writer.Error()
}
