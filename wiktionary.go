package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

type onlineEntry struct {
	cardDetails
	Revision int64
}

var wikiPOS = map[string]string{
	"Noun": "名词", "Proper noun": "专有名词", "Verb": "动词",
	"Adjective": "形容词", "Adverb": "副词", "Preposition": "介词",
	"Pronoun": "代词", "Conjunction": "连词", "Interjection": "感叹词",
	"Determiner": "限定词", "Article": "冠词", "Numeral": "数词",
	"Participle": "分词", "Particle": "小品词", "Phrase": "短语",
	"Proverb": "谚语", "Contraction": "缩约词", "Initialism": "首字母缩略词",
	"Prefix": "前缀", "Suffix": "后缀", "Infix": "中缀",
}

var wikiClient = &http.Client{Timeout: 14 * time.Second}

func fetchWiktionary(ctx context.Context, word string) (onlineEntry, error) {
	params := url.Values{
		"action": {"parse"}, "page": {word}, "prop": {"text|revid"},
		"format": {"json"}, "formatversion": {"2"}, "redirects": {"1"}, "maxlag": {"5"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://en.wiktionary.org/w/api.php?"+params.Encode(), nil)
	if err != nil {
		return onlineEntry{}, err
	}
	req.Header.Set("User-Agent", "WordReviewDesktop/1.0 (personal vocabulary flashcards; https://en.wiktionary.org/wiki/Wiktionary:About)")
	req.Header.Set("Accept", "application/json")
	response, err := wikiClient.Do(req)
	if err != nil {
		return onlineEntry{}, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusTooManyRequests {
		return onlineEntry{}, fmt.Errorf("词典请求过于频繁，请稍后重试")
	}
	if response.StatusCode != http.StatusOK {
		return onlineEntry{}, fmt.Errorf("词典请求失败 (%d)", response.StatusCode)
	}
	var data struct {
		Parse struct {
			Text  string `json:"text"`
			RevID int64  `json:"revid"`
		} `json:"parse"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&data); err != nil {
		return onlineEntry{}, err
	}
	switch data.Error.Code {
	case "missingtitle", "nosuchpage", "invalidtitle":
		return onlineEntry{}, nil
	case "":
	default:
		return onlineEntry{}, fmt.Errorf("词典请求失败 (%s)", data.Error.Code)
	}
	if data.Parse.RevID <= 0 || data.Parse.Text == "" {
		return onlineEntry{}, fmt.Errorf("词典返回的数据不完整")
	}
	return onlineEntry{parseWiktionary(data.Parse.Text), data.Parse.RevID}, nil
}

func wikiSource(revision int64) string {
	return "https://en.wiktionary.org/wiki/Special:PermanentLink/" + strconv.FormatInt(revision, 10) + "#English"
}

func attribute(node *html.Node, key string) string {
	for _, a := range node.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(node *html.Node, name string) bool {
	return slices.Contains(strings.Fields(attribute(node, "class")), name)
}

func walkHTML(node *html.Node, visit func(*html.Node) bool) {
	if visit(node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walkHTML(child, visit)
		}
	}
}

func plainText(node *html.Node) string {
	var text strings.Builder
	walkHTML(node, func(n *html.Node) bool {
		switch n.Data {
		case "sup", "style", "script":
			return false
		}
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
		}
		return true
	})
	return strings.Join(strings.Fields(text.String()), " ")
}

func parseWiktionary(source string) cardDetails {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return cardDetails{}
	}
	var result cardDetails
	var pos, examples, etymologies, chinese []string
	var inEnglish, inEtymology, inTranslations bool
	walkHTML(root, func(node *html.Node) bool {
		if node.Type != html.ElementNode {
			return true
		}
		switch node.Data {
		case "h2":
			inEnglish = attribute(node, "id") == "English"
			inEtymology = false
			inTranslations = false
		case "h3", "h4", "h5", "h6":
			if inEnglish {
				heading := plainText(node)
				inEtymology = heading == "Etymology" || strings.HasPrefix(heading, "Etymology ")
				inTranslations = heading == "Translations"
				if label := wikiPOS[heading]; label != "" && !slices.Contains(pos, label) {
					pos = append(pos, label)
				}
			}
		}
		if !inEnglish {
			return true
		}
		if inEtymology && node.Data == "p" && len(etymologies) < 2 {
			p := plainText(node)
			if len(p) > 0 && utf8.RuneCountInString(p) <= 850 {
				etymologies = append(etymologies, p)
			}
		}
		if len(examples) < 2 && hasClass(node, "e-example") && attribute(node, "lang") == "en" {
			p := plainText(node)
			if len(p) > 0 && utf8.RuneCountInString(p) <= 255 && !slices.Contains(examples, p) {
				examples = append(examples, p)
			}
		}
		if inTranslations && len(chinese) < 6 && hasClass(node, "Hans") && attribute(node, "lang") == "cmn" {
			p := plainText(node)
			if len(p) > 0 && utf8.RuneCountInString(p) <= 32 && !slices.Contains(chinese, p) {
				chinese = append(chinese, p)
			}
		}
		if hasClass(node, "IPA") {
			if ipa := plainText(node); strings.HasPrefix(ipa, "/") && strings.HasSuffix(ipa, "/") {
				ipa = strings.Trim(ipa, "/")
				if result.IPA == "" {
					result.IPA = ipa
				}
				for parent := node.Parent; parent != nil; parent = parent.Parent {
					if parent.Data != "li" {
						continue
					}
					label := plainText(parent)
					if result.IPAUK == "" && (strings.Contains(label, "Received Pronunciation") || strings.HasPrefix(label, "(UK")) {
						result.IPAUK = ipa
					}
					if result.IPAUS == "" && (strings.Contains(label, "General American") || strings.HasPrefix(label, "(US")) {
						result.IPAUS = ipa
					}
					break
				}
			}
		}
		return true
	})
	result.Meaning = strings.Join(chinese, "；")
	result.POS = strings.Join(pos, "、")
	result.Examples = strings.Join(examples, "\n\n")
	result.Etymology = strings.Join(etymologies, "\n\n")
	return result
}
