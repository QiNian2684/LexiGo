"""Assemble the shared offline usage example index from prepared corpora.

The input directory contains the JSON indexes named below. Each entry is
keyed by spelling and holds "examples", optional "exampleTranslation",
and "by" (the original corpus attribution). translations.jsonl holds
[English sentence, simplified Chinese translation] lines from OPUS-MT.
Use local, licensed source material only. See Dictionary-data-notices.txt
for the snapshots and publisher links.
"""

import argparse
import gzip
import json
import re
from pathlib import Path

from build_example_translations import clean, read_gzip, simplifier

ROOT = Path(__file__).resolve().parents[1]
TOKEN = re.compile(r"[a-z]+(?:['-][a-z]+)*")
FOLD = str.maketrans({"’": "'", "‘": "'", "‑": "-", "‐": "-"})


def is_usage_example(word, example):
    normalized = example.lower().translate(FOLD)
    if "citations:" in normalized or "quotations using this term" in normalized:
        return False
    parts = word.split()
    matches = list(TOKEN.finditer(normalized))
    for i in range(len(matches) - len(parts) + 1):
        window = matches[i:i + len(parts)]
        if parts and [match.group() for match in window] == parts:
            if all(not normalized[a.end():b.start()].strip(" ,\t") for a, b in zip(window, window[1:])):
                return True
    return False


def assemble(source, output):
    assets = ROOT / "assets"
    baseline = read_gzip(assets / "examples.json.gz")
    for name in ("example-translations", "example-pairs"):
        baseline.update(read_gzip(assets / f"{name}.json.gz"))
    baseline.update(json.loads((assets / "learning.json").read_text(encoding="utf-8")))
    complete = {word for word, row in baseline.items() if row.get("examples") and row.get("exampleTranslation")}

    def read_corpus(name):
        rows = json.loads((source / f"{name}.json").read_text(encoding="utf-8"))
        return {word: row for word, row in rows.items()
                if row.get("by") and is_usage_example(word, row["examples"])}

    pairs = read_corpus("tatoeba-paired")
    known = complete | pairs.keys()
    candidates = {}
    # More specific usage sources take precedence over broad article context.
    for name in ("wikipedia-secondary", "wiktionary-secondary", "tatoeba-all", "wiki-english"):
        candidates.update(read_corpus(name))
    for word, row in baseline.items():
        if word not in known and row.get("examples") and row.get("by") == "simple":
            candidates[word] = dict(row, by="simple-mt")

    simplify = simplifier(assets / "opencc-t2s.tsv")
    with (source / "translations.jsonl").open(encoding="utf-8") as lines:
        translations = {en: clean(en, simplify(zh)) for en, zh in map(json.loads, lines)}
    for word, row in candidates.items():
        if word in known:
            continue
        translation = translations.get(row["examples"])
        pairs[word] = dict(row, exampleTranslation=translation) if translation else row
    payload = json.dumps(pairs, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()
    output.write_bytes(gzip.compress(payload, mtime=0))
    print(f"{len(pairs):,} example records, {sum(bool(row.get('exampleTranslation')) for row in pairs.values()):,} bilingual")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("prepared_sources", type=Path)
    parser.add_argument("--output", type=Path, default=ROOT / "assets" / "example-expansion.json.gz")
    arguments = parser.parse_args()
    assemble(arguments.prepared_sources, arguments.output)
