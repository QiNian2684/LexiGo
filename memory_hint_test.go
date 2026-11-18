package main

import (
	"os"
	"strings"
	"testing"
)

func TestMemoryAssociationCombinesEvidenceAndSpelling(t *testing.T) {
	for word, want := range map[string][]string{
		"exclusive": {"ex-", "clus", "-ive"}, "exclude": {"ex-", "clud"}, "exclusion": {"ex-", "clus"},
		"inclusive": {"clus", "-ive"}, "degrading": {"de-", "grad", "-ing"},
		"degrade": {"de-", "grad"}, "degraded": {"de-", "grad", "-ed"},
		"express": {"ex-"}, "extract": {"ex-", "tract"}, "preheat": {"pre-", "heat"},
		"rewrite": {"re-"}, "transport": {"trans-", "port"}, "careless": {"care", "-less"},
	} {
		got, extra := memoryAssociation(word)
		t.Logf("%s: %s | %s", word, got, extra)
		for _, fragment := range want {
			if !strings.Contains(got, fragment+"（") {
				t.Errorf("%s missing %q: %s", word, fragment, got)
			}
		}
		if next, more := memoryAssociation(word); next != got || more != extra {
			t.Errorf("%s analysis changed between calls", word)
		}
	}
	for _, word := range []string{"the", "of", "with", "qzxqz"} {
		if got, _ := memoryAssociation(word); got != "" {
			t.Errorf("unwanted cue for %s: %s", word, got)
		}
	}
}

func TestMemoryAssociationCoverage(t *testing.T) {
	raw, err := os.ReadFile("scripts/morphology-benchmark.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var matched, exam, total int
	for row := range strings.SplitSeq(string(raw), "\n") {
		if row == "" || strings.HasPrefix(row, "#") {
			continue
		}
		fields := strings.Split(row, "\t")
		total++
		if got, _ := memoryAssociation(fields[1]); got != "" {
			matched++
			if fields[0] == "考研标签" {
				exam++
			}
		}
	}
	t.Logf("1.10: %d/%d overall (1.9: 570/1200), %d/400 exam-labelled (1.9: 246/400)", matched, total, exam)
	if total != 1200 || matched < 620 {
		t.Fatal("memory cue coverage regressed")
	}
}
