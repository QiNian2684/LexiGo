"""Build a small index of sourced examples from redistributable datasets."""
import bz2
import collections
import gzip
import json
import re
import sys
import tarfile

simple, output, *bilingual = sys.argv[1:]
tokens = re.compile(r"[A-Za-z]+(?:['-][A-Za-z]+)*")
records = {}
with gzip.open(simple, "rt", encoding="utf-8") as source:
    for line in source:
        obj = json.loads(line)
        word = obj.get("word", "").lower().strip()
        if not word or obj.get("lang_code") != "en":
            continue
        for sense in obj.get("senses", []):
            for example in sense.get("examples", []):
                phrase = " ".join(example.get("text", "").split())
                if (6 <= len(phrase) <= 150 and 3 <= len(phrase.split()) <= 22
                        and phrase.endswith((".", "!", "?")) and "''" not in phrase):
                    if word in {match.lower() for match in tokens.findall(phrase)} or (
                        " " in word and f" {word} " in f" {phrase.lower()} "
                    ):
                        records.setdefault(word, {"examples": phrase, "by": "simple"})
                        break
            if word in records:
                break

if bilingual:
    english_file, chinese_file, links_file = bilingual

    def sentences(path):
        with bz2.open(path, "rt", encoding="utf-8") as source:
            for row in source:
                fields = row.rstrip("\r\n").split("\t", 4)
                if len(fields) >= 4 and fields[3] not in ("", r"\N"):
                    yield fields[0].encode(), fields[2], fields[3]

    chinese = {}
    for key, phrase, author in sentences(chinese_file):
        if 5 <= len(phrase) <= 75 and re.search(r"[\u3400-\u9fff]", phrase):
            chinese[key] = (phrase, author)

    linked = collections.defaultdict(list)
    with tarfile.open(links_file, "r|bz2") as archive:
        member = archive.next()
        for row in archive.extractfile(member):
            id1, id2 = row.rstrip(b"\n").split(b"\t")
            if id1 in chinese:
                linked[id2].append(id1)

    scored = {}
    for key, en, en_author in sentences(english_file):
        if key not in linked:
            continue
        words = [w.lower() for w in tokens.findall(en)]
        if not (5 <= len(words) <= 18 and 16 <= len(en) <= 120 and en.endswith((".", "!", "?"))):
            continue
        if "Tom" in en or "Mary" in en or en.count('"') > 1:
            continue
        simplified = set("这说为会认识没来时应发给体馆们书问过学电气实关风国从对经统点带见长种让请当")
        traditional = set("這說為會認識沒來時應發給體館們書問過學電氣實關風國從對經統點帶見長種讓請當")
        def translation_score(zhid):
            zh = chinese[zhid][0]
            return sum(c in simplified for c in zh) - 6 * sum(c in traditional for c in zh)
        zh_id = max(linked[key], key=translation_score)
        zh, zh_author = chinese[zh_id]
        score = abs(len(words) - 8) * 3 + en.count(",") - min(3, translation_score(zh_id))
        details = {"examples": en, "exampleTranslation": zh, "by":
                   f"tatoeba|{key.decode()}:{en_author}|{zh_id.decode()}:{zh_author}"}
        for word in set(words):
            if len(word) > 2 and score < scored.get(word, 100):
                scored[word] = score
                records[word] = details

with open(output, "wb") as target:
    with gzip.GzipFile(fileobj=target, filename="", mode="wb", mtime=0) as packed:
        packed.write(json.dumps(records, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode())
print(f"{len(records):,} words with examples; sources: {collections.Counter(v['by'].split('|')[0] for v in records.values())}")
