package main

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultGoal = 6
const defaultDailyGoal = 3
const defaultLimit = 20

type settings struct {
	Limit     int    `json:"limit"`
	Goal      int    `json:"goal"`
	DailyGoal int    `json:"dailyGoal,omitempty"`
	Online    bool   `json:"online"`
	ShowIPA   bool   `json:"showIPA"`
	ExamDate  string `json:"examDate,omitempty"`
	Rollover  string `json:"rollover,omitempty"`
}

func (s settings) valid() bool {
	return s.validLongGoal() && s.DailyGoal >= 1 && s.DailyGoal <= 99 && validExamDate(s.ExamDate) && validRollover(s.Rollover)
}

func (s settings) validLongGoal() bool {
	return s.Limit >= 0 && s.Limit <= 10000 && s.Goal >= 1 && s.Goal <= 99
}

type progress struct {
	DailyDay       string `json:"dailyDay,omitempty"`
	DailyCount     int    `json:"dailyCount,omitempty"`
	DailyTarget    int    `json:"dailyTarget,omitempty"`
	RetryGap       int    `json:"retryGap,omitempty"`
	LegacyMastered bool   `json:"legacyMastered,omitempty"`
	LastTry        string `json:"lastTry,omitempty"`
	LastPass       string `json:"lastPass,omitempty"`
	LastYes        string `json:"lastYes,omitempty"`
	LastNo         string `json:"lastNo,omitempty"`
	ExtraDay       string `json:"extraDay,omitempty"`
	Mastered       bool   `json:"mastered,omitempty"`
	Deleted        bool   `json:"deleted,omitempty"`
	Streak         int    `json:"streak"`
	Reviews        int    `json:"reviews"`
	Forgot         int    `json:"forgot"`
	Order          uint64 `json:"order"`
	RepeatAt       uint64 `json:"repeatAt,omitempty"`
}

type wordCard struct {
	ID   uint64 `json:"id"`
	Key  string `json:"key"`
	Word string `json:"word"`
	cardDetails
	progress
	PersonalNote         string `json:"personalNote"`
	NoteClock            int64  `json:"noteClock,omitempty"`
	ECDICT               bool   `json:"ecdict,omitempty"`
	WikiSource           string `json:"wikiSource,omitempty"`
	ExampleBy            string `json:"exampleBy,omitempty"`
	ExampleTranslationBy string `json:"exampleTranslationBy,omitempty"`
	ExampleSearchTried   bool   `json:"exampleSearchTried,omitempty"`
	LegacyStreak         int    `json:"legacyStreak,omitempty"`
	Reimports            int    `json:"reimports,omitempty"`
	Lookup               string `json:"lookup"`
	AddedDay             string `json:"addedDay"`
}

type event struct {
	Kind       string     `json:"kind"`
	Day        string     `json:"day"`
	Card       uint64     `json:"card"`
	Word       string     `json:"word"`
	Tick       uint64     `json:"tick,omitempty"`
	Extra      bool       `json:"extra,omitempty"`
	Legacy     bool       `json:"legacy,omitempty"`
	DayPassed  bool       `json:"dayPassed,omitempty"`
	Credit     bool       `json:"credit,omitempty"`
	Practice   bool       `json:"practice,omitempty"`
	Recall     bool       `json:"recall,omitempty"`
	Undone     bool       `json:"undone,omitempty"`
	Waiting    bool       `json:"waitingForNext,omitempty"`
	OccurredAt *time.Time `json:"occurredAt,omitempty"`
	Before     *progress  `json:"before,omitempty"`
}

type metadata struct {
	Version          int          `json:"version"`
	Settings         settings     `json:"settings"`
	NextOrder        uint64       `json:"nextOrder"`
	CumulativePasses bool         `json:"cumulativePasses,omitempty"`
	RecallBatch      *recallBatch `json:"recallBatch,omitempty"`
	StudyDayFloor    string       `json:"studyDayFloor,omitempty"`
}

type database struct {
	metadata
	Words  map[string]*wordCard `json:"words"`
	Events []event              `json:"events"`
}

type store struct {
	mu        sync.Mutex
	db        database
	io        *diskStore
	display   reviewDisplay
	presented presentedCard
	upgraded  bool
}

func (s *store) record(day, kind string, card *wordCard) {
	now := time.Now().UTC()
	s.db.Events = append(s.db.Events, event{Kind: kind, Day: day, Card: card.ID, Word: card.Word, OccurredAt: &now})
}

func (s *store) advance(card *wordCard) {
	s.db.NextOrder++
	card.Order = s.db.NextOrder
}

func (s *store) reset(card *wordCard, day string, extra bool) {
	card.Streak = 0
	card.Mastered = false
	card.Deleted = false
	card.LastPass = ""
	card.DailyDay, card.DailyCount, card.DailyTarget = "", 0, 0
	card.RepeatAt, card.RetryGap = 0, 0
	card.LegacyMastered = false
	if extra {
		card.ExtraDay = day
	}
	s.advance(card)
}

type voteResult struct {
	reviewState
	Graduated   bool `json:"graduated"`
	Credited    bool `json:"credited"`
	DayPassed   bool `json:"dayPassed"`
	DailyCount  int  `json:"dailyCount"`
	DailyTarget int  `json:"dailyTarget"`
}

var errStaleReview = errors.New("复习卡已更新，请按当前卡片继续。")
var errWordConflict = errors.New("词库里已有这个拼写，包括回收站中的词条。")

func (s *store) vote(day, key string, version int, order uint64, remembered bool) (voteResult, error) {
	return s.recordVote(day, key, version, order, remembered, false, false)
}

func (s *store) recordVote(day, key string, version int, order uint64, remembered, hold, passToday bool) (voteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan := s.planReview(day)
	state := s.reviewState(plan)
	if state.Current == nil || state.Waiting || (passToday && state.Current.Recall) || state.Current.Key != key ||
		state.Current.Reviews != version || state.Current.Order != order {
		return voteResult{}, errStaleReview
	}
	card := s.db.Words[key]
	before, oldOrder, count := card.progress, s.db.NextOrder, len(s.db.Events)
	s.advance(card)
	session := plan.session
	now := time.Now().UTC()
	entry := event{Day: day, OccurredAt: &now, Card: card.ID, Word: card.Word, Tick: card.Order, Before: &before, Extra: card.ExtraDay == day, Recall: state.Current.Recall,
		Waiting: hold && !remembered}
	if entry.Recall {
		card.recordAnswer(day, remembered, &entry)
	} else {
		card.answer(day, remembered, passToday, s.db.Settings, session.credited[card.ID], session.votes, &entry)
	}

	s.db.Events = append(s.db.Events, entry)
	if entry.DayPassed && card.qualifiesForMastery(s.db.Settings.Goal, card.ID, s.db.Events) {
		card.Mastered = true
	}
	if err := s.save([]string{key}, count); err != nil {
		card.progress, s.db.NextOrder, s.db.Events = before, oldOrder, s.db.Events[:count]
		return voteResult{}, err
	}
	return voteResult{
		reviewState: s.review(day), Graduated: !before.Mastered && card.Mastered, Credited: entry.Credit,
		DayPassed: entry.DayPassed, DailyCount: card.DailyCount, DailyTarget: card.DailyTarget,
	}, nil
}

func (s *store) nextAfterForgot(day string, order uint64) (reviewState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, card := s.undoCandidate(day)
	if index < 0 || !s.db.Events[index].Waiting || card.Order != order {
		return reviewState{}, errStaleReview
	}
	s.db.Events[index].Waiting = false
	if err := s.save(nil, index); err != nil {
		s.db.Events[index].Waiting = true
		return reviewState{}, err
	}
	return s.review(day), nil
}

func (s *store) snapshotDatabase(day string) database {
	db := s.db
	active, _ := s.undoCandidate(s.activeReviewDay(day))
	cloned := false
	for i, item := range s.db.Events {
		if item.Waiting && i != active {
			if !cloned {
				db.Events = slices.Clone(s.db.Events)
				cloned = true
			}
			db.Events[i].Waiting = false
		}
	}
	return db
}

func (s *store) undoCandidate(day string) (int, *wordCard) {
	for index := len(s.db.Events) - 1; index >= 0; index-- {
		item := s.db.Events[index]
		if item.Day != day || item.Legacy || item.Undone || item.Practice || item.Before == nil {
			continue
		}
		for _, card := range s.db.Words {
			if card.ID == item.Card && !card.Deleted && card.Order == item.Tick {
				return index, card
			}
		}
		return -1, nil
	}
	return -1, nil
}

func (s *store) undo(day string) (reviewState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, card := s.undoCandidate(day)
	if index < 0 {
		return s.review(day), nil
	}
	item := &s.db.Events[index]
	old := card.progress
	card.progress = *item.Before
	wasWaiting := item.Waiting
	item.Waiting = false
	item.Undone = true
	firstEvent := index
	var adjusted *event
	var previous event
	if !item.Recall && card.DailyDay == day && card.DailyTarget != s.db.Settings.DailyGoal {
		last := s.latestAnswerIndexes(day)
		if i, ok := last[card.ID]; ok {
			firstEvent, adjusted = i, &s.db.Events[i]
			previous = *adjusted
		}
		card.reviseDailyGoal(day, s.db.Settings, s.attendance(day).credited[card.ID], adjusted)
	}
	card.Mastered = card.Mastered || card.qualifiesForMastery(s.db.Settings.Goal, card.ID, s.db.Events)
	if err := s.save([]string{card.Key}, firstEvent); err != nil {
		card.progress = old
		item.Waiting = wasWaiting
		item.Undone = false
		if adjusted != nil {
			*adjusted = previous
		}
		return reviewState{}, err
	}
	s.presented = presentedCard{}
	if s.display.day != day {
		s.display = reviewDisplay{day: day}
	}
	s.display.undoCard, s.display.undoOrder = card.ID, card.Order
	return s.review(day), nil
}

func (s *store) latestAnswerIndexes(day string) map[uint64]int {
	last := make(map[uint64]int)
	for i, item := range s.db.Events {
		if item.Day == day && !item.Undone && !item.Legacy && !item.Practice && !item.Recall && (item.Kind == "yes" || item.Kind == "no") {
			last[item.Card] = i
		}
	}
	return last
}

func (s *store) changeSettings(day string, target settings) (reviewState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateSettings(day, s.db.StudyDayFloor, target)
}

func (s *store) changeSettingsAt(now time.Time, target settings) (reviewState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	floor := s.db.StudyDayFloor
	today := s.db.studyDay(now)
	if target.rolloverTime() != s.db.Settings.rolloverTime() && target.studyDay(now) < today {
		floor = today
	}
	return s.updateSettings(s.activeReviewDay(max(floor, target.studyDay(now))), floor, target)
}

func (s *store) updateSettings(day, floor string, target settings) (reviewState, error) {
	old := s.db.Settings
	oldFloor := s.db.StudyDayFloor
	s.db.Settings = target
	s.db.StudyDayFloor = floor
	var keys []string
	oldProgress := make(map[string]progress)
	oldEvents := make(map[int]event)
	firstEvent := len(s.db.Events)
	var last map[uint64]int
	var credited map[uint64]bool
	for key, card := range s.db.Words {
		if !card.Mastered && target.Goal < old.Goal && card.qualifiesForMastery(target.Goal, card.ID, s.db.Events) {
			keys = append(keys, key)
			oldProgress[key] = card.progress
			card.Mastered = true
		}
		if card.Deleted || card.Mastered || card.DailyDay != day || card.passedOn(day) || card.DailyTarget == target.DailyGoal {
			continue
		}
		var lastAnswer *event
		if card.DailyCount >= target.DailyGoal {
			if last == nil {
				last = s.latestAnswerIndexes(day)
				credited = s.attendance(day).credited
			}
			i, ok := last[card.ID]
			if !ok || s.db.Events[i].Kind != "yes" {
				continue
			}
			firstEvent = min(firstEvent, i)
			oldEvents[i] = s.db.Events[i]
			lastAnswer = &s.db.Events[i]
		}
		keys = append(keys, key)
		oldProgress[key] = card.progress
		card.reviseDailyGoal(day, target, credited[card.ID], lastAnswer)
		if lastAnswer != nil && card.qualifiesForMastery(target.Goal, card.ID, s.db.Events) {
			card.Mastered = true
		}
	}
	if err := s.save(keys, firstEvent); err != nil {
		s.db.Settings = old
		s.db.StudyDayFloor = oldFloor
		for key, progress := range oldProgress {
			s.db.Words[key].progress = progress
		}
		for i, item := range oldEvents {
			s.db.Events[i] = item
		}
		return reviewState{}, err
	}
	return s.review(day), nil
}

type selection struct {
	Query  string   `json:"query"`
	Filter string   `json:"filter"`
	Sort   string   `json:"sort"`
	All    bool     `json:"all"`
	Keys   []string `json:"keys"`
}

func (s *store) matching(selecting selection) []*wordCard {
	query := strings.ToLower(strings.TrimSpace(selecting.Query))
	var result []*wordCard
	for _, card := range s.db.Words {
		matched := false
		switch selecting.Filter {
		case "trash":
			matched = card.Deleted
		case "active":
			matched = !card.Deleted && !card.Mastered
		case "mastered":
			matched = !card.Deleted && card.Mastered
		case "recent":
			matched = !card.Deleted && card.AddedDay >= daysAgo(6)
		case "forgotten":
			matched = !card.Deleted && (card.Forgot > 0 || card.Reimports > 0)
		case "partial":
			matched = !card.Deleted && card.needsOnline()
		default:
			matched = !card.Deleted
		}
		if matched && (strings.Contains(card.Key, query) || strings.Contains(strings.ToLower(card.Meaning), query)) {
			result = append(result, card)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		switch selecting.Sort {
		case "added":
			if a.ID != b.ID {
				return a.ID > b.ID
			}
		case "seen":
			if a.LastTry != b.LastTry {
				return a.LastTry > b.LastTry
			}
		case "forgot":
			if a.Forgot != b.Forgot {
				return a.Forgot > b.Forgot
			}
		case "reviews":
			if a.Reviews != b.Reviews {
				return a.Reviews > b.Reviews
			}
		}
		return a.Key < b.Key
	})
	return result
}

func (s *store) selected(input selection) []*wordCard {
	if input.All {
		return s.matching(input)
	}
	var result []*wordCard
	seen := make(map[string]bool)
	for _, key := range input.Keys {
		if card := s.db.Words[key]; card != nil && !seen[key] {
			seen[key] = true
			result = append(result, card)
		}
	}
	return result
}

type cardRow struct {
	Key            string `json:"key"`
	Word           string `json:"word"`
	Meaning        string `json:"meaning"`
	Lookup         string `json:"lookup"`
	Mastered       bool   `json:"mastered"`
	Partial        bool   `json:"partial"`
	Deleted        bool   `json:"deleted"`
	Streak         int    `json:"streak"`
	LegacyStreak   int    `json:"legacyStreak,omitempty"`
	LegacyMastered bool   `json:"legacyMastered,omitempty"`
	LastTry        string `json:"lastTry"`
}

type listResult struct {
	Rows    []cardRow `json:"rows"`
	Total   int       `json:"total"`
	Pending int       `json:"pending"`
	Goal    int       `json:"goal"`
}

func (s *store) list(selecting selection, page int) listResult {
	matched := s.matching(selecting)
	result := listResult{Rows: []cardRow{}, Total: len(matched), Goal: s.db.Settings.Goal}
	for _, card := range s.db.Words {
		if card.Lookup == "pending" && !card.Deleted {
			result.Pending++
		}
	}
	const pageSize = 24
	start := min(len(matched), page*pageSize)
	end := min(len(matched), start+pageSize)
	for _, item := range matched[start:end] {
		meaning, _, _ := strings.Cut(item.Meaning, "\n")
		runes := []rune(meaning)
		if len(runes) > 76 {
			meaning = string(runes[:76]) + "…"
		}
		result.Rows = append(result.Rows, cardRow{
			item.Key, item.Word, meaning, item.Lookup, item.Mastered, item.needsOnline(),
			item.Deleted, item.Streak, item.LegacyStreak, item.LegacyMastered, item.LastTry,
		})
	}
	return result
}

func (s *store) batch(day string, input selection, action string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cards []*wordCard
	for _, card := range s.selected(input) {
		switch action {
		case "reset", "remove", "retry":
			if !card.Deleted && (action != "retry" || card.needsOnline()) {
				cards = append(cards, card)
			}
		case "restore":
			if card.Deleted {
				cards = append(cards, card)
			}
		}
	}
	if len(cards) == 0 {
		return 0, nil
	}
	old := make([]wordCard, 0, len(cards))
	keys := make([]string, 0, len(cards))
	oldOrder, count := s.db.NextOrder, len(s.db.Events)
	for _, card := range cards {
		old = append(old, *card)
		keys = append(keys, card.Key)
		switch action {
		case "reset":
			s.reset(card, day, false)
		case "remove", "restore":
			card.Deleted = action == "remove"
			s.advance(card)
		case "retry":
			card.Lookup = "pending"
			card.ExampleSearchTried = false
			continue
		}
		s.record(day, action, card)
	}
	if err := s.save(keys, count); err != nil {
		for i := range cards {
			*cards[i] = old[i]
		}
		s.db.NextOrder, s.db.Events = oldOrder, s.db.Events[:count]
		return 0, err
	}
	return len(cards), nil
}

func (s *store) history(card *wordCard) []event {
	items := make([]event, 0)
	for _, item := range s.db.Events {
		if item.Card == card.ID {
			item.Before = nil
			items = append(items, item)
		}
	}
	slices.Reverse(items)
	return items
}
