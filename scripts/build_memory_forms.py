"""Build a compact regular form-ending index from the existing ECDICT CSV.

Usage: python scripts/build_memory_forms.py ecdict.csv
Only words with an explicit base form + transformation are accepted.
"""
import csv
import re
import sys
from pathlib import Path
from build_morphology import ASSETS, save_index, suffix_spells

ENDINGS = {
    "s": ("es", "s"), "3": ("es", "s"), "p": ("ed",), "d": ("ed",),
    "i": ("ing",), "r": ("er",), "t": ("est",),
}
FORM = re.compile(r"[a-z]{4,32}")

def regular_ending(word, base, suffix):
    return (
        len(word) - len(suffix) >= 2 and word.endswith(suffix) and
        (suffix_spells(base, suffix, word)
         or suffix in {"ed", "er", "est"} and base.endswith("e") and word == base + suffix[1:])
    )

def build(path):
    records = {}
    with path.open(encoding="utf-8-sig", newline="") as file:
        for row in csv.DictReader(file):
            word = row["word"]
            if not FORM.fullmatch(word) or not row["exchange"]:
                continue
            fields = dict(record.split(":", 1) for record in row["exchange"].split("/") if ":" in record)
            base = fields.get("0", "")
            if base == word or len(base) < 3 or not base.isascii() or not base.isalpha() or base != base.lower():
                continue
            for marker in sorted(set(fields.get("1", ""))):
                for suffix in ENDINGS.get(marker, ()):
                    if regular_ending(word, base, suffix):
                        records[word] = marker + suffix
                        break
                if word in records:
                    break
    return records

if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    save_index(ASSETS / "memory-forms.tsv.gz", build(Path(sys.argv[1])))
