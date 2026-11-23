package main

import "math/rand/v2"

type dailyReview struct {
	count  int
	target int
}

func (p progress) dailyState(day string, defaultTarget int) dailyReview {
	if day == p.DailyDay {
		return dailyReview{count: p.DailyCount, target: p.DailyTarget}
	}
	if p.LastPass != "" && p.LastPass < day {
		return dailyReview{target: 1}
	}
	return dailyReview{target: defaultTarget}
}

func (p progress) passedOn(day string) bool {
	return p.DailyDay == day && p.LastPass == day
}

func (p *progress) answer(day string, correct, passToday bool, options settings, dayAlreadyCredited bool, previousAnswers int, entry *event) {
	if p.DailyDay != day {
		target := options.DailyGoal
		if correct {
			target = p.dailyState(day, target).target
		}
		p.DailyDay, p.DailyCount, p.DailyTarget = day, 0, target
	}
	p.recordAnswer(day, correct, entry)
	if correct {
		if passToday {
			p.DailyCount = p.DailyTarget
		} else {
			p.DailyCount++
		}
		if p.DailyCount >= p.DailyTarget {
			p.passDaily(day, dayAlreadyCredited, entry)
			return
		}
	} else {
		p.DailyCount = 0
	}
	p.RepeatAt = uint64(previousAnswers + 1)
	// Keep the existing persisted range for backups. The review planner
	// combines this salt with today's answer position to choose its delay.
	p.RetryGap = 2 + rand.IntN(4)
}

func (p *progress) recordAnswer(day string, correct bool, entry *event) {
	p.LastTry = day
	p.Reviews++
	if correct {
		entry.Kind = "yes"
		p.LastYes = day
	} else {
		entry.Kind = "no"
		p.LastNo = day
		p.Forgot++
	}
}

func (p *progress) passDaily(day string, dayAlreadyCredited bool, entry *event) {
	p.LastPass = day
	p.RepeatAt, p.RetryGap = 0, 0
	entry.DayPassed = true
	if !dayAlreadyCredited {
		p.Streak++
		entry.Credit = true
	}
}

func (p progress) qualifiesForMastery(goal int, id uint64, history []event) bool {
	if p.Streak < goal {
		return false
	}
	type reviewDay struct {
		first    string
		credited bool
	}
	days := make(map[string]reviewDay)
	for i := len(history) - 1; i >= 0; i-- {
		item := history[i]
		if item.Card != id || item.Undone || item.Legacy || item.Practice || item.Recall {
			continue
		}
		if item.Kind == "reset" || item.Kind == "reimport" {
			break
		}
		if item.Kind == "yes" || item.Kind == "no" {
			earliest := days[item.Day]
			earliest.first = item.Kind
			earliest.credited = earliest.credited || item.Credit
			days[item.Day] = earliest
		}
	}
	firstMisses := 0
	for _, day := range days {
		if day.credited && day.first == "no" {
			firstMisses++
		}
	}
	// Defer only when the history proves every credited day began with a miss.
	// Older backups can have credited progress without the full answer history.
	return firstMisses < p.Streak
}

func (p *progress) reviseDailyGoal(day string, options settings, credited bool, lastAnswer *event) {
	if p.DailyCount >= options.DailyGoal {
		// Recognize a pass on the existing correct answer so the history and
		// statistics agree, without inserting an extra answer.
		if lastAnswer == nil || lastAnswer.Kind != "yes" {
			return
		}
		p.DailyCount = options.DailyGoal
		p.passDaily(day, credited, lastAnswer)
	}
	p.DailyTarget = options.DailyGoal
}

func (p progress) validDailyState() bool {
	if p.LegacyMastered && !p.Mastered {
		return false
	}
	if p.DailyDay == "" {
		return p.DailyCount == 0 && p.DailyTarget == 0 && p.RepeatAt == 0 && p.RetryGap == 0 && p.LastPass == ""
	}
	if p.DailyTarget < 1 || p.DailyTarget > 99 || p.DailyCount < 0 || p.DailyCount > p.DailyTarget {
		return false
	}
	if p.passedOn(p.DailyDay) {
		return p.DailyCount == p.DailyTarget && p.RepeatAt == 0 && p.RetryGap == 0
	}
	return p.DailyCount < p.DailyTarget && p.RepeatAt > 0 && p.RetryGap >= 2 && p.RetryGap <= 5
}
