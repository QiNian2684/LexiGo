package main

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestOldLearningHistoryRestoresLostPassDays(t *testing.T) {
	s, path := newTestStore(t)
	importTest(t, s, "2026-11-01", "apple")
	passWord(t, s, "2026-11-01", 3)
	nextVote(t, s, "2026-11-03", false)
	passWord(t, s, "2026-11-05", 1)
	card := s.db.Words["apple"]
	s.db.CumulativePasses = false
	card.Streak = 1 // Simulate the pre-2.2.3 counter that restarted after a failed day.
	previous := s.db.Events[len(s.db.Events)-1].Before
	previous.Streak = 0
	if err := s.save([]string{card.Key}, 0); err != nil {
		t.Fatal(err)
	}
	s.io.Close()
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	if got := reopened.db.Words["apple"].Streak; got != 2 {
		t.Fatalf("expected two historical pass days, got %d", got)
	}
	rolledBack, err := reopened.undo("2026-11-05")
	if err != nil || rolledBack.Current == nil || rolledBack.Current.Streak != 1 {
		t.Fatalf("undo with migrated snapshot: %+v (%v)", rolledBack, err)
	}
	nextVote(t, reopened, "2026-11-05", true)
	options := reopened.db.Settings
	options.Goal = 2
	state, err := reopened.changeSettings("2026-11-06", options)
	if err != nil || state.Due != 0 || state.Mastered != 1 {
		t.Fatalf("lowering goal did not immediately recognize two separate pass days: %+v (%v)", state, err)
	}
}

func TestFiveDayRecallUsesWrongRateAndShowsEachCardOnce(t *testing.T) {
	const first = "2026-11-01"
	const day = "2026-11-06"
	s, path := newTestStore(t)
	created := importTest(t, s, first, "apple,banana,lemon,pear,peach,mango,grape,plum,cherry,lime,melon,fig")
	nextVote(t, s, first, false) // Set the first practice date.
	for i, key := range created.Keys {
		card := s.db.Words[key]
		card.Mastered, card.Streak, card.Reviews = true, 6, 20+i*5
		card.Forgot = 5
	}
	worst := s.db.Words[created.Keys[0]]
	s.db.Words[created.Keys[1]].Deleted = true
	worst.Reviews, worst.Forgot = 25, 13
	if err := s.save(created.Keys, len(s.db.Events)); err != nil {
		t.Fatal(err)
	}
	if err := s.refreshRecall("2026-11-05"); err != nil || s.review("2026-11-05").Due != 0 {
		t.Fatalf("review batch appeared before five days: %v", err)
	}
	if err := s.refreshRecall(day); err != nil {
		t.Fatal(err)
	}
	ids := s.db.RecallBatch.Cards
	if len(ids) != 10 || !slices.Contains(ids, worst.ID) ||
		slices.Contains(ids, s.db.Words[created.Keys[1]].ID) ||
		slices.Contains(ids, s.db.Words[created.Keys[11]].ID) {
		t.Fatalf("incorrect recall batch: %v", ids)
	}
	if next := s.shuffleReview(day); next.RecallDue != 10 || next.Planned != 0 || !next.Current.Recall {
		t.Fatalf("invalid separate recall queue: %+v", next)
	}
	current := s.review(day).Current
	card := s.db.Words[current.Key]
	original := card.progress
	hold, err := s.recordVote(day, card.Key, card.Reviews, card.Order, false, true, false)
	if err != nil || !hold.Waiting || !hold.Current.Recall || hold.RecallDue != 9 {
		t.Fatalf("recall wrong answer: %+v (%v)", hold, err)
	}
	if card.Forgot != original.Forgot+1 || card.Streak != original.Streak || !card.Mastered {
		t.Fatalf("recall wrong answer adjusted mastery or failed to record the mistake: %+v", card.progress)
	}
	afterUndo, err := s.undo(day)
	if err != nil || afterUndo.Waiting || afterUndo.RecallDue != 10 || afterUndo.Current.Key != current.Key || card.progress != original {
		t.Fatalf("undo did not requeue the recall: %+v (%v)", afterUndo, err)
	}
	if result := nextVote(t, s, day, true); result.DayPassed || result.Credited || result.RecallDue != 9 {
		t.Fatalf("recall credited a pass or stayed due: %+v", result)
	}
	today := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	s.pinReview(s.review(day))
	file, err := s.transferBytes(today)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := parseTransfer(file)
	if err != nil || !payload.Task.CurrentRecall || len(payload.Task.Pending) != 9 {
		t.Fatalf("transfer lost the recall queue or pin: %+v (%v)", payload.Task, err)
	}
	s.io.Close()
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	if err := reopened.refreshRecall(day); err != nil || !slices.Equal(reopened.db.RecallBatch.Cards, ids) {
		t.Fatalf("recall batch was regenerated on restart: %v", err)
	}
	for i := 8; i >= 0; i-- {
		if state := nextVote(t, reopened, day, false); state.Due != i || state.RecallDue != i {
			t.Fatalf("recall cards should be asked once per batch: %+v", state)
		}
	}
	if err := reopened.refreshRecall("2026-11-11"); err != nil || reopened.review("2026-11-11").RecallDue != 10 {
		t.Fatalf("recall was not scheduled five days later: %v", err)
	}
}

func TestRecallDoesNotDuplicateOrdinaryCard(t *testing.T) {
	s, _ := newTestStore(t)
	const first, day = "2026-11-01", "2026-11-06"
	importTest(t, s, first, "crucial")
	nextVote(t, s, first, false)
	if err := s.refreshRecall(day); err != nil {
		t.Fatal(err)
	}
	state := s.review(day)
	if state.Due != 1 || state.RecallDue != 0 || state.Current.Recall {
		t.Fatalf("ordinary card duplicated in recall queue: %+v", state)
	}
	card := state.Current
	if pass, err := s.recordVote(day, card.Key, card.Reviews, card.Order, true, false, true); err != nil || pass.Due != 0 {
		t.Fatalf("ordinary answer did not satisfy recall: %+v (%v)", pass, err)
	}
	if _, err := s.recordVote(day, card.Key, card.Reviews, card.Order, true, false, true); !errors.Is(err, errStaleReview) {
		t.Fatalf("replayed answer was not rejected: %v", err)
	}
}
