package main

import (
	"encoding/json"
	"testing"
	"time"
)

func enableFirstTryMastery(t *testing.T, s *store, day string) {
	t.Helper()
	prefs := s.db.Settings
	prefs.FirstTryMastery = true
	if _, err := s.changeSettings(day, prefs); err != nil {
		t.Fatal(err)
	}
}

func TestFirstTryMasteryForPreviouslyUnseenWord(t *testing.T) {
	s, _ := newTestStore(t)
	const added, day = "2026-11-24", "2026-11-26"
	importTest(t, s, added, "crucial")

	if s.review(day).Settings.FirstTryMastery {
		t.Fatal("new databases must default to the existing learning rules")
	}
	enableFirstTryMastery(t, s, day)
	if got := nextVote(t, s, day, true); !got.Graduated || !got.DayPassed || !got.Credited ||
		!got.TaskCompleted || got.DailyTarget != 1 || got.TaskDone != 1 || got.TaskTotal != 1 ||
		got.Due != 0 || got.Mastered != 1 {
		t.Fatalf("first correct answer did not finish the word and today's task: %+v", got)
	}
	card := s.db.Words["crucial"]
	if card.Streak != 1 || card.Reviews != 1 || !s.db.Events[len(s.db.Events)-1].FirstTryMastered {
		t.Fatalf("expected one real answer and one credited day: %+v", card.progress)
	}
	stats := s.stats(day)
	if today := stats.Days[len(stats.Days)-1]; today.Yes != 1 || today.Passed != 1 || today.Distinct != 1 ||
		stats.TotalReviews != 1 || stats.Mastered != 1 {
		t.Fatalf("first-try mastery counted unearned days or answers: %+v", today)
	}
	if next := s.review("2026-11-27"); next.Due != 0 || next.Mastered != 1 {
		t.Fatalf("the mastered word was scheduled again: %+v", next)
	}
}

func TestFirstMissStillRequiresTheNormalLearningCycle(t *testing.T) {
	s, _ := newTestStore(t)
	const first, second, third, fourth = "2026-11-25", "2026-11-26", "2026-11-27", "2026-11-28"
	importTest(t, s, first, "crucial")
	options := s.db.Settings
	options.Goal, options.DailyGoal, options.FirstTryMastery = 3, 2, true
	if _, err := s.changeSettings(first, options); err != nil {
		t.Fatal(err)
	}
	if got := nextVote(t, s, first, false); got.DayPassed || got.DailyTarget != 2 {
		t.Fatalf("a miss must start the original daily target: %+v", got)
	}
	if got := passWord(t, s, first, 2); got.Graduated || got.Mastered != 0 {
		t.Fatalf("a correct retest was treated as a first answer: %+v", got)
	}
	if got := passWord(t, s, second, 1); got.Graduated || got.Mastered != 0 || s.db.Words["crucial"].Streak != 2 {
		t.Fatalf("first answer of a later day bypassed the long-term goal: %+v", got)
	}
	nextVote(t, s, third, false)
	if got := passWord(t, s, third, 2); got.Graduated || got.Mastered != 0 {
		t.Fatalf("missing the first answer on the threshold day should postpone mastery: %+v", got)
	}
	if got := passWord(t, s, fourth, 1); !got.Graduated || s.db.Words["crucial"].Streak != 4 {
		t.Fatalf("normal mastery was not awarded on the next eligible day: %+v", got)
	}
}

func TestFirstTryMasteryFollowsCurrentCycleAfterReset(t *testing.T) {
	const day = "2026-11-25"
	for _, method := range []string{"single", "checked", "all", "reimport"} {
		t.Run(method, func(t *testing.T) {
			s, _ := newTestStore(t)
			importTest(t, s, day, "crucial, apple")
			s.db.Words["crucial"].PersonalNote = "Keep this note"
			enableFirstTryMastery(t, s, day)
			nextVote(t, s, day, true)
			nextVote(t, s, day, false)
			nextVote(t, s, day, true) // Merely a retest for apple.
			apply := selection{Keys: []string{"crucial"}}
			switch method {
			case "checked", "all":
				apply = selection{Keys: []string{"crucial", "apple"}, All: method == "all"}
			case "reimport":
				importTest(t, s, day, "crucial, apple")
			}
			if method != "reimport" {
				count, err := s.batch(day, apply, "reset")
				want := 2
				if method == "single" {
					want = 1
				}
				if err != nil || count != want {
					t.Fatalf("reset %s selected the wrong words: %d, %v", method, count, err)
				}
			}
			// The active ordinary retry may come before the freshly reset word.
			for rounds := 0; rounds < 8 && s.review(day).Current != nil; rounds++ {
				key := s.review(day).Current.Key
				vote := nextVote(t, s, day, true)
				card := s.db.Words[key]
				if key == "crucial" || method != "single" {
					if !vote.Graduated || card.Streak != 1 || !card.Mastered {
						t.Fatalf("reset %s did not give %s a fresh cycle: %+v, %+v", method, key, vote, card.progress)
					}
				} else if vote.Graduated {
					t.Fatalf("single reset also changed apple's existing cycle: %+v", vote)
				}
			}
			if open := s.review(day); open.Due != 0 || open.TaskDone != 2 {
				t.Fatalf("reset %s left the queue incomplete: %+v", method, open)
			}
			if note := s.db.Words["crucial"].PersonalNote; note != "Keep this note" {
				t.Fatalf("reset lost the user's note: %q", note)
			}
			if card := s.db.Words["apple"]; card.Forgot != 1 || card.Reviews < 3 {
				t.Fatalf("reset discarded recorded answers: %+v", card.progress)
			}
			stats := s.stats(day)
			if today := stats.Days[len(stats.Days)-1]; today.Passed != 2 {
				t.Fatalf("same-day reset must count each word once in statistics: %+v", today)
			}
		})
	}
}

func TestFirstTryMasterySwitchAndDirectDailyPass(t *testing.T) {
	s, _ := newTestStore(t)
	const day, nextDay = "2026-11-25", "2026-11-26"
	importTest(t, s, day, "crucial, apple, pear")
	first := s.review(day).Current.Key
	if got := nextVote(t, s, day, true); got.DayPassed || got.Graduated || got.DailyCount != 1 {
		t.Fatalf("default must retain the existing first answer behavior: %+v", got)
	}
	enableFirstTryMastery(t, s, day)
	present := s.review(day).Current
	got, err := s.recordVote(day, present.Key, present.Reviews, present.Order, true, false, true)
	if err != nil || !got.DayPassed || got.Graduated {
		t.Fatalf("the direct daily pass remains separate from first-try mastery: %+v (%v)", got, err)
	}
	if quick := nextVote(t, s, day, true); !quick.Graduated {
		t.Fatalf("enabling the option did not affect an unanswered word: %+v", quick)
	}
	if current := s.review(day).Current; current.Key != first || current.DailyCount != 1 {
		t.Fatalf("enabling the option changed a learning cycle already started: %+v", current)
	}
	for range 2 {
		if got := nextVote(t, s, day, true); got.Graduated {
			t.Fatalf("a later answer jumped to mastery: %+v", got)
		}
	}
	options := s.db.Settings
	options.FirstTryMastery = false
	if got, err := s.changeSettings(nextDay, options); err != nil || got.Mastered != 1 {
		t.Fatalf("disabling the option revoked mastery: %+v (%v)", got, err)
	}
	importTest(t, s, nextDay, "banana")
	if got := nextVote(t, s, nextDay, true); got.DayPassed || got.Graduated || got.DailyCount != 1 {
		t.Fatalf("disabled option should keep the normal target for a newly added word: %+v", got)
	}
}

func TestFirstTryMasteryUndoRestartAndTransfer(t *testing.T) {
	s, path := newTestStore(t)
	const day = "2026-11-25"
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	importTest(t, s, day, "crucial")
	oldFile, err := s.transferBytes(now)
	if err != nil {
		t.Fatal(err)
	}
	var envelope transferEnvelope
	if err := json.Unmarshal(oldFile, &envelope); err != nil || envelope.Version != 5 {
		t.Fatalf("data without the new option should still use the 3.5.2 format: %d (%v)", envelope.Version, err)
	}
	parsed, err := parseTransfer(oldFile)
	if err != nil || parsed.Database.Settings.FirstTryMastery {
		t.Fatalf("reading an older transfer changed the default: %v", err)
	}

	enableFirstTryMastery(t, s, day)
	before := s.db.Words["crucial"].progress
	current := s.review(day).Current
	held, err := s.recordVote(day, current.Key, current.Reviews, current.Order, false, true, false)
	if err != nil || !held.Waiting {
		t.Fatalf("wrong answer should keep the card for feedback: %+v (%v)", held, err)
	}
	if back, err := s.undo(day); err != nil || back.Waiting || s.db.Words["crucial"].progress != before {
		t.Fatalf("undo of the first miss failed: %+v (%v)", back, err)
	}
	if got := nextVote(t, s, day, true); !got.Graduated || !got.TaskCompleted {
		t.Fatalf("undo of the first miss did not restore eligibility: %+v", got)
	}

	s.io.Close()
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	if state := reopened.review(day); !state.Settings.FirstTryMastery || state.Mastered != 1 || state.TaskDone != 1 {
		t.Fatalf("restarting lost the setting or mastery: %+v", state)
	}
	file, err := reopened.transferBytes(now)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(file, &envelope); err != nil || envelope.Version != transferVersionWithFirstTry {
		t.Fatalf("new transfer features require their format version: %d (%v)", envelope.Version, err)
	}
	snapshot, err := parseTransfer(file)
	if err != nil {
		t.Fatal(err)
	}
	copy, _ := newTestStore(t)
	if err := copy.restore(snapshot.Database); err != nil {
		t.Fatal(err)
	}
	if state := copy.review(day); !state.Settings.FirstTryMastery || state.Mastered != 1 || state.TaskDone != 1 {
		t.Fatalf("transfer lost the setting or mastery: %+v", state)
	}
	if back, err := copy.undo(day); err != nil || back.TaskDone != 0 || back.TaskTotal != 1 || back.Mastered != 0 ||
		copy.db.Words["crucial"].progress != before {
		t.Fatalf("undo of transferred mastery failed to restore the original task: %+v (%v)", back, err)
	}
	if got := nextVote(t, copy, day, true); !got.Graduated || !got.TaskCompleted {
		t.Fatalf("undo of mastery did not restore eligibility: %+v", got)
	}
}

func TestFirstTryMasteryLeavesIndependentRecallWorkIntact(t *testing.T) {
	s, _ := newTestStore(t)
	const earlier, day = "2026-11-24", "2026-11-25"
	words := importTest(t, s, earlier, "orange, apple")
	options := s.db.Settings
	options.FirstTryMastery, options.Limit = true, 1
	if _, err := s.changeSettings(day, options); err != nil {
		t.Fatal(err)
	}
	card := s.db.Words[words.Keys[1]]
	s.db.RecallBatch = &recallBatch{Day: day, Cards: []uint64{card.ID}, Rolling: true, Retest: true}
	s.display = reviewDisplay{day: day, ranks: map[uint64]int{card.ID: 1}}
	if face := s.review(day).Current; face.Key != card.Key || !face.Recall {
		t.Fatalf("fixture must first show an independent recall: %+v", face)
	}
	nextVote(t, s, day, false)
	if card.DailyDay != "" || card.Mastered {
		t.Fatalf("standalone recall altered ordinary learning: %+v", card.progress)
	}
	options.Limit = 0
	if _, err := s.changeSettings(day, options); err != nil {
		t.Fatal(err)
	}
	first := s.review(day).Current
	if first.Key != card.Key || first.Recall || !first.RecallRetesting {
		t.Fatalf("expected a shared first ordinary answer and recall retest: %+v", first)
	}
	if got := nextVote(t, s, day, true); !got.Graduated || got.RecallDue != 1 || got.TaskDone != 0 || got.TaskTotal != 2 {
		t.Fatalf("ordinary mastery should still leave the recall requirements pending: %+v", got)
	}
	if got := nextVote(t, s, day, true); !got.Graduated || got.RecallDue != 1 || got.TaskDone != 1 {
		t.Fatalf("other ordinary work should complete before recall repeats: %+v", got)
	}
	nextVote(t, s, day, true)
	if last := nextVote(t, s, day, true); last.Graduated || !last.TaskCompleted || last.RecallDue != 0 || card.Streak != 1 {
		t.Fatalf("last real recall should finish the task without a second ordinary credit: %+v", last)
	}
	if err := s.refreshRecall("2026-11-26"); err != nil || s.review("2026-11-26").RecallDue != 1 {
		t.Fatalf("mastery incorrectly cleared the next-day recall: %v", err)
	}
}
