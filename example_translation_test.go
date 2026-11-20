package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMyMemoryRequiresExactSentenceAndSimplifies(t *testing.T) {
	const sentence = "See Appendix B for a list of irregular verbs."
	for _, tc := range []struct {
		name, body, want string
		quota            bool
	}{
		{"complete", `{"responseStatus":200,"matches":[
			{"segment":"Appendix B","target":"zh-CN","match":0.99,"translation":"附錄 B"},
			{"segment":"See Appendix B for a list of irregular verbs.","target":"zh-CN","match":0.85,"created-by":"MT!","translation":"不規則動詞表見附錄 B。"}]}`,
			"不规则动词表见附录 B。", false},
		{"fuzzy only", `{"responseStatus":200,"matches":[{"segment":"Appendix B","target":"zh-CN","match":0.99,"translation":"附錄 B"}]}`, "", false},
		{"quota", `{"responseStatus":403,"quotaFinished":true}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				params := r.URL.Query()
				if r.Method != http.MethodGet || params.Get("q") != sentence ||
					params.Get("langpair") != "en|zh-CN" || params.Get("mt") != "1" || len(params) != 3 {
					t.Error("unexpected translation request")
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer api.Close()
			got, err := myMemoryTranslation(context.Background(), api.Client(), api.URL, sentence)
			if got != tc.want || errors.Is(err, errTranslationQuota) != tc.quota || !tc.quota && err != nil {
				t.Fatalf("translation=%q, error=%v", got, err)
			}
		})
	}
}

func TestTranslationQueuePreservesManualAndPersistsPublicExample(t *testing.T) {
	s, path := newTestStore(t)
	result := importTest(t, s, localDay(), "appendix,zany")
	if result.New != 2 {
		t.Fatal(result)
	}
	a := &application{store: s, translations: translationQueue{wake: make(chan struct{}, 1)}}
	public := s.db.Words["appendix"]
	stale := s.db.Words["zany"]
	original, err := bundledExamples()
	if err != nil {
		t.Fatal(err)
	}
	// Use the original untranslated text even when the newer bundle
	// supplies an offline translation for this word.
	for _, card := range []*wordCard{public, stale} {
		card.ExampleBy, card.Examples, card.ExampleTranslation = "simple", original[card.Key].Examples, ""
	}
	if !a.queueTranslationLocked(stale, false) || !a.queueTranslationLocked(public, false) ||
		!a.queueTranslationLocked(stale, true) {
		t.Fatal("public examples were not queued")
	}
	if next, _ := a.nextTranslationLocked(time.Now()); next != stale {
		t.Fatal("expected visible example to have priority")
	}
	private := *public
	private.Examples = "My private example."
	if a.queueTranslationLocked(&private, true) {
		t.Fatal("unverified example was queued")
	}
	s.db.Settings.Online = false
	if a.queueTranslationLocked(public, true) {
		t.Fatal("offline setting was ignored")
	}
	s.db.Settings.Online = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	started := make(chan string, 2)
	resume := make(chan struct{}, 2)
	go func() {
		defer close(done)
		a.completeTranslations(ctx, func(ctx context.Context, sentence string) (string, error) {
			started <- sentence
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-resume:
				return "自动生成的译文。", nil
			}
		})
	}()
	defer func() { cancel(); <-done }()
	readStart := func() string {
		t.Helper()
		select {
		case sentence := <-started:
			return sentence
		case <-time.After(5 * time.Second):
			t.Fatal("background worker did not start")
			return ""
		}
	}
	if sentence := readStart(); sentence != stale.Examples {
		t.Fatalf("started %q before the visible example", sentence)
	}
	s.mu.Lock()
	stale.ExampleTranslation, stale.ExampleTranslationBy = "手动译文", "manual"
	s.mu.Unlock()
	resume <- struct{}{}
	if sentence := readStart(); sentence != public.Examples {
		t.Fatalf("expected the other public example, got %q", sentence)
	}
	resume <- struct{}{}
	for until := time.Now().Add(5 * time.Second); ; {
		s.mu.Lock()
		saved := *public
		manual := stale.ExampleTranslation
		eventCount := len(s.db.Events)
		s.mu.Unlock()
		if saved.ExampleTranslation != "" {
			if saved.ExampleTranslationBy != "mymemory" || manual != "手动译文" || eventCount != 2 {
				t.Fatalf("translation source, manual text, or events changed unexpectedly: %v %q %d",
					saved.ExampleTranslationBy, manual, eventCount)
			}
			break
		}
		if time.Now().After(until) {
			t.Fatal("translation wasn't saved")
		}
		time.Sleep(10 * time.Millisecond)
	}
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	if got := reopened.db.Words["appendix"]; got.ExampleTranslationBy != "mymemory" ||
		got.ExampleTranslation != "自动生成的译文。" || got.Examples != public.Examples {
		t.Fatalf("unexpected reopened card %+v", got)
	}
}

func TestExactUsageExamples(t *testing.T) {
	for _, example := range []struct {
		text, term string
		ok         bool
	}{
		{"She was nodding in agreement.", "nodding", true},
		{"She nodded in agreement.", "nodding", false},
		{"We walked past it yesterday.", "walked", true},
		{"The path ran beside a river.", "ran", true},
		{"For quotations using this term, see Citations:nodding.", "nodding", false},
		{"The sprang-like noise was unusual.", "sprang", false},
	} {
		if got := hasUsageExample(example.text, example.term); got != example.ok {
			t.Errorf("%q in %q: got %v, want %v", example.term, example.text, got, example.ok)
		}
	}
}
