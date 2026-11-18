package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func passOne(t *testing.T, s *store, day string) voteResult {
	t.Helper()
	for i := 1; i <= 3; i++ {
		result := nextVote(t, s, day, true)
		if result.DailyCount != i || result.DailyTarget != 3 || result.DayPassed != (i == 3) {
			t.Fatalf("answer %d: %+v", i, result)
		}
		if i < 3 && (result.Credited || result.Remembered != 0 || result.Due != 1) {
			t.Fatalf("credited before daily pass: %+v", result)
		}
		if i == 3 {
			return result
		}
	}
	panic("three responses did not complete")
}

func TestThreePerDaySixLearningDays(t *testing.T) {
	s, _ := newTestStore(t)
	importTest(t, s, "2026-11-01", "crucial")
	days := []string{"2026-11-01", "2026-11-02", "2026-11-05", "2026-11-08", "2026-11-10", "2026-11-16"}
	for i, day := range days {
		state := s.review(day)
		if state.Current.DailyCount != 0 || state.Current.Streak != i {
			t.Fatalf("gap or new learning day: %+v", state)
		}
		result := passOne(t, s, day)
		card := s.db.Words["crucial"]
		if !result.Credited || result.Due != 0 || result.Remembered != 1 || result.Reviewed != 1 ||
			card.Streak != i+1 || card.Mastered != (i == 5) {
			t.Fatalf("day %s: %+v card=%+v", day, result, card)
		}
		if _, err := s.vote(day, card.Key, card.Reviews, card.Order, true); err != errStaleReview {
			t.Fatal("a passed word was accepted again")
		}
	}
	if state := s.review("2026-11-20"); state.Due != 0 || state.Mastered != 1 {
		t.Fatalf("mastered card reappeared: %+v", state)
	}
}

func TestWrongAnswerAndRollover(t *testing.T) {
	s, path := newTestStore(t)
	importTest(t, s, "2026-11-01", "apple")
	passOne(t, s, "2026-11-01")
	const day = "2026-11-07"
	nextVote(t, s, day, true)
	nextVote(t, s, day, true)
	wrong := nextVote(t, s, day, false)
	if wrong.DailyCount != 0 || wrong.Current.Streak != 1 || wrong.Current.DailyCount != 0 {
		t.Fatalf("wrong answer touched day streak or failed to reset local count: %+v", wrong)
	}
	nextVote(t, s, day, true)
	s.io.Close()
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	if r := reopened.review(day); r.Current.DailyCount != 1 || r.Current.Streak != 1 {
		t.Fatalf("same-day reopen lost progress: %+v", r)
	}
	nextVote(t, reopened, day, true)
	if r := nextVote(t, reopened, day, true); !r.Credited || reopened.db.Words["apple"].Streak != 2 {
		t.Fatalf("wrong answer blocked a later legitimate pass: %+v", r)
	}
	nextVote(t, reopened, "2026-11-10", true)
	if state := reopened.review("2026-11-16"); state.Current.Streak != 0 || state.Current.DailyCount != 0 {
		t.Fatalf("an unfinished day did not interrupt consecutive passes: %+v", state)
	}
	if r := passOne(t, reopened, "2026-11-16"); !r.Credited || reopened.db.Words["apple"].Streak != 1 {
		t.Fatalf("did not restart day streak: %+v", r)
	}
}

func TestFortyWordsThreeRounds(t *testing.T) {
	s, _ := newTestStore(t)
	const day = "2026-11-16"
	options := s.db.Settings
	options.Limit = 40
	if _, err := s.changeSettings(day, options); err != nil {
		t.Fatal(err)
	}
	var input string
	for i := 0; i < 45; i++ {
		input += fmt.Sprintf("schedule%c%c,", 'a'+rune(i/26), 'a'+rune(i%26))
	}
	importTest(t, s, "2026-11-15", input)
	state := s.shuffleReview(day)
	seen := map[string]int{}
	recent := []string{}
	previous := ""
	for i := 0; i < 120; i++ {
		if state.Current == nil || state.Planned != 40 || state.Reviewed > 40 {
			t.Fatalf("attempt %d: %+v", i, state)
		}
		key := state.Current.Key
		if state.Current.DailyCount != seen[key] || (previous == key && state.Due > 1) {
			t.Fatalf("wrong interleaving or repeated card: %q at %d", key, i)
		}
		seen[key]++
		recent = append(recent, key)
		if len(recent) >= 3 && recent[len(recent)-3] == key && state.Due > 3 {
			t.Fatalf("excessive repetition: %v", recent[len(recent)-3:])
		}
		previous = key
		result := nextVote(t, s, day, true)
		if result.DayPassed != (seen[key] == 3) {
			t.Fatalf("wrong pass for %q: %+v", key, result)
		}
		state = result.reviewState
		if i == 24 {
			state = s.shuffleReview(day)
		}
	}
	if state.Due != 0 || state.Remembered != 40 || state.Reviewed != 40 || len(seen) != 40 || state.OverflowNew != 0 {
		t.Fatalf("wrong final progress: %+v", state)
	}
}

func TestThresholdChangeAndManualReset(t *testing.T) {
	s, _ := newTestStore(t)
	const day = "2026-11-16"
	importTest(t, s, day, "crucial")
	nextVote(t, s, day, true)
	options := s.db.Settings
	options.DailyGoal, options.Goal = 2, 5
	if _, err := s.changeSettings(day, options); err != nil {
		t.Fatal(err)
	}
	if r := nextVote(t, s, day, true); r.DailyTarget != 2 || !r.DayPassed || !r.Credited {
		t.Fatalf("daily pass missing: %+v", r)
	}
	if _, err := s.batch(day, selection{Keys: []string{"crucial"}}, "reset"); err != nil {
		t.Fatal(err)
	}
	if p := s.db.Words["crucial"]; p.Streak != 0 || p.DailyCount != 0 || p.LastPass != "" || p.Mastered {
		t.Fatalf("manual reset incomplete: %+v", p)
	}
	if r := nextVote(t, s, day, true); r.DayPassed || r.DailyTarget != 2 {
		t.Fatalf("reset card did not get new threshold: %+v", r)
	}
	if r := nextVote(t, s, day, true); !r.DayPassed || r.Credited {
		t.Fatalf("reset earned a second credit: %+v", r)
	}
}

func TestLowerDailyGoalAppliesToStartedCards(t *testing.T) {
	const day = "2026-11-16"
	for _, count := range []int{2, 3, 4} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s, path := newTestStore(t)
			options := s.db.Settings
			options.DailyGoal = 5
			if _, err := s.changeSettings(day, options); err != nil {
				t.Fatal(err)
			}
			importTest(t, s, day, "crucial")
			for range count {
				nextVote(t, s, day, true)
			}
			options.DailyGoal = 3
			updated, err := s.changeSettings(day, options)
			if err != nil {
				t.Fatal(err)
			}
			card := s.db.Words["crucial"]
			wantPass := count >= 3
			if card.passedOn(day) != wantPass || card.DailyCount != min(count, 3) ||
				card.DailyTarget != 3 || card.Reviews != count || (updated.Current == nil) != wantPass {
				t.Fatalf("unexpected review state %+v, progress %+v", updated, card.progress)
			}
			if stats := s.stats(day); stats.TotalReviews != count || (stats.Days[29].Passed == 1) != wantPass {
				t.Fatalf("resaving changed the answer count or missed the pass: %+v", stats)
			}
			if !validDatabase(s.db) || s.attendance(day).credited[card.ID] != wantPass {
				t.Fatal("daily goal change lost backup compatibility or the earned credit")
			}
			reopened, err := openStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.io.Close()
			if reopened.db.Words["crucial"].progress != card.progress || reopened.db.Settings != options {
				t.Fatal("daily goal change was not persisted")
			}
			if count == 2 {
				if r := nextVote(t, s, day, true); !r.DayPassed || !r.Credited || r.DailyTarget != 3 {
					t.Fatalf("the third answer should pass immediately: %+v", r)
				}
				return
			}
			// Raising the setting does not revoke an existing pass.
			options.DailyGoal = 6
			if _, err := s.changeSettings(day, options); err != nil || !card.passedOn(day) || card.Streak != 1 {
				t.Fatalf("existing pass changed: %v, %+v", err, card.progress)
			}
			options.DailyGoal = 3
			if _, err := s.changeSettings(day, options); err != nil {
				t.Fatal(err)
			}
			for remaining := count - 1; remaining >= 2; remaining-- {
				if _, err := s.undo(day); err != nil {
					t.Fatal(err)
				}
				if card.DailyCount != remaining || card.DailyTarget != 3 ||
					card.passedOn(day) != (remaining >= 3) || s.attendance(day).credited[card.ID] != (remaining >= 3) ||
					!validDatabase(s.db) {
					t.Fatalf("undo restored the wrong daily goal or credit: %+v", card.progress)
				}
			}
		})
	}
}

func TestDailyGoalChangeRollsBackIfHistoryWriteFails(t *testing.T) {
	s, path := newTestStore(t)
	const day = "2026-11-16"
	options := s.db.Settings
	options.DailyGoal = 5
	if _, err := s.changeSettings(day, options); err != nil {
		t.Fatal(err)
	}
	importTest(t, s, day, "crucial")
	for range 4 {
		nextVote(t, s, day, true)
	}
	before, err := json.Marshal(s.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.io.Exec("CREATE TRIGGER reject_goal_update BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT, 'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	options.DailyGoal = 3
	if _, err := s.changeSettings(day, options); err == nil {
		t.Fatal("settings change unexpectedly succeeded")
	}
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	for _, db := range []database{s.db, reopened.db} {
		data, err := json.Marshal(db)
		if err != nil || string(data) != string(before) {
			t.Fatal("failed settings change modified the settings, card, or history")
		}
	}
}

func TestExistingSQLiteAndOldJSONRestore(t *testing.T) {
	s, path := newTestStore(t)
	const day = "2026-11-16"
	setTestGoal(t, s, day, 6, 40)
	importTest(t, s, day, "active, mastered")
	active := s.db.Words["active"]
	active.PersonalNote = "要保留的旧备注"
	nextVote(t, s, day, true)
	s.db.Words["mastered"].Mastered = true
	s.db.Words["mastered"].Streak = 6
	s.db.Words["mastered"].Reviews = 6
	s.db.Version = 2
	s.db.Settings.DailyGoal = 0
	keys := []string{"active", "mastered"}
	if err := s.persist(s.db, keys, 0, false); err != nil {
		t.Fatal(err)
	}
	old := s.db
	s.io.Close()
	migrated, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.io.Close()
	card := migrated.db.Words["active"]
	if !migrated.upgraded || migrated.db.Version != 3 || migrated.db.Settings.DailyGoal != 3 ||
		migrated.db.Settings.Limit != 40 || card.PersonalNote != active.PersonalNote || card.Streak != 0 ||
		card.LegacyStreak != 1 || card.DailyDay != "" || !migrated.db.Words["mastered"].LegacyMastered ||
		len(migrated.db.Events) != len(old.Events) || !migrated.db.Events[2].Legacy {
		t.Fatalf("lost v2 content: %+v", migrated.db)
	}
	if state := migrated.review(day); state.Due != 1 || state.Reviewed != 0 || state.Mastered != 1 || state.CanUndo {
		t.Fatalf("legacy answers incorrectly count for new review: %+v", state)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "before-dual-memory-v2-*.json"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected a pre-upgrade backup: %v %v", backups, err)
	}
	raw, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	var saved database
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Version != 2 || saved.Words["active"].Streak != 1 {
		t.Fatalf("pre-upgrade backup changed: %v", err)
	}
	if err := migrated.restore(saved); err != nil {
		t.Fatalf("legacy JSON is no longer accepted: %v", err)
	}
	if state := migrated.review(day); state.Mastered != 1 || state.Current.Streak != 0 {
		t.Fatalf("legacy backup restore did not follow migration rules: %+v", state)
	}
}
