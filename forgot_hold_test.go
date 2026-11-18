package main

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestForgotHoldSurvivesRestartTransferAndUndo(t *testing.T) {
	s, path := newTestStore(t)
	date := time.Date(2026, 11, 17, 13, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	day := date.Format(time.DateOnly)
	importTest(t, s, day, "abundance, crucial, novel")
	first := s.shuffleReview(day).Current
	result, err := s.recordVote(day, first.Key, first.Reviews, first.Order, false, true)
	if err != nil || !result.Waiting || result.Current.Key != first.Key || result.Current.Reviews != 1 ||
		result.DailyCount != 0 || s.attendance(day).votes != 1 {
		t.Fatalf("forgot submission: %+v (%v)", result, err)
	}
	card := result.Current
	if _, err := s.recordVote(day, card.Key, card.Reviews, card.Order, true, false); !errors.Is(err, errStaleReview) {
		t.Fatalf("held card accepted another vote: %v", err)
	}
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	if report := reopened.shuffleReview(day); !report.Waiting || report.Current.Key != first.Key {
		t.Fatalf("restart did not restore the held card: %+v", report)
	}
	file, err := reopened.transferBytes(date)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := parseTransfer(file)
	if err != nil {
		t.Fatal(err)
	}
	dest, _ := newTestStore(t)
	if err := dest.restore(payload.Database); err != nil {
		t.Fatal(err)
	}
	if report := dest.review(day); !report.Waiting || report.Current.Key != first.Key || dest.attendance(day).votes != 1 {
		t.Fatalf("transfer did not resume the hold: %+v", report)
	}
	if report, err := dest.undo(day); err != nil || report.Waiting || report.Current.Key != first.Key ||
		report.Current.Reviews != 0 || dest.attendance(day).votes != 0 {
		t.Fatalf("undo did not restore the unanswered card: %+v (%v)", report, err)
	}
	if report, err := reopened.nextAfterForgot(day, card.Order); err != nil || report.Waiting ||
		report.Current.Key == first.Key || reopened.attendance(day).votes != 1 {
		t.Fatalf("next should only advance the display: %+v (%v)", report, err)
	}
	if _, err := reopened.nextAfterForgot(day, card.Order); !errors.Is(err, errStaleReview) {
		t.Fatalf("repeated next should be rejected: %v", err)
	}
	tomorrow := date.Add(24 * time.Hour)
	if s.review(tomorrow.Format(time.DateOnly)).Waiting {
		t.Fatal("yesterday's hold surfaced on a new day")
	}
	file, err = s.transferBytes(tomorrow.Add(24 * time.Hour))
	if err != nil || bytes.Contains(file, []byte("waitingForNext")) {
		t.Fatal("expired hold leaked into a new day's transfer")
	}
}
