"""Measure exact-spelling bilingual example coverage through LexiGo's import API."""

import argparse
import collections
import http.cookiejar
import json
import os
from pathlib import Path
import re
import subprocess
from tempfile import TemporaryDirectory
import time
import urllib.parse
import urllib.request

from assemble_example_expansion import is_usage_example

HAN = re.compile(r"[\u3400-\u9fff]")
DEFAULT_SAMPLE = Path(__file__).with_name("example-coverage-single.tsv")


def paired(word, card):
    en = card["examples"].split("\n\n")
    zh = card["exampleTranslation"].split("\n\n")
    return len(en) == len(zh) and any(
        is_usage_example(word, example) and HAN.search(translation)
        for example, translation in zip(en, zh)
    )


def load_sample(source):
    terms = {}
    for line in source.read_text(encoding="utf-8").splitlines():
        if line and not line.startswith("#"):
            category, term, *_ = line.split("\t")
            if not any(char.isspace() for char in term):
                terms[term] = category
    return terms


def measure(binary, terms, online, wait_seconds):
    with TemporaryDirectory() as data, subprocess.Popen(
        [str(binary.resolve()), "--serve"],
        env={**os.environ, "WORDREVIEW_DATA": data},
        stdout=subprocess.PIPE, text=True,
    ) as server:
        try:
            start = server.stdout.readline().strip()
            if not start.startswith("http://127.0.0.1:"):
                raise RuntimeError(f"Unexpected startup URL from LexiGo: {start!r}")
            host = start.split("/start/", 1)[0]
            client = urllib.request.build_opener(
                urllib.request.ProxyHandler({}),
                urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
            )
            with client.open(start, timeout=20):
                pass

            def request(path, body=None):
                payload = None if body is None else json.dumps(body).encode()
                headers = {} if payload is None else {"Content-Type": "application/json", "Origin": host}
                with client.open(urllib.request.Request(host + path, payload, headers), timeout=20) as response:
                    return json.load(response)

            settings = {"limit": 20, "goal": 6, "dailyGoal": 3, "online": False, "showIPA": True}
            request("/api/settings", settings)
            words = sorted(terms)
            for start_index in range(0, len(words), 400):
                batch = words[start_index:start_index + 400]
                payload = {"text": ",".join(batch), "format": "words"}
                preview = request("/api/import-preview", payload)
                imported = request("/api/import", dict(payload, token=preview["token"]))
                if imported["new"] != len(batch) or imported["invalid"]:
                    raise ValueError(f"Not all sample words were imported: {imported}")
            offline = request("/api/backup")["words"]
            if not online:
                return offline, offline
            missing = [term for term in words if not paired(term, offline[term])]
            request("/api/settings", dict(settings, online=True))
            for term in missing:
                request("/api/card/" + urllib.parse.quote(term, safe=""))
            deadline = time.monotonic() + wait_seconds
            while True:
                rows = request("/api/backup")["words"]
                if all(paired(term, rows[term]) for term in missing) or time.monotonic() >= deadline:
                    return offline, rows
                time.sleep(3)
        finally:
            server.terminate()


def report(label, terms, rows):
    groups = collections.Counter(terms.values())
    hits = collections.Counter(group for word, group in terms.items() if paired(word, rows[word]))
    count = hits.total()
    print(f"{label}: {count}/{len(terms)} = {count / len(terms):.2%}")
    for group, total in groups.items():
        print(f"  {group}: {hits[group]}/{total}")
    print("  Missing:", ", ".join(word for word in sorted(terms) if not paired(word, rows[word])))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    parser.add_argument("--sample", type=Path, default=DEFAULT_SAMPLE)
    parser.add_argument("--online", action="store_true", help="Query public dictionary and translation services.")
    parser.add_argument("--wait", type=int, default=300, help="Seconds allowed for background enrichment.")
    args = parser.parse_args()
    sample = load_sample(args.sample)
    offline, final = measure(args.binary, sample, args.online, args.wait)
    report("offline", sample, offline)
    if args.online:
        report("online", sample, final)
