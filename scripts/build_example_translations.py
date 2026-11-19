"""Build the offline, machine-translated example supplement.

Convert Helsinki-NLP/opus-mt-en-zh (revision
408d9bc410a388e1d9aef112a2daba955b945255) with CTranslate2 4.6.0,
using int8 quantization and copying source.spm and target.spm. Pass
that local directory and the int8 M2M100 reference model listed in
Dictionary-data-notices.txt. The script needs no network access.
"""

import argparse
import gzip
import json
import re
from collections import Counter
from difflib import SequenceMatcher
from itertools import pairwise
from pathlib import Path


HAN = re.compile(r"[\u3400-\u9fff]")
REPEATED_TEXT = re.compile(r"(.{2,12})\1{2,}")
NUMBERS = re.compile(r"(?<![\w])\d+(?:[.,]\d+)*(?![\w])", re.ASCII)
LATIN_WORD = re.compile(r"[A-Za-z]{2,}")
SIGNIFICANT = re.compile(r"[\u3400-\u9fffA-Za-z0-9]")

# Edits from checking complete sentence pairs, keyed by exact source text
# so a future edition of the corpus can't accidentally reuse the correction.
CORRECTIONS = {
    "Children who exhibit disruptive behaviour may be expelled from school.":
        "扰乱秩序的孩子可能会被学校开除。",
    "Doctors have a lot of incentive to see many patients and little incentive to spend much time with each one.":
        "医生有很强的动力多接诊病人，却缺乏在每位病人身上多花时间的动力。",
    "Tuck in your shirt.": "把衬衫下摆塞进裤腰里。",
    "There's a good prospect of this building being converted into a school.":
        "这栋楼很有可能被改建成学校。",
    "He was unable to perceive any difference at all in the room after the changes.":
        "改动之后，他一点也看不出房间里有什么不同。",
    "I'd rather date a poor genius than a rich goon.": "我宁愿和一个穷天才约会，也不愿和一个有钱的笨蛋约会。",
    "It was a story of a girl who dressed in armour and fought great battles.": "这是一个身穿盔甲、打过多场大战的女孩的故事。",
    "The farmer used a yoke to connect the oxen.": "农夫用轭把耕牛套在一起。",
    "He had his house painted citron.": "他请人把房子漆成了柠檬黄色。",
    "We started at a brisk walk and ended at a plod.": "我们起初走得很快，到最后只能拖着步子走。",
    "The building was destroyed with a surgical air-strike.": "这栋建筑在一次精准空袭中被摧毁。",
    "His favorite confection was a meringue covered in chocolate.": "他最爱吃的甜点是裹着巧克力的蛋白霜。",
    "Each fall the itinerant workers would arrive, help with bringing in the crops, and then move on to the next valley.":
        "每到秋天，流动工人就会前来帮忙收割庄稼，之后再到下一个山谷去。",
    "It's been very long, very tiresome, and very draining, but at last it's over.": "这事拖了很久，既让人厌烦又耗人精力，但总算结束了。",
    "The floor in the old house creaked loudly when people walked through it.": "人走过时，老房子的地板发出响亮的嘎吱声。",
    "Don't keep us in suspense, my dear! What happened?": "亲爱的，别卖关子了！发生了什么事？",
    "Give the pot a good scour.": "把锅好好刷洗一下。",
    "The waiters were so humourous, one even did a back flip for us, when we asked him.":
        "服务员们很幽默，我们提议后，其中一人甚至给我们表演了一个后空翻。",
    "They beat us last year, so this year's win was payback.": "去年他们赢了我们，今年我们赢回来，算是报了一箭之仇。",
    "He looked rather porky.": "他看起来颇为肥胖。",
    "I could hear the priest intone the prayer.": "我能听见神父吟诵祷文。",
    "IIRC, his name is Damian.": "如果我没记错，他叫达米安。",
    "Almost everybody is born with two kidneys.": "几乎每个人生来都有两个肾。",
    "He then chug-a-lugs a full glass of water.": "然后他咕咚咕咚地喝下了一整杯水。",
    "The audience did a drum roll to reveal the winner.": "观众敲起一连串鼓点，揭晓获胜者。",
    "The country singer at the concert was very busty.": "音乐会上那位乡村歌手胸部很丰满。",
    "The book was so abstruse that I didn't understand it.": "这本书太深奥了，我没读懂。",
    "Don Juan is the classic libertine.": "唐璜是典型的浪荡子。",
    "Gee! It is a new toy!": "哇，是个新玩具！",
    "The bassist sat his guitar down.": "贝斯手放下了琴。",
    "The coving on the old house was very decorative.": "那栋老房子的弧形饰线很有装饰性。",
    "This small toy is a miniature dump truck.": "这个小玩具是辆微缩自卸卡车。",
    "I'll trot down to the store and buy some.": "我小跑去店里买一些。",
    "Saskatchewan is the breadbasket of Canada.": "萨斯喀彻温省是加拿大的粮仓。",
    "The first goal was just a fluke.": "第一粒进球纯属侥幸。",
    "The actors downstage were easier to hear.": "靠近舞台前沿的演员声音更容易听清。",
    "They tried to localise the source of the smell.": "他们试图找出气味的具体来源。",
    "The stomach and intestines are inside the abdomen.": "胃和肠都在腹部。",
    "We need to hire a tradesman to install the toilet.": "我们得请个技工来装马桶。",
}

# The original sentence doesn't illustrate the headword clearly enough.
EXCLUDED = {"There are many hygienes people believe in."}


def read_gzip(path):
    with gzip.open(path, "rt", encoding="utf-8") as stream:
        return json.load(stream)


def clean(source, translated):
    if "�" in source:
        return None
    translated = re.sub(r"\s+", " ", translated).strip()
    translated = re.sub(r" *([,!?;:]) *", lambda m: dict(zip(",!?;:", "，！？；："))[m[1]], translated)
    translated = re.sub(r"([。！？])\1+", r"\1", translated)
    translated = re.sub(r"(?<=[\u3400-\u9fff])\.(?=\s|$)", "。", translated)
    han_count = len(HAN.findall(translated))
    if not 2 <= han_count <= max(25, len(source) * 0.85):
        return None
    if re.search(r"[<>�]|https?://", translated) or REPEATED_TEXT.search(translated):
        return None
    if len(LATIN_WORD.findall(translated)) > max(2, han_count // 3):
        return None
    source_numbers = Counter(NUMBERS.findall(source.replace(",", "")))
    out_numbers = Counter(NUMBERS.findall(translated.replace(",", "")))
    if out_numbers - source_numbers:
        return None
    return translated


def agree(main, reference):
    # Compare both consecutive characters and overall ordering. The
    # independent translation is only a screen for divergent outputs.
    main, reference = ("".join(SIGNIFICANT.findall(text)).lower()
                       for text in (main, reference))
    order = SequenceMatcher(None, main, reference, autojunk=False).ratio()
    common = sum((Counter(pairwise(main)) & Counter(pairwise(reference))).values())
    return order >= 0.45 and 2 * common >= 0.15 * (len(main) + len(reference) - 2)


def build_supplement(gaps, main, reference, simplify):
    supplement = {}
    for word, sentence in sorted(gaps.items()):
        corrected = CORRECTIONS.get(sentence)
        translation = clean(sentence, simplify(corrected or main[sentence]))
        if not translation:
            continue
        if not corrected:
            compared = clean(sentence, simplify(reference[sentence]))
            if not compared or not agree(translation, compared):
                continue
        supplement[word] = {"examples": sentence, "exampleTranslation": translation}
    return supplement


def simplifier(path):
    mappings = dict(line.split("\t") for line in path.read_text(encoding="utf-8").splitlines())
    pattern = re.compile("|".join(re.escape(word) for word in mappings))
    return lambda text: pattern.sub(lambda match: mappings[match[0]], text)


def generate(model, sentences, threads, marian):
    import ctranslate2
    import sentencepiece
    from sacremoses import MosesPunctNormalizer

    source = "source.spm" if marian else "sentencepiece.bpe.model"
    target = "target.spm" if marian else source
    encoder = sentencepiece.SentencePieceProcessor(model_file=str(model / source))
    decoder = sentencepiece.SentencePieceProcessor(model_file=str(model / target))
    translator = ctranslate2.Translator(str(model), device="cpu", compute_type="int8",
                                       intra_threads=threads)
    normalize = MosesPunctNormalizer(lang="en").normalize if marian else str
    prefix = ">>cmn_Hans<<" if marian else "__en__"
    translations = {}
    for offset in range(0, len(sentences), 32):
        batch = sentences[offset:offset + 32]
        tokens = [[prefix, *encoder.encode(normalize(sentence), out_type=str), "</s>"]
                  for sentence in batch]
        prefix_options = {} if marian else {"target_prefix": [["__zh__"]] * len(batch)}
        results = translator.translate_batch(tokens, beam_size=4 if marian else 3,
                                             max_decoding_length=125, no_repeat_ngram_size=5,
                                             disable_unk=True, **prefix_options)
        for sentence, result in zip(batch, results):
            tokens = [token for token in result.hypotheses[0] if token != "__zh__"]
            translations[sentence] = decoder.decode(tokens)
        if offset % 256 == 0:
            print(f"{model.name}: {offset + len(batch)}/{len(sentences)}", flush=True)
    return translations


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("opus_model", type=Path)
    parser.add_argument("reference_model", type=Path)
    parser.add_argument("--threads", type=int, default=6)
    args = parser.parse_args()

    assets = Path(__file__).resolve().parents[1] / "assets"
    original = read_gzip(assets / "examples.json.gz")
    human = read_gzip(assets / "example-pairs.json.gz")
    handwritten = json.loads((assets / "learning.json").read_text(encoding="utf-8"))
    gaps = {word: entry["examples"] for word, entry in original.items()
            if entry["by"] == "simple" and not entry.get("exampleTranslation")
            and word not in human and word not in handwritten and entry["examples"] not in EXCLUDED}
    sentences = sorted(set(gaps.values()) - CORRECTIONS.keys(), key=lambda text: (len(text), text))
    main = generate(args.opus_model, sentences, args.threads, True)
    reference = generate(args.reference_model, sentences, args.threads, False)
    supplement = build_supplement(gaps, main, reference, simplifier(assets / "opencc-t2s.tsv"))
    data = json.dumps(supplement, ensure_ascii=False, separators=(",", ":")).encode()
    (assets / "example-translations.json.gz").write_bytes(gzip.compress(data, mtime=0))
    print(f"{len(supplement)}/{len(gaps)} missing example translations accepted", flush=True)


if __name__ == "__main__":
    main()
