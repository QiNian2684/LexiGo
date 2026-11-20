package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// proseSentences only accepts complete sentences. An extract's truncated
// ending, headings and standalone fragments aren't returned.
func proseSentences(prose string) []string {
	var results []string
	for _, paragraph := range strings.Split(prose, "\n") {
		start := 0
		for j, r := range paragraph {
			if r != '.' && r != '!' && r != '?' {
				continue
			}
			end := j + 1
			remaining := paragraph[end:]
			following := strings.TrimSpace(remaining)
			first, _ := utf8.DecodeRuneInString(following)
			if remaining != "" && (following == remaining || (following != "" && !unicode.IsUpper(first))) {
				continue
			}
			text := strings.Join(strings.Fields(paragraph[start:end]), " ")
			start = end
			size := len(strings.Fields(text))
			initial, _ := utf8.DecodeRuneInString(text)
			if len(text) <= 240 && size >= 4 && size <= 26 &&
				unicode.IsUpper(initial) && !strings.ContainsAny(text, "<>[]…\uFFFD") && !strings.Contains(text, "http") {
				results = append(results, text)
			}
		}
	}
	return results
}

type wikipediaExtract struct {
	ID    int64  `json:"pageid"`
	Text  string `json:"extract"`
	Index int    `json:"index"`
}

func wikipediaExtracts(ctx context.Context, filters url.Values, full bool) ([]wikipediaExtract, error) {
	params := url.Values{
		"action": {"query"}, "prop": {"extracts"}, "format": {"json"}, "formatversion": {"2"},
		"exintro": {"1"}, "explaintext": {"1"}, "exlimit": {"4"}, "exchars": {"900"}, "maxlag": {"5"},
	}
	if full {
		params.Del("exintro")
		params.Del("exchars")
		params.Set("exlimit", "1")
	}
	for key, values := range filters {
		params[key] = values
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://en.wikipedia.org/w/api.php?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "LexiGo (+https://github.com/QiNian2684/LexiGo)")
	request.Header.Set("Accept", "application/json")
	reply, err := wikiClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer reply.Body.Close()
	if reply.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Wikipedia returned HTTP %d", reply.StatusCode)
	}
	const maxWikipediaJSON = 256 << 10
	body, err := io.ReadAll(io.LimitReader(reply.Body, maxWikipediaJSON+1))
	if err != nil || len(body) > maxWikipediaJSON {
		return nil, err
	}
	var data struct {
		Query struct {
			Pages []wikipediaExtract `json:"pages"`
		} `json:"query"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err = json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	if data.Error.Code != "" {
		return nil, fmt.Errorf("Wikipedia returned %s", data.Error.Code)
	}
	return data.Query.Pages, nil
}

func wikipediaExample(pages []wikipediaExtract, key, source string) sourcedExample {
	var best sourcedExample
	bestScore := 1000
	for _, page := range pages {
		if page.ID <= 0 {
			continue
		}
		for _, sentence := range proseSentences(page.Text) {
			if !hasUsageExample(sentence, key) {
				continue
			}
			size := len(strings.Fields(sentence))
			score := max(size-12, 12-size) + page.Index*2
			if score < bestScore {
				best = sourcedExample{Examples: sentence, By: source + "|" + strconv.FormatInt(page.ID, 10)}
				bestScore = score
			}
		}
	}
	return best
}

func fetchWikipediaExample(ctx context.Context, key string) (sourcedExample, error) {
	pages, err := wikipediaExtracts(ctx, url.Values{
		"generator": {"search"}, "gsrsearch": {`"` + key + `"`}, "gsrnamespace": {"0"}, "gsrlimit": {"4"},
	}, false)
	if err != nil {
		return sourcedExample{}, err
	}
	if candidate := wikipediaExample(pages, key, "wikipedia-online"); candidate.Examples != "" {
		return candidate, nil
	}
	// The term can appear in the article body without appearing in the
	// introduction. Fetch the most relevant article once, with a bounded response.
	var first *wikipediaExtract
	for i := range pages {
		if pages[i].ID > 0 && (first == nil || pages[i].Index < first.Index) {
			first = &pages[i]
		}
	}
	if first == nil {
		return sourcedExample{}, nil
	}
	details, err := wikipediaExtracts(ctx, url.Values{"pageids": {strconv.FormatInt(first.ID, 10)}}, true)
	if err != nil {
		return sourcedExample{}, nil
	}
	return wikipediaExample(details, key, "wikipedia-context"), nil
}

func onlineWikipediaID(source string) string {
	kind, value, ok := strings.Cut(source, "|")
	id, err := strconv.ParseInt(value, 10, 64)
	if ok && (kind == "wikipedia-online" || kind == "wikipedia-context") && err == nil && id > 0 {
		return value
	}
	return ""
}
