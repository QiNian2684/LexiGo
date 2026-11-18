package main

import "time"

func daysAgo(days int) string {
	return time.Now().AddDate(0, 0, -days).Format(time.DateOnly)
}

type dailyStats struct {
	Day      string `json:"day"`
	Distinct int    `json:"distinct"`
	Passed   int    `json:"passed"`
	Yes      int    `json:"yes"`
	No       int    `json:"no"`
}
type statistics struct {
	Imported      int          `json:"imported"`
	Current       int          `json:"current"`
	Mastered      int          `json:"mastered"`
	Unmastered    int          `json:"unmastered"`
	Trashed       int          `json:"trashed"`
	TotalReviews  int          `json:"totalReviews"`
	TotalForgot   int          `json:"totalForgot"`
	TotalReimport int          `json:"totalReimport"`
	Days          []dailyStats `json:"days"`
}

func (s *store) stats(day string) statistics {
	t, _ := time.Parse(time.DateOnly, day)
	result := statistics{Days: make([]dailyStats, 7)}
	dayIndex := make(map[string]int, 7)
	dayWords := make([]map[uint64]bool, 7)
	passedWords := make([]map[uint64]bool, 7)
	for i := range result.Days {
		key := t.AddDate(0, 0, i-6).Format(time.DateOnly)
		dayIndex[key] = i
		dayWords[i] = make(map[uint64]bool)
		passedWords[i] = make(map[uint64]bool)
		result.Days[i].Day = key
	}
	for _, item := range s.db.Words {
		if item.Deleted {
			result.Trashed++
		} else {
			result.Current++
			if item.Mastered {
				result.Mastered++
			} else {
				result.Unmastered++
			}
		}
	}
	imported := make(map[string]bool)
	for _, item := range s.db.Events {
		if item.Undone || item.Practice {
			continue
		}
		switch item.Kind {
		case "add", "reimport":
			_, key, _ := normalize(item.Word)
			imported[key] = true
			if item.Kind == "reimport" {
				result.TotalReimport++
			}
		case "yes", "no":
			result.TotalReviews++
			if item.Kind == "no" {
				result.TotalForgot++
			}
			if i, ok := dayIndex[item.Day]; ok {
				dayWords[i][item.Card] = true
				if item.DayPassed && !item.Legacy {
					passedWords[i][item.Card] = true
				}
				if item.Kind == "yes" {
					result.Days[i].Yes++
				} else {
					result.Days[i].No++
				}
			}
		}
	}
	for i := range result.Days {
		result.Days[i].Distinct = len(dayWords[i])
		result.Days[i].Passed = len(passedWords[i])
	}
	result.Imported = len(imported)
	return result
}
