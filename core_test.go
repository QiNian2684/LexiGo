package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) (*store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vocabulary.db")
	s, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.io.Close() })
	return s, path
}

var testDictionary dictionary

func importTest(t *testing.T, s *store, day, content string) importResult {
	t.Helper()
	items, err := parseImport(importInput{Text: content})
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := s.preview(items)
	result, err := s.add(day, items, plan.Token, &testDictionary)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func nextVote(t *testing.T, s *store, day string, correct bool) voteResult {
	t.Helper()
	current := s.review(day).Current
	if current == nil {
		t.Fatal("no current card")
	}
	result, err := s.vote(day, current.Key, current.Reviews, current.Order, correct)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func setTestGoal(t *testing.T, s *store, day string, goal, limit int) {
	t.Helper()
	options := s.db.Settings
	options.Goal, options.Limit, options.DailyGoal = goal, limit, 1
	options.Online = false
	if _, err := s.changeSettings(day, options); err != nil {
		t.Fatal(err)
	}
}

func mustSave(t *testing.T, s *store, key string) {
	t.Helper()
	if err := s.save([]string{key}, len(s.db.Events)); err != nil {
		t.Fatal(err)
	}
}

func TestGraduationReimportPreservesContentAndSameDayCredit(t *testing.T) {
	s, path := newTestStore(t)
	importTest(t, s, "2026-11-01", "crucial")
	card := s.db.Words["crucial"]
	card.PersonalNote = "我的记忆笔记\n不应被覆盖"
	mustSave(t, s, card.Key)
	for i, day := range []string{"2026-11-01", "2026-11-03", "2026-11-08", "2026-11-09", "2026-11-11", "2026-11-16"} {
		result := passOne(t, s, day)
		if result.Graduated != (i == 5) || !result.Credited || result.Due != 0 {
			t.Fatalf("day %s: %+v", day, result)
		}
	}
	options := s.db.Settings
	options.Goal = 8
	if _, err := s.changeSettings("2026-11-16", options); err != nil {
		t.Fatal(err)
	}
	if after := s.review("2026-11-17"); after.Mastered != 1 || after.Due != 0 {
		t.Fatal("raising the goal from 6 to 8 must preserve mastery")
	}
	options.Goal = 6
	if _, err := s.changeSettings("2026-11-16", options); err != nil {
		t.Fatal(err)
	}
	plan := importTest(t, s, "2026-11-16", " Crucial , CRUCIAL ")
	card = s.db.Words["crucial"]
	if plan.Reset != 1 || plan.New != 0 || plan.Duplicates != 1 || len(s.db.Words) != 1 {
		t.Fatalf("wrong reimport result: %+v", plan)
	}
	if card.Mastered || card.Streak != 0 || card.PersonalNote == "" || len(s.history(card)) != 20 || card.DailyCount != 0 || card.DailyDay != "" || card.LastPass != "" || card.Meaning == "" {
		t.Fatalf("reset changed unrelated data: %+v", card)
	}
	if r := passOne(t, s, "2026-11-16"); r.Credited || r.Reviewed != 1 || r.Due != 0 || card.Streak != 0 {
		t.Fatalf("reimport earned an extra credit: %+v", r)
	}
	if err := s.io.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.io.Close()
	if reloaded.db.Words["crucial"].PersonalNote != card.PersonalNote || reloaded.review("2026-11-16").Due != 0 {
		t.Fatal("reopen did not preserve note and date")
	}
	if r := passOne(t, reloaded, "2026-11-20"); !r.Credited || reloaded.db.Words["crucial"].Streak != 1 {
		t.Fatal("new study date did not credit")
	}
}

func TestErrorsKeepOldStreakAndRetryCanPass(t *testing.T) {
	s, path := newTestStore(t)
	setTestGoal(t, s, "2026-11-01", 6, 20)
	importTest(t, s, "2026-11-01", "apple")
	nextVote(t, s, "2026-11-01", true)
	day := "2026-11-04"
	for i := 0; i < 3; i++ {
		r := nextVote(t, s, day, false)
		if r.Due != 1 || r.Reviewed != 1 || r.Current.Streak != 1 {
			t.Fatalf("wrong retry %d: %+v", i, r)
		}
	}
	if err := s.io.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.io.Close()
	if r := nextVote(t, s, day, true); r.Due != 0 || !r.Credited || s.db.Words["apple"].Streak != 2 {
		t.Fatalf("retry graded as successful: %+v", r)
	}
	stats := s.stats(day)
	today := stats.Days[29]
	if today.Distinct != 1 || today.Yes != 1 || today.No != 3 || stats.TotalReviews != 5 || stats.TotalForgot != 3 {
		t.Fatalf("incorrect statistics: %+v", stats)
	}
	if r, err := s.undo(day); err != nil || r.Due != 1 || r.Current.Reviews != 4 || s.stats(day).Days[29].Yes != 0 {
		t.Fatalf("undo failed: %+v %v", r, err)
	}
}

func TestQuotaAndExtraReimport(t *testing.T) {
	s, _ := newTestStore(t)
	day := "2026-11-16"
	setTestGoal(t, s, day, 6, 3)
	importTest(t, s, "2026-11-15", "abundance, crucial, sophisticated, forthright, fiscal")
	if r := s.review(day); r.Due != 3 {
		t.Fatalf("plan: %+v", r)
	}
	if r := nextVote(t, s, day, false); r.Due != 3 || r.Current.Word == "abundance" {
		t.Fatalf("repeat should be delayed: %+v", r)
	}
	nextVote(t, s, day, true)
	if r := nextVote(t, s, day, true); r.Current.Word != "abundance" || r.Reviewed != 3 || r.Due != 1 {
		t.Fatalf("wrong third slot: %+v", r)
	}
	nextVote(t, s, day, true)
	if r := s.review(day); r.Due != 0 || r.Reviewed != 3 {
		t.Fatalf("incorrect quotas: %+v", r)
	}
	importTest(t, s, day, "forthright, crucial")
	r := s.review(day)
	if r.Due != 2 || r.Reviewed != 3 {
		t.Fatalf("reset should schedule two extra cards: %+v", r)
	}
	if r := nextVote(t, s, day, true); !r.Credited || r.Reviewed != 4 {
		t.Fatalf("previously unseen extra should earn first credit: %+v", r)
	}
	if r := nextVote(t, s, day, true); r.Credited || r.Reviewed != 4 || r.Due != 0 {
		t.Fatalf("extra previously credited: %+v", r)
	}
}

func TestGoalChangeAndTrash(t *testing.T) {
	s, _ := newTestStore(t)
	day := "2026-11-05"
	setTestGoal(t, s, day, 1, 20)
	importTest(t, s, day, "chronic, counsel")
	if !nextVote(t, s, day, true).Graduated {
		t.Fatal("goal 1 should master immediately")
	}
	setTestGoal(t, s, day, 8, 20)
	if !s.db.Words["chronic"].Mastered || nextVote(t, s, day, true).Graduated {
		t.Fatal("existing mastery or new goal incorrectly applied")
	}
	for _, action := range []string{"remove", "restore", "reset"} {
		if count, err := s.batch(day, selection{Keys: []string{"chronic", "chronic"}}, action); err != nil || count != 1 {
			t.Fatalf("batch %s: %d %v", action, count, err)
		}
	}
	if c := s.db.Words["chronic"]; c.Deleted || c.Mastered || c.Streak != 0 || len(s.history(c)) != 5 {
		t.Fatalf("word management: %+v", c)
	}
}

func TestLargeImportPreviewAndBackup(t *testing.T) {
	s, path := newTestStore(t)
	day := "2026-11-16"
	setTestGoal(t, s, day, 6, 20)
	items := []importedWord{{Word: " apple ", cardDetails: cardDetails{Meaning: "苹果"}, PersonalNote: "备注"},
		{Word: "Apple"}, {Word: "concrete example"}, {Word: "中文"}}
	for i := 0; i < 400; i++ {
		items = append(items, importedWord{Word: fmt.Sprintf("word%c%c", 'a'+rune(i/26), 'a'+rune(i%26))})
	}
	plan, _ := s.preview(items)
	if plan.New != 402 || plan.Duplicates != 1 || len(plan.Invalid) != 1 || len(s.db.Words) != 0 {
		t.Fatalf("wrong preview: %+v", plan)
	}
	if _, err := s.add(day, items, "stale", &testDictionary); !errors.Is(err, errImportChanged) {
		t.Fatal("unconfirmed import succeeded")
	}
	if _, err := s.add(day, items, plan.Token, &testDictionary); err != nil {
		t.Fatal(err)
	}
	nextVote(t, s, day, true)
	data, _ := json.Marshal(s.db)
	var saved database
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	importTest(t, s, day, "apple")
	if err := s.restore(saved); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "backups", "before-restore.json")); err != nil {
		t.Fatal(err)
	}
	if s.db.Words["apple"].PersonalNote != "备注" || s.db.Words["apple"].Streak != 1 || len(s.db.Events) != 403 {
		t.Fatal("backup does not contain all study state")
	}
}

func TestImportStructuredFieldsAndWikiEnglishSection(t *testing.T) {
	data, err := parseImport(importInput{Format: "csv", Text: "单词,中文释义,例句翻译,备注\nsubtle,微妙,细微差别,考研"})
	if err != nil || len(data) != 1 || data[0].ExampleTranslation != "细微差别" || data[0].PersonalNote != "考研" {
		t.Fatalf("CSV: %+v %v", data, err)
	}
	src := `<h2 id="English">English</h2><h3>Etymology</h3><p>From <i>earlier</i> + form.</p>
	<h3>Verb</h3><ol><li>To test. <i class="Latn e-example" lang="en">They test it.</i></li></ol>
	<h4>Translations</h4><span class="Hant" lang="cmn">測試</span><span class="Hans" lang="cmn">测试</span>
	<h2 id="French">French</h2><h3>Noun</h3><i class="e-example" lang="en">Ignore me.</i>`
	card := parseWiktionary(src)
	if card.POS != "动词" || card.Etymology != "From earlier + form." || card.Examples != "They test it." || card.Meaning != "测试" {
		t.Fatalf("parsed: %+v", card)
	}
}

func TestDefaultTwentyWithRepeatedFailures(t *testing.T) {
	s, _ := newTestStore(t)
	const day = "2026-11-16"
	var input string
	for i := 0; i < 25; i++ {
		input += fmt.Sprintf("capword%c,", 'a'+rune(i))
	}
	importTest(t, s, "2026-11-15", input)
	initial := s.review(day)
	if initial.Due != 20 || initial.Settings.Limit != 20 {
		t.Fatalf("default plan: %+v", initial)
	}
	attempts := make(map[string]int)
	for count := 0; count < 100 && s.review(day).Due > 0; count++ {
		cur := s.review(day).Current
		attempts[cur.Key]++
		nextVote(t, s, day, attempts[cur.Key] >= 3)
	}
	if current := s.review(day); current.Due != 0 || current.Reviewed != 20 || len(attempts) != 20 {
		t.Fatalf("retries used new places: %+v", current)
	}
	if stats := s.stats(day); stats.TotalReviews != 100 || stats.TotalForgot != 40 || stats.Days[29].Distinct != 20 {
		t.Fatalf("unexpected statistics: %+v", stats)
	}
}
