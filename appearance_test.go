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
	if shown, _ := prefs.windowPreferences(); shown.Theme != "system" {
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
	if prefs, _ := openAppearance(dir).windowPreferences(); prefs.Theme != "dark" {
		t.Fatal("saved mode was not read after restart")
	}
	if response := set(`{"theme":"system"}`); response.Code != http.StatusOK {
		t.Fatalf("saving follow system: %s", response.Body)
	}
	if prefs, _ := openAppearance(dir).windowPreferences(); prefs.Theme != "system" {
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
	if prefs, _ := openAppearance(dir).windowPreferences(); prefs.Theme != "system" {
		t.Fatalf("corrupt appearance mode %s", prefs.Theme)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != invalid {
		t.Fatalf("reading invalid preferences modified the file: %q %v", saved, err)
	}
}

func TestGuideRepeatsOnLaunchUntilOptOut(t *testing.T) {
	dir := t.TempDir()
	app := &application{appearance: openAppearance(dir)}
	for range 2 {
		if prefs, show := app.appearance.windowPreferences(); !show || prefs.HideGuide {
			t.Fatal("guide must show on launch by default")
		}
		if _, show := app.appearance.windowPreferences(); show {
			t.Fatal("guide was repeated in the same launch")
		}
		app.appearance = openAppearance(dir)
	}
	post := func(path, body string) {
		t.Helper()
		mux := http.NewServeMux()
		app.appearanceRoutes(mux)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("could not save settings: %d %s", w.Code, w.Body)
		}
	}
	post("/api/guide", `{"hidden":true}`)
	post("/api/appearance", `{"theme":"dark"}`)
	app.appearance = openAppearance(dir)
	if prefs, show := app.appearance.windowPreferences(); show || !prefs.HideGuide || prefs.Theme != "dark" {
		t.Fatalf("opt-out or theme did not survive restart: %+v", prefs)
	}
	post("/api/guide", `{"hidden":false}`)
	if prefs, show := openAppearance(dir).windowPreferences(); !show || prefs.HideGuide || prefs.Theme != "dark" {
		t.Fatalf("re-enabling did not preserve theme and guide: %+v", prefs)
	}
}
