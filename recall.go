package main

import (
	"math/rand/v2"
	"slices"
	"sort"
	"time"
)

const recallIntervalDays = 5
const recallBatchLimit = 10

type recallBatch struct {
	Day   string   `json:"day"`
	Cards []uint64 `json:"cards,omitempty"`
}

func (b *recallBatch) available(day string) bool {
	return b != nil && b.Day <= day && len(b.Cards) > 0
}

// refreshRecall is called with the store locked, before presenting a review.
// Pick at most one batch on returning after a break, even if several are due.
func (s *store) refreshRecall(day string) error {
	old := s.db.RecallBatch
	anchor := ""
	if old != nil {
		anchor = old.Day
	} else {
		for _, item := range s.db.Events {
			if item.Day <= day && !item.Undone && !item.Practice && (item.Kind == "yes" || item.Kind == "no") &&
				(anchor == "" || item.Day < anchor) {
				anchor = item.Day
			}
		}
	}
	start, err := time.Parse(time.DateOnly, anchor)
	if err != nil || day < start.AddDate(0, 0, recallIntervalDays).Format(time.DateOnly) {
		return nil
	}
	eligible := make([]*wordCard, 0)
	for _, card := range s.db.Words {
		if !card.Deleted && card.Forgot > 0 && card.Reviews > 0 {
			eligible = append(eligible, card)
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		aRate, bRate := float64(a.Forgot)/float64(a.Reviews), float64(b.Forgot)/float64(b.Reviews)
		if aRate != bRate {
			return aRate > bRate
		}
		if a.Forgot != b.Forgot {
			return a.Forgot > b.Forgot
		}
		return a.ID < b.ID
	})
	batch := &recallBatch{Day: day}
	for _, card := range eligible[:min(len(eligible), recallBatchLimit)] {
		batch.Cards = append(batch.Cards, card.ID)
	}
	rand.Shuffle(len(batch.Cards), func(i, j int) { batch.Cards[i], batch.Cards[j] = batch.Cards[j], batch.Cards[i] })
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
	pending := make(map[uint64]*wordCard)
	ids := db.RecallBatch.Cards
	for _, card := range db.Words {
		if !card.Deleted && card.DailyDay <= p.state.Date && !card.passedOn(p.state.Date) &&
			!ordinary[card.ID] && !p.session.recalled[card.ID] && slices.Contains(ids, card.ID) {
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
