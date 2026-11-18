package main

import "time"

const defaultExamDate = "12-20"

type examCountdown struct {
	Date string `json:"date"`
	Days int    `json:"days"`
}

func validExamDate(value string) bool {
	switch len(value) {
	case 0:
		return true
	case 5:
		date, err := time.Parse(time.DateOnly, "2000-"+value)
		return err == nil && date.Format("01-02") == value
	case 10:
		date, err := time.Parse(time.DateOnly, value)
		return err == nil && date.Year() >= 1900 && date.Format(time.DateOnly) == value
	}
	return false
}

func countdown(day, target string) examCountdown {
	today, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return examCountdown{}
	}
	if target == "" {
		target = defaultExamDate
	}
	if len(target) == 5 {
		monthDay, err := time.Parse(time.DateOnly, "2000-"+target)
		if err != nil {
			return examCountdown{}
		}
		for year := today.Year(); ; year++ {
			date := time.Date(year, monthDay.Month(), monthDay.Day(), 0, 0, 0, 0, time.UTC)
			if date.Month() == monthDay.Month() && !date.Before(today) {
				target = date.Format(time.DateOnly)
				break
			}
		}
	}
	date, err := time.Parse(time.DateOnly, target)
	if err != nil {
		return examCountdown{}
	}
	// Both inputs describe calendar days, so DST and local time offsets do not
	// affect the difference. Unix seconds also avoid time.Duration overflow.
	return examCountdown{target, int((date.Unix() - today.Unix()) / 86400)}
}
