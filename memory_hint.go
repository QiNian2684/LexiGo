package main

import (
	"cmp"
	_ "embed"
	"maps"
	"slices"
	"strings"
)

type memoryFragment struct {
	associationPart
	start, end, rank int
}

var shortMemoryPrefixes = map[string]string{
	"ab": "", "ad": "", "af": "f", "ag": "g", "ap": "p", "as": "s", "at": "t",
	"bi": "", "co": "", "de": "", "di": "",
	"em": "bmp", "en": "", "ex": "", "il": "l", "im": "bmp", "in": "",
	"ir": "r", "ob": "", "re": "", "un": "",
}

var compactRoots = strings.Fields("aud cap ced cid civ cre duc dur fac fer fin flu gen geo grad ject lat leg loc luc lud mem mit mot mut nas nat nav nom nov pac ped pel per pet pon pop pos reg rid rog rot sac sap sec seq sol son spir sta sub ten ter tex tor tra vac val ven ver vid vis vit viv voc vol")

//go:embed assets/memory-forms.tsv.gz
var memoryFormsData []byte

var memoryForms = loadMemoryForms()

func loadMemoryForms() map[string]string {
	data, err := expand(memoryFormsData)
	if err != nil {
		panic("invalid embedded memory forms: " + err.Error())
	}
	forms := make(map[string]string)
	for row := range strings.SplitSeq(string(data), "\n") {
		word, marker, ok := strings.Cut(row, "\t")
		if ok && len(marker) > 1 {
			forms[word] = marker
		}
	}
	return forms
}

var formGloss = map[byte]string{
	's': "名词复数", '3': "动词第三人称单数", 'p': "过去式或过去分词", 'd': "过去式或过去分词",
	'i': "进行形式；动作", 'r': "比较级", 't': "最高级",
}

var commonMemoryWords = map[string]string{
	"back": "背；向后", "bird": "鸟", "book": "书", "break": "打破", "child": "孩子", "come": "来",
	"down": "向下", "door": "门", "drop": "落下", "fall": "落下", "fire": "火", "fish": "鱼", "food": "食物",
	"foot": "脚", "free": "自由；不受约束", "ground": "地面", "hand": "手；交付", "head": "头；朝向",
	"help": "帮助", "hold": "抓住", "home": "家", "house": "房屋", "land": "陆地", "light": "光；轻",
	"like": "相似；喜欢", "line": "线；队列", "look": "看", "make": "制作", "move": "移动", "news": "消息",
	"night": "夜晚", "part": "部分；分开", "play": "玩；演奏", "power": "力量", "rain": "雨", "road": "路",
	"room": "房间；空间", "self": "自己", "side": "边；侧面", "snow": "雪", "stand": "站立", "state": "状态",
	"step": "步；步骤", "take": "拿；取", "time": "时间", "turn": "转动；转向", "view": "看；观点",
	"walk": "行走", "wall": "墙", "water": "水", "wind": "风", "word": "单词", "work": "工作",
	"base": "底；基础", "bath": "洗浴", "blood": "血", "broad": "宽；广泛", "cast": "抛；投放", "class": "班级；类别",
	"coat": "外衣；涂层", "craft": "工艺；手艺", "data": "数据", "fare": "费用；境遇", "field": "田地；领域",
	"flow": "流动", "fore": "之前；前面", "form": "形状；形成", "friend": "朋友", "full": "满；充分",
	"game": "游戏", "girl": "女孩", "good": "好", "graph": "记录；图", "grass": "草", "green": "绿色",
	"hard": "硬；难", "heart": "心", "high": "高", "horse": "马", "life": "生命；生活", "load": "装载",
	"mark": "标记", "milk": "奶", "mood": "情绪", "more": "更多", "mount": "上升；安装", "outer": "外部的",
	"over": "在上方；过度", "paper": "纸", "place": "地点；放置", "print": "印刷", "proof": "证明；防护",
	"safe": "安全", "sale": "出售", "shape": "形状", "short": "短", "shop": "店；购物", "sleep": "睡觉",
	"soft": "软", "some": "某些", "sound": "声音", "stair": "楼梯", "stone": "石头", "store": "储存；商店",
	"street": "街道", "suit": "适合；套装", "tail": "尾巴", "tool": "工具", "trade": "交易；行业",
	"ware": "制品", "wash": "清洗", "week": "周", "well": "好；健康", "where": "哪里", "wide": "宽广",
	"wild": "野生的", "wise": "聪明；方式", "world": "世界",
}

func loadMemoryLexicon() map[string][]associationPart {
	lexicon := maps.Clone(associationLexicon)
	for line := range strings.SplitSeq(morphologyData, "\n") {
		fields := strings.Split(line, "|")
		if len(fields) != 4 {
			continue
		}
		gloss, _, _ := strings.Cut(fields[2], "（")
		for spelling := range strings.SplitSeq(fields[1], " / ") {
			label, _, _ := strings.Cut(spelling, "（")
			form := strings.Trim(label, "-")
			_, short := shortMemoryPrefixes[form]
			switch {
			case fields[0] == "prefix" && short,
				fields[0] == "suffix" && slices.Contains([]string{"al", "er", "or", "ic", "ed", "en", "ly"}, form),
				fields[0] == "root" && slices.Contains(compactRoots, form):
				if !slices.ContainsFunc(lexicon[form], func(p associationPart) bool { return p.kind == fields[0] }) {
					lexicon[form] = append(slices.Clone(lexicon[form]), associationPart{form, fields[0], label, gloss})
				}
			}
		}
	}
	for _, part := range []associationPart{
		{"in", "prefix", "in-", "向内；有时表否定"},
		{"ed", "suffix", "-ed", "过去式或过去分词"},
		{"ing", "suffix", "-ing", "进行；动作或过程"},
	} {
		lexicon[part.spelling] = append([]associationPart{part},
			slices.DeleteFunc(slices.Clone(lexicon[part.spelling]), func(p associationPart) bool { return p.kind == part.kind })...)
	}
	return lexicon
}

var memoryLexicon = loadMemoryLexicon()

func memoryAssociation(word string) (string, string) {
	if !alphabetic(word) || len(word) < 4 {
		return "", ""
	}
	fragments := make(map[[2]int]memoryFragment)
	add := func(part associationPart, baseRank int) {
		size := len(part.spelling)
		if size > len(word) || size < 2 {
			return
		}
		var start int
		switch part.kind {
		case "prefix":
			if !strings.HasPrefix(word, part.spelling) {
				return
			}
		case "suffix":
			if !strings.HasSuffix(word, part.spelling) {
				return
			}
			start = len(word) - size
		default:
			start = strings.Index(word, part.spelling)
			if start < 0 {
				return
			}
		}
		rank := size*5 + baseRank
		if part.kind != "root" {
			rank += 6
		}
		if size == 2 {
			rank -= 8
		}
		key := [2]int{start, start + size}
		if previous, ok := fragments[key]; !ok || rank > previous.rank {
			fragments[key] = memoryFragment{part, key[0], key[1], rank}
		}
	}
	addEvidence := func(hint string, rank int) {
		for section := range strings.SplitSeq(hint, " + ") {
			forms, gloss, ok := strings.Cut(section, "（")
			if !ok {
				continue
			}
			gloss, _, _ = strings.Cut(strings.TrimSuffix(gloss, "）"), "（")
			for label := range strings.SplitSeq(forms, " / ") {
				kind := "root"
				if strings.HasPrefix(label, "-") {
					kind = "suffix"
				} else if strings.HasSuffix(label, "-") {
					kind = "prefix"
				}
				form := strings.Trim(label, "-")
				if alphabetic(form) {
					add(associationPart{form, kind, label, gloss}, rank)
				}
			}
		}
	}
	if marker := memoryForms[word]; marker != "" {
		form := marker[1:]
		add(associationPart{form, "suffix", "-" + form, formGloss[marker[0]]}, 22)
	}
	addEvidence(constructions[word], 40)
	for _, hint := range constructionSources[word] {
		addEvidence(hint, 32)
	}
	for start := range len(word) {
		for end := start + 2; end <= min(len(word), start+maxAssociationSpelling); end++ {
			form := word[start:end]
			if (start == 0 || end == len(word)) && len(word)-len(form) >= 3 {
				if gloss := commonMemoryWords[form]; gloss != "" {
					add(associationPart{form, "root", form, gloss}, 6)
				}
			}
			for _, part := range memoryLexicon[form] {
				switch part.kind {
				case "prefix":
					if start != 0 || len(word)-end < 3 {
						continue
					}
					if after := shortMemoryPrefixes[part.spelling]; after != "" && !strings.ContainsRune(after, rune(word[end])) {
						continue
					}
				case "suffix":
					if end != len(word) || start < 3 || part.spelling == "ess" && !strings.HasSuffix(word, "tress") || len(part.spelling) == 2 && len(word) < 6 {
						continue
					}
				case "root":
					if end-start == 3 && (len(word) < 6 || start == 0 && len(word) < 7) {
						continue
					}
				}
				add(part, 0)
			}
		}
	}
	ordered := slices.Collect(maps.Values(fragments))
	slices.SortFunc(ordered, func(a, b memoryFragment) int {
		if rank := cmp.Compare(b.rank, a.rank); rank != 0 {
			return rank
		}
		if order := cmp.Compare(a.start, b.start); order != 0 {
			return order
		}
		return cmp.Compare(a.end, b.end)
	})
	primary := make([]memoryFragment, 0, 3)
	extras := make([]memoryFragment, 0, 4)
	for _, item := range ordered {
		if len(primary) < 3 && !slices.ContainsFunc(primary, func(chosen memoryFragment) bool {
			return chosen.start < item.end && item.start < chosen.end
		}) {
			primary = append(primary, item)
		} else {
			extras = append(extras, item)
		}
	}
	// Keep useful partial cues even for words with no nonoverlapping decomposition.
	count := min(max(0, 3-len(primary)), len(extras))
	slices.SortFunc(primary, func(a, b memoryFragment) int { return cmp.Compare(a.start, b.start) })
	hint := formatMemoryFragments(primary, true)
	if count > 0 {
		hint += "、" + formatMemoryFragments(extras[:count], false)
	}
	return hint, formatMemoryFragments(extras[count:min(len(extras), count+8)], false)
}

func formatMemoryFragments(parts []memoryFragment, combine bool) string {
	var out strings.Builder
	for i, fragment := range parts {
		if i > 0 {
			separator := "、"
			if combine && parts[i-1].end == fragment.start {
				separator = " + "
			}
			out.WriteString(separator)
		}
		out.WriteString(fragment.label + "（" + fragment.meaning + "）")
	}
	return out.String()
}
