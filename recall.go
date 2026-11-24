package main

import (
	"slices"
	"sort"
	"time"
)

const recallIntervalDays = 5
const recallBatchLimit = 10

type recallBatch struct {
	Day     string   `json:"day"`
	Cards   []uint64 `json:"cards,omitempty"`
	Rolling bool     `json:"rolling,omitempty"`
}

func (b *recallBatch) available(day string) bool {
	return b != nil && b.Day <= day && len(b.Cards) > 0
}

// refreshRecall is called with the store locked, before presenting a review.
func (s *store) refreshRecall(day string) error {
	old := s.db.RecallBatch
	if old != nil && (old.Day > day || (old.Day == day && old.Rolling)) {
		return nil
	}
	current, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return err
	}
	lastSource := current.AddDate(0, 0, -recallIntervalDays).Format(time.DateOnly)
	firstSource := lastSource
	if old != nil && old.Rolling {
		previous, _ := time.Parse(time.DateOnly, old.Day)
		firstSource = previous.AddDate(0, 0, 1-recallIntervalDays).Format(time.DateOnly)
	}
	active := make(map[uint64]bool, len(s.db.Words))
	for _, card := range s.db.Words {
		active[card.ID] = !card.Deleted
	}
	failures := make(map[string]map[uint64]int)
	remembered := make(map[uint64]bool)
	firstAnswers := make(map[string]map[uint64]bool)
	for _, item := range s.db.Events {
		if item.Undone || item.Practice || item.Legacy || !active[item.Card] ||
			(item.Kind != "no" && item.Kind != "yes") {
			continue
		}
		if item.Kind == "no" && !item.Recall && item.Day >= firstSource && item.Day <= lastSource {
			if failures[item.Day] == nil {
				failures[item.Day] = make(map[uint64]int)
			}
			failures[item.Day][item.Card]++
		}
		if old == nil || item.Day < old.Day || item.Day >= day {
			continue
		}
		if firstAnswers[item.Day] == nil {
			firstAnswers[item.Day] = make(map[uint64]bool)
		}
		if !firstAnswers[item.Day][item.Card] {
			firstAnswers[item.Day][item.Card] = true
			remembered[item.Card] = remembered[item.Card] || item.Kind == "yes"
		}
	}
	batch := &recallBatch{Day: day, Rolling: true}
	queued := make(map[uint64]bool)
	if old != nil {
		for _, id := range old.Cards {
			if active[id] && !remembered[id] {
				batch.Cards = append(batch.Cards, id)
				queued[id] = true
			}
		}
	}
	dates := make([]string, 0, len(failures))
	for date := range failures {
		dates = append(dates, date)
	}
	slices.Sort(dates)
	for _, date := range dates {
		counts := failures[date]
		ranked := make([]uint64, 0, len(counts))
		for id := range counts {
			ranked = append(ranked, id)
		}
		sort.Slice(ranked, func(i, j int) bool {
			a, b := ranked[i], ranked[j]
			if counts[a] != counts[b] {
				return counts[a] > counts[b]
			}
			return a < b
		})
		for _, id := range ranked[:min(len(ranked), recallBatchLimit)] {
			if !queued[id] {
				batch.Cards = append(batch.Cards, id)
				queued[id] = true
			}
		}
	}
	s.db.RecallBatch = batch
	if err := s.save(nil, len(s.db.Events)); err != nil {
		s.db.RecallBatch = old
		return err
	}
	return nil
}

func (p *reviewPlan) addRecall(db database) {
	ordinary := make(map[uint64]bool)
	for _, group := range [][]*wordCard{p.old, p.new, p.extras, p.repeats} {
		for _, card := range group {
			ordinary[card.ID] = true
		}
	}
	ids := db.RecallBatch.Cards
	pending := make(map[uint64]*wordCard)
	for _, card := range db.Words {
		if !card.Deleted && card.DailyDay <= p.state.Date && !card.passedOn(p.state.Date) &&
			!ordinary[card.ID] && !p.session.recalled[card.ID] {
			pending[card.ID] = card
		}
	}
	for _, id := range ids {
		if card := pending[id]; card != nil {
			p.recall = append(p.recall, card)
		}
	}
	p.state.RecallDue = len(p.recall)
}
