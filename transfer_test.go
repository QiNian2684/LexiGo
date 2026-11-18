package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func startTransferTest(t *testing.T) (*store, *store, []byte) {
	t.Helper()
	source, _ := newTestStore(t)
	dest, _ := newTestStore(t)
	day := localDay()
	options := source.db.Settings
	options.Online = false
	options.Limit = 40
	if _, err := source.changeSettings(day, options); err != nil {
		t.Fatal(err)
	}
	var words []string
	for index := 0; index < 44; index++ {
		words = append(words, "stage"+string(rune('a'+index/26))+string(rune('a'+index%26)))
	}
	importTest(t, source, day, strings.Join(words, ","))
	initial := source.shuffleReview(day).Current.Key
	source.db.Words[initial].PersonalNote = "中文与 🎧\n反斜线 \\ < >"
	source.db.Words[initial].Meaning = "保留被修改的义项"
	mustSave(t, source, initial)
	for i := 0; i < 64; i++ {
		nextVote(t, source, day, i != 10)
	}
	importTest(t, dest, day, "receiveronly")
	file, err := source.transferBytes(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return source, dest, file
}

func previewFor(t *testing.T, a *application, file []byte) transferSummary {
	t.Helper()
	w := httptest.NewRecorder()
	a.previewTransfer(w, file)
	var data transferSummary
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &data) != nil || data.Token == "" {
		t.Fatalf("preview failed: %d %s", w.Code, w.Body)
	}
	return data
}

func commitFor(a *application, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	data, _ := json.Marshal(map[string]string{"token": token})
	a.commitTransfer(w, httptest.NewRequest("POST", "/api/transfer/commit", bytes.NewReader(data)))
	return w
}

func TestTransferRestoresQueueProgressAndPreImportBackup(t *testing.T) {
	source, dest, file := startTransferTest(t)
	original, _ := json.Marshal(dest.db)
	a := &application{store: dest}
	data := previewFor(t, a, file)
	if !data.Continuation || data.Words != 44 || data.Pending == 0 || data.Events != len(source.db.Events) ||
		len(dest.db.Words) != 1 || dest.db.Words["receiveronly"] == nil {
		t.Fatalf("incorrect non-mutating preview: %+v", data)
	}
	if w := commitFor(a, "other-token"); w.Code != 409 {
		t.Fatalf("wrong token accepted: %s", w.Body)
	}
	if w := commitFor(a, data.Token); w.Code != 200 {
		t.Fatalf("restore failed: %s", w.Body)
	}
	if !reflect.DeepEqual(source.db, dest.db) {
		t.Fatal("restore changed words, notes, history or progress")
	}
	day := localDay()
	if !reflect.DeepEqual(source.captureTask(day), dest.captureTask(day)) ||
		!reflect.DeepEqual(source.review(day).Current, dest.review(day).Current) {
		t.Fatal("the pending queue or current review card changed")
	}

	candidates, _ := filepath.Glob(filepath.Join(filepath.Dir(dest.io.file), "backups", "before-transfer-*.json"))
	if len(candidates) != 1 {
		t.Fatalf("expected recovery backup: %v", candidates)
	}
	recovery, err := os.ReadFile(candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	payload, err := parseTransfer(recovery)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := json.Marshal(payload.Database)
	if !bytes.Equal(saved, original) {
		t.Fatal("backup does not contain original destination")
	}

	// Independent recipients simulate continuing the Android session and
	// replacing the Windows session again using the exact same codec.
	nextVote(t, dest, day, true)
	key := dest.review(day).Current.Key
	dest.db.Words[key].PersonalNote = "从另一台设备修改"
	mustSave(t, dest, key)
	travel, err := dest.transferBytes(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	back := &application{store: source}
	backPreview := previewFor(t, back, travel)
	if w := commitFor(back, backPreview.Token); w.Code != 200 || !reflect.DeepEqual(source.db, dest.db) {
		t.Fatalf("second restore changed notes or learning times: %d %s", w.Code, w.Body)
	}
	if w := commitFor(back, backPreview.Token); w.Code != 409 {
		t.Fatal("an already used confirmation token was accepted")
	}
	if err := source.io.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(source.io.file)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.io.Close()
	if !reflect.DeepEqual(reopened.db, dest.db) || reopened.review(day).Due != dest.review(day).Due {
		t.Fatal("the database did not keep the imported session after reopening")
	}
}

func TestTransferRejectsCorruptionAndPreservesDatabaseOnFailure(t *testing.T) {
	_, dest, file := startTransferTest(t)
	before, _ := json.Marshal(dest.db)
	a := &application{store: dest}
	damaged := bytes.Replace(file, []byte("stageaa"), []byte("stagete"), 1)
	version := bytes.Replace(file, []byte(`"version":2`), []byte(`"version":999`), 1)
	for _, bad := range [][]byte{damaged, version, file[:len(file)-5], []byte(`{"words":[]}`)} {
		w := httptest.NewRecorder()
		a.previewTransfer(w, bad)
		if w.Code != 400 {
			t.Fatalf("a broken package was accepted: %d", w.Code)
		}
	}
	if _, err := dest.io.Exec("CREATE TRIGGER reject_import BEFORE INSERT ON cards BEGIN SELECT RAISE(ABORT, 'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	data := previewFor(t, a, file)
	if w := commitFor(a, data.Token); w.Code != 500 {
		t.Fatalf("transaction failure not reported: %s", w.Body)
	}
	after, _ := json.Marshal(dest.db)
	ondisk, err := readDatabase(dest.io.DB)
	if err != nil || !reflect.DeepEqual(ondisk, dest.db) || !bytes.Equal(before, after) {
		t.Fatal("failed replacement modified the current database")
	}
}

func TestTransferDayBoundary(t *testing.T) {
	s, _ := newTestStore(t)
	location := time.FixedZone("source", -6*3600)
	when := time.Date(2026, 11, 16, 21, 0, 0, 0, location)
	importTest(t, s, "2026-11-15", "crucial")
	passOne(t, s, "2026-11-15")
	nextVote(t, s, "2026-11-16", true)
	file, err := s.transferBytes(when)
	if err != nil {
		t.Fatal(err)
	}
	p, err := parseTransfer(file)
	if err != nil {
		t.Fatal(err)
	}
	receiver := time.FixedZone("receiver", 8*3600)
	if p.dateConflict(when.In(receiver)) == "" {
		t.Fatal("transfer allowed while the source day was still active in a different date")
	}
	if result := p.dateConflict(when.AddDate(0, 0, 1).In(receiver)); result != "" {
		t.Fatal(result)
	}
	if p.dateConflict(when.Add(-24*time.Hour).In(location)) == "" {
		t.Fatal("transfer allowed a file from the future")
	}
	if p.dateConflict(when.Add(time.Minute)) != "" {
		t.Fatal("same day import was rejected")
	}
	receiverStore := &store{db: p.Database}
	next := receiverStore.review("2026-11-17")
	if next.Current == nil || next.Current.DailyCount != 0 || next.Current.Streak != 0 {
		t.Fatalf("past unfinished task was treated as present-day progress: %+v", next)
	}
}
