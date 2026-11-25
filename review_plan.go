package main

import (
	"math/rand/v2"
	"slices"
	"sort"
)

type reviewState struct {
	Date        string        `json:"date"`
	Exam        examCountdown `json:"exam"`
	Settings    settings      `json:"settings"`
	Due         int           `json:"due"`
	RecallDue   int           `json:"recallDue,omitempty"`
	TaskDone    int           `json:"taskDone"`
	TaskTotal   int           `json:"taskTotal"`
	OverflowNew int           `json:"overflowNew"`
	Reviewed    int           `json:"reviewed"`
	Remembered  int           `json:"remembered"`
	Planned     int           `json:"planned"`
	Total       int           `json:"total"`
	Mastered    int           `json:"mastered"`
	CanUndo     bool          `json:"canUndo"`
	Current     *cardFace     `json:"current"`
	Waiting     bool          `json:"waitingForNext,omitempty"`
}

type cardFace struct {
	Key             string `json:"key"`
	Word            string `json:"word"`
	Streak          int    `json:"streak"`
	DailyCount      int    `json:"dailyCount"`
	DailyTarget     int    `json:"dailyTarget"`
	Reviews         int    `json:"reviews"`
	Order           uint64 `json:"order"`
	Recall          bool   `json:"recall,omitempty"`
	RecallPending   bool   `json:"recallPending,omitempty"`
	RecallRetesting bool   `json:"recallRetesting,omitempty"`
	RecallCount     int    `json:"recallCount,omitempty"`
}

const (
	minTargetGap      = 13
	maxTargetGap      = 17
	repeatGapDeadline = 18
	distinctGapGoal   = 12
)

type attendance struct {
	lastShown      map[uint64]int
	reviewed       map[uint64]bool
	recalled       map[uint64]bool
	recallProgress map[uint64]recallProgress
	regular        map[uint64]bool
	credited       map[uint64]bool
	imports        map[uint64]bool
	votes          int
	recent         [repeatGapDeadline]uint64
}

func (s *store) attendance(day string) attendance {
	return s.attendanceSince(day, "", nil)
}

func (s *store) attendanceSince(day, recallFrom string, batch *recallBatch) attendance {
	result := attendance{
		lastShown: make(map[uint64]int), reviewed: make(map[uint64]bool),
		regular: make(map[uint64]bool), credited: make(map[uint64]bool), imports: make(map[uint64]bool),
	}
	if batch != nil {
		result.recallProgress = make(map[uint64]recallProgress, len(batch.Cards))
		for _, id := range batch.Cards {
			result.recallProgress[id] = recallProgress{}
		}
	}
	if recallFrom != "" && batch == nil {
		result.recalled = make(map[uint64]bool)
	}
	for _, item := range s.db.Events {
		if item.Legacy || item.Undone || item.Practice {
			continue
		}
		answer := item.Kind == "yes" || item.Kind == "no"
		if result.recalled != nil && answer && item.Day >= recallFrom && item.Day <= day {
			result.recalled[item.Card] = true
		}
		if item.Day != day {
			continue
		}
		if item.Kind == "add" || item.Kind == "reimport" {
			result.imports[item.Card] = true
		}
		if !answer {
			continue
		}
		result.recent[result.votes%len(result.recent)] = item.Card
		result.votes++
		result.reviewed[item.Card] = true
		if progress, selected := result.recallProgress[item.Card]; selected {
			if item.RecallAnswer {
				result.recallProgress[item.Card] = progress.answer(item.Kind == "yes", result.votes)
			} else if batch.RetestAfter > 0 && item.Tick <= batch.RetestAfter {
				result.recallProgress[item.Card] = recallProgress{done: true}
			}
		}
		if item.Recall {
			continue
		}
		if item.Credit {
			result.credited[item.Card] = true
		}
		if result.lastShown[item.Card] == 0 && !item.Extra {
			result.regular[item.Card] = true
		}
		result.lastShown[item.Card] = result.votes
	}
	return result
}

func (a attendance) hasVarietySince(last, wanted int) bool {
	if wanted <= 0 {
		return true
	}
	// The caller only checks positions inside the retained history window.
	var seen [repeatGapDeadline]uint64
	distinct := 0
	for index := last; index < a.votes; index++ {
		id := a.recent[index%len(a.recent)]
		if id == 0 || slices.Contains(seen[:distinct], id) {
			continue
		}
		seen[distinct] = id
		distinct++
		if distinct >= wanted {
			return true
		}
	}
	return false
}

type reviewPlan struct {
	state    reviewState
	old      []*wordCard
	new      []*wordCard
	overflow []*wordCard
	extras   []*wordCard
	repeats  []*wordCard
	carry    []*wordCard
	recall   []*wordCard
	retests  []*wordCard
	session  attendance
	previous attendance
}

func importOrder(a, b *wordCard) bool {
	if a.AddedDay != b.AddedDay {
		return a.AddedDay < b.AddedDay
	}
	return a.ID < b.ID
}

func (s *store) planReview(day string) reviewPlan {
	return s.planReviewRules(day, false)
}

func (s *store) planReviewRules(day string, legacy bool) reviewPlan {
	recallFrom := ""
	var batch *recallBatch
	if !legacy && s.db.RecallBatch.available(day) {
		recallFrom = s.db.RecallBatch.Day
		if s.db.RecallBatch.Retest && recallFrom == day {
			batch = s.db.RecallBatch
		}
	}
	session := s.attendanceSince(day, recallFrom, batch)
	plan := reviewPlan{
		state:   reviewState{Date: day, Exam: countdown(day, s.db.Settings.ExamDate), Settings: s.db.Settings, Reviewed: len(session.reviewed)},
		session: session,
	}
	usedNew := 0
	usedOld := 0
	freshCount := 0
	yesterday := priorStudyDay(day)
	pinned := s.pinnedCard(day)
	if s.presented.recall {
		pinned = nil
	}
	for _, card := range s.db.Words {
		if card.AddedDay == day && session.regular[card.ID] {
			usedNew++
		}
		if card.Deleted {
			if session.regular[card.ID] && card.AddedDay != day && !session.imports[card.ID] {
				usedOld++
			}
			continue
		}
		fresh := card.AddedDay == day || session.imports[card.ID]
		if fresh {
			freshCount++
		} else if session.regular[card.ID] {
			usedOld++
		}
		plan.state.Total++
		if card.passedOn(day) {
			plan.state.Remembered++
		}
		if card.Mastered {
			plan.state.Mastered++
		}
		if card.Mastered || card.passedOn(day) || card.DailyDay > day {
			continue
		}
		switch {
		case session.lastShown[card.ID] > 0:
			plan.repeats = append(plan.repeats, card)
		case !legacy && fresh:
			plan.new = append(plan.new, card)
		case card.ExtraDay == day:
			plan.extras = append(plan.extras, card)
		case card.AddedDay == day:
			plan.new = append(plan.new, card)
		default:
			plan.old = append(plan.old, card)
		}
	}
	sort.Slice(plan.old, func(i, j int) bool {
		a, b := plan.old[i], plan.old[j]
		if !legacy {
			if a == pinned || b == pinned {
				return a == pinned && b != pinned
			}
			if (a.DailyDay == yesterday) != (b.DailyDay == yesterday) {
				return a.DailyDay == yesterday
			}
		}
		return importOrder(a, b)
	})
	sort.Slice(plan.new, func(i, j int) bool { return plan.new[i].ID < plan.new[j].ID })
	sort.Slice(plan.extras, func(i, j int) bool { return plan.extras[i].Order < plan.extras[j].Order })
	sort.Slice(plan.repeats, func(i, j int) bool {
		return session.lastShown[plan.repeats[i].ID] < session.lastShown[plan.repeats[j].ID]
	})
	if limit := s.db.Settings.Limit; limit != 0 {
		if !legacy {
			remaining := max(0, limit-usedOld-freshCount)
			if len(plan.old) > 0 && plan.old[0] == pinned {
				remaining = max(1, remaining)
			}
			plan.old = plan.old[:min(len(plan.old), remaining)]
		} else {
			remaining := max(0, limit-len(session.regular))
			// One third, rounded up, goes to today's imports while older words wait.
			// Unused places on either side go to the other side.
			reservedNew := max(0, (limit+2)/3-usedNew)
			newCount := min(len(plan.new), remaining, reservedNew)
			oldCount := min(len(plan.old), remaining-newCount)
			newCount = min(len(plan.new), remaining-oldCount)
			plan.old = plan.old[:oldCount]
			plan.overflow = plan.new[newCount:]
			plan.new = plan.new[:newCount]
		}
	}
	if !legacy {
		for _, card := range plan.old {
			if card.DailyDay == yesterday && !card.passedOn(yesterday) {
				plan.carry = append(plan.carry, card)
			}
		}
		if len(plan.carry) > 0 {
			plan.previous = s.attendance(yesterday)
			sort.Slice(plan.carry, func(i, j int) bool {
				return plan.previous.lastShown[plan.carry[i].ID] < plan.previous.lastShown[plan.carry[j].ID]
			})
		}
	}
	plan.state.Due = len(plan.old) + len(plan.new) + len(plan.extras) + len(plan.repeats)
	plan.state.OverflowNew = len(plan.overflow)
	plan.state.Planned = plan.state.Remembered + plan.state.Due
	plan.state.TaskDone, plan.state.TaskTotal = plan.state.Remembered, plan.state.Planned
	plan.retests = plan.repeats
	if batch != nil {
		plan.retests = slices.Clone(plan.repeats)
		plan.addRecall(s.db)
	} else if recallFrom != "" {
		plan.addLegacyRecall(s.db)
		plan.state.Due += len(plan.recall)
		plan.state.TaskTotal += len(plan.recall)
	}
	return plan
}

type reviewDisplay struct {
	day                 string
	ranks               map[uint64]int
	undoCard, undoOrder uint64
}

// Prior ranks are considered only to avoid giving the exact same order back
// when a group of unfinished cards can be shown in more than one order.
func shuffleGroup(group []*wordCard, prior map[uint64]int) {
	rand.Shuffle(len(group), func(i, j int) { group[i], group[j] = group[j], group[i] })
	if len(group) < 2 {
		return
	}
	last := 0
	for _, card := range group {
		rank := prior[card.ID]
		if rank <= last {
			return
		}
		last = rank
	}
	group[0], group[1] = group[1], group[0]
}

func presentationOrder(plan reviewPlan, prior map[uint64]int) []*wordCard {
	old, fresh := slices.Clone(plan.old), slices.Clone(plan.new)
	extras := slices.Clone(plan.extras)
	for start := 0; start < len(old); {
		end := start + 1
		for end < len(old) && end-start < 5 {
			end++
		}
		shuffleGroup(old[start:end], prior)
		start = end
	}
	shuffleGroup(fresh, prior)
	shuffleGroup(extras, prior)
	ordered := make([]*wordCard, 0, len(old)+len(fresh)+len(extras)+len(plan.recall))
	ordered = append(ordered, fresh...)
	ordered = append(ordered, extras...)
	ordered = append(ordered, old...)
	for _, card := range plan.freshRecall() {
		ordered = slices.Insert(ordered, rand.IntN(len(ordered)+1), card)
	}
	return ordered
}

func (s *store) shuffleReview(day string) reviewState {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan := s.planReview(day)
	if state := s.reviewState(plan); state.Waiting {
		return state
	}
	s.setDisplay(day, plan)
	return s.reviewState(plan)
}

func (s *store) setDisplay(day string, plan reviewPlan) {
	var prior map[uint64]int
	if s.display.day == day {
		prior = s.display.ranks
	}
	ranks := make(map[uint64]int)
	for i, card := range presentationOrder(plan, prior) {
		ranks[card.ID] = i + 1
	}
	s.display = reviewDisplay{day: day, ranks: ranks}
}

func preferred(ranks map[uint64]int, groups ...[]*wordCard) *wordCard {
	var first *wordCard
	bestRank := 0
	for _, group := range groups {
		for _, card := range group {
			rank := ranks[card.ID]
			if first == nil || (rank > 0 && (bestRank == 0 || rank < bestRank)) {
				first, bestRank = card, rank
			}
		}
	}
	return first
}

func targetRepeatGap(card *wordCard, last int) int {
	width := uint64(maxTargetGap - minTargetGap + 1)
	return minTargetGap + int((3*(card.ID%width)+uint64(last)+uint64(card.RetryGap))%width)
}

func (p reviewPlan) lastRetest(card *wordCard) int {
	return max(p.session.lastShown[card.ID], p.session.recallProgress[card.ID].last)
}

func (p reviewPlan) freshRecall() []*wordCard {
	if p.session.recallProgress == nil {
		return p.recall
	}
	var fresh []*wordCard
	for _, card := range p.recall {
		if p.session.recallProgress[card.ID].last == 0 {
			fresh = append(fresh, card)
		}
	}
	return fresh
}

func (p reviewPlan) readyRepeat() *wordCard {
	wanted := min(distinctGapGoal, p.state.Due-1)
	for _, card := range p.retests {
		last := p.lastRetest(card)
		gap := p.session.votes - last
		// At most one card can have a given last position. Serving the oldest
		// eligible card and capping the variety preference prevents starvation.
		if gap >= repeatGapDeadline {
			return card
		}
		// Reuse the persisted 1.2 jitter value as a salt. The card's stable ID
		// and last answered position keep the new target stable after a reopen.
		target := targetRepeatGap(card, last)
		if gap >= target && p.session.hasVarietySince(last, wanted) {
			return card
		}
	}
	return nil
}

func (p reviewPlan) readyCarry() *wordCard {
	for _, card := range p.carry {
		last := p.previous.lastShown[card.ID]
		gap := p.previous.votes - last + p.session.votes
		if last == 0 || gap >= repeatGapDeadline {
			return card
		}
		target := targetRepeatGap(card, last)
		if gap >= target {
			return card
		}
	}
	return nil
}

func (s *store) reviewState(plan reviewPlan) reviewState {
	state := plan.state
	var ranks map[uint64]int
	if s.display.day == state.Date {
		ranks = s.display.ranks
	}
	next := plan.readyRepeat()
	if next == nil {
		next = plan.readyCarry()
	}
	if next == nil {
		old := plan.old
		if len(plan.carry) > 0 {
			yesterday := priorStudyDay(state.Date)
			old = slices.DeleteFunc(slices.Clone(old), func(card *wordCard) bool {
				return card.DailyDay == yesterday
			})
		}
		switch {
		case len(plan.new) > 0:
			next = preferred(ranks, plan.new)
		case len(plan.extras) > 0:
			next = preferred(ranks, plan.extras)
		case len(old) > 0:
			next = preferred(ranks, old)
		case len(plan.carry) > 0:
			next = plan.carry[0]
		}
		// Shuffle the extra checks into the queue without displacing ready
		// repeats or changing the order of the normal groups.
		if next == nil && plan.session.recallProgress == nil && len(plan.retests) > 0 {
			next = plan.retests[0]
		}
		if recall := preferred(ranks, plan.freshRecall()); recall != nil &&
			(next == nil || (ranks[recall.ID] > 0 && (ranks[next.ID] == 0 || ranks[recall.ID] < ranks[next.ID]))) {
			next = recall
		}
		if next == nil && len(plan.retests) > 0 {
			// With too few different cards, use the longest-waiting one.
			next = plan.retests[0]
		}
	}
	if s.display.day == state.Date && s.display.undoCard != 0 {
		for _, group := range [][]*wordCard{plan.old, plan.new, plan.extras, plan.repeats, plan.recall} {
			for _, card := range group {
				if card.ID == s.display.undoCard && card.Order == s.display.undoOrder {
					next = card
					break
				}
			}
		}
	}
	if pinned := s.pinnedCard(state.Date); pinned != nil && (!s.presented.recall || slices.Contains(plan.recall, pinned)) {
		next = pinned
	}
	recall := slices.Contains(plan.recall, next) && next != nil
	index, previous := s.undoCandidate(state.Date)
	state.CanUndo = index >= 0
	if state.CanUndo && s.db.Events[index].Waiting {
		state.Waiting = true
		next = previous
		recall = s.db.Events[index].Recall
	}
	if next != nil {
		daily := next.dailyState(state.Date, state.Settings.DailyGoal)
		check, selected := plan.session.recallProgress[next.ID]
		state.Current = &cardFace{
			Key: next.Key, Word: next.Word, Streak: next.Streak, Recall: recall,
			DailyCount: daily.count, DailyTarget: daily.target, Reviews: next.Reviews, Order: next.Order,
			RecallPending: selected && !check.done, RecallRetesting: check.retesting && !check.done, RecallCount: check.count,
		}
	}
	return state
}

func (s *store) review(day string) reviewState {
	return s.reviewState(s.planReview(day))
}
