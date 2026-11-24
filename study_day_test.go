package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestStudyDayUsesLocalFiveOClock(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	for _, test := range []struct {
		hour, minute int
		day          string
	}{
		{0, 0, "2026-11-17"}, {1, 0, "2026-11-17"}, {4, 59, "2026-11-17"}, {5, 0, "2026-11-18"}, {23, 59, "2026-11-18"},
	} {
		now := time.Date(2026, 11, 18, test.hour, test.minute, 0, 0, loc)
		if got := studyDay(now); got != test.day || nextStudyDay(now).Hour() != 5 {
			t.Fatalf("%v mapped to %s; next %v", now, got, nextStudyDay(now))
		}
	}
	transition := time.Date(2026, 1, 1, 4, 59, 59, 0, loc)
	if studyDay(transition) != "2025-12-31" || studyDay(transition.Add(time.Second)) != "2026-01-01" {
		t.Fatal("new year rollover")
	}
}

func TestCustomRolloverAndExportKeepStartedDay(t *testing.T) {
	s, _ := newTestStore(t)
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 18, 5, 15, 0, 0, loc)
	importTest(t, s, "2026-11-18", "apple")
	options := s.db.Settings
	options.Rollover = "07:45"
	state, err := s.changeSettingsAt(now, options)
	if err != nil || state.Date != "2026-11-18" || s.db.StudyDayFloor != state.Date {
		t.Fatalf("changed time rewound the active day: %+v %v", state, err)
	}
	file, err := s.transferBytes(now)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := parseTransfer(file)
	if err != nil || backup.Database.Settings.Rollover != "07:45" || backup.dateConflict(now) != "" {
		t.Fatalf("custom rollover failed to round-trip: %+v %v", backup, err)
	}
	ends := time.Date(2026, 11, 19, 7, 45, 0, 0, loc)
	if !backup.DayEnds.Equal(ends) || s.db.studyDay(ends.Add(-time.Second)) != "2026-11-18" ||
		s.db.studyDay(ends) != "2026-11-19" {
		t.Fatalf("wrong minute or boundary: %+v", backup.DayEnds)
	}
	for _, value := range []string{"7:45", "07:60", "24:00", "03:01:02"} {
		if validRollover(value) {
			t.Errorf("accepted invalid rollover %q", value)
		}
	}
}

func TestFreshPriorityAndPinnedReview(t *testing.T) {
	s, _ := newTestStore(t)
	const day = "2026-11-18"
	importTest(t, s, "2026-11-16", numberedWords("oldword", 40))
	open := s.review(day)
	s.pinReview(open)
	importTest(t, s, day, numberedWords("newword", 15))
	if plan := s.planReview(day); len(plan.new) != 15 || len(plan.old) != 5 || s.review(day).Current.Key != open.Current.Key {
		t.Fatalf("new set or pin did not hold: %+v", plan.state)
	}
	nextVote(t, s, day, true)
	if card := s.db.Words[s.review(day).Current.Key]; card.AddedDay != day {
		t.Fatalf("new word did not overtake old word: %s", card.Key)
	}
	importTest(t, s, day, numberedWords("extra", 15))
	if state := s.review(day); state.Due != 31 || state.Settings.Limit != 20 || state.Reviewed != 1 {
		t.Fatalf("excess fresh items or unfinished old card lost: %+v", state)
	}
	importTest(t, s, day, "oldwordaz")
	if plan := s.planReview(day); len(plan.new) != 31 || len(plan.repeats) != 1 {
		t.Fatalf("reimport not in priority set: new=%d repeats=%d", len(plan.new), len(plan.repeats))
	}
}

func numberedWords(prefix string, count int) string {
	words := make([]string, count)
	for i := range words {
		words[i] = fmt.Sprintf("%s%c%c", prefix, 'a'+rune(i/26), 'a'+rune(i%26))
	}
	return strings.Join(words, ",")
}

func TestRolloverCompletesDisplayedCardAndKeepsForgotHold(t *testing.T) {
	s, _ := newTestStore(t)
	clock := time.Date(2026, 11, 18, 4, 59, 0, 0, time.FixedZone("CST", 8*3600))
	lastDay := studyDay(clock)
	importTest(t, s, lastDay, "exclusive, degrading")
	current := s.review(lastDay)
	s.pinReview(current)
	clock = clock.Add(2 * time.Minute)
	day := studyDay(clock)
	a := &application{store: s}
	state := a.reviewResponse(clock, false)
	if state.Date != lastDay || state.Current.Key != current.Current.Key {
		t.Fatalf("card interrupted: %+v", state)
	}
	hold, err := s.recordVote(lastDay, state.Current.Key, state.Current.Reviews, state.Current.Order, false, true, false)
	if err != nil || !hold.Waiting {
		t.Fatalf("could not finish old card: %+v %v", hold, err)
	}
	if p := a.reviewResponse(clock, true); !p.Waiting || p.Date != lastDay {
		t.Fatal("lost pending next operation")
	}
	file, err := s.transferBytes(clock)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := parseTransfer(file)
	if err != nil {
		t.Fatal(err)
	}
	recovered := &store{db: payload.Database}
	if p := recovered.review(recovered.activeReviewDay(day)); !p.Waiting || p.Current.Key != state.Current.Key {
		t.Fatal("same-boundary transfer lost pending next")
	}
	if _, err = s.nextAfterForgot(lastDay, hold.Current.Order); err != nil {
		t.Fatal(err)
	}
	s.presented = presentedCard{}
	if p := a.reviewResponse(clock, true); p.Date != day || p.Waiting || p.Due != 2 || p.Current.Key == state.Current.Key {
		t.Fatalf("did not safely cross boundary: %+v", p)
	}
	stats := s.stats(day)
	if len(stats.Days) != 30 || stats.Days[29].Distinct != 0 || stats.Days[28].No != 1 || s.db.Events[len(s.db.Events)-1].OccurredAt == nil {
		t.Fatalf("history or missing-day zeros changed: %+v", stats.Days[27:])
	}
}

func TestLegacyTransferQueueStillImports(t *testing.T) {
	s, _ := newTestStore(t)
	const day = "2026-11-18"
	when := time.Date(2026, 11, 18, 1, 0, 0, 0, time.UTC)
	importTest(t, s, day, numberedWords("fresh", 30))
	payload := transferPayload{CreatedAt: when, DayEnds: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC),
		Database: s.db, Task: s.captureTaskRules(day, true)}
	content, _ := json.Marshal(payload)
	checksum := sha256.Sum256(content)
	file, _ := json.Marshal(transferEnvelope{transferFormat, 1, fmt.Sprintf("%x", checksum), content})
	value, err := parseTransfer(file)
	if err != nil || !value.legacy || value.dateConflict(when) != "" || len(value.Task.Pending) != 20 {
		t.Fatalf("legacy format could not be restored: %d %v", len(value.Task.Pending), err)
	}
}
