"""Attribute first rejection for each MorphyNet row through the 1.8 pipeline."""
import csv
import gzip
import json
from collections import Counter, defaultdict
from pathlib import Path
import sys

import build_morphology as old

def audit(path, dictionary):
    counts, edges = Counter(), defaultdict(set)
    for row in csv.reader(path.open(encoding="utf-8"), delimiter="\t"):
        if len(row) != 6:
            counts["invalid_row"] += 1
            continue
        base, word, a, b, affix, kind = row
        if base not in dictionary or word not in dictionary:
            counts["not_in_dictionary"] += 1
        elif word in old.BLOCKED:
            counts["blocklist"] += 1
        elif len(base) < 3:
            counts["short_stem"] += 1
        elif not old.SPELLING.fullmatch(base) or not old.SPELLING.fullmatch(word) or base == word:
            counts["unsupported_spelling_or_self"] += 1
        elif kind not in {"prefix", "suffix"}:
            counts["other_relation"] += 1
        elif kind == "prefix" and word not in {affix + base, affix + "-" + base}:
            counts["prefix_boundary"] += 1
        elif kind == "suffix" and (not affix or not old.suffix_spells(base, affix, word)):
            counts["suffix_boundary"] += 1
        elif kind == "prefix" and affix not in old.PREFIX and affix != "un":
            counts["prefix_not_in_inventory"] += 1
        elif kind == "suffix" and affix not in {*old.SUFFIX, "ly", "er", "or"}:
            counts["suffix_not_in_inventory"] += 1
        else:
            meaning = (old.PREFIX if kind == "prefix" else old.SUFFIX).get(affix)
            pos_fail = (
                kind == "prefix" and affix in {"re", "out"} and (a, b) != ("V", "V")
                or kind == "prefix" and affix == "un" and (a, b) not in {("J", "J"), ("V", "V")}
                or kind == "suffix" and affix in {"y", "able", "ible"} and b != "J"
                or kind == "suffix" and affix == "en" and (a, b) != ("J", "V")
                or kind == "suffix" and affix == "ly" and (a, b) != ("J", "R")
                or kind == "suffix" and affix in {"er", "or"} and (a, b) != ("V", "N")
            )
            if pos_fail:
                counts["pos_missing" if "U" in (a, b) or not a or not b else "pos_rejected"] += 1
            else:
                counts["accepted_edge"] += 1
                sense = a if kind == "prefix" and affix == "un" else ""
                edges[word].add((kind, affix, meaning, sense))
    return dict(counts), sum(len(values) > 1 for values in edges.values())


if __name__ == "__main__":
    with gzip.open(old.ASSETS / "ecdict.tsv.gz", "rt", encoding="utf-8") as file:
        dictionary = {row.split("\t", 1)[0] for row in file}
    reasons, ambiguous = audit(Path(sys.argv[1]), dictionary)
    print(json.dumps({"rows": reasons, "total": sum(reasons.values()), "targets_dropped_for_multiple_hints": ambiguous}, indent=2))
