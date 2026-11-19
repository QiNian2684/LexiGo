package main

import (
	"errors"
	"testing"
)

func TestPassTodayCompletesDailyGoalWithOneUndoableReview(t *testing.T) {
	const day = "2026-11-19"
	s, path := newTestStore(t)
	importTest(t, s, day, "crucial")
	first := s.shuffleReview(day).Current
	result, err := s.recordVote(day, first.Key, first.Reviews, first.Order, true, false, true)
	if err != nil || !result.DayPassed || !result.Credited || result.Graduated ||
		result.DailyCount != defaultDailyGoal || result.Due != 0 ||
		result.Remembered != 1 || result.Current != nil || !result.CanUndo {
		t.Fatalf("direct pass: %+v (%v)", result, err)
	}
	if card := s.db.Words[first.Key]; card.Reviews != 1 || card.Streak != 1 || card.RepeatAt != 0 {
		t.Fatalf("expected one review and credit, with no remaining retry: %+v", card.progress)
	}
	if _, err := s.recordVote(day, first.Key, first.Reviews, first.Order, true, false, true); !errors.Is(err, errStaleReview) {
		t.Fatalf("duplicate direct pass accepted: %v", err)
	}
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	back, err := reopened.undo(day)
	if err != nil || back.Current == nil || back.Current.Key != first.Key || back.Current.Reviews != 0 ||
		back.Current.DailyCount != 0 || back.Current.Streak != 0 || back.Remembered != 0 || back.Due != 1 {
		t.Fatalf("undo did not restore the original review: %+v (%v)", back, err)
	}
}

func TestPassTodayOnForgottenCardCanRestoreHeldAnswer(t *testing.T) {
	const day = "2026-11-19"
	s, _ := newTestStore(t)
	importTest(t, s, day, "crucial")
	first := s.shuffleReview(day).Current
	hold, err := s.recordVote(day, first.Key, first.Reviews, first.Order, false, true, false)
	if err != nil || !hold.Waiting || hold.Current == nil {
		t.Fatalf("forget answer not held: %+v (%v)", hold, err)
	}
	card := hold.Current
	pass, err := s.recordVote(day, card.Key, card.Reviews, card.Order, true, false, true)
	if err != nil || pass.Waiting || pass.Due != 0 || !pass.DayPassed {
		t.Fatalf("direct pass did not clear held review: %+v (%v)", pass, err)
	}
	back, err := s.undo(day)
	if err != nil || !back.Waiting || back.Current == nil || *back.Current != *card || back.Remembered != 0 {
		t.Fatalf("undo did not restore the held wrong answer: %+v (%v)", back, err)
	}
}
