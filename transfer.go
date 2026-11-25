package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	transferFormat                 = "wordreview.learning-state"
	transferVersion                = 6
	transferVersionWithoutFirstTry = 5
	maxTransfer                    = 128 << 20
	studyFileExt                   = ".lexi"
)

type transferEnvelope struct {
	Format  string          `json:"format"`
	Version int             `json:"version"`
	SHA256  string          `json:"sha256"`
	Payload json.RawMessage `json:"payload"`
}

type transferTask struct {
	Day           string   `json:"day"`
	Pending       []uint64 `json:"pending"`
	Passed        []uint64 `json:"passed"`
	Ranked        []uint64 `json:"ranked"`
	Reviewed      int      `json:"reviewed"`
	Current       uint64   `json:"current,omitempty"`
	CurrentRecall bool     `json:"currentRecall,omitempty"`
}

type transferPayload struct {
	CreatedAt time.Time    `json:"createdAt"`
	Offset    int          `json:"offsetSeconds"`
	DayEnds   time.Time    `json:"sourceDayEnds"`
	Database  database     `json:"database"`
	Task      transferTask `json:"task"`
	legacy    bool
}

type transferDraft struct {
	token   string
	expires time.Time
	payload transferPayload
}

type transferSummary struct {
	Token        string    `json:"token"`
	CreatedAt    time.Time `json:"createdAt"`
	SourceDay    string    `json:"sourceDay"`
	DeviceDay    string    `json:"deviceDay"`
	Words        int       `json:"words"`
	Mastered     int       `json:"mastered"`
	Notes        int       `json:"notes"`
	Events       int       `json:"events"`
	Passed       int       `json:"passed"`
	Pending      int       `json:"pending"`
	Continuation bool      `json:"continuation"`
	Blocked      string    `json:"blocked,omitempty"`
}

func (s *store) captureTask(day string) transferTask {
	return s.captureTaskRules(day, false)
}

func (s *store) captureTaskRules(day string, legacy bool) transferTask {
	plan := s.planReviewRules(day, legacy)
	task := transferTask{Day: day, Pending: []uint64{}, Passed: []uint64{}, Ranked: []uint64{}, Reviewed: plan.state.Reviewed}
	for _, group := range [][]*wordCard{plan.old, plan.new, plan.extras, plan.repeats, plan.recall} {
		for _, card := range group {
			task.Pending = append(task.Pending, card.ID)
		}
	}
	slices.Sort(task.Pending)
	for _, card := range s.db.Words {
		_, pending := slices.BinarySearch(task.Pending, card.ID)
		recall, hasRecall := plan.session.recallProgress[card.ID]
		if !card.Deleted && (hasRecall || card.passedOn(day)) &&
			(!hasRecall || recall.done) && !pending {
			task.Passed = append(task.Passed, card.ID)
		}
	}
	if card := s.pinnedCard(day); !legacy && card != nil && (!s.presented.recall || slices.Contains(plan.recall, card)) {
		task.Current = card.ID
		task.CurrentRecall = s.presented.recall
	}
	slices.Sort(task.Passed)
	if s.display.day == day {
		for _, id := range task.Pending {
			if s.display.ranks[id] > 0 {
				task.Ranked = append(task.Ranked, id)
			}
		}
		sort.Slice(task.Ranked, func(i, j int) bool {
			return s.display.ranks[task.Ranked[i]] < s.display.ranks[task.Ranked[j]]
		})
	}
	return task
}

// Caller holds the store lock while the live database is marshalled.
func (s *store) transferBytes(now time.Time) ([]byte, error) {
	_, offset := now.Zone()
	day := s.db.studyDay(now)
	payload := transferPayload{CreatedAt: now.UTC(), Offset: offset, DayEnds: s.db.nextStudyDay(now).UTC(),
		Database: s.snapshotDatabase(day), Task: s.captureTask(day)}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	version := transferVersionWithoutFirstTry
	if payload.Database.Settings.FirstTryMastery ||
		slices.ContainsFunc(payload.Database.Events, func(item event) bool { return item.FirstTryMastered }) {
		version = transferVersion
	}
	file, err := json.Marshal(transferEnvelope{transferFormat, version, hex.EncodeToString(digest[:]), raw})
	if len(file) > maxTransfer {
		return nil, errors.New("完整学习数据超过 128 MB。")
	}
	return file, err
}

func decodeExact(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return errors.New("JSON 后有多余内容")
	}
	return nil
}

func parseTransfer(file []byte) (transferPayload, error) {
	var p transferPayload
	var envelope transferEnvelope
	if len(file) == 0 || len(file) > maxTransfer || !utf8.Valid(file) || decodeExact(file, &envelope) != nil ||
		envelope.Format != transferFormat {
		return p, errors.New("请选择由「导出数据」「分享数据」或「保存到本机」生成的学习数据文件。文件可能已经损坏或超过 128 MB。")
	}
	if envelope.Version < 1 || envelope.Version > transferVersion {
		return p, errors.New("此文件的数据交换版本不兼容。请使用支持该版本的LexiGo。")
	}
	digest := sha256.Sum256(envelope.Payload)
	if hex.EncodeToString(digest[:]) != envelope.SHA256 {
		return p, errors.New("学习数据的完整性校验失败。请重新复制或导出原文件。")
	}
	if decodeExact(envelope.Payload, &p) != nil || p.Database.Version != 3 || !validDatabase(p.Database) ||
		p.CreatedAt.IsZero() || p.Offset < -14*3600 || p.Offset > 14*3600 ||
		!p.DayEnds.After(p.CreatedAt) || p.DayEnds.Sub(p.CreatedAt) > 50*time.Hour {
		return transferPayload{}, errors.New("文件中的日期、词库或学习记录无效。")
	}
	p.legacy = envelope.Version == 1
	local := p.CreatedAt.In(time.FixedZone("", p.Offset))
	day := p.Database.studyDay(local)
	if p.legacy {
		day = local.Format(time.DateOnly)
	}
	check := &store{db: p.Database}
	if !p.legacy && p.Task.Current != 0 {
		for _, card := range p.Database.Words {
			if card.ID == p.Task.Current {
				check.presented = presentedCard{day, card.Key, card.Order, p.Task.CurrentRecall}
				break
			}
		}
	}
	if p.Task.Day != day {
		return transferPayload{}, errors.New("文件中的学习日与导出时间不符。")
	}
	expected := check.captureTaskRules(day, p.legacy)
	if !slices.Equal(expected.Pending, p.Task.Pending) || !slices.Equal(expected.Passed, p.Task.Passed) ||
		expected.Reviewed != p.Task.Reviewed || expected.Current != p.Task.Current ||
		expected.CurrentRecall != p.Task.CurrentRecall || !validRanks(p.Task.Pending, p.Task.Ranked) {
		return transferPayload{}, errors.New("文件中的复习队列与学习记录不一致。")
	}
	return p, nil
}

func validRanks(pending, ranked []uint64) bool {
	seen := make(map[uint64]bool, len(ranked))
	for _, id := range ranked {
		if _, ok := slices.BinarySearch(pending, id); !ok || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func (p transferPayload) dateConflict(now time.Time) string {
	day := p.Database.studyDay(now)
	// A pre-1.10 file labels midnight-to-five activity with the civil date.
	// Keep those historical labels and let the matching study day start at 05:00.
	if p.legacy {
		day = now.Format(time.DateOnly)
	}
	if p.CreatedAt.After(now.Add(5*time.Minute)) || p.Task.Day > day {
		return "文件日期晚于本机。请核对两台设备的日期与时区，再导入。"
	}
	if batch := p.Database.RecallBatch; batch != nil && batch.Day > day {
		return "文件中的易错词回顾日期晚于本机。请核对两台设备日期与时区。"
	}
	if p.Task.Day != day && now.Before(p.DayEnds) {
		return "两台设备的学习日期不同，导出端的当天还未结束。请统一时区，或等导出端过了当天再恢复。"
	}
	for _, entry := range p.Database.Events {
		if entry.Day > day {
			return "记录里有晚于本机日期的答题。请核对两台设备日期与时区。"
		}
	}
	for _, card := range p.Database.Words {
		for _, cardDay := range []string{card.AddedDay, card.DailyDay, card.LastTry, card.LastPass, card.LastNo, card.LastYes, card.ExtraDay} {
			if cardDay > day {
				return "词库里有晚于本机日期的学习状态。请核对两台设备日期与时区。"
			}
		}
	}
	return ""
}

func (p transferPayload) summarize(token string, now time.Time) transferSummary {
	info := transferSummary{
		Token: token, CreatedAt: p.CreatedAt, SourceDay: p.Task.Day, DeviceDay: p.Database.studyDay(now),
		Words: len(p.Database.Words), Events: len(p.Database.Events), Passed: len(p.Task.Passed),
		Pending: len(p.Task.Pending), Blocked: p.dateConflict(now),
	}
	info.Continuation = info.Blocked == "" && info.SourceDay == info.DeviceDay
	for _, card := range p.Database.Words {
		if card.Mastered && !card.Deleted {
			info.Mastered++
		}
		if card.PersonalNote != "" {
			info.Notes++
		}
	}
	return info
}

func transferFilename(now time.Time) string {
	return "LexiGo-" + now.Format("2006-01-02-15-04-05") + studyFileExt
}

func (a *application) exportTransfer() ([]byte, error) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	return a.store.transferBytes(time.Now())
}

func (a *application) previewTransfer(w http.ResponseWriter, file []byte) {
	payload, err := parseTransfer(file)
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		saveError(w, err)
		return
	}
	token := hex.EncodeToString(secret[:])
	now := time.Now()
	a.transferMu.Lock()
	a.transfer = transferDraft{token, now.Add(15 * time.Minute), payload}
	a.transferMu.Unlock()
	respond(w, 200, payload.summarize(token, now))
}

func (a *application) commitTransfer(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !requestJSON(w, r, 4096, &input) {
		return
	}
	a.transferMu.Lock()
	defer a.transferMu.Unlock()
	draft := a.transfer
	now := time.Now()
	if input.Token == "" || input.Token != draft.token || !now.Before(draft.expires) {
		problem(w, 409, "导入预览已过期。请重新打开文件检查。")
		return
	}
	if reason := draft.payload.dateConflict(now); reason != "" {
		problem(w, 409, reason)
		return
	}

	a.store.mu.Lock()
	backup, err := a.store.transferBytes(now)
	if err == nil {
		name := "before-transfer-" + now.UTC().Format("20060102T150405.000000000Z") + ".json"
		err = a.store.replaceLocked(draft.payload.Database, name, backup)
	}
	if err == nil {
		a.generation++
		a.resetTranslationQueueLocked()
		a.paused = time.Time{}
		if a.store.db.studyDay(now) == draft.payload.Task.Day {
			ranks := make(map[uint64]int, len(draft.payload.Task.Ranked))
			for i, id := range draft.payload.Task.Ranked {
				ranks[id] = i + 1
			}
			a.store.display = reviewDisplay{day: draft.payload.Task.Day, ranks: ranks}
			for _, card := range a.store.db.Words {
				if card.ID == draft.payload.Task.Current {
					a.store.presented = presentedCard{draft.payload.Task.Day, card.Key, card.Order, draft.payload.Task.CurrentRecall}
					break
				}
			}
		}
	}
	a.store.mu.Unlock()
	if err != nil {
		saveError(w, err)
		return
	}
	a.transfer = transferDraft{}
	a.poke()
	respond(w, 200, map[string]bool{"ok": true})
}

func readTransfer(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxTransfer+1))
	if err != nil || len(data) > maxTransfer {
		return nil, errors.New("无法读取完整学习数据，文件可能超过 128 MB。")
	}
	return data, nil
}

func (a *application) latestTransferBackup() (string, error) {
	dir := filepath.Join(filepath.Dir(a.store.io.file), "backups")
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for i := len(files) - 1; i >= 0; i-- {
		name := files[i].Name()
		if files[i].Type().IsRegular() && strings.HasPrefix(name, "before-transfer-") && strings.HasSuffix(name, ".json") {
			return filepath.Join(dir, name), nil
		}
	}
	return "", nil
}

func (a *application) transferRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/transfer/recovery", func(w http.ResponseWriter, _ *http.Request) {
		file, err := a.latestTransferBackup()
		if err != nil {
			saveError(w, err)
			return
		}
		respond(w, 200, map[string]bool{"available": file != ""})
	})
	mux.HandleFunc("POST /api/transfer/windows/recovery", func(w http.ResponseWriter, _ *http.Request) {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		file, err := a.latestTransferBackup()
		if err != nil {
			saveError(w, err)
			return
		}
		if file == "" {
			problem(w, 404, "没有可导出的覆盖前备份。")
			return
		}
		target, err := chooseDataFile(true, transferFilename(time.Now()), studyFileExt)
		if err != nil {
			saveError(w, err)
			return
		}
		if target == "" {
			respond(w, 200, map[string]bool{"cancelled": true})
			return
		}
		data, err := os.ReadFile(file)
		if err == nil {
			err = atomicWrite(target, data)
		}
		if err != nil {
			saveError(w, err)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/transfer/export", func(w http.ResponseWriter, _ *http.Request) {
		data, err := a.exportTransfer()
		if err != nil {
			problem(w, 500, err.Error())
			return
		}
		download(w, "application/json", transferFilename(time.Now()), data)
	})
	mux.HandleFunc("POST /api/transfer/preview", func(w http.ResponseWriter, r *http.Request) {
		data, err := readTransfer(r.Body)
		if err != nil {
			problem(w, 400, err.Error())
			return
		}
		a.previewTransfer(w, data)
	})
	mux.HandleFunc("POST /api/transfer/commit", a.commitTransfer)
	for _, save := range []bool{true, false} {
		mode := "open"
		if save {
			mode = "save"
		}
		mux.HandleFunc("POST /api/transfer/windows/"+mode, func(w http.ResponseWriter, _ *http.Request) {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
			file, err := chooseDataFile(save, transferFilename(time.Now()), studyFileExt)
			if err != nil {
				problem(w, 500, err.Error())
				return
			}
			if file == "" {
				respond(w, 200, map[string]bool{"cancelled": true})
				return
			}
			if save {
				data, err := a.exportTransfer()
				if err == nil {
					err = atomicWrite(file, data)
				}
				if err != nil {
					problem(w, 500, fmt.Sprintf("保存学习数据失败：%v", err))
					return
				}
				respond(w, 200, map[string]bool{"ok": true})
				return
			}
			f, err := os.Open(file)
			if err != nil {
				problem(w, 400, "无法打开选择的文件。")
				return
			}
			defer f.Close()
			data, err := readTransfer(f)
			if err != nil {
				problem(w, 400, err.Error())
				return
			}
			a.previewTransfer(w, data)
		})
	}
}
