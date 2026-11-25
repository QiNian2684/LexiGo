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

func TestRollingRecallRanksSourceDayMissesAndRetriesOnFollowingDay(t *testing.T) {
	const first = "2026-11-01"
	const day = "2026-11-06"
	s, path := newTestStore(t)
	created := importTest(t, s, first, "apple,banana,lemon,pear,peach,mango,grape,plum,cherry,lime,melon,fig")
	for i, key := range created.Keys {
		card := s.db.Words[key]
		before := card.progress
		card.Mastered, card.Streak, card.Reviews = true, 6, 20+i*5
		card.Forgot = 5
		for range 1 + i%3 {
			s.db.Events = append(s.db.Events, event{Kind: "no", Day: first, Card: card.ID, Word: card.Word, Before: &before})
		}
		// Older mistakes must not improve this word's ranking.
		if i == 9 {
			for range 12 {
				s.db.Events = append(s.db.Events, event{Kind: "no", Day: "2026-10-30", Card: card.ID, Word: card.Word, Before: &before})
			}
		}
	}
	s.db.Words[created.Keys[1]].Deleted = true
	// Import keys are in input order. Earlier IDs break ties.
	worst := s.db.Words[created.Keys[11]]
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
		slices.Contains(ids, s.db.Words[created.Keys[9]].ID) {
		t.Fatalf("incorrect recall batch: %v", ids)
	}
	if next := s.shuffleReview(day); next.RecallDue != 10 || next.Planned != 0 || !next.Current.Recall {
		t.Fatalf("invalid separate recall queue: %+v", next)
	}
	current := s.review(day).Current
	card := s.db.Words[current.Key]
	original := card.progress
	hold, err := s.recordVote(day, card.Key, card.Reviews, card.Order, false, true, false)
	if err != nil || !hold.Waiting || !hold.Current.Recall || hold.RecallDue != 10 || !hold.Current.RecallRetesting || hold.TaskDone != 0 {
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
	for range 9 {
		if state := nextVote(t, reopened, day, false); state.Due != 9 || state.RecallDue != 9 || state.TaskDone != 1 || state.TaskTotal != 10 {
			t.Fatalf("a missed word must remain an unfinished task: %+v", state)
		}
	}
	for i := range 27 {
		result := nextVote(t, reopened, day, true)
		if i == 26 && result.TaskDone != 10 {
			t.Fatalf("third correct answer did not complete all today's recalls: %+v", result)
		}
		if result.TaskTotal != 10 || result.TaskCompleted != (i == 26) {
			t.Fatalf("recall changed today's target or completion: %+v", result)
		}
	}
	if err := reopened.refreshRecall("2026-11-07"); err != nil || reopened.review("2026-11-07").RecallDue != 9 {
		t.Fatalf("failed recalls must come back the next day: %v", err)
	}
	for i := 8; i >= 0; i-- {
		if next := nextVote(t, reopened, "2026-11-07", true); next.RecallDue != i {
			t.Fatalf("remembering once must clear only the answered recall: %+v", next)
		}
	}
	if err := reopened.refreshRecall("2026-11-11"); err != nil || reopened.review("2026-11-11").RecallDue != 0 {
		t.Fatalf("recall misses must not generate another five-day batch: %v", err)
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
	if state.Due != 1 || state.RecallDue != 1 || state.TaskTotal != 1 || state.Current.Recall || !state.Current.RecallPending {
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

func TestRecallRetestResetUndoAndTransfer(t *testing.T) {
	s, path := newTestStore(t)
	const first, day = "2026-11-01", "2026-11-06"
	importTest(t, s, first, "crucial")
	nextVote(t, s, first, false)
	card := s.db.Words["crucial"]
	card.Mastered, card.Streak = true, 6
	card.Reviews = 8
	if err := s.save([]string{card.Key}, len(s.db.Events)); err != nil {
		t.Fatal(err)
	}
	if err := s.refreshRecall(day); err != nil {
		t.Fatal(err)
	}
	for _, vote := range []struct {
		yes   bool
		count int
	}{{false, 0}, {true, 1}, {true, 2}, {false, 0}, {true, 1}, {true, 2}} {
		got := nextVote(t, s, day, vote.yes)
		if got.Current == nil || !got.Current.RecallRetesting || got.Current.RecallCount != vote.count ||
			got.TaskDone != 0 || got.TaskTotal != 1 || got.DayPassed || got.TaskCompleted {
			t.Fatalf("recall after %+v: %+v / %+v", vote, got, got.Current)
		}
	}
	if final := nextVote(t, s, day, true); !final.TaskCompleted || final.TaskDone != 1 || final.Due != 0 || final.Remembered != 0 {
		t.Fatalf("completed recall should count once without ordinary credit: %+v", final)
	}
	undone, err := s.undo(day)
	if err != nil || undone.TaskDone != 0 || undone.TaskTotal != 1 || undone.Current == nil || undone.Current.RecallCount != 2 {
		t.Fatalf("undo restored the wrong recall progress: %+v / %+v (%v)", undone, undone.Current, err)
	}
	s.pinReview(undone)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	file, err := s.transferBytes(now)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := parseTransfer(file)
	if err != nil {
		t.Fatal(err)
	}
	migrated := &store{db: copy.Database}
	migratedState := migrated.review(day)
	if len(copy.Task.Pending) != 1 || !copy.Task.CurrentRecall || migratedState.Current == nil || migratedState.Current.RecallCount != 2 {
		t.Fatalf("transfer lost recall progress: %+v / %+v", copy.Task, migratedState.Current)
	}
	s.io.Close()
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	if final := nextVote(t, reopened, day, true); !final.TaskCompleted || final.TaskDone != 1 {
		t.Fatalf("reopening did not preserve the two prior correct answers: %+v", final)
	}
	importTest(t, reopened, day, "definite,advantage")
	if got := reopened.review(day); got.TaskDone != 1 || got.TaskTotal != 3 || got.Due != 2 {
		t.Fatalf("adding new words changed completed recall progress: %+v", got)
	}
	if card := reopened.db.Words["crucial"]; !card.Mastered || card.Streak != 6 {
		t.Fatalf("recall changed mastery: %+v", card.progress)
	}
}

func TestSharedRecallAndOrdinaryRetestGoals(t *testing.T) {
	for _, goal := range []int{1, 3, 5} {
		s, _ := newTestStore(t)
		const first, day = "2026-11-01", "2026-11-06"
		importTest(t, s, first, "crucial")
		nextVote(t, s, first, false)
		options := s.db.Settings
		options.DailyGoal = goal
		if _, err := s.changeSettings(day, options); err != nil {
			t.Fatal(err)
		}
		if err := s.refreshRecall(day); err != nil {
			t.Fatal(err)
		}
		nextVote(t, s, day, false)
		for i := 1; i <= max(goal, recallRetestGoal); i++ {
			current := s.review(day).Current
			if current == nil || current.Recall != (i > goal) || current.RecallPending != (i <= recallRetestGoal) {
				t.Fatalf("goal %d, answer %d: wrong pending requirements %+v", goal, i, current)
			}
			got := nextVote(t, s, day, true)
			done := i == max(goal, recallRetestGoal)
			if got.TaskTotal != 1 || (got.TaskDone == 1) != done || got.TaskCompleted != done {
				t.Fatalf("goal %d, answer %d: %+v", goal, i, got)
			}
		}
		if s.db.Words["crucial"].Streak != 1 {
			t.Fatal("shared answers must credit just one ordinary study day")
		}
		if err := s.refreshRecall("2026-11-07"); err != nil || s.review("2026-11-07").RecallDue != 1 {
			t.Fatalf("shared first miss lost its carry: %v", err)
		}
	}
}

func TestDirectPassPreservesOngoingRecall(t *testing.T) {
	s, _ := newTestStore(t)
	const first, day = "2026-11-01", "2026-11-06"
	importTest(t, s, first, "crucial")
	nextVote(t, s, first, false)
	if err := s.refreshRecall(day); err != nil {
		t.Fatal(err)
	}
	nextVote(t, s, day, false)
	nextVote(t, s, day, true)
	current := s.review(day).Current
	got, err := s.recordVote(day, current.Key, current.Reviews, current.Order, true, false, true)
	if err != nil || !got.DayPassed || got.TaskDone != 0 || !got.Current.Recall || got.Current.RecallCount != 1 || got.Due != 1 {
		t.Fatalf("direct pass changed recall progress: %+v / %+v (%v)", got, got.Current, err)
	}
	nextVote(t, s, day, true)
	if last := nextVote(t, s, day, true); !last.TaskCompleted {
		t.Fatalf("two real checks should finish the task: %+v", last)
	}
}

func TestUpgradeLegacyRecallKeepsCurrentDayCompletion(t *testing.T) {
	s, _ := newTestStore(t)
	const first, day = "2026-11-01", "2026-11-06"
	importTest(t, s, first, "apple")
	nextVote(t, s, first, false)
	card := s.db.Words["apple"]
	card.Mastered, card.Streak, card.Reviews = true, 6, 8
	s.db.RecallBatch = &recallBatch{Day: day, Rolling: true, Cards: []uint64{card.ID}}
	if got := nextVote(t, s, day, false); got.Due != 0 {
		t.Fatalf("legacy behavior should check a word only once: %+v", got)
	}
	if err := s.refreshRecall(day); err != nil {
		t.Fatal(err)
	}
	if got := s.review(day); got.TaskDone != 1 || got.TaskTotal != 1 || got.Due != 0 {
		t.Fatalf("updating a live batch reopened an already handled word: %+v", got)
	}
	if err := s.refreshRecall("2026-11-07"); err != nil || s.review("2026-11-07").RecallDue != 1 {
		t.Fatalf("legacy miss lost its carry: %v", err)
	}
}

func TestRecallRetestUsesOrdinarySpacing(t *testing.T) {
	s, _ := newTestStore(t)
	const first, day = "2026-11-01", "2026-11-06"
	importTest(t, s, first, "apple")
	nextVote(t, s, first, false)
	apple := s.db.Words["apple"]
	apple.Mastered, apple.Streak, apple.Reviews = true, 6, 8
	options := s.db.Settings
	options.DailyGoal = 1
	if _, err := s.changeSettings(day, options); err != nil {
		t.Fatal(err)
	}
	newWords := importTest(t, s, day, "banana,lemon,pear,peach,mango,grape,plum,cherry,lime,melon,fig,kiwi,orange,papaya,pineapple,berry,apricot,tangerine,guava,pomegranate")
	if err := s.refreshRecall(day); err != nil {
		t.Fatal(err)
	}
	ranks := map[uint64]int{apple.ID: 1}
	for i, key := range newWords.Keys {
		ranks[s.db.Words[key].ID] = i + 2
	}
	s.display = reviewDisplay{day: day, ranks: ranks}
	if state := s.review(day); state.Current.Key != apple.Key || state.TaskTotal != len(newWords.Keys)+1 {
		t.Fatalf("unexpected starting queue: %+v", state)
	}
	nextVote(t, s, day, false)
	for i := 0; i <= repeatGapDeadline; i++ {
		if state := s.review(day); state.Current.Key == apple.Key {
			if i < minTargetGap || i > maxTargetGap || state.Current.RecallCount != 0 || !state.Current.RecallRetesting {
				t.Fatalf("wrong first retest after %d different cards: %+v", i, state.Current)
			}
			return
		}
		nextVote(t, s, day, true)
	}
	t.Fatal("recall retest did not return at the existing interval")
}
