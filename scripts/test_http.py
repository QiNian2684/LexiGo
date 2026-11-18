import csv
import http.cookiejar
import io
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import sys
import urllib.error
import urllib.request

if len(sys.argv) != 2:
    raise SystemExit('Usage: python scripts/test_http.py /path/to/WordReview.exe')
exe = Path(sys.argv[1]).resolve()

def start(directory):
    process = subprocess.Popen([str(exe), '--serve'], env={**os.environ, 'WORDREVIEW_DATA': str(directory)}, stdout=subprocess.PIPE, text=True)
    url = process.stdout.readline().strip()
    assert url.startswith('http://127.0.0.1:'), url
    host = url.split('/start/')[0]
    agent = urllib.request.build_opener(urllib.request.ProxyHandler({}),
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    with agent.open(url) as response:
        assert response.status == 200 and '背词本' in response.read().decode()
    records = {}
    def call(path, body=None, raw=False, expect=200):
        headers = {'Origin': host, 'Content-Type': 'application/json'} if body is not None else {}
        req = urllib.request.Request(host+path, data=None if body is None else json.dumps(body).encode(), headers=headers)
        try:
            result = agent.open(req,timeout=20)
        except urllib.error.HTTPError as failure:
            result = failure
        with result:
            content=result.read()
            assert result.status == expect, (path, result.status, content)
            return content if raw else json.loads(content)
    def load(text, fmt='words'):
        payload={'text':text,'format':fmt}
        count=call('/api/state')['total']
        preview=call('/api/import-preview',payload)
        assert call('/api/state')['total']==count
        return call('/api/import', {**payload,'token':preview['token']})
    def vote(yes):
        state=call('/api/state')
        return call('/api/vote',{**state['current'],'date':state['date'],'remembered':yes})
    return process, call, load, vote

with tempfile.TemporaryDirectory() as temp:
    directory=Path(temp)
    process,call,load,vote=start(directory)
    try:
        call('/api/settings',{'goal':1,'dailyGoal':1,'limit':2,'showIPA':True,'online':False})
        row=load('crucial, abundance, subtle, CRUCIAL, 中文')
        assert (row['new'],row['duplicates'],len(row['invalid']))==(3,1,1)
        assert call('/api/card/crucial')['exampleTranslation']
        assert call('/api/card/subtle')['meaning']
        call('/api/note/crucial',{'note':'备注\n本地持久化','clock':50})
        call('/api/note/crucial',{'note':'旧请求','clock':49})
        assert call('/api/card/crucial')['personalNote']=='备注\n本地持久化'
        no=vote(False)
        assert no['current']['key']=='abundance'
        assert vote(True)['graduated']
        assert vote(False)['due']==1
        reply=vote(True)
        assert reply['credited'] and reply['reviewed']==2 and reply['due']==0
        assert call('/api/undo',{})['due']==1
        assert vote(True)['credited']
        again=load(' ABUNDANCE, abundance ')
        assert again['reset']==1 and again['new']==0 and again['duplicates']==1
        assert call('/api/state')['due']==1
        assert not vote(True)['credited']
        stats=call('/api/stats')
        assert stats['imported']==3 and stats['days'][-1]=={'day':stats['days'][-1]['day'],'yes':3,'no':2,'distinct':2,'passed':2}
        backup=call('/api/backup')
        assert backup['words']['crucial']['personalNote']=='备注\n本地持久化'
        call('/api/card/crucial',{'word':'subtle'},expect=409)
        call('/api/card/crucial',{'word':'crucially','meaning':'新的释义'})
        assert call('/api/history/crucially')
        csv_data=call('/api/export',{'all':True,'query':'新的'},raw=True)
        rows=list(csv.DictReader(io.StringIO(csv_data.decode('utf-8-sig'))))
        assert rows[0]['word']=='crucially' and rows[0]['personalNote'].replace('\r\n','\n')=='备注\n本地持久化'
        assert call('/api/batch/remove',{'all':True,'query':'新的'})['count']==1
        assert call('/api/cards?filter=trash')['total']==1
        assert call('/api/batch/restore',{'all':True,'filter':'trash'})['count']==1
        call('/api/restore',backup)
        assert json.loads((directory/'backups'/'before-restore.json').read_text())['words']['crucially']
        assert call('/api/card/crucial')['meaning']
        assert load('[{"word":"solar flare","meaning":"太阳耀斑","examples":"A flare erupted.","exampleTranslation":"耀斑爆发。","personalNote":"天文"}]','json')['new']==1
    finally:
        process.kill()
        process.wait()
    process,call,load,vote=start(directory)
    try:
        assert call('/api/state')['reviewed']==2
        assert call('/api/state')['due']==0
        assert call('/api/card/solar%20flare')['personalNote']=='天文'
        assert call('/api/card/crucial')['personalNote']=='备注\n本地持久化'
        with sqlite3.connect(directory/'vocabulary.db') as db:
            assert db.execute('PRAGMA integrity_check').fetchone()[0]=='ok'
    finally:
        process.kill()
        process.wait()
print('HTTP end-to-end PASS: offline settings, import preview, reimport, notes, answer/retry/undo, statistics, rename, history, CSV, trash, full backup/restore, killed-process recovery and SQLite integrity.')
