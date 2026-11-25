package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

//go:embed assets/index.html
var indexHTML []byte

//go:embed assets/app.css
var appCSS []byte

//go:embed assets/android.css
var androidCSS []byte

//go:embed assets/stats-chart.js
var statsChartJS []byte

//go:embed assets/celebration.js
var celebrationJS []byte

//go:embed assets/app.js
var appJS []byte

//go:embed assets/appearance.js
var appearanceJS []byte

//go:embed assets/controls.js
var controlsJS []byte

//go:embed assets/tokens.css
var tokensCSS []byte

//go:embed assets/icons.svg
var iconsSVG []byte

//go:embed assets/instrument-sans.woff2
var instrumentSans []byte

//go:embed assets/ECDICT-LICENSE.txt
var ecdictLicense []byte

//go:embed assets/favicon.png
var favicon []byte

//go:embed assets/guide/*.webp
var guideImages embed.FS

//go:embed assets/templates/LexiGo-template-common.csv
var commonCSVTemplate []byte

//go:embed assets/templates/LexiGo-template-full.csv
var fullCSVTemplate []byte

//go:embed licenses/*.txt
var licenseFiles embed.FS

type application struct {
	store           *store
	dict            dictionary
	token           string
	host            string
	lastSeen        atomic.Int64
	windowTicket    atomic.Uint64
	closedTicket    atomic.Uint64
	windowClosedAt  atomic.Int64
	wake            chan struct{}
	generation      uint64
	paused          time.Time
	lookupFirst     string
	exit            chan struct{}
	migrationNotice string
	mobile          bool
	transferMu      sync.Mutex
	transfer        transferDraft
	appearance      *appearanceSettings
	translations    translationQueue
}

func setExamDate(state *reviewState, now time.Time) {
	state.Exam = countdown(now.Format(time.DateOnly), state.Settings.ExamDate)
}

func (a *application) reviewResponse(now time.Time, pin bool) reviewState {
	s := a.store
	s.mu.Lock()
	defer s.mu.Unlock()
	day := s.activeReviewDay(s.db.studyDay(now))
	if err := s.refreshRecall(day); err != nil {
		log.Printf("saving recall batch: %v", err)
	}
	state := s.review(day)
	if pin {
		s.pinReview(state)
		if state.Current != nil {
			a.viewExampleLocked(s.db.Words[state.Current.Key])
		}
	}
	setExamDate(&state, now)
	return state
}

func (a *application) reviewDay() string {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	return a.store.activeReviewDay(a.store.db.studyDay(time.Now()))
}

func (a *application) currentStudyDay() string {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	return a.store.db.studyDay(time.Now())
}

func (a *application) csvFilename() string {
	return "WordReview-words-" + a.currentStudyDay() + ".csv"
}

func (a *application) exportCSV(req selection) ([]byte, error) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	return asCSV(a.store.selected(req))
}

func (a *application) backupBytes() ([]byte, error) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	return json.Marshal(a.store.snapshotDatabase(a.store.db.studyDay(time.Now())))
}

func saveWindowsFile(w http.ResponseWriter, name, extension string, contents func() ([]byte, error)) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	target, err := chooseDataFile(true, name, extension)
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	if target == "" {
		respond(w, 200, map[string]bool{"cancelled": true})
		return
	}
	data, err := contents()
	if err == nil {
		err = atomicWrite(target, data)
	}
	if err != nil {
		problem(w, 500, fmt.Sprintf("保存文件失败：%v", err))
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *application) poke() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *application) enrich(ctx context.Context) {
	for ctx.Err() == nil {
		a.store.mu.Lock()
		remaining := time.Until(a.paused)
		var pending *wordCard
		generation := a.generation
		if remaining <= 0 && a.store.db.Settings.Online {
			first := a.store.db.Words[a.lookupFirst]
			if first != nil && !first.Deleted && first.Lookup == "pending" {
				pending = first
			} else {
				for _, item := range a.store.db.Words {
					if item.Deleted || item.Lookup != "pending" {
						continue
					}
					missing := needsPublicExample(item)
					priority := needsPublicExample(pending)
					if pending == nil || missing && !priority || missing == priority && item.ID < pending.ID {
						pending = item
					}
				}
			}
		}
		var key string
		var id uint64
		var tryExample bool
		if pending != nil {
			key, id = pending.Key, pending.ID
			tryExample = needsPublicExample(pending)
		}
		a.store.mu.Unlock()

		if key == "" {
			if remaining <= 0 {
				remaining = time.Hour
			}
			timer := time.NewTimer(remaining)
			select {
			case <-ctx.Done():
			case <-a.wake:
			case <-timer.C:
			}
			timer.Stop()
			continue
		}
		entry, err := fetchWiktionary(ctx, key)
		var candidate sourcedExample
		usedAlternate := false
		if tryExample && ctx.Err() == nil {
			entry.cardDetails = filterWikiExamples(entry.cardDetails, key)
			if entry.ExampleTranslation == "" {
				var sourceErr error
				candidate, sourceErr = fetchTatoebaExample(ctx, key)
				if candidate.Examples == "" && entry.Examples == "" && ctx.Err() == nil {
					var lastErr error
					candidate, lastErr = fetchWikipediaExample(ctx, key)
					if sourceErr == nil {
						sourceErr = lastErr
					}
				}
				if err == nil && entry.Examples == "" && candidate.Examples == "" {
					err = sourceErr
				}
				usedAlternate = candidate.ExampleTranslation != "" ||
					(candidate.Examples != "" && entry.Examples == "")
			}
		}
		if ctx.Err() != nil {
			return
		}
		a.store.mu.Lock()
		current := a.store.db.Words[key]
		if generation == a.generation && a.store.db.Settings.Online &&
			current != nil && current.ID == id && !current.Deleted && current.Lookup == "pending" {
			if err != nil && !usedAlternate {
				a.paused = time.Now().Add(90 * time.Second)
			} else {
				old := *current
				current.Lookup = "done"
				current.ExampleSearchTried = current.ExampleSearchTried || tryExample
				if current.ExampleTranslationBy == "manual" {
					entry.ExampleTranslation = ""
				}
				if usedAlternate && current.Examples == "" && current.ExampleTranslation == "" &&
					current.ExampleTranslationBy != "manual" {
					current.Examples = candidate.Examples
					current.ExampleTranslation = simplifyExampleTranslation(candidate.ExampleTranslation)
					current.ExampleBy = candidate.By
				}
				wikiExampleBefore := current.Examples
				if current.cardDetails.fill(entry.cardDetails) {
					if current.WikiSource == "" || wikiExampleBefore != current.Examples {
						current.WikiSource = wikiSource(entry.Revision)
					}
				}
				if err := a.store.save([]string{key}, len(a.store.db.Events)); err != nil {
					*current = old
					a.paused = time.Now().Add(90 * time.Second)
					log.Printf("save lookup: %v", err)
				}
				a.queueTranslationLocked(current, false)
			}
		}
		a.store.mu.Unlock()

		timer := time.NewTimer(900 * time.Millisecond)
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
}

func needsPublicExample(card *wordCard) bool {
	return card != nil && !card.ExampleSearchTried && card.Examples == "" &&
		card.ExampleTranslation == "" && card.ExampleTranslationBy != "manual"
}

func (a *application) viewExampleLocked(card *wordCard) bool {
	if card != nil && !card.Deleted && a.store.db.Settings.Online && needsPublicExample(card) {
		card.Lookup = "pending"
		a.lookupFirst = card.Key
		a.poke()
	}
	return a.queueTranslationLocked(card, true)
}

func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("write response: %v", err)
	}
}

func problem(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

func requestJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	if err := decoder.Decode(dst); err != nil {
		problem(w, 400, "无法读取提交内容，或内容超过大小限制。")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		problem(w, 400, "提交内容的格式无效。")
		return false
	}
	return true
}

func saveError(w http.ResponseWriter, err error) {
	log.Printf("storage error: %v", err)
	problem(w, 500, "无法保存词库，请检查磁盘空间和写入权限。")
}

func (a *application) handler() http.Handler {
	mux := http.NewServeMux()
	a.transferRoutes(mux)
	a.appearanceRoutes(mux)
	cookieName := "wordreview_" + a.token[:10]
	mux.HandleFunc("GET /start/{token}", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.PathValue("token")), []byte(a.token)) != 1 {
			http.NotFound(w, r)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: a.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	asset := func(contentType string, data []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write(data)
		}
	}
	sample := strings.TrimRight(strings.TrimPrefix(string(commonCSVTemplate), "\ufeff"), "\r\n")
	page := bytes.Replace(indexHTML, []byte("<code data-csv-sample></code>"),
		[]byte("<code>"+html.EscapeString(sample)+"</code>"), 1)
	if a.mobile {
		page = bytes.Replace(page, []byte("</head>"), []byte("<link rel=\"stylesheet\" href=\"/android.css\"></head>"), 1)
		page = bytes.Replace(page, []byte("<body "), []byte("<body class=\"platform-android\" "), 1)
		mux.HandleFunc("GET /android.css", asset("text/css; charset=utf-8", androidCSS))
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		prefs, showGuide := appearancePreferences{Theme: "system"}, false
		if a.appearance != nil {
			prefs, showGuide = a.appearance.windowPreferences()
		}
		// These attributes contain only known theme names, never user-entered text.
		attributes := fmt.Sprintf(`<html lang="zh-CN" data-theme-preference="%s" data-system-theme="%s" data-show-guide="%t" data-hide-guide="%t">`,
			prefs.Theme, systemAppearance(), showGuide, prefs.HideGuide)
		if runtime.GOOS == "windows" && !a.mobile {
			extra := fmt.Sprintf(` data-review-shortcuts="windows" data-window-ticket="%d">`, a.windowTicket.Add(1))
			attributes = strings.Replace(attributes, ">", extra, 1)
		}
		rendered := bytes.Replace(page, []byte(`<html lang="zh-CN">`), []byte(attributes), 1)
		asset("text/html; charset=utf-8", rendered)(w, r)
	})
	mux.HandleFunc("GET /tokens.css", asset("text/css; charset=utf-8", tokensCSS))
	mux.HandleFunc("GET /appearance.js", asset("text/javascript; charset=utf-8", appearanceJS))
	mux.HandleFunc("GET /controls.js", asset("text/javascript; charset=utf-8", controlsJS))
	mux.HandleFunc("GET /icons.svg", asset("image/svg+xml; charset=utf-8", iconsSVG))
	mux.HandleFunc("GET /instrument-sans.woff2", asset("font/woff2", instrumentSans))
	mux.HandleFunc("GET /app.css", asset("text/css; charset=utf-8", appCSS))
	mux.HandleFunc("GET /app.js", asset("text/javascript; charset=utf-8", appJS))
	mux.HandleFunc("GET /stats-chart.js", asset("text/javascript; charset=utf-8", statsChartJS))
	mux.HandleFunc("GET /celebration.js", asset("text/javascript; charset=utf-8", celebrationJS))
	mux.HandleFunc("GET /favicon.png", asset("image/png", favicon))
	mux.HandleFunc("GET /guide/{image}", func(w http.ResponseWriter, r *http.Request) {
		data, err := guideImages.ReadFile("assets/guide/" + r.PathValue("image"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		asset("image/webp", data)(w, r)
	})
	mux.HandleFunc("GET /licenses", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		entries, _ := licenseFiles.ReadDir("licenses")
		for _, entry := range entries {
			data, _ := licenseFiles.ReadFile("licenses/" + entry.Name())
			fmt.Fprintf(w, "\n%s\n\n%s\n", entry.Name(), data)
		}
	})
	mux.HandleFunc("GET /ecdict-license", asset("text/plain; charset=utf-8", ecdictLicense))

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, a.reviewResponse(time.Now(), false))
	})
	mux.HandleFunc("GET /api/storage", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, map[string]string{"database": a.store.io.file, "notice": a.migrationNotice})
	})
	mux.HandleFunc("POST /api/quit", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, map[string]bool{"ok": true})
		select {
		case a.exit <- struct{}{}:
		default:
		}
	})
	mux.HandleFunc("POST /api/window/closed", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Ticket uint64 `json:"ticket"`
		}
		if !requestJSON(w, r, 1024, &req) {
			return
		}
		if req.Ticket != 0 && req.Ticket == a.windowTicket.Load() {
			a.windowClosedAt.Store(time.Now().UnixNano())
			a.closedTicket.Store(req.Ticket)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/review/start", func(w http.ResponseWriter, _ *http.Request) {
		now := time.Now()
		a.store.mu.Lock()
		day := a.store.activeReviewDay(a.store.db.studyDay(now))
		if err := a.store.refreshRecall(day); err != nil {
			a.store.mu.Unlock()
			saveError(w, err)
			return
		}
		plan := a.store.planReview(day)
		state := a.store.reviewState(plan)
		if !state.Waiting && a.store.pinnedCard(day) == nil {
			a.store.setDisplay(day, plan)
			state = a.store.reviewState(plan)
		}
		a.store.pinReview(state)
		if state.Current != nil {
			a.viewExampleLocked(a.store.db.Words[state.Current.Key])
		}
		a.store.mu.Unlock()
		setExamDate(&state, now)
		respond(w, 200, state)
	})
	mux.HandleFunc("POST /api/review/stop", func(w http.ResponseWriter, _ *http.Request) {
		a.store.mu.Lock()
		a.store.presented = presentedCard{}
		a.store.mu.Unlock()
		respond(w, 200, a.reviewResponse(time.Now(), false))
	})
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, _ *http.Request) {
		a.store.mu.Lock()
		data := a.store.stats(a.store.db.studyDay(time.Now()))
		a.store.mu.Unlock()
		respond(w, 200, data)
	})
	mux.HandleFunc("GET /api/cards", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		page, _ := strconv.Atoi(query.Get("page"))
		page = min(100_000, max(0, page))
		a.store.mu.Lock()
		data := a.store.list(selection{Query: query.Get("query"), Filter: query.Get("filter"), Sort: query.Get("sort")}, page)
		paused, online := time.Now().Before(a.paused), a.store.db.Settings.Online
		a.store.mu.Unlock()
		respond(w, 200, struct {
			listResult
			Paused bool `json:"paused"`
			Online bool `json:"online"`
		}{data, paused, online})
	})
	mux.HandleFunc("GET /api/card/{key}", func(w http.ResponseWriter, r *http.Request) {
		a.store.mu.Lock()
		card := a.store.db.Words[r.PathValue("key")]
		if card == nil {
			a.store.mu.Unlock()
			problem(w, 404, "找不到这个单词。")
			return
		}
		data := *card
		translating := a.viewExampleLocked(card)
		data.Lookup = card.Lookup
		a.store.mu.Unlock()
		details := presentCard(data)
		details.TranslationPending = translating
		respond(w, 200, details)
	})
	mux.HandleFunc("GET /api/history/{key}", func(w http.ResponseWriter, r *http.Request) {
		a.store.mu.Lock()
		card := a.store.db.Words[r.PathValue("key")]
		if card == nil {
			a.store.mu.Unlock()
			problem(w, 404, "找不到这个单词。")
			return
		}
		data := a.store.history(card)
		a.store.mu.Unlock()
		respond(w, 200, data)
	})
	mux.HandleFunc("POST /api/vote", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key        string `json:"key"`
			Reviews    int    `json:"reviews"`
			Order      uint64 `json:"order"`
			Date       string `json:"date"`
			Remembered bool   `json:"remembered"`
			Stay       bool   `json:"stay"`
			PassToday  bool   `json:"passToday"`
		}
		if !requestJSON(w, r, 4096, &req) {
			return
		}
		if req.PassToday && (!req.Remembered || req.Stay) {
			problem(w, 400, "直接完成今日记忆时，请提交「记住了」。")
			return
		}
		day := a.reviewDay()
		if req.Date != day {
			problem(w, 409, errStaleReview.Error())
			return
		}
		data, err := a.store.recordVote(day, req.Key, req.Reviews, req.Order, req.Remembered, req.Stay, req.PassToday)
		if errors.Is(err, errStaleReview) {
			problem(w, 409, err.Error())
		} else if err != nil {
			saveError(w, err)
		} else {
			data.reviewState = a.reviewResponse(time.Now(), true)
			respond(w, 200, data)
		}
	})
	mux.HandleFunc("POST /api/review/next", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Order uint64 `json:"order"`
		}
		if !requestJSON(w, r, 4096, &req) {
			return
		}
		_, err := a.store.nextAfterForgot(a.reviewDay(), req.Order)
		if errors.Is(err, errStaleReview) {
			problem(w, 409, err.Error())
		} else if err != nil {
			saveError(w, err)
		} else {
			a.store.mu.Lock()
			a.store.presented = presentedCard{}
			a.store.mu.Unlock()
			respond(w, 200, a.reviewResponse(time.Now(), true))
		}
	})
	mux.HandleFunc("POST /api/undo", func(w http.ResponseWriter, _ *http.Request) {
		data, err := a.store.undo(a.reviewDay())
		if err != nil {
			saveError(w, err)
			return
		}
		a.store.mu.Lock()
		a.store.pinReview(data)
		if data.Current != nil {
			a.viewExampleLocked(a.store.db.Words[data.Current.Key])
		}
		a.store.mu.Unlock()
		setExamDate(&data, time.Now())
		respond(w, 200, data)
	})
	mux.HandleFunc("POST /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var req settings
		if !requestJSON(w, r, 4096, &req) {
			return
		}
		if !validExamDate(req.ExamDate) {
			problem(w, 400, "考试日期无效，请选择日历上的有效日期。")
			return
		}
		if !validRollover(req.Rollover) {
			problem(w, 400, "请输入有效的学习日结束时间，例如 05:00。")
			return
		}
		if !req.valid() {
			problem(w, 400, "每日记忆所需连对次数与长期掌握所需累计天数均须为 1 至 99。每日上限须为 1 至 10,000，或设为全部。")
			return
		}
		now := time.Now()
		_, err := a.store.changeSettingsAt(now, req)
		if err != nil {
			saveError(w, err)
			return
		}
		a.resumeLookup()
		respond(w, 200, a.reviewResponse(now, false))
	})
	mux.HandleFunc("POST /api/import-preview", func(w http.ResponseWriter, r *http.Request) {
		_, items, ok := readImport(w, r)
		if !ok {
			return
		}
		a.store.mu.Lock()
		plan, _ := a.store.preview(items)
		a.store.mu.Unlock()
		respond(w, 200, plan)
	})
	mux.HandleFunc("POST /api/import", func(w http.ResponseWriter, r *http.Request) {
		req, items, ok := readImport(w, r)
		if !ok {
			return
		}
		result, err := a.store.add(a.currentStudyDay(), items, req.Token, &a.dict)
		if errors.Is(err, errImportChanged) {
			problem(w, 409, err.Error())
			return
		}
		if err != nil {
			saveError(w, err)
			return
		}
		a.store.mu.Lock()
		for _, key := range result.Keys {
			a.queueTranslationLocked(a.store.db.Words[key], false)
		}
		a.store.mu.Unlock()
		a.poke()
		respond(w, 200, result)
	})
	mux.HandleFunc("POST /api/import-status", func(w http.ResponseWriter, r *http.Request) {
		var req selection
		if !requestJSON(w, r, 256<<10, &req) {
			return
		}
		type lookupStatus struct {
			Pending  int  `json:"pending"`
			Complete int  `json:"complete"`
			Partial  int  `json:"partial"`
			Online   bool `json:"online"`
			Paused   bool `json:"paused"`
		}
		a.store.mu.Lock()
		data := lookupStatus{Online: a.store.db.Settings.Online, Paused: time.Now().Before(a.paused)}
		for _, item := range a.store.selected(req) {
			_, translating := a.translations.pending[item.Key]
			switch {
			case !item.needsOnline() && item.ExampleTranslation != "":
				data.Complete++
			case (item.Lookup == "pending" || translating) && data.Online:
				data.Pending++
			default:
				data.Partial++
			}
		}
		a.store.mu.Unlock()
		respond(w, 200, data)
	})
	mux.HandleFunc("POST /api/batch/{action}", func(w http.ResponseWriter, r *http.Request) {
		var req selection
		if !requestJSON(w, r, 2<<20, &req) {
			return
		}
		action := r.PathValue("action")
		switch action {
		case "retry", "reset", "remove", "restore":
		default:
			problem(w, 400, "未知的词库操作。")
			return
		}
		count, err := a.store.batch(a.currentStudyDay(), req, action)
		if err != nil {
			saveError(w, err)
			return
		}
		if action == "retry" || action == "restore" {
			a.resumeLookup()
		}
		respond(w, 200, map[string]int{"count": count})
	})
	mux.HandleFunc("POST /api/note/{key}", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Note  string `json:"note"`
			Clock int64  `json:"clock"`
		}
		if !requestJSON(w, r, 128<<10, &req) {
			return
		}
		if utf8.RuneCountInString(req.Note) > 12000 || req.Clock < 0 {
			problem(w, 400, "备注最多 12,000 字。")
			return
		}
		a.updateCard(w, r.PathValue("key"), func(c *wordCard) error {
			if req.Clock >= c.NoteClock {
				c.PersonalNote = req.Note
				c.NoteClock = req.Clock
			}
			return nil
		})
	})
	mux.HandleFunc("POST /api/card/{key}", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		if !requestJSON(w, r, 512<<10, &req) {
			return
		}
		a.updateCard(w, r.PathValue("key"), func(c *wordCard) error {
			oldExample, oldTranslation := c.Examples, c.ExampleTranslation
			fields := map[string]*string{
				"meaning": &c.Meaning, "pos": &c.POS, "ipa": &c.IPA, "ipaUK": &c.IPAUK, "ipaUS": &c.IPAUS,
				"examples": &c.Examples, "exampleTranslation": &c.ExampleTranslation, "etymology": &c.Etymology,
				"collocations": &c.Collocations, "note": &c.Note,
			}
			for field, value := range req {
				switch {
				case field == "word":
					name, key, ok := normalize(value)
					if !ok {
						return errors.New("请输入有效的英文单词或短语。")
					}
					c.Word, c.Key = name, key
				case field == "personalNote":
					c.PersonalNote = value
					c.NoteClock = max(c.NoteClock+1, time.Now().UnixMilli()*1000)
				case fields[field] != nil:
					*fields[field] = strings.TrimSpace(value)
				}
			}
			if c.cardDetails.size() > 12000 || utf8.RuneCountInString(c.PersonalNote) > 12000 {
				return errors.New("卡片资料及备注各最多 12,000 字。")
			}
			if c.ExampleTranslation != oldTranslation {
				c.ExampleTranslationBy = "manual"
				if c.Examples == oldExample && (c.ExampleBy == "simple" || c.ExampleBy == "simple-mt") {
					c.ExampleBy = "simple-edited"
				}
			} else if c.Examples != oldExample {
				if c.ExampleTranslationBy == "mymemory" || bundledMachineTranslation(c.ExampleBy) {
					c.ExampleTranslation = ""
				}
				c.ExampleTranslationBy = ""
			}
			if c.Examples != oldExample {
				c.ExampleBy = ""
			}
			return nil
		})
	})
	mux.HandleFunc("GET /api/export/filename", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, 200, map[string]string{"filename": a.csvFilename()})
	})
	mux.HandleFunc("POST /api/export", func(w http.ResponseWriter, r *http.Request) {
		var req selection
		if !requestJSON(w, r, 2<<20, &req) {
			return
		}
		data, err := a.exportCSV(req)
		if err != nil {
			saveError(w, err)
			return
		}
		download(w, "text/csv; charset=utf-8", a.csvFilename(), data)
	})
	mux.HandleFunc("POST /api/export/windows", func(w http.ResponseWriter, r *http.Request) {
		var req selection
		if requestJSON(w, r, 2<<20, &req) {
			saveWindowsFile(w, a.csvFilename(), ".csv", func() ([]byte, error) { return a.exportCSV(req) })
		}
	})
	for kind, data := range map[string][]byte{"common": commonCSVTemplate, "full": fullCSVTemplate} {
		name := "LexiGo-template-" + kind + ".csv"
		mux.HandleFunc("GET /api/csv-template/"+kind, func(w http.ResponseWriter, _ *http.Request) {
			download(w, "text/csv; charset=utf-8", name, data)
		})
		mux.HandleFunc("POST /api/csv-template/"+kind+"/windows", func(w http.ResponseWriter, _ *http.Request) {
			saveWindowsFile(w, name, ".csv", func() ([]byte, error) { return data, nil })
		})
	}
	mux.HandleFunc("GET /api/backup", func(w http.ResponseWriter, _ *http.Request) {
		data, err := a.backupBytes()
		if err != nil {
			saveError(w, err)
			return
		}
		download(w, studyFileMIME, transferFilename(time.Now()), data)
	})
	mux.HandleFunc("POST /api/backup/windows", func(w http.ResponseWriter, _ *http.Request) {
		saveWindowsFile(w, transferFilename(time.Now()), studyFileExt, a.backupBytes)
	})
	mux.HandleFunc("POST /api/restore", func(w http.ResponseWriter, r *http.Request) {
		var incoming database
		if !requestJSON(w, r, 128<<20, &incoming) {
			return
		}
		if err := a.restore(incoming); err != nil {
			if errors.Is(err, errInvalidBackup) {
				problem(w, 400, err.Error())
			} else {
				saveError(w, err)
			}
			return
		}
		a.resumeLookup()
		respond(w, 200, map[string]bool{"ok": true})
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		if r.Host != a.host {
			problem(w, 403, "请从LexiGo程序打开此窗口。")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/start/") {
			cookie, err := r.Cookie(cookieName)
			if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(a.token)) != 1 {
				problem(w, 403, "请双击 LexiGo.exe 重新打开LexiGo。")
				return
			}
			if r.Method == http.MethodPost {
				origin := r.Header.Get("Origin")
				sameOrigin := origin == "http://"+a.host ||
					(origin == "" && r.Header.Get("Sec-Fetch-Site") == "same-origin")
				if !sameOrigin || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
					problem(w, 403, "请求必须来自当前LexiGo窗口。")
					return
				}
			}
			a.lastSeen.Store(time.Now().Unix())
		}
		mux.ServeHTTP(w, r)
	})
}

func readImport(w http.ResponseWriter, r *http.Request) (importInput, []importedWord, bool) {
	var req importInput
	if !requestJSON(w, r, 8<<20, &req) {
		return req, nil, false
	}
	items, err := parseImport(req)
	if err != nil {
		problem(w, 400, err.Error())
		return req, nil, false
	}
	return req, items, true
}

func (a *application) resumeLookup() {
	a.store.mu.Lock()
	a.paused = time.Time{}
	if !a.store.db.Settings.Online && a.translations.cancel != nil {
		a.translations.cancel()
	}
	a.store.mu.Unlock()
	a.poke()
	a.wakeTranslations()
}

func (a *application) restore(incoming database) error {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	if err := a.store.restoreLocked(incoming); err != nil {
		return err
	}
	a.generation++
	a.resetTranslationQueueLocked()
	return nil
}

func download(w http.ResponseWriter, contentType, filename string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_, _ = w.Write(data)
}

func (a *application) updateCard(w http.ResponseWriter, key string, update func(*wordCard) error) {
	a.store.mu.Lock()
	card := a.store.db.Words[key]
	if card == nil {
		a.store.mu.Unlock()
		problem(w, 404, "找不到这个单词。")
		return
	}
	next := *card
	if err := update(&next); err != nil {
		a.store.mu.Unlock()
		problem(w, 400, err.Error())
		return
	}
	keys := []string{key}
	if next.Key != key {
		if a.store.db.Words[next.Key] != nil {
			a.store.mu.Unlock()
			problem(w, 409, errWordConflict.Error())
			return
		}
		keys = append(keys, next.Key)
		delete(a.store.db.Words, key)
	}
	a.store.db.Words[next.Key] = &next
	err := a.store.save(keys, len(a.store.db.Events))
	if err != nil {
		delete(a.store.db.Words, next.Key)
		a.store.db.Words[key] = card
	}
	a.store.mu.Unlock()
	if err != nil {
		saveError(w, err)
		return
	}
	a.poke()
	respond(w, 200, presentCard(next))
}
