package main

import (
	"slices"
	"strings"
)

const associationLimit = 3
const maxAssociationSpelling = 11

type associationPart struct {
	spelling string
	kind     string
	label    string
	meaning  string
}

var associationLexicon = loadAssociationLexicon()

func alphabetic(word string) bool {
	for i := range len(word) {
		if word[i] < 'a' || word[i] > 'z' {
			return false
		}
	}
	return word != ""
}

func loadAssociationLexicon() map[string][]associationPart {
	lexicon := make(map[string][]associationPart)
	for line := range strings.SplitSeq(morphologyData, "\n") {
		fields := strings.Split(line, "|")
		if len(fields) != 4 || (fields[0] != "prefix" && fields[0] != "root" && fields[0] != "suffix") {
			continue
		}
		gloss, _, _ := strings.Cut(fields[2], "（")
		for alias := range strings.SplitSeq(fields[1], " / ") {
			label, _, _ := strings.Cut(alias, "（")
			spelling := strings.Trim(label, "-")
			if !alphabetic(spelling) || len(spelling) > maxAssociationSpelling {
				continue
			}
			if !usefulAssociation(fields[0], spelling) {
				continue
			}
			if !slices.ContainsFunc(lexicon[spelling], func(p associationPart) bool { return p.kind == fields[0] }) {
				lexicon[spelling] = append(lexicon[spelling], associationPart{spelling, fields[0], label, gloss})
			}
		}
	}
	lexicon["bio"] = append(lexicon["bio"], associationPart{"bio", "prefix", "bio-", "生命；生物"})
	return lexicon
}

func usefulAssociation(kind, form string) bool {
	if kind == "root" {
		return len(form) >= 4 || slices.Contains([]string{"bio", "duc", "fer", "geo", "vid", "vis", "voc"}, form)
	}
	if len(form) > 2 {
		return true
	}
	return kind == "prefix" && slices.Contains([]string{"bi", "co", "il", "im", "ir", "re", "un"}, form) ||
		kind == "suffix" && form == "ly"
}

type associationCandidate struct {
	associationPart
	score int
}

// Associations are orthographic cues. They never enter the verified index.
// Returning the ranked candidates also lets the test suite evaluate filtering.
func associationCandidates(word, verified string) []associationCandidate {
	if len(word) < 5 || !alphabetic(word) {
		return nil
	}
	var candidates []associationCandidate
	for start := range len(word) {
		for end := start + 2; end <= min(len(word), start+maxAssociationSpelling); end++ {
			length := end - start
			if len(word)-length < 2 {
				continue
			}
			for _, part := range associationLexicon[word[start:end]] {
				switch part.kind {
				case "prefix":
					if start != 0 {
						continue
					}
				case "suffix":
					if end != len(word) {
						continue
					}
				case "root":
					if length == 3 && len(word) < 8 {
						continue
					}
				}
				if containsVerifiedPart(verified, part) {
					continue
				}
				score := length * 6
				if part.kind != "root" {
					score += 12
				}
				if length == 2 {
					score -= 11
				}
				candidates = append(candidates, associationCandidate{part, score})
			}
		}
	}
	slices.SortFunc(candidates, func(a, b associationCandidate) int {
		if a.score != b.score {
			return b.score - a.score
		}
		if a.label != b.label {
			return strings.Compare(a.label, b.label)
		}
		return strings.Compare(a.kind, b.kind)
	})
	return slices.CompactFunc(candidates, func(a, b associationCandidate) bool {
		return a.label == b.label && a.meaning == b.meaning
	})
}

func containsVerifiedPart(verified string, candidate associationPart) bool {
	for part := range strings.SplitSeq(verified, " + ") {
		label, _, ok := strings.Cut(part, "（")
		if !ok {
			continue
		}
		for form := range strings.SplitSeq(label, " / ") {
			spelling := strings.Trim(form, "-")
			if spelling == candidate.spelling ||
				(candidate.kind == "prefix" && strings.HasSuffix(form, "-") && strings.HasPrefix(spelling, candidate.spelling)) ||
				(candidate.kind == "suffix" && strings.HasPrefix(form, "-") && strings.HasSuffix(spelling, candidate.spelling)) {
				return true
			}
		}
	}
	return false
}

func associationHint(word, verified string) string {
	matches := associationCandidates(word, verified)
	var out strings.Builder
	for i, item := range matches[:min(len(matches), associationLimit)] {
		if i > 0 {
			out.WriteString("、")
		}
		out.WriteString(item.label)
		out.WriteString("（")
		out.WriteString(item.meaning)
		out.WriteString("）")
	}
	return out.String()
}
