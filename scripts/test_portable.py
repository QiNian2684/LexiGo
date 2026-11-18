"""Exercise a built program in a temporary portable folder, without touching user data."""
import datetime
import http.cookiejar
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.request

if len(sys.argv) != 2:
    raise SystemExit("Usage: python scripts/test_portable.py /path/to/LexiGo.exe")

exe = Path(sys.argv[1]).resolve(strict=True)


def legacy_fixture(path, word):
    path.parent.mkdir(parents=True, exist_ok=True)
    day = str(datetime.date.today())
    with sqlite3.connect(path) as db:
        db.executescript("""
          CREATE TABLE IF NOT EXISTS meta (id INTEGER PRIMARY KEY, body BLOB NOT NULL);
          CREATE TABLE IF NOT EXISTS cards (key TEXT PRIMARY KEY, body BLOB NOT NULL);
          CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY, body BLOB NOT NULL);
          DELETE FROM cards;
          DELETE FROM events;
        """)
        db.execute("INSERT OR REPLACE INTO meta VALUES (1,?)", (json.dumps({
            "version": 2, "nextOrder": 1, "settings": {"limit": 9, "goal": 8, "online": False, "showIPA": True}
        }),))
        db.execute("INSERT INTO cards VALUES (?,?)", (word, json.dumps({
            "id": 1, "key": word, "word": word, "order": 1, "addedDay": day, "lookup": "done",
            "meaning": "中文释义", "personalNote": "迁移备注"
        })))
        db.execute("INSERT INTO events VALUES (1,?)", (json.dumps({
            "kind": "add", "day": day, "card": 1, "word": word
        }),))


def launch(program, cwd, environment):
    process = subprocess.Popen([str(program), "--serve"], cwd=cwd, env=environment, stdout=subprocess.DEVNULL)
    instance = program.parent / "Data" / "instance"
    try:
        for _ in range(180):
            if instance.is_file():
                link = instance.read_text(encoding="utf-8").strip()
                if link.startswith("http://127.0.0.1:"):
                    break
            if process.poll() is not None:
                raise RuntimeError(f"app stopped with code {process.returncode}")
            time.sleep(0.05)
        else:
            raise RuntimeError("startup timeout")
        root = link.split("/start/")[0]
        opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}),
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
        )
        with opener.open(link, timeout=5) as response:
            assert response.status == 200

        def api(route, body=None):
            headers = {"Content-Type": "application/json", "Origin": root}
            data = None if body is None else json.dumps(body).encode()
            with opener.open(urllib.request.Request(root + route, data=data, headers=headers), timeout=5) as response:
                return json.load(response)
        return process, api
    except Exception:
        process.kill()
        process.wait()
        raise


def exit_program(process, api):
    assert api("/api/quit", {})["ok"]
    assert process.wait(timeout=10) == 0


with tempfile.TemporaryDirectory(prefix="wordreview-portable-") as temp:
    base = Path(temp)
    first = base / "first folder"
    first.mkdir()
    destination = first / exe.name
    shutil.copy2(exe, destination)
    unrelated = base / "different working folder"
    unrelated.mkdir()
    environment = os.environ.copy()
    environment.pop("WORDREVIEW_DATA", None)
    environment["LOCALAPPDATA"] = str(base / "旧 profile # 10%")
    old_db = Path(environment["LOCALAPPDATA"]) / "WordReview" / "vocabulary.db"
    if sys.platform == "win32":
        legacy_fixture(old_db, "migration")
    process, api = launch(destination, unrelated, environment)
    try:
        assert Path(api("/api/storage")["database"]).samefile(first / "Data" / "vocabulary.db")
        assert not (unrelated / "Data").exists()
        if sys.platform == "win32":
            assert "已迁移" in api("/api/storage")["notice"]
            assert api("/api/state")["settings"]["goal"] == 8
            assert api("/api/card/migration")["personalNote"] == "迁移备注"
            assert old_db.is_file()
        api("/api/settings", {"goal": 6, "dailyGoal": 3, "limit": 5, "showIPA": True, "online": False})
        request = {"text": "portable", "format": "words"}
        preview = api("/api/import-preview", request)
        assert api("/api/import", {**request, "token": preview["token"]})["new"] == 1
        api("/api/note/portable", {"note": "保留这条备注", "clock": 1})
        exit_program(process, api)
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()

    moved = base / "moved folder"
    first.rename(moved)
    if sys.platform == "win32":
        legacy_fixture(old_db, "different")
    process, api = launch(moved / exe.name, unrelated, environment)
    try:
        assert Path(api("/api/storage")["database"]).samefile(moved / "Data" / "vocabulary.db")
        assert api("/api/card/portable")["personalNote"] == "保留这条备注"
        assert api("/api/state")["settings"]["limit"] == 5
        if sys.platform == "win32":
            assert "两处文件都已保留" in api("/api/storage")["notice"]
            assert api("/api/card/migration")["personalNote"] == "迁移备注"
            with sqlite3.connect(old_db) as untouched:
                assert untouched.execute("SELECT key FROM cards").fetchone()[0] == "different"
        exit_program(process, api)
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
    if sys.platform != "win32":
        (moved / "Data").chmod(0o500)
        try:
            blocked = subprocess.run([str(moved / exe.name), "--serve"], cwd=unrelated, env=environment,
                                     capture_output=True, text=True, timeout=5)
            assert blocked.returncode != 0 and "无法写入程序同级的数据目录" in blocked.stderr
            assert not (unrelated / "Data").exists()
        finally:
            (moved / "Data").chmod(0o700)

print("Portable runtime PASS: real executable path, separate working directory, clean shutdown, folder move and persistent notes.")
if sys.platform == "win32":
    print("Windows migration from isolated LOCALAPPDATA and without overwriting either database: PASS.")
else:
    print("Unwritable Data directory refusal without fallback: PASS.")
