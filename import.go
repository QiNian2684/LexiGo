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
	"slices"
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
	RawEtymology string `json:"rawEtymology,omitempty"`
	PersonalNote string `json:"personalNote"`
}

type importInput struct {
	Text   string `json:"text"`
	Format string `json:"format"`
	Token  string `json:"token"`
}

func parseImport(input importInput) ([]importedWord, error) {
	if len(input.Text) > 4<<20 {
		return nil, errors.New("词表超过 4 MiB，请拆成多批。")
	}
	text := strings.TrimPrefix(input.Text, "\ufeff")
	if (input.Format == "" || input.Format == "words") && strings.HasPrefix(strings.TrimSpace(text), "[") {
		input.Format = "json"
	}
	var words []importedWord
	switch input.Format {
	case "json":
		if err := json.Unmarshal([]byte(text), &words); err != nil || words == nil {
			return nil, errors.New("词表 JSON 应为词条数组，如 [{\"word\":\"crucial\",\"meaning\":\"关键的\"}]。完整备份请在设置中恢复。")
		}
	case "", "words", "csv":
		parsed, err := parseWordCSV(text, input.Format == "csv")
		if err != nil {
			return nil, err
		}
		if parsed != nil {
			words = parsed
		} else {
			for _, word := range splitWords(text) {
				words = append(words, importedWord{Word: word})
			}
		}
	default:
		return nil, errors.New("请使用 TXT、CSV 或 JSON 词表。")
	}
	if len(words) > 1000 {
		return nil, errors.New("每批最多 1,000 项，请分批导入。")
	}
	return words, nil
}

func parseWordCSV(text string, declared bool) ([]importedWord, error) {
	text = strings.TrimSpace(text)
	line, _, multiline := strings.Cut(text, "\n")
	if !multiline && !declared && !strings.ContainsAny(line, ",，;；\t") {
		return nil, nil
	}
	// A recognizable spreadsheet header with the wrong separator is not a word list.
	for _, separator := range []string{"，", ";", "；", "\t"} {
		first, rest, found := strings.Cut(line, separator)
		if found && normalizeHeader(first) == "word" &&
			(declared || normalizeHeader(strings.SplitN(rest, separator, 2)[0]) != "") {
			return nil, errors.New("CSV 表头请用英文半角逗号分隔，如 word,meaning。")
		}
	}

	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	first, err := reader.Read()
	if err == io.EOF {
		if declared {
			return nil, errors.New("CSV 中没有词条。")
		}
		return nil, nil
	}
	if err != nil {
		if declared || normalizeHeader(strings.Trim(strings.SplitN(line, ",", 2)[0], "\" ")) == "word" {
			return nil, errors.New("无法读取 CSV 表头，请检查英文逗号和双引号。")
		}
		return nil, nil
	}
	headers := make([]string, len(first))
	for i, name := range first {
		headers[i] = normalizeHeader(name)
	}
	hasDetails, misplacedWord := false, false
	for _, column := range headers[1:] {
		hasDetails = hasDetails || column != ""
		misplacedWord = misplacedWord || column == "word"
	}
	if headers[0] != "word" {
		// A CSV can also be a headerless word list. Leave unambiguous lists on
		// the original path, without interpreting table cells as word entries.
		wrongFirst := strings.ToLower(strings.TrimSpace(first[0]))
		switch wrongFirst {
		case "term", "headword", "vocabulary", "english", "words":
			wrongFirst = "word"
		}
		if len(first) > 1 && (declared && (hasDetails || !plainWordList(text)) ||
			multiline && hasDetails && (misplacedWord || headers[0] != "" || wrongFirst == "word" || !plainWordList(text))) {
			return nil, errors.New("CSV 第一列的表头应为 word 或「单词」，其余列填写释义、词性等资料。")
		}
		return nil, nil
	}
	if !declared && !multiline && !hasDetails {
		return nil, nil
	}
	seen := make(map[string]bool)
	for _, column := range headers {
		if column == "" {
			continue
		}
		if seen[column] {
			return nil, fmt.Errorf("CSV 表头中 %s 重复，每个字段只能有一列。", column)
		}
		seen[column] = true
	}

	exported := slices.Equal(first, libraryCSVColumns)
	legacyExport := slices.Equal(first, libraryCSVColumns[:len(libraryCSVColumns)-1])
	var words []importedWord
	for {
		row, err := reader.Read()
		if err == io.EOF {
			if len(words) == 0 {
				return nil, errors.New("CSV 只有表头，请从第二行填写词条。")
			}
			return words, nil
		}
		if err != nil {
			var parseErr *csv.ParseError
			if errors.As(err, &parseErr) {
				return nil, fmt.Errorf("CSV 第 %d 行无法读取，请检查逗号和双引号。", parseErr.Line)
			}
			return nil, err
		}
		if len(row) > len(headers) {
			line, _ := reader.FieldPos(0)
			return nil, fmt.Errorf("CSV 第 %d 行的列数多于表头，字段内的英文逗号请用双引号包裹。", line)
		}
		if len(words) == 1000 {
			return nil, errors.New("每批最多 1,000 项，请分批导入。")
		}
		var item importedWord
		for i, value := range row {
			if exported {
				// Undo the spreadsheet protection applied by this export format.
				value = strings.TrimPrefix(value, "'")
			}
			if strings.TrimSpace(value) == "" {
				continue
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
				if legacyExport {
					item.RawEtymology = value
				} else {
					item.Etymology = value
				}
			case "rawEtymology":
				item.RawEtymology = value
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
}

func plainWordList(text string) bool {
	for _, word := range splitWords(text) {
		if _, _, valid := normalize(word); !valid {
			return false
		}
	}
	return true
}

var importColumns = map[string]string{
	"word": "word", "单词": "word", "meaning": "meaning", "中文释义": "meaning", "释义": "meaning",
	"pos": "pos", "词性": "pos", "ipa": "ipa", "音标": "ipa",
	"ipauk": "ipaUK", "英式音标": "ipaUK", "ipaus": "ipaUS", "美式音标": "ipaUS",
	"examples": "examples", "例句": "examples", "英文例句": "examples",
	"exampletranslation": "exampleTranslation", "例句翻译": "exampleTranslation", "例句中文翻译": "exampleTranslation",
	"etymology": "etymology", "词根词缀": "etymology", "词根和词缀": "etymology", "词源": "etymology",
	"rawetymology": "rawEtymology", "原始词源": "rawEtymology", "原始词源资料": "rawEtymology",
	"collocations": "collocations", "搭配": "collocations", "常见搭配": "collocations",
	"note": "note", "记忆提示": "note", "记忆技巧": "note", "记忆联想": "note",
	"personalnote": "personalNote", "备注": "personalNote", "个人备注": "personalNote", "我的备注": "personalNote",
}

func normalizeHeader(header string) string {
	return importColumns[strings.ToLower(strings.TrimSpace(header))]
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
		if !valid || utf8.RuneCountInString(item.PersonalNote) > 12000 ||
			item.cardDetails.size()+utf8.RuneCountInString(item.RawEtymology) > 12000 {
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
		card := &wordCard{Key: key, Word: item.Word, cardDetails: item.cardDetails,
			Construction: strings.TrimSpace(item.Etymology), MemoryTip: strings.TrimSpace(item.Note),
			RawEtymology: strings.TrimSpace(item.RawEtymology), ECDICT: offline.ECDICT, PersonalNote: item.PersonalNote,
			AddedDay: day, Lookup: "done"}
		card.Etymology, card.Note = "", ""
		card.fillFromDictionary(offline.cardDetails)
		if card.Examples != item.Examples || card.ExampleTranslation != item.ExampleTranslation {
			card.ExampleBy = offline.ExampleBy
		}
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

var libraryCSVColumns = []string{"word", "meaning", "pos", "ipa", "ipaUK", "ipaUS", "examples",
	"exampleTranslation", "etymology", "collocations", "note", "personalNote", "status", "streak", "lastReview", "rawEtymology"}

func asCSV(cards []*wordCard) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString("\ufeff")
	writer := csv.NewWriter(&buffer)
	writer.UseCRLF = true
	if err := writer.Write(libraryCSVColumns); err != nil {
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
			c.ExampleTranslation, c.Construction, c.Collocations, c.MemoryTip, c.PersonalNote, status, strconv.Itoa(c.Streak), c.LastTry,
			c.RawEtymology}
		for i, value := range values {
			if trimmed := strings.TrimLeft(value, " \r\n\t"); strings.HasPrefix(value, "'") ||
				trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
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
