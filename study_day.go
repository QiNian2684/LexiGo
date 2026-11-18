package main

import "time"

const studyRolloverHour = 5

func studyDay(now time.Time) string {
	if now.Hour() < studyRolloverHour {
		now = now.AddDate(0, 0, -1)
	}
	return now.Format(time.DateOnly)
}

func nextStudyDay(now time.Time) time.Time {
	year, month, day := now.Date()
	if now.Hour() >= studyRolloverHour {
		day++
	}
	return time.Date(year, month, day, studyRolloverHour, 0, 0, 0, now.Location())
}

func priorStudyDay(day string) string {
	date, _ := time.Parse(time.DateOnly, day)
	return date.AddDate(0, 0, -1).Format(time.DateOnly)
}

func localDay() string { return studyDay(time.Now()) }

type presentedCard struct {
	day, key string
	order    uint64
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
	if card == nil || card.Order != s.presented.order || card.Deleted || card.Mastered || card.passedOn(day) || card.DailyDay > day {
		return nil
	}
	return card
}

func (s *store) pinReview(state reviewState) {
	s.presented = presentedCard{}
	if state.Current != nil {
		s.presented = presentedCard{state.Date, state.Current.Key, state.Current.Order}
	}
}
