package main

import (
	"errors"
	"reflect"
	"slices"
	"sort"
	"testing"
)

func planNames(cards []*wordCard) []string {
	keys := make([]string, 0, len(cards))
	for _, card := range cards {
		keys = append(keys, card.Key)
	}
	return keys
}

func presentationNames(plan reviewPlan, ranks map[uint64]int) []string {
	cards := append(slices.Clone(plan.old), plan.new...)
	sort.Slice(cards, func(i, j int) bool { return ranks[cards[i].ID] < ranks[cards[j].ID] })
	return planNames(cards)
}

func TestReviewPrioritizesOldBatchesAndReservesToday(t *testing.T) {
	s, _ := newTestStore(t)
	const day = "2026-11-16"
	setTestGoal(t, s, day, 6, 6)
	importTest(t, s, "2026-11-01", "alpha, beta, gamma, delta, epsilon")
	importTest(t, s, "2026-11-05", "zeta, eta, theta")
	importTest(t, s, "2026-11-10", "iota, kappa")
	importTest(t, s, day, "today first, today second, today third")
	plan := s.planReview(day)
	if !reflect.DeepEqual(planNames(plan.old), []string{"alpha", "beta", "gamma", "delta"}) ||
		!reflect.DeepEqual(planNames(plan.new), []string{"today first", "today second"}) ||
		!reflect.DeepEqual(planNames(plan.overflow), []string{"today third"}) {
		t.Fatalf("unexpected allocation: old=%v today=%v overflow=%v", planNames(plan.old), planNames(plan.new), planNames(plan.overflow))
	}

	first := s.shuffleReview(day)
	for i := 0; i < 6; i++ {
		card := s.db.Words[s.review(day).Current.Key]
		if (card.AddedDay == day) != (i%3 == 2) {
			t.Fatalf("wanted two old then one new, got %q at %d", card.Key, i)
		}
		nextVote(t, s, day, true)
	}
	if end := s.review(day); first.Due != 6 || end.Current != nil || end.Reviewed != 6 || end.OverflowNew != 1 {
		t.Fatalf("unexpected regular completion: first=%+v end=%+v", first, end)
	}
}

func TestReenterShufflesUnfinishedSetAndKeepsWrongWords(t *testing.T) {
	s, _ := newTestStore(t)
	const day = "2026-11-16"
	setTestGoal(t, s, day, 6, 8)
	importTest(t, s, "2026-11-01", "alpha, beta, gamma, delta, epsilon, zeta")
	importTest(t, s, day, "first fresh, second fresh, third fresh, fourth fresh")
	s.shuffleReview(day)
	done := s.review(day).Current.Key
	nextVote(t, s, day, true)
	wrong := s.review(day).Current.Key
	afterWrong := nextVote(t, s, day, false)
	if afterWrong.Current.Key == wrong {
		t.Fatal("immediate repeat with other words available")
	}

	before := presentationNames(s.planReview(day), s.display.ranks)
	s.shuffleReview(day)
	after := presentationNames(s.planReview(day), s.display.ranks)
	if slices.Equal(before, after) {
		t.Fatalf("reentry reused order: %v", after)
	}
	sort.Strings(before)
	sortedAfter := slices.Clone(after)
	sort.Strings(sortedAfter)
	if !slices.Equal(before, sortedAfter) || slices.Contains(after, done) || slices.Contains(after, wrong) {
		t.Fatalf("reentry changed planned membership: previous=%v next=%v", before, after)
	}
	seenWrong := false
	for i := 0; i < 8 && s.review(day).Due > 0; i++ {
		key := s.review(day).Current.Key
		if key == done {
			t.Fatal("completed word came back")
		}
		answer := nextVote(t, s, day, true)
		if key == wrong {
			seenWrong = true
			if !answer.Credited || s.db.Words[wrong].Streak != 1 {
				t.Fatal("same-day retry did not earn its first credit")
			}
		}
	}
	if !seenWrong || s.review(day).Reviewed != 8 || s.review(day).OverflowNew != 1 {
		t.Fatalf("retry or cap not preserved: seen=%t final=%+v", seenWrong, s.review(day))
	}
}

func TestLateImportsAreExplicitSupplementAndSurviveReopen(t *testing.T) {
	s, path := newTestStore(t)
	const day = "2026-11-16"
	setTestGoal(t, s, day, 6, 2)
	importTest(t, s, "2026-11-01", "apple, pear, banana")
	nextVote(t, s, day, true)
	nextVote(t, s, day, true)
	importTest(t, s, day, "one new, two new, three new, four new")
	if end := s.review(day); end.Due != 0 || end.OverflowNew != 4 || end.Reviewed != 2 {
		t.Fatalf("late imports silently exceeded limit: %+v", end)
	}
	if _, err := s.addTodaySupplement(day, 3); !errors.Is(err, errStaleSupplement) {
		t.Fatalf("accepted stale count: %v", err)
	}
	state, err := s.addTodaySupplement(day, 4)
	if err != nil || state.Due != 4 || state.OverflowNew != 0 || state.Settings.Limit != 2 {
		t.Fatalf("supplement: %+v %v", state, err)
	}
	nextVote(t, s, day, false)
	if err := s.io.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	importTest(t, reopened, day, "another new")
	r := reopened.shuffleReview(day)
	if r.Due != 4 || r.OverflowNew != 1 || r.Reviewed != 3 {
		t.Fatalf("reopen lost queue or authorized future imports: %+v", r)
	}
	for reopened.review(day).Due > 0 {
		nextVote(t, reopened, day, true)
	}
	if r = reopened.review(day); r.Reviewed != 6 || r.OverflowNew != 1 {
		t.Fatalf("wrong final count: %+v", r)
	}
}

func TestNewWordsUseOpenPlacesAndUnlimitedHasNoOverflow(t *testing.T) {
	s, _ := newTestStore(t)
	const day = "2026-11-16"
	importTest(t, s, "2026-11-01", "apple")
	importTest(t, s, day, "pear, peach, plum, orange, kiwi, grape")
	setTestGoal(t, s, day, 6, 5)
	p := s.planReview(day)
	if len(p.old) != 1 || len(p.new) != 4 || p.state.OverflowNew != 2 {
		t.Fatalf("unused old slots were not reused: %+v", p.state)
	}
	setTestGoal(t, s, day, 6, 0)
	p = s.planReview(day)
	if p.state.Due != 7 || p.state.OverflowNew != 0 {
		t.Fatalf("unlimited caps the queue: %+v", p.state)
	}
}
