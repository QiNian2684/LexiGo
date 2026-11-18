package main

import (
	"os"
	"strings"
	"testing"
)

func TestAssociationDistinguishesEvidenceAndAppearance(t *testing.T) {
	if constructions["airport"] != "" || !strings.Contains(associationHint("airport", ""), "port（") {
		t.Fatal("airport should get spelling-based port only")
	}
	if shape := associationHint("transport", constructions["transport"]); strings.Contains(shape, "trans-（") || strings.Contains(shape, "port（") {
		t.Fatalf("verified pieces were repeated as spelling guesses: %q", shape)
	}
	nested := associationCandidates("biophysiology", "")
	for _, want := range []string{"bio-", "bi-"} {
		found := false
		for _, hit := range nested {
			found = found || hit.label == want
		}
		if !found {
			t.Fatalf("nested prefix %s not kept: %+v", want, nested)
		}
	}
	if nested[0].label == "bi-" {
		t.Fatal("two-letter cue should not outrank longer cues")
	}
	if got := associationHint("qzxqz", ""); got != "" || associationHint("the", "") != "" {
		t.Fatal("created a cue for nonsense or a short function word")
	}
	if len(strings.Split(associationHint("biophysiology", ""), "、")) > associationLimit {
		t.Fatal("displayed too many candidates")
	}
}

func TestMorphologyIndependentValidation(t *testing.T) {
	data, err := os.ReadFile("scripts/morphology-validation.tsv")
	if err != nil {
		t.Fatal(err)
	}
	type count struct{ total, old, strict, any, both int }
	results := make(map[string]count)
	for row := range strings.SplitSeq(string(data), "\n") {
		if row == "" || strings.HasPrefix(row, "#") {
			continue
		}
		fields := strings.Split(row, "\t")
		group, word := fields[0], fields[1]
		tip := constructions[word]
		association := associationHint(word, tip)
		r := results[group]
		r.total++
		if fields[2] == "1" {
			r.old++
		}
		if tip != "" {
			r.strict++
		}
		if tip != "" || association != "" {
			r.any++
		}
		if tip != "" && association != "" {
			r.both++
		}
		results[group] = r
	}
	for _, group := range []string{"prefix", "suffix", "root", "unsuitable"} {
		r := results[group]
		t.Logf("%s: %d words, %d original, %d strict, %d any, %d both", group, r.total, r.old, r.strict, r.any, r.both)
	}
	for _, word := range strings.Fields("airport portion opportune coriander corner coral island winter honey number relation cordial biology biophysiology constitution reassurance repeatedly reflectable remarkable considerably reconstruction thanksgiving unbelievable newsworthy hospitable uninteresting knowledgeable revocable unthoughtful") {
		t.Logf("review %-18s %s | %s", word, constructions[word], associationHint(word, constructions[word]))
	}
}

func BenchmarkAssociationHint(b *testing.B) {
	for b.Loop() {
		associationHint("biocompatibility", constructions["biocompatibility"])
	}
}
