"""Build the offline index from pinned MorphyNet and LexiRoot data (stdlib only).

Usage: python scripts/build_morphology.py eng.derivational.v1.tsv morphemes.json ecdict.csv
See licenses/Morphology-1.8.txt for source revisions, licences and downloads.
"""

import csv
import gzip
import json
from collections import defaultdict
from pathlib import Path
import re
import sys

ASSETS = Path(__file__).resolve().parents[1] / "assets"
SPELLING = re.compile(r"[a-z]+(?:-[a-z]+)*")

# Keep only meanings that are productive in this data. Homonymous affixes
# with no dependable POS distinction (e.g. in-/com-) are deliberately omitted.
PREFIX = {
    "anti": "反对；防止", "auto": "自己；自动", "bi": "双；二", "bio": "生命；生物",
    "co": "共同", "counter": "反制；相反", "de": "去除；逆转", "dis": "否定；分离",
    "eco": "生态；环境", "extra": "超出；在外", "fore": "预先；在前",
    "hyper": "过度；超出", "hypo": "低于；不足", "inter": "相互；在……之间",
    "intra": "在……内部", "macro": "大；宏观", "micro": "微小；微观",
    "mid": "中间", "mini": "小型", "mis": "错误；不当", "mono": "单一",
    "multi": "多个", "neo": "新的", "non": "不；非", "out": "超越",
    "over": "过度；在上", "poly": "多个", "post": "在……之后", "pre": "预先；在前",
    "pseudo": "假的；貌似", "re": "再次；重新", "semi": "半；部分", "sub": "在下；次级",
    "super": "在上；超过", "trans": "跨越；转移", "tri": "三",
    "ultra": "超出；极端", "under": "在下；不足",
}
SUFFIX = {
    "ability": "可……的性质", "ibility": "可……的性质", "able": "可……的",
    "ible": "可……的", "al": "与……有关，或表示动作", "ial": "与……有关",
    "ical": "与……有关", "ance": "行为；状态", "ancy": "性质；状态",
    "ence": "行为；状态", "ency": "性质；状态", "ary": "与……有关",
    "ation": "过程；结果", "ition": "动作；过程", "tion": "动作；过程",
    "sion": "动作；过程", "ion": "动作；过程", "en": "使变得",
    "ful": "具有；充满", "hood": "身份；时期", "ic": "与……有关",
    "ify": "使……化", "ish": "有点像；有……特征", "ism": "主张；制度；倾向",
    "ist": "从事者；主张者", "ity": "性质；状态", "ive": "具有……性质或倾向",
    "ise": "使……化", "ize": "使……化", "isation": "……化的过程",
    "ization": "……化的过程", "less": "缺少；没有", "like": "类似",
    "ally": "以……方式", "ment": "动作；结果或状态", "ness": "性质；状态",
    "ous": "具有……性质", "ship": "身份；关系或状态", "ward": "朝向",
    "wards": "朝向", "worthy": "值得", "wise": "以……方式；在……方面",
    "proof": "防……的", "y": "有……特征的",
}
# These meanings are narrower than the imported database's conflated glosses.
# Require both a documented example and an attested spelling variant.
ROOTS = {
    "anthrop": "人；人类", "aqu": "水", "archaeo": "古代", "aud": "听",
    "biblio": "书", "brev": "短", "cardio": "心脏", "centr": "中心",
    "chron": "时间", "cogn": "知道；认识", "corp": "身体；实体", "cosm": "宇宙；世界",
    "cred": "相信；信任", "derm": "皮肤", "dict": "说；指明", "doc": "教",
    "dorm": "睡", "duct": "引导；带领", "fer": "带来；携带", "fin": "边界；结束",
    "flect": "弯曲", "flex": "弯曲", "fract": "打破", "gen": "产生；种类",
    "geo": "土地；地球", "grad": "步；行进", "graph": "写；记录", "gress": "行进",
    "hydr": "水", "ject": "投；扔", "junct": "连接", "juven": "年轻", "lingu": "舌；语言",
    "lumin": "光", "magn": "大", "manu": "手", "memor": "记忆",
    "migr": "迁移", "mobil": "移动", "morph": "形状；形态", "mort": "死亡",
    "neur": "神经", "odont": "牙齿", "ortho": "正；直",
    "pend": "悬挂", "phon": "声音", "phot": "光", "physio": "身体机能",
    "plic": "折叠；卷入", "posit": "放置", "press": "压",
    "psych": "心理；精神", "radi": "光线；放射", "rect": "直",
    "rupt": "破裂", "scrib": "写", "script": "写", "sect": "切；划分",
    "sens": "感觉", "sequ": "跟随", "sign": "标记", "soci": "同伴；社会",
    "spect": "看", "spir": "呼吸", "struct": "搭建", "tempor": "时间",
    "term": "界限；结束", "terr": "土地", "therm": "热", "tract": "拉；拖",
    "urb": "城市", "vert": "转", "vers": "转", "vit": "活；生命",
}
BLOCKED = {
    "inside", "ink", "portion", "airport", "counsel", "stone",
    "uncle", "unite", "understand", "income", "inland", "inflammable", "infamous",
    "interest", "integer", "insight", "input", "institute", "internationalize",
    "repeat", "report", "receive", "record", "relish", "religion", "regard",
    "former", "corner", "father", "mother", "other", "fatherly", "butter",
    "disease", "dismay", "display", "disaster", "distress", "dispose",
    "opportune", "opportunity", "portico", "portal", "ferry", "fertile",
    "terror", "terrify", "territoriality", "spin", "termite", "significant",
    "universe", "anniversary", "physician", "quarantine", "actual", "status", "analysis",
    "oblivion", "palace", "particle", "planet", "candy", "office", "orange", "whenever",
    "scarcity", "lethargy", "ill", "outer", "parasite", "impassioned",
    "county", "happen", "hooven", "tender", "undertake",
}


def suffix_spells(base, suffix, word):
    if word == base + suffix:
        return True
    if suffix[0] in "aeiou" and base.endswith("e") and word == base[:-1] + suffix:
        return True
    if base.endswith("y") and base[-2] not in "aeiou" and word == base[:-1] + "i" + suffix:
        return True
    if suffix[0] in "aeiou" and base[-1] not in "aeiouwxy" and base[-2] in "aeiou" and base[-3] not in "aeiou":
        return word == base + base[-1] + suffix
    return False


def derivations(source, dictionary):
    found = defaultdict(set)
    with source.open(encoding="utf-8") as file:
        for row in csv.reader(file, delimiter="\t"):
            if len(row) != 6:
                continue
            base, word, base_pos, word_pos, affix, kind = row
            if (base not in dictionary or word not in dictionary or word in BLOCKED
                    or len(base) < 3 or not SPELLING.fullmatch(base)
                    or not SPELLING.fullmatch(word) or base == word):
                continue
            if kind == "prefix" and word in {affix + base, affix + "-" + base}:
                meaning = PREFIX.get(affix)
                if affix in {"re", "out"} and (base_pos, word_pos) != ("V", "V"):
                    meaning = None
                if affix == "un" and (base_pos, word_pos) == ("J", "J"):
                    meaning = "不；非"
                if affix == "un" and (base_pos, word_pos) == ("V", "V"):
                    meaning = "解除；逆转"
                if meaning:
                    found[word].add((base, affix + "-（" + meaning + "）"))
            if kind == "suffix" and affix and suffix_spells(base, affix, word):
                meaning = SUFFIX.get(affix)
                if affix in {"y", "able", "ible"} and word_pos != "J":
                    meaning = None
                if affix == "en" and (base_pos, word_pos) != ("J", "V"):
                    meaning = None
                if affix == "ly" and (base_pos, word_pos) == ("J", "R"):
                    meaning = "以……方式，构成副词"
                if affix in {"er", "or"} and (base_pos, word_pos) == ("V", "N"):
                    meaning = "动作的执行者或工具"
                if meaning:
                    found[word].add((base, "-" + affix + "（" + meaning + "）"))

    # Preserve only one unambiguous hint. Multiple bases can legitimately
    # support the same affix, but conflicting hints require human review.
    return {
        word: next(iter(hints))
        for word, edges in found.items()
        if len(hints := {hint for _, hint in edges}) == 1
    }


def root_examples(source, dictionary):
    data = json.loads(source.read_text(encoding="utf-8"))
    found = defaultdict(set)
    for root, meaning in ROOTS.items():
        entry = data.get(root, {})
        for word in entry.get("examples", ()):
            if (word in dictionary and word not in BLOCKED and word != root
                    and SPELLING.fullmatch(word) and root in word):
                found[word].add(root + "（" + meaning + "）")
    return {word: next(iter(hints)) for word, hints in found.items() if len(hints) == 1}


def inherited_forms(source, hints):
    # ECDICT supplies the lemma and transformation independently of the
    # spelling check. Only regular, directly supported changes inherit hints.
    endings = {"s": ("s", "es"), "3": ("s", "es"), "p": ("ed",), "d": ("ed",),
               "i": ("ing",), "r": ("er",), "t": ("est",)}
    with source.open(encoding="utf-8-sig", newline="") as file:
        for row in csv.DictReader(file):
            word = row["word"]
            if word in hints or word in BLOCKED or not SPELLING.fullmatch(word):
                continue
            fields = dict(part.split(":", 1) for part in row["exchange"].split("/") if ":" in part)
            base = fields.get("0", "")
            if len(base) < 3 or base not in hints:
                continue
            typed_endings = (suffix for marker in fields.get("1", "") for suffix in endings.get(marker, ()))
            if any(suffix_spells(base, suffix, word) or (suffix == "ed" and base.endswith("e") and word == base + "d")
                   for suffix in typed_endings):
                yield word, hints[base]


def save_index(target, entries):
    raw = "".join(f"{word}\t{entries[word]}\n" for word in sorted(entries))
    target.write_bytes(gzip.compress(raw.encode(), mtime=0))
    print(f"{target.name}: {len(entries)} spellings, {target.stat().st_size} bytes")


def main():
    if len(sys.argv) != 4:
        raise SystemExit(__doc__)
    with gzip.open(ASSETS / "ecdict.tsv.gz", "rt", encoding="utf-8") as file:
        dictionary = {row.split("\t", 1)[0] for row in file}
    derivatives = derivations(Path(sys.argv[1]), dictionary)
    roots = root_examples(Path(sys.argv[2]), dictionary)
    known = {**roots, **derivatives}
    for line in (ASSETS / "morphology.txt").read_text(encoding="utf-8").splitlines():
        if not line or line.startswith("#"):
            continue
        fields = line.split("|")
        if len(fields) == 4:
            for word in fields[3].split():
                known[word] = fields[1] + "（" + fields[2] + "）"
        elif fields[0] == "word":
            known[fields[1]] = fields[2]
    save_index(ASSETS / "morphynet-hints.tsv.gz", derivatives)
    save_index(ASSETS / "lexiroot-hints.tsv.gz", roots)
    save_index(ASSETS / "morphology-inflections.tsv.gz", dict(inherited_forms(Path(sys.argv[3]), known)))


if __name__ == "__main__":
    main()
