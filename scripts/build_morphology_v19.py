"""Build 1.9 evidence-based morphology overlays. Runtime uses only the gzipped TSVs.

Usage:
  python scripts/build_morphology_v19.py MorphyNet.tsv linxie.csv MorphoLEX_en.xlsx etymdb.csv ecdict.csv
The workbook reader needs openpyxl when rebuilding from xlsx. Existing assets need no Python.
"""
import csv
import gzip
import re
import sys
import unicodedata
from collections import Counter, defaultdict
from functools import lru_cache
from pathlib import Path
from typing import NamedTuple

import build_morphology as legacy

AFFIX = re.compile(r"^[a-z]{2,8}$")
MARKED = re.compile(r"<([a-z]+)<|\(([a-z]+)\)|>([a-z]+)>")
PREFIX = {
    **legacy.PREFIX,
    "ante": "在前", "circum": "围绕", "contra": "相反", "en": "置于；使成为",
    "em": "置于；使成为", "ex": "向外", "extra": "在外；超出",
    "iso": "相等", "mal": "不良", "meta": "关于自身；超越", "omni": "一切",
    "peri": "周围", "poly": "多", "pro": "向前；支持", "retro": "向后；回顾",
    "un": "不；解除",
}
NEGATIVE = {"in": None, "im": "bmp", "il": "l", "ir": "r"}
AD_VARIANTS = {"ad": None, "ac": "c", "af": "f", "ag": "g", "al": "l", "an": "n",
               "ap": "p", "ar": "r", "as": "s", "at": "t"}
CON_VARIANTS = {"con": None, "com": "bmp", "col": "l", "cor": "r"}
SUFFIX = {
    **legacy.SUFFIX, "acy": "状态；性质", "age": "集合、产物或行为", "ant": "执行者；有此性质的",
    "ent": "执行者；有此性质的", "ate": "使成为；进行", "ee": "动作的承受者",
    "er": "执行者或工具", "or": "执行者或工具", "ery": "场所、行业或行为",
    "ification": "……化的过程", "ism": "学说；制度；倾向", "ology": "学科；研究",
    "ory": "与……相关", "ure": "行为；结果",
}
ROOTS = {**legacy.ROOTS,
    "bio": "生命", "capt": "取；抓住", "ceiv": "取；接受", "cept": "取；接受", "cip": "取；接受", "clud": "关闭",
    "clus": "关闭", "duc": "引导；带领", "fac": "做；制造", "fact": "做；制造",
    "fect": "做；造成", "flu": "流动", "form": "形状；形成",
    "fract": "打破", "frag": "破碎", "lect": "挑选；收集",
    "leg": "挑选；读", "linqu": "离开；放弃", "lus": "玩耍", "lud": "玩耍",
    "mis": "送出", "miss": "送出", "mit": "送出", "mov": "移动", "mot": "移动",
    "pel": "推；驱赶", "puls": "推；驱赶", "plic": "折叠；卷入",
    "pon": "放置", "pos": "放置", "port": "搬运", "prehend": "抓住；领会",
    "prehens": "抓住；领会", "scrib": "写", "script": "写", "sed": "坐；停留",
    "sess": "坐；停留", "sid": "坐；停留", "sent": "感受", "sist": "站；使站立",
    "spec": "看；观察", "tact": "触碰", "tang": "触碰", "tag": "触碰",
    "ten": "握持；保持", "tain": "握持；保持", "tent": "伸展", "tens": "伸展",
    "ven": "来", "vent": "来", "vid": "看", "vis": "看", "voc": "叫；声音",
}
BASES = {
    "build": "建造", "care": "关心", "change": "改变", "do": "做", "heat": "加热",
    "help": "帮助", "kind": "善良的", "like": "喜欢；像", "move": "移动", "play": "玩",
    "read": "读", "set": "设置；放置", "think": "思考", "trust": "信任", "use": "使用",
    "view": "看", "work": "工作", "write": "写",
}
BLOCKED = legacy.BLOCKED | {
    "adore", "audacity", "audacious", "audit", "hospital", "hospitality", "item",
    "opera", "comet", "combat", "comfort", "committee", "salary", "island", "brother",
    "analysis", "chaos", "civet", "winter", "caterpillar", "marriage", "vaccine",
    "honey", "similar", "discuss", "biscuit", "digest", "angel",
}
# These depend on exact historical families. Only dataset-attested paths from
# one of these normalized lexemes may suggest one of the listed English stems.
LATIN = {
    "capio": "cept cip capt", "capere": "cept cip capt", "dicere": "dict",
    "dico": "dict", "pono": "posit pon pos", "ponere": "posit pon pos",
    "ducere": "duct duc", "duco": "duct duc", "facere": "fact fect fac",
    "facio": "fact fect fac", "fero": "fer", "ferre": "fer",
    "flecto": "flect flex", "frango": "fract frag", "frangere": "fract frag",
    "gradior": "gress grad", "lego": "lect", "legere": "lect",
    "mitto": "miss mis mit", "mittere": "miss mis mit", "moveo": "mov mot",
    "pello": "pel puls", "pellere": "pel puls", "portare": "port", "porto": "port",
    "prehendo": "prehend prehens", "rumpo": "rupt", "rumpere": "rupt",
    "scribo": "scrib script", "scribere": "scrib script",
    "sedeo": "sed sess sid", "sentio": "sent sens", "sisto": "sist",
    "specto": "spect spec", "specio": "spect spec", "specere": "spect spec",
    "tango": "tact tang", "teneo": "ten tain", "traho": "tract", "trahere": "tract",
    "venio": "ven vent", "venire": "ven vent", "verto": "vert vers",
    "vertere": "vert vers",
    "videre": "vid vis", "video": "vid vis", "voco": "voc",
}
GREEK = {
    "γράφω": "graph", "γραφή": "graph",
    "ἄνθρωπος": "anthrop", "γῆ": "geo", "φωνή": "phon", "χρόνος": "chron", "ψυχή": "psych",
    "θερμός": "therm", "ὕδωρ": "hydr", "δέρμα": "derm", "καρδία": "cardio", "βίος": "bio",
}
SHORT_BASES = {"do", "go", "be"}

class Part(NamedTuple):
    kind: str
    text: str

def hint(kind, form, gloss):
    label = {"prefix": form + "-", "suffix": "-" + form, "root": form}[kind]
    return Part(kind, label + "（" + gloss + "）")

def canonical(word):
    return "".join(c for c in unicodedata.normalize("NFD", word.lower()) if not unicodedata.combining(c))

def prefix_gloss(affix, stem, pos=""):
    if affix in NEGATIVE and (not NEGATIVE[affix] or stem[:1] in NEGATIVE[affix]):
        return "不；非"
    if affix in AD_VARIANTS and (not AD_VARIANTS[affix] or stem[:1] in AD_VARIANTS[affix]):
        return "向；加强"
    if affix in CON_VARIANTS and (not CON_VARIANTS[affix] or stem[:1] in CON_VARIANTS[affix]):
        return "共同；加强"
    if affix == "un":
        return "解除；逆转" if pos in {"V", "VB"} else "不；非"
    return PREFIX.get(affix)

def suffix_surface(stem, suffix, word):
    if len(stem) >= 3 and legacy.suffix_spells(stem, suffix, word):
        return True
    if word == stem + suffix:
        return True
    if suffix == "ity" and stem.endswith(("ble", "ile")) and word == stem[:-2] + "ility":
        return True
    if suffix in {"ation", "ion", "ification"} and stem.endswith("ify") and word == stem[:-3] + "ification":
        return True
    return False

def stem_part(stem):
    gloss = BASES.get(stem)
    return (hint("root", stem, gloss),) if gloss else ()

def common(analyses):
    analyses = tuple(dict.fromkeys(analyses))
    if len(analyses) <= 1:
        return analyses[0] if analyses else ()
    return tuple(part for part in analyses[0] if all(part in item for item in analyses[1:]))

def regular_edges(source, dictionary):
    edges = defaultdict(list)
    counts = Counter()
    with Path(source).open(encoding="utf-8") as file:
        for row in csv.reader(file, delimiter="\t"):
            if len(row) != 6:
                continue
            base, word, from_pos, to_pos, affix, kind = row
            if (word not in dictionary or base not in dictionary or word in BLOCKED
                    or (len(base) < 3 and base not in SHORT_BASES) or base == word
                    or not legacy.SPELLING.fullmatch(base) or not legacy.SPELLING.fullmatch(word)
                    or not AFFIX.fullmatch(affix)):
                continue
            if kind == "prefix" and word in {affix + base, affix + "-" + base}:
                # Classical allomorphs require a structured lexical analysis,
                # rather than a MorphyNet spelling edge on its own.
                if affix in NEGATIVE or affix in AD_VARIANTS or affix in CON_VARIANTS:
                    continue
                meaning = prefix_gloss(affix, base, from_pos)
                if affix == "out" and (from_pos, to_pos) != ("V", "V"):
                    continue
                if affix == "un" and (from_pos, to_pos) not in {("V", "V"), ("J", "J"), ("N", "N")}:
                    continue
                if affix == "re" and from_pos != to_pos:
                    continue
                if affix == "re" and word == "remark":
                    continue
            elif kind == "suffix" and suffix_surface(base, affix, word):
                meaning = SUFFIX.get(affix)
                if affix in {"able", "ible", "y"} and to_pos != "J":
                    continue
                if affix in {"er", "or"} and (from_pos, to_pos) != ("V", "N"):
                    continue
                if affix == "en" and (from_pos, to_pos) != ("J", "V"):
                    continue
                if affix == "ly":
                    meaning = "以……方式" if to_pos == "R" else None
                if affix == "ate" and to_pos != "V":
                    meaning = None
                if word.endswith("ification") and base.endswith("ify"):
                    affix, meaning = "ification", SUFFIX["ification"]
            else:
                continue
            if meaning:
                edges[word].append((hint(kind, affix, meaning), base))
                counts[kind] += 1

    @lru_cache(maxsize=None)
    def visit(word, depth):
        choices = []
        if depth:
            for part, stem in edges.get(word, ()):
                if len(stem) >= len(word):
                    continue
                inner = visit(stem, depth - 1)
                pieces = (part, *inner) if part.kind == "prefix" else (*inner, part)
                choices.append(pieces)
        return common(choices) if choices else stem_part(word)

    result = {w: visit(w, 3) for w in edges}
    print("MorphyNet accepted edges:", dict(counts), file=sys.stderr)
    return {w: parts for w, parts in result.items() if parts}

def linxie_rows(path, dictionary):
    for row in csv.DictReader(Path(path).open(encoding="utf-8-sig", newline="")):
        word = row["word"]
        if word in BLOCKED or word not in dictionary:
            continue
        forms = row["segmentation"].split("+")
        morphemes = [m.strip(" -") for m in row["morphemes"].split("+")]
        roles = row["structure"].split("+")
        if word.replace("-", "") != "".join(forms).replace("-", "") or not len(forms) == len(morphemes) == len(roles):
            continue
        parts = []
        for surface, morph, role in zip(forms, morphemes, roles):
            if role == "prefix" and surface == morph and morph not in {*NEGATIVE, *CON_VARIANTS, *AD_VARIANTS, "un", "re", "out"}:
                gloss = PREFIX.get(morph)
            elif role == "suffix" and surface == morph and morph not in {"er", "or", "y", "en"}:
                gloss = "以……方式" if morph == "ly" else SUFFIX.get(morph)
            elif role == "root":
                # Linxie labels modern English bases.
                gloss = BASES.get(morph)
            else:
                gloss = None
            if gloss:
                parts.append(hint(role, morph, gloss))
        if any(part.kind != "root" for part in parts):
            yield word, tuple(parts)

def rows(path):
    path = Path(path)
    if path.suffix != ".xlsx":
        yield from csv.reader(path.open(encoding="utf-8"), delimiter="\t")
        return
    import openpyxl
    workbook = openpyxl.load_workbook(path, read_only=True, data_only=True)
    for sheet in workbook:
        if not sheet.title[0].isdigit():
            continue
        table = iter(sheet.values)
        header = next(table)
        indexes = [header.index(k) for k in ("Word", "POS", "MorphoLexSegm")]
        for row in table:
            word, pos, marked = (row[i] for i in indexes)
            if isinstance(word, str) and isinstance(marked, str):
                yield word, pos, marked

def regular_lemmas(path):
    endings = {"s": ("s", "es"), "3": ("s", "es"), "p": ("ed",), "d": ("ed",),
               "i": ("ing",), "r": ("er",), "t": ("est",)}
    with Path(path).open(encoding="utf-8-sig", newline="") as file:
        for row in csv.DictReader(file):
            word = row["word"]
            if not legacy.SPELLING.fullmatch(word) or word in BLOCKED:
                continue
            fields = dict(term.split(":", 1) for term in row["exchange"].split("/") if ":" in term)
            base = fields.get("0", "")
            if len(base) < 3 or not legacy.SPELLING.fullmatch(base):
                continue
            suffixes = (suffix for marker in fields.get("1", "") for suffix in endings.get(marker, ()))
            if any(suffix_surface(base, suffix, word) or (suffix == "ed" and word == base + "d" and base.endswith("e"))
                   for suffix in suffixes):
                yield word, base

INNER_SUFFIX = {"able": ("abil",), "ible": ("ibil",), "ive": ("iv",), "ize": ("iz",), "ise": ("is",),
                "ate": ("at",), "ity": ("iti",), "ory": ("ori",), "ic": ("ic",), "ous": ("os",)}

def morpholex_rows(path, dictionary, lemmas):
    for word, pos, marked in rows(path):
        if word not in dictionary or not legacy.SPELLING.fullmatch(word) or word in BLOCKED:
            continue
        surface = lemmas.get(word, word)
        # Only parse standard annotation. Skip punctuation/uncertain alternatives.
        if re.search(r"[^a-zA-Z<>(){}]", marked):
            continue
        prefix, root, suffix = [], [], []
        tokens = MARKED.findall(marked.lower())
        if not tokens or sum(bool(r) for _, r, _ in tokens) != 1:
            continue
        for p, stem, s in tokens:
            if p:
                prefix.append(p)
            if stem:
                root.append(stem)
            if s:
                suffix.append(s)
        parts = []
        for affix in prefix:
            if affix == "co" and surface.startswith("con" + root[0]):
                affix = "con"
            if not surface.startswith(affix) or len(surface) <= len(affix):
                break
            rest = surface[len(affix):].lstrip("-")
            gloss = prefix_gloss(affix, rest, "VB" if pos == "VB" else "")
            if affix in NEGATIVE and (not pos or "JJ" not in pos or "VB" in pos or root[0] not in dictionary):
                gloss = None
            # un- on an adjective ending in -ed often denotes a negation,
            # whereas its verb counterpart can express reversing an action.
            if affix == "un" and pos and "|" in pos:
                gloss = "不；解除"
            if gloss:
                parts.append(hint("prefix", affix, gloss))
            surface = rest

        ending = []
        for affix in reversed(suffix):
            labels = (affix, *INNER_SUFFIX.get(affix, ()))
            spelling = next((label for label in labels if surface.endswith(label) and len(surface) > len(label)), "")
            if not spelling:
                break
            gloss = SUFFIX.get(affix)
            if affix == "ly":
                gloss = "以……方式" if pos and "RB" in pos else "有……特征的"
            if affix == "en" and (not pos or "VB" not in pos):
                gloss = None
            if affix == "ate" and (not pos or "VB" not in pos):
                gloss = None
            if affix in {"er", "or"} and (not pos or ("NN" not in pos and "VB" not in pos)):
                gloss = None
            if affix == "er" and pos and "JJ" in pos:
                gloss = None
            if affix == "y" and (not pos or "JJ" not in pos):
                gloss = None
            if gloss:
                ending.append(hint("suffix", affix, gloss))
            surface = surface[:-len(spelling)]
        stem, = root
        if stem in BASES and (parts or ending) and (
                surface == stem or len(stem) >= 3 and (surface in {stem[:-1], stem[:-1] + "i", stem + stem[-1]})):
            parts.extend(stem_part(stem))
        if not parts and not ending:
            continue
        # The academic dataset also uses many whole English words as roots.
        # Only bound stems inside an independently confirmed family are added
        # by the EtymDB layer or the original reviewed vocabulary.
        yield word, tuple(parts + list(reversed(ending)))

def etymdb_roots(path, dictionary):
    latin = {canonical(name): stems.split() for name, stems in LATIN.items()}
    greek = {canonical(name): stems.split() for name, stems in GREEK.items()}
    assert all(form in ROOTS for source in (latin, greek) for family in source.values() for form in family)
    languages = {"en", "enm", "ang", "xno", "fro", "frm", "fr", "la", "grc", "el"}
    lexemes, seeds, words, branches = set(), {}, defaultdict(list), {}
    with Path(path).open(encoding="utf-8") as file:
        for line in file:
            fields = line.rstrip("\n").split("\t", 4)
            if fields[0].startswith("-"):
                branches[int(fields[0])] = tuple(int(v) for v in line.split()[1:])
                continue
            if len(fields) < 4 or not fields[0].isdigit():
                continue
            code, lang, _, word = fields[:4]
            if lang in languages:
                ident = int(code)
                lexemes.add(ident)
                if lang == "en" and word in dictionary and word not in BLOCKED and legacy.SPELLING.fullmatch(word):
                    words[word].append(ident)
                if lang in {"la", "grc"}:
                    info = (latin if lang == "la" else greek).get(canonical(word))
                    if info:
                        seeds[ident] = info
    edges = defaultdict(set)
    with Path(path).open(encoding="utf-8") as file:
        for line in file:
            fields = line.rstrip("\n").split("\t")
            if len(fields) != 3 or fields[0] not in {"bor", "inh", "der", "der(p)", "der(s)", "cmpd+bor"}:
                continue
            child, parent = map(int, fields[1:])
            if child in lexemes:
                edges[child].update(v for v in branches.get(parent, (parent,)) if v in lexemes)

    @lru_cache(maxsize=None)
    def origins(ident, depth):
        result = set(seeds.get(ident, ()))
        if depth:
            for parent in edges.get(ident, ()):
                result.update(origins(parent, depth - 1))
        return result

    for word, ids in words.items():
        candidates = {form for i in ids for form in origins(i, 5) if form in word and form != word}
        # One historical stem only. Collisions across etymologies are left blank.
        if len(candidates) == 1:
            form, = candidates
            yield word, (hint("root", form, ROOTS[form]),)

def rank(parts):
    return sum({"prefix": 2, "root": 3, "suffix": 1}[part.kind] for part in set(parts))

def write_index(name, analyses):
    out = []
    for word, parts in sorted(analyses.items()):
        pieces = tuple(dict.fromkeys(parts))[:4]
        if pieces:
            out.append(f"{word}\t{rank(pieces)}\t{' + '.join(p.text for p in pieces)}\n")
    file = legacy.ASSETS / name
    file.write_bytes(gzip.compress("".join(out).encode(), mtime=0))
    print(f"{name}: {len(out)} records, {file.stat().st_size} bytes")

def main():
    if len(sys.argv) != 6:
        raise SystemExit(__doc__)
    morphynet, linxie, morpholex, etymdb, ecdict = sys.argv[1:]
    with gzip.open(legacy.ASSETS / "ecdict.tsv.gz", "rt", encoding="utf8") as file:
        dictionary = {line.split("\t", 1)[0] for line in file}
    inflections = dict(regular_lemmas(ecdict))
    morphy = regular_edges(morphynet, dictionary)
    for w, parts in linxie_rows(linxie, dictionary):
        current = morphy.get(w, ())
        # A fuller source analysis can replace a partial hint when it agrees
        # with the existing parts, or provides the first supported analysis.
        if rank(parts) > rank(current) and all(p.kind == "root" or any(v.kind == p.kind and v.text.split("（")[0] == p.text.split("（")[0] for v in parts) for p in current):
            morphy[w] = parts
    academic = dict(morpholex_rows(morpholex, dictionary, inflections))
    roots = dict(etymdb_roots(etymdb, dictionary))
    for source in (morphy, academic, roots):
        for word, lemma in inflections.items():
            if word not in source and lemma in source:
                source[word] = source[lemma]
    write_index("morphology-derivations-v19.tsv.gz", morphy)
    write_index("morphology-morpholex-v19.tsv.gz", academic)
    write_index("morphology-etymdb-v19.tsv.gz", roots)

if __name__ == "__main__":
    main()
