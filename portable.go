package main

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	databaseName     = "vocabulary.db"
	dataLocationFile = "LexiGo.data-dir"
)

func dataDirectory() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法识别LexiGo程序的位置：%w", err)
	}
	return portableDataDirectory(executable)
}

func portableDataDirectory(executable string) (string, error) {
	realPath, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("无法识别LexiGo程序的真实位置：%w", err)
	}
	dir := filepath.Dir(realPath)
	config := filepath.Join(dir, dataLocationFile)
	data, err := os.ReadFile(config)
	if errors.Is(err, os.ErrNotExist) {
		return filepath.Join(dir, "Data"), nil
	}
	if err != nil {
		return "", fmt.Errorf("无法读取数据目录设置 %s：%w", config, err)
	}
	location := strings.TrimRight(decodeDataLocation(data), "\r\n")
	if strings.EqualFold(location, "Data") {
		return filepath.Join(dir, "Data"), nil
	}
	if !filepath.IsAbs(location) || strings.ContainsAny(location, "\x00\r\n") {
		return "", fmt.Errorf("数据目录设置无效：%s\n请重新运行安装向导，选择正确的数据文件夹。", config)
	}
	return filepath.Clean(location), nil
}

func decodeDataLocation(data []byte) string {
	// NSIS writes Windows paths as UTF-16LE with a byte order mark.
	if body, ok := bytes.CutPrefix(data, []byte{0xff, 0xfe}); ok {
		if len(body)%2 != 0 {
			return ""
		}
		units := make([]uint16, len(body)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(body[i*2:])
		}
		return string(utf16.Decode(units))
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return ""
}

func dataWriteError(dir string, err error) error {
	return fmt.Errorf("无法写入数据目录：\n%s\n请检查此文件夹的写入权限及可用空间。\n%w", dir, err)
}

func checkDirectoryWritable(dir string) error {
	file, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return dataWriteError(dir, err)
	}
	defer os.Remove(file.Name())
	if err := file.Close(); err != nil {
		return dataWriteError(dir, err)
	}
	return nil
}

func fileInfo(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("此路径不是普通文件：%s", path)
	}
	return info, nil
}

func migrateLegacy(dir, legacy string) (string, error) {
	target := filepath.Join(dir, databaseName)
	targetInfo, err := fileInfo(target)
	if err != nil {
		return "", err
	}
	if targetInfo == nil {
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			info, err := fileInfo(target + suffix)
			if err != nil {
				return "", err
			}
			if info != nil {
				return "", fmt.Errorf("Data 中仅找到 SQLite 附属文件，缺少 %s。请从原文件夹带齐数据库文件后重试。", databaseName)
			}
		}
	}
	if legacy == "" {
		return "", nil
	}

	source := filepath.Join(legacy, databaseName)
	if strings.EqualFold(target, source) {
		return "", nil
	}
	oldInfo, err := fileInfo(source)
	if err != nil {
		return "", fmt.Errorf("无法检查旧词库：%w", err)
	}
	if oldInfo == nil {
		return "", nil
	}
	if targetInfo != nil {
		if os.SameFile(targetInfo, oldInfo) {
			return "", nil
		}
		if targetInfo.Size() == 0 {
			return "", fmt.Errorf("Data 中的 %s 是空文件，旧词库仍在 %s。请先移走空文件，再启动程序迁移旧词库。", databaseName, legacy)
		}
		return "检测到旧位置仍有词库。本次使用当前数据目录中的词库，两处文件都已保留。", nil
	}

	// Take the old executable's own lock before reading its last WAL state.
	lock, err := os.OpenFile(filepath.Join(legacy, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", fmt.Errorf("无法锁定旧词库：%w", err)
	}
	defer lock.Close()
	locked, err := tryLock(lock)
	if err != nil {
		return "", fmt.Errorf("无法锁定旧词库：%w", err)
	}
	if !locked {
		return "", errors.New("旧版LexiGo仍在运行。请关闭旧版并等待后台退出，或在任务管理器结束旧版进程，然后重新启动新版。旧词库已保留。")
	}

	if err = snapshotDatabase(source, target); err != nil {
		return "", fmt.Errorf("旧词库迁移未完成，原数据库已保留。\n%w", err)
	}
	if err = preserveLegacyBackup(legacy, dir); err != nil {
		return "词库已迁移到 Data。旧版的恢复前备份仍在原位置，可手动复制。", nil
	}
	return "旧词库已迁移到当前数据目录，原数据库仍保留在旧位置。", nil
}

func sqliteReadOnly(path string) (*sql.DB, error) {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path // Windows drive letter, e.g. /C:/Users/...
	}
	location := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	return sql.Open(sqliteDriverName, location.String())
}

func snapshotDatabase(source, target string) error {
	in, err := sqliteReadOnly(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), ".migrate-*.db")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Close(); err != nil {
		return err
	}
	// SQLite materializes a consistent snapshot including committed WAL pages.
	if _, err = in.Exec("VACUUM INTO ?", tmp.Name()); err != nil {
		return err
	}
	if err = verifyDatabaseSnapshot(tmp.Name()); err != nil {
		return err
	}
	out, err := os.OpenFile(tmp.Name(), os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	syncErr, closeErr := out.Sync(), out.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return err
	}
	return publishFile(tmp.Name(), target)
}

func verifyDatabaseSnapshot(path string) error {
	check, err := sqliteReadOnly(path)
	if err != nil {
		return err
	}
	defer check.Close()
	var integrity string
	if err := check.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("数据库副本完整性检查未通过：%s", integrity)
	}
	_, err = readDatabase(check)
	return err
}

func preserveLegacyBackup(legacy, dir string) error {
	from := filepath.Join(legacy, "before-restore.json")
	info, err := fileInfo(from)
	if err != nil || info == nil {
		return err
	}
	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	target := filepath.Join(backupDir, "legacy-before-restore.json")
	if info, err := fileInfo(target); err != nil || info != nil {
		return err
	}
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	staged, err := os.CreateTemp(backupDir, ".legacy-backup-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	_, err = io.Copy(staged, source)
	if err == nil {
		err = staged.Sync()
	}
	if err := errors.Join(err, staged.Close()); err != nil {
		return err
	}
	return publishFile(staged.Name(), target)
}
