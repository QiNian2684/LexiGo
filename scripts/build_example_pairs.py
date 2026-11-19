"""Supplement existing English-only examples with direct Tatoeba sentence pairs.

Arguments: examples.json.gz, eng_sentences_detailed.tsv.bz2,
cmn_sentences_detailed.tsv.bz2, eng-cmn_links.tsv.bz2, output.json.gz.
Download the last three source files from https://downloads.tatoeba.org/exports/per_language/.
"""

import argparse
import bz2
import gzip
import json
import re
from collections import defaultdict
from pathlib import Path


TOKEN = re.compile(r"[A-Za-z]+(?:['-][A-Za-z]+)*")
HAN = re.compile(r"[\u3400-\u9fff]")
MARKUP = re.compile(r"[<>#\r\n]|https?://")
NAMES = re.compile(r"\b(?:Tom|Mary)\b")
REJECTED_LINKS = {
    (10860375, 13109043),  # “monthly” was translated as “每天”.
    (8987965, 11841623),  # “on the committee” modifies the wrong phrase.
}


def sentences(path):
    with bz2.open(path, "rt", encoding="utf-8") as source:
        for row in source:
            fields = row.rstrip("\r\n").split("\t", 4)
            if len(fields) >= 4 and fields[3] not in ("", r"\N"):
                yield int(fields[0]), fields[2], fields[3]


def normalized(sentence):
    return " ".join(sentence.replace("’", "'").split())


def build(base, english, chinese, links):
    with gzip.open(base, "rt", encoding="utf-8") as source:
        gaps = {word: item["examples"] for word, item in json.load(source).items()
                if item.get("by") == "simple" and not item.get("exampleTranslation")}
    exact = defaultdict(list)
    for word, example in gaps.items():
        exact[normalized(example)].append(word)
    max_phrase = max((len(word.split()) for word in gaps), default=1)
    translations = {
        num: (phrase, author) for num, phrase, author in sentences(chinese)
        if 4 <= len(phrase) <= 145 and HAN.search(phrase) and not MARKUP.search(phrase)
    }

    def translation_score(num):
        phrase, _ = translations[num]
        return (6 <= len(phrase) <= 80, phrase.endswith((".", "!", "?", "。", "！", "？")), -num)

    # This export contains direct English-to-Mandarin links. No translation
    # is inferred from a shared headword or through an intermediate language.
    paired = {}
    with bz2.open(links, "rt", encoding="utf-8") as source:
        for row in source:
            eng_id, zh_id = map(int, row.split())
            if (eng_id, zh_id) in REJECTED_LINKS:
                continue
            if zh_id in translations and (eng_id not in paired or
                    translation_score(zh_id) > translation_score(paired[eng_id])):
                paired[eng_id] = zh_id

    best = {}
    for eng_id, phrase, author in sentences(english):
        if eng_id not in paired or len(phrase) > 160 or MARKUP.search(phrase):
            continue
        clean = normalized(phrase)
        if not clean.endswith((".", "!", "?")):
            continue
        folded = clean.lower()
        matches = list(TOKEN.finditer(folded))
        if not 3 <= len(matches) <= 25:
            continue
        words = set(exact.get(clean, ()))
        for start, first in enumerate(matches):
            for end in range(start, min(len(matches), start + max_phrase)):
                term = folded[first.start():matches[end].end()]
                if term in gaps:
                    words.add(term)
        if not words:
            continue
        zh_id = paired[eng_id]
        translation, zh_author = translations[zh_id]
        rank = -abs(len(matches) - 8) - 5 * bool(NAMES.search(phrase)) - phrase.count('"')
        item = {
            "examples": phrase, "exampleTranslation": translation,
            "by": f"tatoeba|{eng_id}:{author}|{zh_id}:{zh_author}",
        }
        for word in words:
            score = (clean == normalized(gaps[word]), rank, translation_score(zh_id), -eng_id)
            if word not in best or score > best[word][0]:
                best[word] = (score, item)
    return best


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("base", "english", "chinese", "links", "output"):
        parser.add_argument(name, type=Path)
    args = parser.parse_args()
    pairs = build(args.base, args.english, args.chinese, args.links)
    payload = json.dumps({key: value for key, (_, value) in pairs.items()},
                         ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
    with args.output.open("wb") as output:
        with gzip.GzipFile(fileobj=output, filename="", mode="wb", mtime=0) as archive:
            archive.write(payload)
    print(f"{len(pairs):,} pairs added, {sum(score[0] for score, _ in pairs.values()):,} exact sentence matches")
