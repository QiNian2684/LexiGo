package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestOfflineConstructionUsesExactWords(t *testing.T) {
	for word, want := range map[string]string{
		"transport":     "trans-（跨越） + port（搬运）",
		"import":        "im-（向内，in- 的变体） + port（搬运）",
		"unhelpful":     "un-（不） + help（帮助） + -ful（具有……性质的）",
		"unforgettable": "t 双写",
		"acquaintance":  "-ance",
		"perception":    "cept",
		"aggression":    "gress",
		"sophisticated": "soph",
		"unbelievable":  "believe（相信）",
		"visibility":    "-ibil-",
		"friendliness":  "y 变 i",
		"efflorescence": "-ence",
		"valediction":   "dict",
		"perceived":     "ceiv",
	} {
		if !strings.Contains(constructions[word], want) {
			t.Errorf("%s: %q should contain %q", word, constructions[word], want)
		}
	}
	for _, word := range []string{"inside", "ink", "portion", "airport", "counsel", "stone", "opportune", "happen", "hooven", "tender", "county", "transportunhelpfully"} {
		if hint := constructions[word]; hint != "" {
			t.Errorf("false positive %s: %q", word, hint)
		}
	}
	if len(constructions) < 50000 {
		t.Errorf("word coverage shrank to %d", len(constructions))
	}
	for key := range constructions {
		if _, normalized, ok := normalize(key); !ok || key != normalized {
			t.Errorf("non-canonical word in built-in resource: %q", key)
		}
	}
}

func TestMorphologyCoverageBenchmark(t *testing.T) {
	input, err := os.ReadFile("scripts/morphology-benchmark.tsv")
	if err != nil {
		t.Fatal(err)
	}
	type coverage struct{ total, previous, current, both, shapeOnly, none int }
	groups := make(map[string]coverage)
	seen := make(map[string]bool)
	var candidates, visible int
	newRoles, associationRoles := map[string]int{}, map[string]int{}
	for line := range strings.SplitSeq(string(input), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || seen[fields[1]] || (fields[3] != "0" && fields[3] != "1") {
			t.Fatalf("invalid benchmark row %q", line)
		}
		label, word := fields[0], fields[1]
		seen[word] = true
		group := groups[label]
		group.total++
		if fields[3] == "1" {
			group.previous++
		}
		tip := constructions[word]
		shape := associationCandidates(word, tip)
		if fields[3] == "0" && tip != "" {
			roles := make(map[string]bool)
			for part := range strings.SplitSeq(tip, " + ") {
				form, _, _ := strings.Cut(part, "（")
				switch {
				case strings.HasSuffix(form, "-"):
					roles["prefix"] = true
				case strings.HasPrefix(form, "-"):
					roles["suffix"] = true
				default:
					roles["root-or-base"] = true
				}
			}
			for role := range roles {
				newRoles[role]++
			}
		}
		for _, part := range shape {
			associationRoles[part.kind]++
		}
		candidates += len(shape)
		visible += min(len(shape), associationLimit)
		switch {
		case tip != "" && len(shape) > 0:
			group.current++
			group.both++
		case tip != "":
			group.current++
		case len(shape) > 0:
			group.shapeOnly++
		default:
			group.none++
		}
		groups[label] = group
	}
	var oldHits, newHits int
	for label, result := range groups {
		t.Logf("%s: %d/%d → %d/%d strict, %d shape only, %d both, %d none", label, result.previous, result.total, result.current, result.total, result.shapeOnly, result.both, result.none)
		oldHits += result.previous
		newHits += result.current
	}
	t.Logf("totals: %d → %d strict; %d ranked contenders, %d shown on study cards", oldHits, newHits, candidates, visible)
	t.Logf("new verified words by displayed role: %v; association candidates by role: %v", newRoles, associationRoles)
	if len(seen) != 1200 || newHits < oldHits+35 {
		t.Fatalf("coverage on %d words: %d → %d", len(seen), oldHits, newHits)
	}
}

func TestConstructionIsPresentationOnly(t *testing.T) {
	card := wordCard{Key: "transport"}
	card.Meaning = "手动释义"
	card.PersonalNote = "我的备注"
	card.Etymology = "我的旧资料"
	public, _ := json.Marshal(presentCard(card))
	saved, _ := json.Marshal(card)
	for _, value := range []string{"手动释义", "我的备注", "我的旧资料"} {
		if !strings.Contains(string(public), value) || !strings.Contains(string(saved), value) {
			t.Errorf("lost %q", value)
		}
	}
	if !strings.Contains(string(public), `"construction"`) || strings.Contains(string(saved), `"construction"`) {
		t.Fatal("offline analysis should be derived and absent from user data")
	}
}
