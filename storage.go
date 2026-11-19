package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type diskStore struct {
	*sql.DB
	file string
}

func openStore(file string) (*store, error) {
	disk, err := sql.Open(sqliteDriverName, file)
	if err != nil {
		return nil, err
	}
	disk.SetMaxOpenConns(1)
	var ok bool
	defer func() {
		if !ok {
			disk.Close()
		}
	}()
	for _, command := range []string{
		"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA busy_timeout=5000",
		"CREATE TABLE IF NOT EXISTS meta (id INTEGER PRIMARY KEY CHECK(id=1), body BLOB NOT NULL)",
		"CREATE TABLE IF NOT EXISTS cards (key TEXT PRIMARY KEY, body BLOB NOT NULL)",
		"CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY, body BLOB NOT NULL)",
	} {
		if _, err := disk.Exec(command); err != nil {
			return nil, fmt.Errorf("无法打开本地词库：%w", err)
		}
	}
	db, err := readDatabase(disk)
	if err != nil {
		return nil, err
	}
	s := &store{io: &diskStore{DB: disk, file: file}, db: db}
	if db.Version == 2 {
		if err := s.upgradeStoredRules(); err != nil {
			return nil, fmt.Errorf("无法升级复习规则，旧数据已保留：%w", err)
		}
	}
	if !s.db.CumulativePasses {
		keys, first := upgradePassCounts(&s.db)
		if err := s.save(keys, first); err != nil {
			return nil, fmt.Errorf("无法升级累计过关记录：%w", err)
		}
	}
	keys, err := updateSavedExamples(s.db.Words)
	if err != nil {
		return nil, fmt.Errorf("无法读取内置例句：%w", err)
	}
	if len(keys) > 0 {
		if err := s.save(keys, len(s.db.Events)); err != nil {
			return nil, fmt.Errorf("无法保存补齐的例句翻译：%w", err)
		}
	}
	ok = true
	return s, nil
}

func readDatabase(disk *sql.DB) (database, error) {
	db := database{
		metadata: metadata{Version: 3, CumulativePasses: true, Settings: settings{Limit: defaultLimit, Goal: defaultGoal, DailyGoal: defaultDailyGoal, Online: true, ShowIPA: true}},
		Words:    make(map[string]*wordCard),
		Events:   []event{},
	}
	var data []byte
	err := disk.QueryRow("SELECT body FROM meta WHERE id=1").Scan(&data)
	if err == nil {
		var saved metadata
		if err = json.Unmarshal(data, &saved); err != nil {
			return database{}, err
		}
		db.metadata = saved
	} else if !errors.Is(err, sql.ErrNoRows) {
		return database{}, err
	}
	if err = readRows(disk, "SELECT body FROM cards", func(data []byte) error {
		var card wordCard
		if err := json.Unmarshal(data, &card); err != nil {
			return err
		}
		db.Words[card.Key] = &card
		return nil
	}); err != nil {
		return database{}, err
	}
	if err = readRows(disk, "SELECT body FROM events ORDER BY id", func(data []byte) error {
		var item event
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		db.Events = append(db.Events, item)
		return nil
	}); err != nil {
		return database{}, err
	}
	if !validDatabase(db) {
		return database{}, errors.New("本地词库的格式不受支持或文件已经损坏。")
	}
	return db, nil
}

func readRows(disk *sql.DB, query string, read func([]byte) error) error {
	rows, err := disk.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			return err
		}
		if err = read(data); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *store) save(keys []string, firstEvent int) error {
	return s.persist(s.db, keys, firstEvent, false)
}

func (s *store) persist(db database, keys []string, firstEvent int, replace bool) error {
	tx, err := s.io.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if replace {
		if _, err = tx.Exec("DELETE FROM cards"); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM events"); err != nil {
			return err
		}
	}
	meta, err := json.Marshal(db.metadata)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT OR REPLACE INTO meta(id,body) VALUES(1,?)", meta); err != nil {
		return err
	}

	for _, key := range keys {
		if card := db.Words[key]; card != nil {
			data, err := json.Marshal(card)
			if err != nil {
				return err
			}
			if _, err = tx.Exec("INSERT OR REPLACE INTO cards(key,body) VALUES(?,?)", key, data); err != nil {
				return err
			}
		} else if _, err = tx.Exec("DELETE FROM cards WHERE key=?", key); err != nil {
			return err
		}
	}
	if firstEvent < len(db.Events) {
		saveEvent, err := tx.Prepare("INSERT OR REPLACE INTO events(id,body) VALUES(?,?)")
		if err != nil {
			return err
		}
		defer saveEvent.Close()
		for i := firstEvent; i < len(db.Events); i++ {
			data, err := json.Marshal(db.Events[i])
			if err != nil {
				return err
			}
			if _, err = saveEvent.Exec(i+1, data); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *store) restore(incoming database) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restoreLocked(incoming)
}

const ruleUpgradeNotice = "旧学习记录已保留。旧规则的已掌握词仍为已掌握；其余词的旧进度标在词库中，新规则从零累计。"

func upgradedRules(old database) database {
	if old.Version != 2 {
		return old
	}
	current := old
	current.Version = 3
	current.CumulativePasses = true
	current.Settings.DailyGoal = defaultDailyGoal
	current.Words = make(map[string]*wordCard, len(old.Words))
	for key, previous := range old.Words {
		card := *previous
		card.LegacyStreak, card.LegacyMastered = previous.Streak, previous.Mastered
		card.Streak, card.LastPass, card.RepeatAt, card.RetryGap = 0, "", 0, 0
		card.DailyDay, card.DailyCount, card.DailyTarget = "", 0, 0
		current.Words[key] = &card
	}
	current.Events = append([]event{}, old.Events...)
	for i := range current.Events {
		item := &current.Events[i]
		if item.Kind == "yes" || item.Kind == "no" {
			item.Legacy = true
		}
	}
	return current
}

func (s *store) upgradeStoredRules() error {
	old := s.db
	data, err := json.Marshal(old)
	if err != nil {
		return err
	}
	backupDir := filepath.Join(filepath.Dir(s.io.file), "backups")
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	if err = atomicWrite(filepath.Join(backupDir, "before-dual-memory-v2-"+stamp+".json"), data); err != nil {
		return err
	}
	current := upgradedRules(old)
	keys := make([]string, 0, len(current.Words))
	for key := range current.Words {
		keys = append(keys, key)
	}
	if err = s.persist(current, keys, 0, false); err != nil {
		return err
	}
	s.db, s.upgraded = current, true
	return nil
}

func (s *store) restoreLocked(incoming database) error {
	if !validDatabase(incoming) {
		return errInvalidBackup
	}
	incoming = upgradedRules(incoming)
	data, err := json.Marshal(s.db)
	if err != nil {
		return err
	}
	return s.replaceLocked(incoming, "before-restore.json", data)
}

func (s *store) replaceLocked(incoming database, backupName string, backup []byte) error {
	if !incoming.CumulativePasses {
		upgradePassCounts(&incoming)
	}
	if _, err := updateSavedExamples(incoming.Words); err != nil {
		return err
	}
	keys := make([]string, 0, len(incoming.Words))
	for key := range incoming.Words {
		keys = append(keys, key)
	}
	backupDir := filepath.Join(filepath.Dir(s.io.file), "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(backupDir, backupName), backup); err != nil {
		return err
	}
	if err := s.persist(incoming, keys, 0, true); err != nil {
		return err
	}
	s.db = incoming
	s.display = reviewDisplay{}
	s.presented = presentedCard{}
	return nil
}

func atomicWrite(file string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(file), ".wordreview-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceFile(temp.Name(), file)
}

var errInvalidBackup = errors.New("备份格式无效。请选择从LexiGo导出的 JSON 备份。")

func validDatabase(db database) bool {
	if (db.Version != 2 && db.Version != 3) || !db.Settings.validLongGoal() || db.Words == nil ||
		(db.Version == 3 && !db.Settings.valid()) {
		return false
	}
	ids := make(map[uint64]bool, len(db.Words))
	validDate := func(value string) bool {
		_, err := time.Parse(time.DateOnly, value)
		return err == nil
	}
	for key, card := range db.Words {
		if card == nil || card.ID == 0 || ids[card.ID] || card.Order < card.ID ||
			card.Order > db.NextOrder || card.Key != key || !validDate(card.AddedDay) ||
			card.Streak < 0 || card.Forgot < 0 || card.Reviews < max(card.Forgot, card.Streak) ||
			(card.Lookup != "pending" && card.Lookup != "done") {
			return false
		}
		name, normalized, ok := normalize(card.Word)
		if !ok || name != card.Word || normalized != key {
			return false
		}
		for _, day := range []string{card.LastPass, card.LastTry, card.LastYes, card.LastNo, card.ExtraDay, card.DailyDay} {
			if day != "" && !validDate(day) {
				return false
			}
		}
		if db.Version == 3 && !card.validDailyState() {
			return false
		}
		ids[card.ID] = true
	}
	for _, item := range db.Events {
		if item.Waiting && (item.Kind != "no" || item.Legacy || item.Practice || item.Undone || item.Before == nil) {
			return false
		}
		if !ids[item.Card] || !validDate(item.Day) || item.Tick > db.NextOrder {
			return false
		}
		switch item.Kind {
		case "yes", "no":
			if item.Before == nil || (db.Version == 3 && !item.Legacy &&
				(!item.Before.validDailyState() || (item.Credit && !item.DayPassed) ||
					(item.DayPassed && item.Kind != "yes"))) ||
				(item.Recall && (item.Credit || item.DayPassed || item.Legacy || item.Practice)) {
				return false
			}
		case "add", "reimport", "reset", "remove", "restore":
		default:
			return false
		}
	}
	if batch := db.RecallBatch; batch != nil {
		if !db.CumulativePasses || !validDate(batch.Day) || len(batch.Cards) > recallBatchLimit {
			return false
		}
		seen := make(map[uint64]bool, len(batch.Cards))
		for _, id := range batch.Cards {
			if !ids[id] || seen[id] {
				return false
			}
			seen[id] = true
		}
	}
	return true
}

// The old counter could lose earlier days, but credited answers remain in
// the history. Preserve explicit resets and the snapshots used by Undo.
func upgradePassCounts(db *database) ([]string, int) {
	db.CumulativePasses = true
	days := make(map[uint64]map[string]bool)
	first := len(db.Events)
	for i := range db.Events {
		item := &db.Events[i]
		switch item.Kind {
		case "reimport", "reset":
			delete(days, item.Card)
		case "yes", "no":
			if item.Legacy || item.Practice {
				continue
			}
			if item.Before != nil && item.Before.Streak < len(days[item.Card]) {
				before := *item.Before
				before.Streak = len(days[item.Card])
				item.Before = &before
				first = min(first, i)
			}
			if item.Credit && !item.Undone {
				if days[item.Card] == nil {
					days[item.Card] = make(map[string]bool)
				}
				days[item.Card][item.Day] = true
			}
		}
	}
	var keys []string
	for key, card := range db.Words {
		total := max(card.Streak, len(days[card.ID]))
		mastered := card.Mastered || total >= db.Settings.Goal
		if total != card.Streak || mastered != card.Mastered {
			card.Streak, card.Mastered = total, mastered
			keys = append(keys, key)
		}
	}
	return keys, first
}
