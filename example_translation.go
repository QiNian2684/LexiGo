package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const myMemoryURL = "https://api.mymemory.translated.net/get"

var (
	errTranslationQuota = errors.New("translation quota exhausted")
	errVerifyExample    = errors.New("could not verify the public example")
	translationClient   = &http.Client{Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
)

func myMemoryTranslation(ctx context.Context, client *http.Client, endpoint, sentence string) (string, error) {
	params := url.Values{"q": {sentence}, "langpair": {"en|zh-CN"}, "mt": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LexiGo/3.5.1 (+https://github.com/QiNian2684/LexiGo)")
	reply, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer reply.Body.Close()
	if reply.StatusCode == http.StatusTooManyRequests {
		return "", errTranslationQuota
	}
	if reply.StatusCode != http.StatusOK {
		return "", fmt.Errorf("translation service HTTP %d", reply.StatusCode)
	}
	var data struct {
		Status        int  `json:"responseStatus"`
		QuotaFinished bool `json:"quotaFinished"`
		Matches       []struct {
			Segment     string  `json:"segment"`
			Translation string  `json:"translation"`
			Target      string  `json:"target"`
			Match       float64 `json:"match"`
			Creator     string  `json:"created-by"`
		} `json:"matches"`
	}
	if err := json.NewDecoder(io.LimitReader(reply.Body, 256<<10)).Decode(&data); err != nil {
		return "", err
	}
	if data.QuotaFinished || data.Status == http.StatusTooManyRequests {
		return "", errTranslationQuota
	}
	if data.Status != http.StatusOK {
		return "", fmt.Errorf("translation service status %d", data.Status)
	}
	for _, match := range data.Matches {
		// Translation-memory results may refer to a similar but different sentence.
		if html.UnescapeString(match.Segment) != sentence ||
			!strings.HasPrefix(strings.ToLower(match.Target), "zh") ||
			(match.Match < 1 && match.Creator != "MT!") {
			continue
		}
		text := strings.Join(strings.Fields(html.UnescapeString(match.Translation)), " ")
		if strings.ContainsAny(text, "<>\uFFFD") || utf8.RuneCountInString(text) > 500 ||
			!strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			continue
		}
		return simplifyExampleTranslation(text), nil
	}
	return "", nil
}

func publicWikiRevision(source string) string {
	value, ok := strings.CutPrefix(source, "https://en.wiktionary.org/wiki/Special:PermanentLink/")
	revision, suffix := strings.CutSuffix(value, "#English")
	n, err := strconv.ParseInt(revision, 10, 64)
	if ok && suffix && err == nil && n > 0 {
		return revision
	}
	return ""
}

func translatableExample(card *wordCard) bool {
	if card == nil || card.Deleted || card.ExampleTranslationBy == "manual" ||
		strings.TrimSpace(card.Examples) == "" || strings.TrimSpace(card.ExampleTranslation) != "" {
		return false
	}
	if card.ExampleBy == "simple" {
		sources, _ := bundledExamples()
		source := sources[card.Key]
		return source.By == "simple" && source.Examples == card.Examples
	}
	if bundledMachineTranslation(card.ExampleBy) {
		examples, _ := expandedExamples()
		source := examples[card.Key]
		return card.ExampleBy == source.By && card.Examples == source.Examples
	}
	if onlineTatoebaID(card.ExampleBy) > 0 || onlineWikipediaID(card.ExampleBy) != "" {
		return true
	}
	return card.ExampleBy == "" && publicWikiRevision(card.WikiSource) != ""
}

type translateSentence func(context.Context, string) (string, error)

func translatePublicExample(ctx context.Context, card wordCard, translate translateSentence) (string, error) {
	if id := onlineTatoebaID(card.ExampleBy); id > 0 {
		entry, err := publicTatoebaExample(ctx, id)
		if err != nil && !errors.Is(err, errMissingSentence) {
			return "", errVerifyExample
		}
		if err != nil || entry.Text != card.Examples && !slices.Contains(proseSentences(entry.Text), card.Examples) {
			return "", nil
		}
	} else if id := onlineWikipediaID(card.ExampleBy); id != "" {
		pages, err := wikipediaExtracts(ctx, url.Values{"pageids": {id}}, strings.HasPrefix(card.ExampleBy, "wikipedia-context|"))
		if err != nil {
			return "", errVerifyExample
		}
		if len(pages) != 1 || !slices.Contains(proseSentences(pages[0].Text), card.Examples) {
			return "", nil
		}
	} else if card.ExampleBy != "simple" && !bundledMachineTranslation(card.ExampleBy) {
		// Legacy cards could retain a source link after being edited. Verify the
		// sentence against the actual public revision before sending it anywhere.
		entry, err := fetchWikiEntry(ctx, "oldid", publicWikiRevision(card.WikiSource))
		if err != nil {
			return "", errVerifyExample
		}
		if entry.Examples != card.Examples && filterWikiExamples(entry.cardDetails, card.Key).Examples != card.Examples {
			return "", nil
		}
	}
	sentences := strings.Split(card.Examples, "\n\n")
	for _, sentence := range sentences {
		if len(strings.TrimSpace(sentence)) == 0 || len(sentence) > 500 {
			return "", nil
		}
	}
	for i, sentence := range sentences {
		translated, err := translate(ctx, strings.TrimSpace(sentence))
		if err != nil || translated == "" {
			return "", err
		}
		sentences[i] = translated
	}
	return strings.Join(sentences, "\n\n"), nil
}

// All fields except wake are guarded by store.mu.
type translationQueue struct {
	wake    chan struct{}
	pending map[string]translationTask
	skipped map[uint64]string
	readyAt time.Time
	cancel  context.CancelFunc
}

type translationTask struct {
	visible bool
	retryAt time.Time
}

func (a *application) queueTranslationLocked(card *wordCard, visible bool) bool {
	q := &a.translations
	if q.wake == nil || !a.store.db.Settings.Online || !translatableExample(card) ||
		q.skipped[card.ID] == card.Examples {
		return false
	}
	task, exists := q.pending[card.Key]
	if !exists || visible && !task.visible {
		if q.pending == nil {
			q.pending = make(map[string]translationTask)
		}
		task.visible = visible
		q.pending[card.Key] = task
		a.wakeTranslations()
	}
	return true
}

func (a *application) wakeTranslations() {
	select {
	case a.translations.wake <- struct{}{}:
	default:
	}
}

func (a *application) nextTranslationLocked(now time.Time) (*wordCard, time.Duration) {
	q := &a.translations
	var next *wordCard
	wait := time.Hour
	for key, task := range q.pending {
		current := a.store.db.Words[key]
		if !translatableExample(current) || q.skipped[current.ID] == current.Examples {
			delete(q.pending, key)
			continue
		}
		if remaining := task.retryAt.Sub(now); remaining > 0 {
			wait = min(wait, remaining)
			continue
		}
		if next == nil || task.visible && !q.pending[next.Key].visible ||
			task.visible == q.pending[next.Key].visible && current.ID < next.ID {
			next = current
		}
	}
	return next, wait
}

func (a *application) completeTranslations(ctx context.Context, translate translateSentence) {
	s, q := a.store, &a.translations
	for ctx.Err() == nil {
		s.mu.Lock()
		now := time.Now()
		wait := q.readyAt.Sub(now)
		var next *wordCard
		if wait <= 0 && s.db.Settings.Online {
			next, wait = a.nextTranslationLocked(now)
		}
		if next == nil {
			s.mu.Unlock()
			if wait <= 0 {
				wait = time.Hour
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
			case <-q.wake:
			case <-timer.C:
			}
			timer.Stop()
			continue
		}
		card, generation := *next, a.generation
		job, cancel := context.WithCancel(ctx)
		q.cancel = cancel
		s.mu.Unlock()

		text, err := translatePublicExample(job, card, translate)
		s.mu.Lock()
		if job.Err() == nil && s.db.Settings.Online && generation == a.generation {
			q.readyAt = time.Now().Add(time.Second)
			current := s.db.Words[card.Key]
			if current != nil && current.ID == card.ID && !current.Deleted &&
				current.Examples == card.Examples && current.ExampleBy == card.ExampleBy &&
				current.WikiSource == card.WikiSource && current.ExampleTranslation == card.ExampleTranslation &&
				current.ExampleTranslationBy == card.ExampleTranslationBy {
				a.saveTranslationLocked(current, text, err)
			}
		}
		q.cancel = nil
		cancel()
		s.mu.Unlock()
	}
}

func (a *application) saveTranslationLocked(card *wordCard, text string, err error) {
	s, q := a.store, &a.translations
	switch {
	case errors.Is(err, errTranslationQuota):
		q.readyAt = time.Now().Add(12 * time.Hour)
	case err != nil:
		task := q.pending[card.Key]
		task.retryAt = time.Now().Add(time.Minute)
		q.pending[card.Key] = task
	case text == "" || card.cardDetails.size()+utf8.RuneCountInString(text) > 12000:
		if q.skipped == nil {
			q.skipped = make(map[uint64]string)
		}
		q.skipped[card.ID] = card.Examples
		delete(q.pending, card.Key)
	default:
		oldText, oldSource := card.ExampleTranslation, card.ExampleTranslationBy
		card.ExampleTranslation, card.ExampleTranslationBy = text, "mymemory"
		if err := s.save([]string{card.Key}, len(s.db.Events)); err != nil {
			card.ExampleTranslation, card.ExampleTranslationBy = oldText, oldSource
			q.readyAt = time.Now().Add(time.Minute)
			log.Printf("save example translation: %v", err)
			return
		}
		delete(q.pending, card.Key)
	}
}

func (a *application) resetTranslationQueueLocked() {
	if a.translations.cancel != nil {
		a.translations.cancel()
	}
	a.translations.pending = nil
	a.translations.skipped = nil
	a.wakeTranslations()
}
