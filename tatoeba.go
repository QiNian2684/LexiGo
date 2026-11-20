package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const tatoebaAPI = "https://api.tatoeba.org/v1/sentences"

var (
	errMissingSentence = errors.New("public sentence is no longer available")
	sentenceTokens     = regexp.MustCompile(`[a-z]+(?:['-][a-z]+)*`)
	tatoebaClient      = &http.Client{Timeout: 7 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
)

type tatoebaSentence struct {
	ID           int64             `json:"id"`
	Text         string            `json:"text"`
	Lang         string            `json:"lang"`
	Owner        string            `json:"owner"`
	License      string            `json:"license"`
	Unapproved   bool              `json:"is_unapproved"`
	Direct       bool              `json:"is_direct"`
	Translations []tatoebaSentence `json:"translations"`
}

func containsTerm(example, term string) bool {
	example = strings.ToLower(strings.NewReplacer("’", "'", "‘", "'", "‑", "-", "‐", "-").Replace(example))
	indices := sentenceTokens.FindAllStringIndex(example, -1)
	tokens := strings.Fields(term)
	for i := range indices {
		if i+len(tokens) > len(indices) {
			break
		}
		matches := len(tokens) > 0
		for j, token := range tokens {
			index := indices[i+j]
			if example[index[0]:index[1]] != token ||
				(j > 0 && strings.Trim(example[indices[i+j-1][1]:index[0]], " ,\t") != "") {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func hasUsageExample(sentence, key string) bool {
	lower := strings.ToLower(sentence)
	return !strings.Contains(lower, "citations:") &&
		!strings.Contains(lower, "quotations using this term") && containsTerm(sentence, key)
}

func tatoebaGet(ctx context.Context, endpoint string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LexiGo (+https://github.com/QiNian2684/LexiGo)")
	res, err := tatoebaClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(into)
	case http.StatusNotFound:
		return errMissingSentence
	default:
		return fmt.Errorf("Tatoeba returned HTTP %d", res.StatusCode)
	}
}

func (s tatoebaSentence) usable(language string) bool {
	return s.ID > 0 && s.Lang == language && s.Owner != "" && !strings.Contains(s.Owner, "|") &&
		s.License == "CC BY 2.0 FR" && !s.Unapproved && len(s.Text) < 10000 &&
		!strings.ContainsAny(s.Text, "<>\r\n\uFFFD")
}

func (s tatoebaSentence) credit() string {
	return strconv.FormatInt(s.ID, 10) + ":" + s.Owner
}

func pickTatoebaExample(rows []tatoebaSentence, key string) sourcedExample {
	var best sourcedExample
	bestScore := 1000
	for _, row := range rows {
		if !row.usable("eng") {
			continue
		}
		if len(row.Text) >= 240 || len(strings.Fields(row.Text)) > 23 {
			for _, part := range proseSentences(row.Text) {
				if len(part) >= 240 || len(strings.Fields(part)) > 23 {
					continue
				}
				fragment := row
				fragment.Text, fragment.Translations = part, nil
				suggestion := pickTatoebaExample([]tatoebaSentence{fragment}, key)
				if suggestion.Examples != "" && bestScore > 20 {
					best, bestScore = suggestion, 20
					break
				}
			}
			continue
		}
		size := len(strings.Fields(row.Text))
		lower := strings.ToLower(row.Text)
		if size < 3 ||
			strings.Contains(lower, "the word ") || strings.Contains(lower, "the phrase ") ||
			!strings.ContainsAny(row.Text[len(row.Text)-1:], ".!?") || !hasUsageExample(row.Text, key) {
			continue
		}
		score := max(size-9, 9-size)
		candidate := sourcedExample{Examples: row.Text, By: "tatoeba-online|" + row.credit()}
		for _, translated := range row.Translations {
			if translated.usable("cmn") && translated.Direct && len(translated.Text) < 500 &&
				strings.ContainsFunc(translated.Text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
				candidate.ExampleTranslation = translated.Text
				candidate.By = "tatoeba|" + row.credit() + "|" + translated.credit()
				score -= 50
				break
			}
		}
		if score < bestScore {
			best, bestScore = candidate, score
		}
	}
	return best
}

func fetchTatoebaExample(ctx context.Context, key string) (sourcedExample, error) {
	terms := strings.Fields(key)
	query := "=" + strings.Join(terms, " =")
	if len(terms) > 1 {
		query = `"` + query + `"`
	}
	params := url.Values{
		"lang": {"eng"}, "sort": {"relevance"}, "q": {query}, "limit": {"16"},
		"showtrans:lang": {"cmn"}, "showtrans:is_direct": {"yes"},
	}
	var matches struct {
		Data []tatoebaSentence `json:"data"`
	}
	if err := tatoebaGet(ctx, tatoebaAPI+"?"+params.Encode(), &matches); err != nil {
		return sourcedExample{}, err
	}
	return pickTatoebaExample(matches.Data, key), nil
}

func onlineTatoebaID(source string) int64 {
	tail, ok := strings.CutPrefix(source, "tatoeba-online|")
	if !ok {
		return 0
	}
	id, _, _ := strings.Cut(tail, ":")
	n, _ := strconv.ParseInt(id, 10, 64)
	return n
}

func publicTatoebaExample(ctx context.Context, id int64) (tatoebaSentence, error) {
	var data struct {
		Data tatoebaSentence `json:"data"`
	}
	err := tatoebaGet(ctx, tatoebaAPI+"/"+strconv.FormatInt(id, 10), &data)
	if err == nil && (data.Data.ID != id || !data.Data.usable("eng")) {
		err = errMissingSentence
	}
	return data.Data, err
}
