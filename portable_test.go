package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func portableTestDirectories(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	legacy := filepath.Join(root, "旧 数据 # 50%")
	destination := filepath.Join(root, "新版 # 10%", "Data")
	for _, dir := range []string{legacy, destination} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return legacy, destination
}

func TestPortableMigrationPreservesProgressAndCommittedWAL(t *testing.T) {
	legacy, dest := portableTestDirectories(t)
	db, err := openStore(filepath.Join(legacy, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.io.Close()
	if _, err := db.io.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	const day = "2026-11-16"
	setTestGoal(t, db, day, 8, 100)
	importTest(t, db, day, "crucial, computer, engine")
	db.db.Words["crucial"].PersonalNote = "我的笔记"
	mustSave(t, db, "crucial")
	nextVote(t, db, day, true)
	nextVote(t, db, day, false)
	backup := []byte(`{"example":"existing auto backup"}`)
	if err := os.WriteFile(filepath.Join(legacy, "before-restore.json"), backup, 0600); err != nil {
		t.Fatal(err)
	}
	wal, err := os.Stat(filepath.Join(legacy, databaseName+"-wal"))
	if err != nil || wal.Size() == 0 {
		t.Fatalf("fixture has no committed WAL: %v", err)
	}
	notice, err := migrateLegacy(dest, legacy)
	if err != nil || !strings.Contains(notice, "已迁移") {
		t.Fatalf("migration: %s %v", notice, err)
	}
	next, err := openStore(filepath.Join(dest, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	defer next.io.Close()
	if !reflect.DeepEqual(next.db, db.db) {
		t.Fatal("migration lost settings, cards or event history")
	}
	if p := next.planReview(day); len(p.repeats) != 1 || p.state.Reviewed != 2 || next.db.Words["crucial"].PersonalNote != "我的笔记" {
		t.Fatalf("day and notes not preserved: %+v", p.state)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "backups", "legacy-before-restore.json")); err != nil || !bytes.Equal(data, backup) {
		t.Fatalf("legacy backup not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacy, databaseName)); err != nil {
		t.Fatalf("original removed: %v", err)
	}
}

func TestPortableMigrationLeavesDifferentDatabasesAndRejectsOrphan(t *testing.T) {
	legacy, dest := portableTestDirectories(t)
	for dir, word := range map[string]string{legacy: "original", dest: "other"} {
		s, err := openStore(filepath.Join(dir, databaseName))
		if err != nil {
			t.Fatal(err)
		}
		importTest(t, s, "2026-11-16", word)
		if err := s.io.Close(); err != nil {
			t.Fatal(err)
		}
	}
	targetPath := filepath.Join(dest, databaseName)
	before, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	notice, err := migrateLegacy(dest, legacy)
	if err != nil || !strings.Contains(notice, "两处文件都已保留") {
		t.Fatalf("conflict missing: %s %v", notice, err)
	}
	if data, err := os.ReadFile(targetPath); err != nil || !bytes.Equal(data, before) {
		t.Fatalf("target was overwritten: %v", err)
	}
	orphan := t.TempDir()
	if err := os.WriteFile(filepath.Join(orphan, databaseName+"-wal"), []byte("partial copy"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = migrateLegacy(orphan, legacy); err == nil {
		t.Fatal("an orphan WAL was treated as a new data directory")
	}
}

func TestPortableMigrationRejectsBusyAndBrokenSource(t *testing.T) {
	legacy, dest := portableTestDirectories(t)
	source := filepath.Join(legacy, databaseName)
	if err := os.WriteFile(source, []byte("proxy for a broken database"), 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(legacy, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := tryLock(lock)
	if err != nil || !ok {
		lock.Close()
		t.Fatalf("fixture lock: %v", err)
	}
	if _, err := migrateLegacy(dest, legacy); err == nil || !strings.Contains(err.Error(), "仍在运行") {
		lock.Close()
		t.Fatalf("busy old application did not block migration: %v", err)
	}
	lock.Close()
	if _, err = migrateLegacy(dest, legacy); err == nil || !strings.Contains(err.Error(), "原数据库已保留") {
		t.Fatalf("broken source did not fail safely: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, databaseName)); !os.IsNotExist(err) {
		t.Fatalf("migration published an invalid file: %v", err)
	}
}

func TestDirectoryUsesExecutableLocation(t *testing.T) {
	appDir := t.TempDir()
	exePath := filepath.Join(appDir, "背词本.exe")
	if err := os.WriteFile(exePath, []byte("fake executable path"), 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := portableDataDirectory(exePath)
	if err != nil || dir != filepath.Join(appDir, "Data") {
		t.Fatalf("directory = %q, err = %v", dir, err)
	}
}

func TestInstalledDirectoryUsesChosenPathAndKeepsPortableDefault(t *testing.T) {
	appDir := t.TempDir()
	exePath := filepath.Join(appDir, "LexiGo.exe")
	if err := os.WriteFile(exePath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	location := filepath.Join(t.TempDir(), "词库空格 🗂")
	file := filepath.Join(appDir, dataLocationFile)
	writePath := func(value string) {
		t.Helper()
		data := []byte{0xff, 0xfe}
		for _, unit := range utf16.Encode([]rune(value)) {
			data = binary.LittleEndian.AppendUint16(data, unit)
		}
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writePath(location)
	if dir, err := portableDataDirectory(exePath); err != nil || dir != location {
		t.Fatalf("configured directory = %q (%v)", dir, err)
	}
	writePath("Data")
	if dir, err := portableDataDirectory(exePath); err != nil || dir != filepath.Join(appDir, "Data") {
		t.Fatalf("relative directory = %q (%v)", dir, err)
	}
	writePath(filepath.Join("not", "absolute"))
	if dir, err := portableDataDirectory(exePath); err == nil || dir != "" {
		t.Fatalf("invalid setting silently chose %q", dir)
	}
}
