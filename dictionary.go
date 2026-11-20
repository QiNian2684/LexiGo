package main

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

//go:embed assets/ecdict.tsv.gz
var dictionaryBytes []byte

//go:embed assets/learning.json
var learningJSON []byte

//go:embed assets/examples.json.gz
var exampleBytes []byte

//go:embed assets/example-pairs.json.gz
var examplePairBytes []byte

//go:embed assets/example-translations.json.gz
var exampleTranslationBytes []byte

//go:embed assets/example-expansion.json.gz
var exampleExpansionBytes []byte

type cardDetails struct {
	Meaning            string `json:"meaning"`
	POS                string `json:"pos"`
	IPA                string `json:"ipa"`
	IPAUK              string `json:"ipaUK"`
	IPAUS              string `json:"ipaUS"`
	Examples           string `json:"examples"`
	ExampleTranslation string `json:"exampleTranslation"`
	Etymology          string `json:"etymology"`
	Collocations       string `json:"collocations"`
	Note               string `json:"note"`
}

func (c cardDetails) size() int {
	return utf8.RuneCountInString(c.Meaning + c.POS + c.IPA + c.IPAUK + c.IPAUS +
		c.Examples + c.ExampleTranslation + c.Etymology + c.Collocations + c.Note)
}

type offlineEntry struct {
	cardDetails
	ECDICT    bool
	ExampleBy string
}

type sourcedExample struct {
	Examples           string `json:"examples"`
	ExampleTranslation string `json:"exampleTranslation"`
	By                 string `json:"by"`
}

type dictionary struct {
	once     sync.Once
	data     []byte
	index    []int
	notes    map[string]cardDetails
	examples map[string]sourcedExample
	pairs    map[string]sourcedExample
	expanded map[string]sourcedExample
	err      error
}

var bundledExamples = sync.OnceValues(func() (map[string]sourcedExample, error) {
	return readExamples(exampleBytes)
})

var bundledExamplePairs = sync.OnceValues(func() (map[string]sourcedExample, error) {
	pairs, err := readExamples(examplePairBytes)
	if err != nil {
		return nil, err
	}
	machine, err := readExamples(exampleTranslationBytes)
	if err != nil {
		return nil, err
	}
	originals, err := bundledExamples()
	if err != nil {
		return nil, err
	}
	for word, pair := range machine {
		original := originals[word]
		if _, exists := pairs[word]; !exists && original.By == "simple" &&
			original.ExampleTranslation == "" && original.Examples == pair.Examples {
			pair.By = "simple-mt"
			pairs[word] = pair
		}
	}
	return pairs, nil
})

var expandedExamples = sync.OnceValues(func() (map[string]sourcedExample, error) {
	return readExamples(exampleExpansionBytes)
})

func readExamples(packed []byte) (map[string]sourcedExample, error) {
	data, err := expand(packed)
	if err != nil {
		return nil, err
	}
	var examples map[string]sourcedExample
	err = json.Unmarshal(data, &examples)
	return examples, err
}

func expand(data []byte) ([]byte, error) {
	input, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer input.Close()
	return io.ReadAll(input)
}

func (d *dictionary) load() {
	d.once.Do(func() {
		d.data, d.err = expand(dictionaryBytes)
		if d.err != nil {
			return
		}
		d.index = make([]int, 0, 750_000)
		for position := 0; position < len(d.data); {
			d.index = append(d.index, position)
			end := bytes.IndexByte(d.data[position:], '\n')
			if end < 0 {
				break
			}
			position += end + 1
		}
		if d.err = json.Unmarshal(learningJSON, &d.notes); d.err != nil {
			return
		}
		d.examples, d.err = bundledExamples()
		if d.err == nil {
			d.pairs, d.err = bundledExamplePairs()
		}
		if d.err == nil {
			d.expanded, d.err = expandedExamples()
		}
	})
}

func (d *dictionary) lookup(word string) (offlineEntry, error) {
	d.load()
	if d.err != nil {
		return offlineEntry{}, fmt.Errorf("读取内置词典：%w", d.err)
	}
	key := []byte(word)
	getKey := func(i int) []byte {
		line := d.data[d.index[i]:]
		return line[:bytes.IndexByte(line, '\t')]
	}
	i := sort.Search(len(d.index), func(i int) bool {
		return bytes.Compare(getKey(i), key) >= 0
	})
	result := offlineEntry{}
	if i < len(d.index) && bytes.Equal(getKey(i), key) {
		line := d.data[d.index[i]:]
		line = line[:bytes.IndexByte(line, '\n')]
		fields := strings.SplitN(string(line), "\t", 4)
		result.IPA, result.POS = fields[1], fields[2]
		result.Meaning = strings.ReplaceAll(fields[3], "\x1f", "\n")
		result.ECDICT = true
	}
	if override, ok := d.notes[word]; ok {
		ipa := result.IPA
		result.cardDetails = override
		result.IPA = ipa
	} else {
		example := d.examples[word]
		if pair, ok := d.pairs[word]; ok {
			example = pair
		}
		if supplemental, ok := d.expanded[word]; ok &&
			(example.Examples == "" || example.ExampleTranslation == "" && supplemental.ExampleTranslation != "") {
			example = supplemental
		}
		result.Examples = example.Examples
		result.ExampleTranslation = example.ExampleTranslation
		result.ExampleBy = example.By
	}
	return result, nil
}

func (c cardDetails) needsOnline() bool {
	return c.Meaning == "" || c.POS == "" || c.Examples == "" || c.ExampleTranslation == "" || c.Etymology == ""
}

func (c *cardDetails) fill(from cardDetails) (used bool) {
	if c.Examples == "" && c.ExampleTranslation == "" && from.Examples != "" {
		c.Examples = from.Examples
		used = true
	}
	if c.ExampleTranslation == "" {
		if translation := matchingExampleTranslation(c.Examples, from); translation != "" {
			c.ExampleTranslation = simplifyExampleTranslation(translation)
			used = true
		}
	}
	for _, pair := range [][2]*string{
		{&c.Meaning, &from.Meaning}, {&c.POS, &from.POS}, {&c.IPA, &from.IPA},
		{&c.IPAUK, &from.IPAUK}, {&c.IPAUS, &from.IPAUS}, {&c.Etymology, &from.Etymology},
		{&c.Collocations, &from.Collocations}, {&c.Note, &from.Note},
	} {
		if *pair[0] == "" && *pair[1] != "" {
			*pair[0] = *pair[1]
			used = true
		}
	}
	return used
}

func matchingExampleTranslation(examples string, source cardDetails) string {
	if examples == "" || source.ExampleTranslation == "" {
		return ""
	}
	if examples == source.Examples {
		return source.ExampleTranslation
	}
	sentences := strings.Split(source.Examples, "\n\n")
	translations := strings.Split(source.ExampleTranslation, "\n\n")
	if len(sentences) != len(translations) {
		return ""
	}
	var matched []string
	for _, sentence := range strings.Split(examples, "\n\n") {
		index := slices.Index(sentences, sentence)
		if index < 0 || translations[index] == "" {
			return ""
		}
		matched = append(matched, translations[index])
	}
	return strings.Join(matched, "\n\n")
}
