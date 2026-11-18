package main

import (
	"fmt"
	"testing"
)

const spacingTestDay = "2026-11-16"

func spacingWords(t *testing.T, s *store, count int) {
	t.Helper()
	options := s.db.Settings
	options.Limit, options.Online = count, false
	if _, err := s.changeSettings(spacingTestDay, options); err != nil {
		t.Fatal(err)
	}
	var words string
	for i := 0; i < count; i++ {
		words += fmt.Sprintf("spacing%c%c,", 'a'+rune(i/26), 'a'+rune(i%26))
	}
	importTest(t, s, spacingTestDay, words)
}

type gapStats struct {
	count, inRange, sum, distinctSum int
	minimum, maximum                 int
}

func (g *gapStats) record(gap, distinct int) {
	if g.count == 0 {
		g.minimum = gap
	}
	g.count++
	g.minimum = min(g.minimum, gap)
	g.maximum = max(g.maximum, gap)
	g.sum += gap
	g.distinctSum += distinct
	if gap >= 12 && gap <= 18 {
		g.inRange++
	}
}

func (g gapStats) String() string {
	return fmt.Sprintf("%d gaps, %d within 12–18 (%.1f%%), mean %.2f, min %d, max %d, mean distinct %.2f",
		g.count, g.inRange, float64(g.inRange)*100/float64(g.count), float64(g.sum)/float64(g.count),
		g.minimum, g.maximum, float64(g.distinctSum)/float64(g.count))
}

func TestActualSpacingAcrossFortyWordSessions(t *testing.T) {
	const (
		words = 40
		runs  = 24
	)
	var all, normal, second, third gapStats
	firstSequences := map[[5]string]bool{}
	for run := 0; run < runs; run++ {
		s, _ := newTestStore(t)
		spacingWords(t, s, words)
		state := s.shuffleReview(spacingTestDay)
		sequence := make([]string, 0, 120)
		var first [5]string
		last, answers := map[string]int{}, map[string]int{}
		for position := 0; position < words*3; position++ {
			if state.Current == nil || state.Planned != words || state.Reviewed > words {
				t.Fatalf("invalid plan at %d: %+v", position, state)
			}
			key := state.Current.Key
			if position < len(first) {
				first[position] = key
			}
			if state.Current.DailyCount != answers[key] {
				t.Fatalf("wrong daily count for %q at %d", key, position)
			}
			if previous, found := last[key]; found {
				gap := position - previous - 1
				intervening := map[string]bool{}
				for _, word := range sequence[previous+1:] {
					intervening[word] = true
				}
				if gap == 0 && state.Due > 1 {
					t.Fatalf("consecutive repeat with %d options at %d, sequence=%v", state.Due, position, sequence)
				}
				all.record(gap, len(intervening))
				if answers[key] == 1 {
					second.record(gap, len(intervening))
				} else {
					third.record(gap, len(intervening))
				}
				if state.Due >= 16 {
					normal.record(gap, len(intervening))
					if gap < 12 || gap > 18 {
						t.Fatalf("repeat gap %d with %d words remaining at %d: %v", gap, state.Due, position, sequence)
					}
				}
			}
			last[key] = position
			answers[key]++
			sequence = append(sequence, key)
			result := nextVote(t, s, spacingTestDay, true)
			if result.DayPassed != (answers[key] == 3) || result.Credited != result.DayPassed {
				t.Fatalf("wrong completion at %d: %+v", position, result)
			}
			state = result.reviewState
			if position == 21 {
				state = s.shuffleReview(spacingTestDay)
			}
		}
		firstSequences[first] = true
		if state.Due != 0 || state.Planned != words || state.Remembered != words || len(answers) != words {
			t.Fatalf("did not finish exactly forty words: %+v", state)
		}
	}
	if all.count != runs*words*2 || normal.count == 0 || len(firstSequences) < 2 {
		t.Fatalf("repeat count or randomized first order: all=%v normal=%v orders=%d", all, normal, len(firstSequences))
	}
	t.Logf("all %d sessions: %s", runs, all)
	t.Logf("16 or more words remaining: %s", normal)
	t.Logf("first-to-second: %s", second)
	t.Logf("second-to-third: %s", third)
}

func TestWrongAnswerWaitsAgain(t *testing.T) {
	s, _ := newTestStore(t)
	spacingWords(t, s, 40)
	first := s.shuffleReview(spacingTestDay).Current.Key
	appearances, lastWrong := 0, -1
	for i := 0; i < 150; i++ {
		state := s.review(spacingTestDay)
		if state.Current == nil {
			break
		}
		if state.Current.Key != first {
			nextVote(t, s, spacingTestDay, true)
			continue
		}
		appearances++
		if appearances == 3 {
			result := nextVote(t, s, spacingTestDay, false)
			if result.DailyCount != 0 || result.DayPassed {
				t.Fatalf("wrong third answer did not reset: %+v", result)
			}
			lastWrong = i
			continue
		}
		if appearances == 4 {
			if state.Current.DailyCount != 0 || i-lastWrong-1 < 12 || i-lastWrong-1 > 18 {
				t.Fatalf("wrong-answer gap=%d, state=%+v", i-lastWrong-1, state)
			}
		}
		result := nextVote(t, s, spacingTestDay, true)
		if appearances == 6 {
			if !result.DayPassed || !result.Credited || s.db.Words[first].Streak != 1 {
				t.Fatalf("retry lost learning rules: %+v", result)
			}
			return
		}
	}
	t.Fatalf("the marked word did not finish after a mistake, appearances=%d", appearances)
}

func TestThreeWordsRemainWithoutConsecutiveCards(t *testing.T) {
	s, _ := newTestStore(t)
	spacingWords(t, s, 3)
	state := s.shuffleReview(spacingTestDay)
	previous := ""
	days := 0
	for i := 0; i < 9; i++ {
		if state.Current == nil || (state.Due > 1 && state.Current.Key == previous) {
			t.Fatalf("stalled or repeated early at step %d: %+v", i, state)
		}
		previous = state.Current.Key
		result := nextVote(t, s, spacingTestDay, true)
		if result.DayPassed {
			days++
		}
		state = result.reviewState
	}
	if state.Current != nil || state.Remembered != 3 || state.Reviewed != 3 || days != 3 {
		t.Fatalf("small group failed to finish: %+v", state)
	}
}

func TestReopenAndShuffleKeepRecordedSpacing(t *testing.T) {
	s, path := newTestStore(t)
	spacingWords(t, s, 40)
	first := s.shuffleReview(spacingTestDay).Current.Key
	s.db.Words[first].PersonalNote = "保持到下次打开"
	mustSave(t, s, first)
	nextVote(t, s, spacingTestDay, true)
	origin := *s.db.Words[first]
	for i := 0; i < 3; i++ {
		if s.review(spacingTestDay).Current.Key == first {
			t.Fatal("the word returned before closing")
		}
		nextVote(t, s, spacingTestDay, true)
	}
	if err := s.io.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.io.Close()

	preserved := s.db.Words[first]
	if preserved.progress != origin.progress || preserved.PersonalNote != origin.PersonalNote {
		t.Fatalf("reopening changed the existing progress: %+v, before=%+v", preserved, origin)
	}
	var interval []string
	unique := map[string]bool{}
	for i := 3; i <= 18; i++ {
		state := s.shuffleReview(spacingTestDay)
		if state.Current.Key == first {
			if i < 12 || state.Current.DailyCount != 1 || len(unique) < 9 {
				t.Fatalf("reopened session repeated too early or lost progress: gap=%d, state=%+v", i, state)
			}
			return
		}
		interval = append(interval, state.Current.Key)
		unique[state.Current.Key] = true
		nextVote(t, s, spacingTestDay, true)
	}
	t.Fatalf("reopening starved the previously answered card: %v", interval)
}
