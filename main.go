package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

var launchPattern = regexp.MustCompile(`^http://127\.0\.0\.1:[0-9]{1,5}/start/[a-f0-9]{48}$`)

func main() {
	if err := run(); err != nil {
		reportStartupError(err)
		os.Exit(1)
	}
}

func run() error {
	serveOnly := slices.Contains(os.Args[1:], "--serve")
	var directory string
	if serveOnly {
		directory = os.Getenv("WORDREVIEW_DATA")
	}
	return runEngine(serveOnly, directory, func(link string) { fmt.Println(link) })
}

func runEngine(serveOnly bool, dir string, ready func(string)) error {
	explicitDir := dir != ""
	if !explicitDir {
		var err error
		dir, err = dataDirectory()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return dataWriteError(dir, err)
	}

	if err := checkDirectoryWritable(dir); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return dataWriteError(dir, err)
	}
	defer lock.Close()
	instance := filepath.Join(dir, "instance")
	acquired, err := tryLock(lock)
	if err != nil {
		return err
	}
	if !acquired {
		for attempt := 0; attempt < 24; attempt++ {
			link, _ := os.ReadFile(instance)
			if value := strings.TrimSpace(string(link)); launchPattern.MatchString(value) && instanceRunning(value) {
				if serveOnly {
					ready(value)
					return nil
				}
				return openWindow(value)
			}
			time.Sleep(100 * time.Millisecond)
		}
		return errors.New("LexiGo正在启动，请稍等后再打开。")
	}
	_ = os.Remove(instance)
	defer os.Remove(instance)

	legacy, err := legacyDataDirectory()
	if err != nil {
		return fmt.Errorf("无法识别旧版数据目录：%w", err)
	}
	if serveOnly && explicitDir {
		legacy = ""
	}
	notice, err := migrateLegacy(dir, legacy)
	if err != nil {
		return err
	}
	s, err := openStore(filepath.Join(dir, databaseName))
	if err != nil {
		return fmt.Errorf("无法在 %s 打开或写入词库：%w", dir, err)
	}
	defer s.io.Close()
	if s.upgraded {
		notice = strings.TrimSpace(notice + " " + ruleUpgradeNotice)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("无法打开本地窗口：%w", err)
	}
	defer listener.Close()
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return err
	}
	app := &application{
		store: s, token: hex.EncodeToString(secret[:]),
		host: listener.Addr().String(), wake: make(chan struct{}, 1),
		exit: make(chan struct{}, 1), migrationNotice: notice,
		mobile:     runtime.GOOS == "android" || (serveOnly && os.Getenv("WORDREVIEW_MOBILE") == "1"),
		appearance: openAppearance(dir),
	}
	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		app.enrich(ctx)
	}()
	defer func() {
		cancel()
		<-workerDone
	}()
	server := &http.Server{
		Handler: app.handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 25 * time.Second, WriteTimeout: 35 * time.Second,
		IdleTimeout: 25 * time.Second, MaxHeaderBytes: 32 << 10,
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()

	link := "http://" + app.host + "/start/" + app.token
	if err := atomicWrite(instance, []byte(link)); err != nil {
		return err
	}
	var windowDone <-chan struct{}
	if serveOnly {
		ready(link)
		log.SetOutput(os.Stderr)
	} else {
		var closeWindow func()
		windowDone, closeWindow, err = startWindow(link, dir)
		if err != nil {
			return err
		}
		if closeWindow != nil {
			defer closeWindow()
		}
	}

	shutDown := func() error {
		cancel()
		shutdown, finish := context.WithTimeout(context.Background(), 4*time.Second)
		defer finish()
		err := server.Shutdown(shutdown)
		if err != nil {
			_ = server.Close()
		}
		return err
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	start := time.Now()
	for {
		select {
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-app.exit:
			return shutDown()
		case <-windowDone:
			// Allow final keepalive requests from the app window to reach SQLite.
			time.Sleep(500 * time.Millisecond)
			return shutDown()
		case now := <-tick.C:
			if serveOnly {
				continue
			}
			if ticket := app.closedTicket.Load(); ticket != 0 && ticket == app.windowTicket.Load() &&
				now.Sub(time.Unix(0, app.windowClosedAt.Load())) >= 3*time.Second {
				return shutDown()
			}
			if windowDone != nil {
				continue
			}
			last := app.lastSeen.Load()
			if (last != 0 && now.Sub(time.Unix(last, 0)) >= 4*time.Minute) ||
				(last == 0 && now.Sub(start) >= 5*time.Minute) {
				return shutDown()
			}
		}
	}
}

func instanceRunning(link string) bool {
	position := strings.Index(link, "/start/")
	if position < 0 {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, link[:position]+"/api/state", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(req)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 2048))
	return response.StatusCode == http.StatusForbidden &&
		response.Header.Get("X-Content-Type-Options") == "nosniff"
}
