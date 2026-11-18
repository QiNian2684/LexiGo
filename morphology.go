package main

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

//go:embed assets/morphology.txt
var morphologyData string

//go:embed assets/morphynet-hints.tsv.gz
var derivationData []byte

//go:embed assets/lexiroot-hints.tsv.gz
var rootData []byte

//go:embed assets/morphology-inflections.tsv.gz
var inflectionData []byte

//go:embed assets/morphology-derivations-v19.tsv.gz
var improvedDerivations []byte

//go:embed assets/morphology-morpholex-v19.tsv.gz
var annotatedDerivations []byte

//go:embed assets/morphology-etymdb-v19.tsv.gz
var historicalRoots []byte

var constructions, constructionSources = loadMorphology()

func loadMorphology() (map[string]string, map[string][]string) {
	// The index is built once at release time and shared by all cards.
	// Curated hints take precedence over imported examples and derived forms.
	matches := loadConstructions()
	sources := make(map[string][]string)
	for line := range strings.SplitSeq(morphologyData, "\n") {
		fields := strings.Split(line, "|")
		if len(fields) == 4 {
			for _, word := range strings.Fields(fields[3]) {
				sources[word] = append(sources[word], fields[1]+"（"+fields[2]+"）")
			}
		}
	}
	for _, compressed := range [][]byte{derivationData, rootData, inflectionData} {
		data, err := expand(compressed)
		if err != nil {
			panic(fmt.Sprintf("invalid embedded morphology index: %v", err))
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			word, hint, ok := strings.Cut(line, "\t")
			if ok {
				sources[word] = append(sources[word], hint)
				if matches[word] == "" {
					matches[word] = hint
				}
			}
		}
	}
	manual := make(map[string]bool)
	for line := range strings.SplitSeq(morphologyData, "\n") {
		if record, ok := strings.CutPrefix(line, "word|"); ok {
			key, _, _ := strings.Cut(record, "|")
			manual[key] = true
		}
	}
	scores := make(map[string]int)
	for _, compressed := range [][]byte{improvedDerivations, annotatedDerivations, historicalRoots} {
		data, err := expand(compressed)
		if err != nil {
			panic(fmt.Sprintf("invalid embedded morphology index: %v", err))
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			key, value, ok := strings.Cut(line, "\t")
			if !ok {
				continue
			}
			scoreText, tip, _ := strings.Cut(value, "\t")
			score, err := strconv.Atoi(scoreText)
			if err != nil || score < 1 || tip == "" {
				panic("invalid embedded morphology analysis")
			}
			sources[key] = append(sources[key], tip)
			if manual[key] {
				continue
			}
			previous, exists := scores[key]
			if !exists {
				previous = constructionRank(matches[key])
			}
			if score > previous {
				scores[key], matches[key] = score, tip
			}
		}
	}
	return matches, sources
}

func constructionRank(tip string) int {
	var score int
	for part := range strings.SplitSeq(tip, " + ") {
		label, _, ok := strings.Cut(part, "（")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(label, "-"):
			score++
		case strings.HasSuffix(label, "-"):
			score += 2
		default:
			score += 3
		}
	}
	return score
}

func loadConstructions() map[string]string {
	matches := make(map[string]string)
	overrides := make(map[string]string)
	for i, line := range strings.Split(morphologyData, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "|")
		switch {
		case len(fields) == 3 && fields[0] == "word":
			overrides[fields[1]] = fields[2]
		case len(fields) == 4 && (fields[0] == "prefix" || fields[0] == "suffix" || fields[0] == "root"):
			for _, word := range strings.Fields(fields[3]) {
				matches[word] = fields[1] + "（" + fields[2] + "）"
			}
		default:
			panic(fmt.Sprintf("invalid morphology record on line %d", i+1))
		}
	}
	for word, hint := range overrides {
		matches[word] = hint
	}
	return matches
}

func presentCard(card wordCard) any {
	hint, more := memoryAssociation(card.Key)
	return struct {
		wordCard
		MemoryHint string `json:"memoryHint,omitempty"`
		MoreMemory string `json:"moreMemory,omitempty"`
	}{card, hint, more}
}
