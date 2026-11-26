package main

import (
	"fmt"
	"math"
	"testing"
)

func TestLibraryClampsPagesWithinFilteredResults(t *testing.T) {
	s := store{db: database{Words: make(map[string]*wordCard)}}
	for i := 0; i < 1036; i++ {
		key := fmt.Sprintf("word%04d", i)
		card := &wordCard{Key: key, Word: key}
		if i < 25 {
			card.Meaning = "一个筛选结果"
		}
		s.db.Words[key] = card
	}

	check := func(criteria selection, requested, page, pages, total, rows int) listResult {
		t.Helper()
		result := s.list(criteria, requested)
		if result.Page != page || result.Pages != pages || result.Total != total || len(result.Rows) != rows {
			t.Fatalf("%+v page %d: got page %d/%d with %d of %d words", criteria, requested, result.Page, result.Pages, len(result.Rows), result.Total)
		}
		return result
	}
	check(selection{}, 4, 4, 44, 1036, 24)
	check(selection{}, math.MaxInt, 43, 44, 1036, 4)
	subset := selection{Query: "筛选"}
	last := check(subset, 1, 1, 2, 25, 1)
	s.db.Words[last.Rows[0].Key].Deleted = true
	check(subset, 1, 0, 1, 24, 24)
	check(selection{Filter: "trash"}, -1, 0, 1, 1, 1)
	check(selection{Query: "不存在"}, 1, 0, 0, 0, 0)
}
