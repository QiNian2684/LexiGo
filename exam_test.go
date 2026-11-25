package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestExamCountdown(t *testing.T) {
	for _, tc := range []struct {
		today, set, date string
		days             int
	}{
		{"2026-12-19", "", "2026-12-20", 1},
		{"2026-12-20", "", "2026-12-20", 0},
		{"2026-12-21", "", "2027-12-20", 364},
		{"2027-12-31", "01-01", "2028-01-01", 1},
		{"2027-02-28", "02-29", "2028-02-29", 366},
		{"2028-02-29", "02-29", "2028-02-29", 0},
		{"2099-01-01", "02-29", "2104-02-29", 1884},
		{"2027-12-19", "2027-12-18", "2027-12-18", -1},
	} {
		want := examCountdown{tc.date, tc.days}
		if got := countdown(tc.today, tc.set); got != want {
			t.Errorf("today %s, setting %q: got %+v, want %+v", tc.today, tc.set, got, want)
		}
	}
	for _, invalid := range []string{"2-09", "02-30", "04-31", "12-00", "2027-02-29", "2027-2-01", "1899-01-01", "bad"} {
		if validExamDate(invalid) {
			t.Errorf("accepted %q", invalid)
		}
	}
}

func TestExamPersistsAndTransfersWithoutChangingReview(t *testing.T) {
	source, path := newTestStore(t)
	dest, _ := newTestStore(t)
	now := time.Now()
	day := source.db.studyDay(now)
	importTest(t, source, day, "transport, crucial")
	first := source.shuffleReview(day).Current
	held, err := source.recordVote(day, first.Key, first.Reviews, first.Order, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	before := source.captureTask(day)
	prefs := held.Settings
	prefs.ExamDate = "2027-12-18"
	next, err := source.changeSettings(day, prefs)
	if err != nil || !next.Waiting || !reflect.DeepEqual(held.Current, next.Current) ||
		!reflect.DeepEqual(before, source.captureTask(day)) {
		t.Fatalf("exam date changed review: %+v (%v)", next, err)
	}
	file, err := source.transferBytes(now)
	if err != nil {
		t.Fatal(err)
	}
	app := &application{store: dest}
	preview := previewFor(t, app, file)
	if apply := commitFor(app, preview.Token); apply.Code != 200 {
		t.Fatal(apply.Body.String())
	}
	if !reflect.DeepEqual(source.db, dest.db) || dest.review(day).Exam.Date != prefs.ExamDate {
		t.Fatal("restore did not preserve the exam date and learning records")
	}
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	if reopened.db.Settings.ExamDate != prefs.ExamDate {
		t.Fatal("date did not survive restart")
	}
	var legacy settings
	if err := json.Unmarshal([]byte(`{"limit":20,"goal":6,"dailyGoal":3,"online":true,"showIPA":true}`), &legacy); err != nil ||
		!legacy.valid() || countdown("2026-12-19", legacy.ExamDate).Days != 1 {
		t.Fatalf("legacy settings failed to use default date: %+v (%v)", legacy, err)
	}
}
