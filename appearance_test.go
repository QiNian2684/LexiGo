package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppearancePersistsAndRejectsUnknownModes(t *testing.T) {
	dir := t.TempDir()
	prefs := openAppearance(dir)
	if prefs.get() != "system" {
		t.Fatal("a new device should follow the system")
	}
	mux := http.NewServeMux()
	(&application{appearance: prefs}).appearanceRoutes(mux)
	set := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/appearance", strings.NewReader(body)))
		return w
	}
	if response := set(`{"theme":"dark"}`); response.Code != http.StatusOK {
		t.Fatalf("saving dark mode: %s", response.Body)
	}
	if response := set(`{"theme":"invalid"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown theme accepted: %s", response.Body)
	}
	if reopened := openAppearance(dir); reopened.get() != "dark" {
		t.Fatal("saved mode was not read after restart")
	}
	if response := set(`{"theme":"system"}`); response.Code != http.StatusOK {
		t.Fatalf("saving follow system: %s", response.Body)
	}
	if reopened := openAppearance(dir); reopened.get() != "system" {
		t.Fatal("the system preference was replaced by an effective palette")
	}
}

func TestCorruptAppearanceFallsBackWithoutChangingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, appearanceFile)
	const invalid = `{"theme":"blue"}`
	if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	if mode := openAppearance(dir).get(); mode != "system" {
		t.Fatalf("corrupt appearance mode %s", mode)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != invalid {
		t.Fatalf("reading invalid preferences modified the file: %q %v", saved, err)
	}
}
