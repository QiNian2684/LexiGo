package main

import "time"

const defaultRollover = "05:00"

func validRollover(value string) bool {
	return value == "" || (len(value) == 5 && validClock(value))
}

func validClock(value string) bool {
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}

func (s settings) rolloverTime() string {
	if s.Rollover == "" {
		return defaultRollover
	}
	return s.Rollover
}

func (s settings) studyDay(now time.Time) string {
	if now.Format("15:04") < s.rolloverTime() {
		now = now.AddDate(0, 0, -1)
	}
	return now.Format(time.DateOnly)
}

func (db database) studyDay(now time.Time) string {
	return max(db.StudyDayFloor, db.Settings.studyDay(now))
}

func (db database) nextStudyDay(now time.Time) time.Time {
	day, _ := time.ParseInLocation(time.DateOnly, db.studyDay(now), now.Location())
	day = day.AddDate(0, 0, 1)
	clock, _ := time.Parse("15:04", db.Settings.rolloverTime())
	return time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
}

func studyDay(now time.Time) string        { return settings{}.studyDay(now) }
func nextStudyDay(now time.Time) time.Time { return database{}.nextStudyDay(now) }

func priorStudyDay(day string) string {
	date, _ := time.Parse(time.DateOnly, day)
	return date.AddDate(0, 0, -1).Format(time.DateOnly)
}

func localDay() string { return studyDay(time.Now()) }

type presentedCard struct {
	day, key string
	order    uint64
	recall   bool
}

// Only an actively displayed question may finish on the previous day. A
// persisted forgotten-card hold also survives a restart just after rollover.
func (s *store) activeReviewDay(today string) string {
	yesterday := priorStudyDay(today)
	if s.presented.day == yesterday && s.pinnedCard(yesterday) != nil {
		return yesterday
	}
	if index, _ := s.undoCandidate(yesterday); index >= 0 && s.db.Events[index].Waiting {
		return yesterday
	}
	return today
}

func (s *store) pinnedCard(day string) *wordCard {
	if s.presented.day != day {
		return nil
	}
	card := s.db.Words[s.presented.key]
	if card == nil || card.Order != s.presented.order || card.Deleted ||
		(!s.presented.recall && (card.Mastered || card.passedOn(day))) || card.DailyDay > day {
		return nil
	}
	return card
}

func (s *store) pinReview(state reviewState) {
	s.presented = presentedCard{}
	if state.Current != nil && !(state.Waiting && state.Current.Recall) {
		s.presented = presentedCard{state.Date, state.Current.Key, state.Current.Order, state.Current.Recall}
	}
}
