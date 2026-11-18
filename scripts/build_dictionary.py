"""Build the compact offline index from a pinned ECDICT checkout."""
import csv
import gzip
import re
import sys

csv.field_size_limit(1_000_000)
entry = re.compile(r"[A-Za-z]+(?:[ '-][A-Za-z]+)*\Z")
pos = {
    "a": "形容词", "adj": "形容词", "ad": "副词", "adv": "副词",
    "n": "名词", "v": "动词", "vt": "及物动词", "vi": "不及物动词",
    "prep": "介词", "pron": "代词", "conj": "连词", "int": "感叹词",
    "interj": "感叹词", "det": "限定词", "art": "冠词", "num": "数词",
    "aux": "助动词", "modal": "情态动词", "abbr": "缩写",
}
rows = {}
with open(sys.argv[1], encoding="utf-8-sig", newline="") as source:
    for item in csv.DictReader(source):
        word = item["word"].strip()
        key = word.lower()
        if not entry.fullmatch(word) or not item["translation"].strip():
            continue
        if key in rows and (rows[key][0] == key or word != key):
            continue
        definitions = item["translation"].replace(r"\n", "\n").splitlines()
        labels = []
        for line in definitions:
            prefix = re.match(r"^\s*([a-z]+)/?\.", line)
            if prefix and prefix[1] in pos and pos[prefix[1]] not in labels:
                labels.append(pos[prefix[1]])
        def clean(s):
            return " ".join(s.replace("\x1f", " ").split())
        meaning = "\x1f".join(clean(d) for d in definitions if d.strip())
        rows[key] = (word, clean(item["phonetic"]), "、".join(labels), meaning)

with open(sys.argv[2], "wb") as target:
    with gzip.GzipFile(fileobj=target, filename="", mode="wb", mtime=0) as compressed:
        for key, (_, ipa, labels, meaning) in sorted(rows.items()):
            compressed.write(f"{key}\t{ipa}\t{labels}\t{meaning}\n".encode())
print(f"{len(rows):,} indexed entries")
