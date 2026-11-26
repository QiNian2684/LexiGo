package main

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewBindingsPersistAndValidate(t *testing.T) {
	dir := t.TempDir()
	settings := openReviewShortcuts(dir)
	if !maps.Equal(settings.snapshot(), defaultReviewBindings()) {
		t.Fatal("missing preferences must use the defaults")
	}
	mux := http.NewServeMux()
	settings.routes(mux)
	post := func(body string, status int, want string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/shortcuts", strings.NewReader(body)))
		if recorder.Code != status || !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("%s: %d %s", body, recorder.Code, recorder.Body)
		}
	}
	post(`{"action":"forgot","binding":"Shift+A"}`, http.StatusOK, "Shift+A")
	post(`{"action":"remember","binding":"Numpad2"}`, http.StatusOK, "Numpad2")
	post(`{"action":"next","binding":"F2"}`, http.StatusOK, "F2")
	post(`{"action":"reveal","binding":"F2"}`, http.StatusOK, "F2")
	post(`{"action":"undo","binding":"F2"}`, http.StatusBadRequest, "同一个快捷键")
	post(`{"action":"undo","binding":"Ctrl+C"}`, http.StatusBadRequest, "保留")
	post(`{"action":"undo","binding":"Alt+F4"}`, http.StatusBadRequest, "保留")
	post(`{"action":"undo","binding":"Escape"}`, http.StatusBadRequest, "支持")
	post(`{"action":"unknown","binding":"B"}`, http.StatusBadRequest, "复习操作")
	reopened := openReviewShortcuts(dir).snapshot()
	want := reviewBindings{"reveal": "F2", "forgot": "Shift+A", "remember": "Numpad2", "next": "F2", "undo": "Ctrl+Z"}
	if !maps.Equal(reopened, want) {
		t.Fatalf("bindings after reopening: %v", reopened)
	}
	post(`{"reset":true}`, http.StatusOK, `"next":"Enter"`)
	if !maps.Equal(openReviewShortcuts(dir).snapshot(), defaultReviewBindings()) {
		t.Fatal("reset did not survive reopening")
	}
}

func TestCorruptOrUnwritableShortcutPreferences(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, shortcutsFile)
	invalid := []byte(`{"reveal":"Enter","forgot":"Enter","remember":"2","next":"Enter","undo":"Ctrl+Z"}`)
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	if keys := openReviewShortcuts(dir).snapshot(); !maps.Equal(keys, defaultReviewBindings()) {
		t.Fatalf("invalid bindings were loaded: %v", keys)
	}
	s := openReviewShortcuts(filepath.Join(dir, "missing"))
	mux := http.NewServeMux()
	s.routes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/shortcuts", strings.NewReader(`{"action":"forgot","binding":"A"}`)))
	if w.Code != http.StatusInternalServerError || !maps.Equal(s.snapshot(), defaultReviewBindings()) {
		t.Fatalf("an unsuccessful write changed the active bindings: %d %v", w.Code, s.snapshot())
	}
}
