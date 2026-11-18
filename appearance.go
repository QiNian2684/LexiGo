package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

const appearanceFile = "appearance.json"

type appearancePreferences struct {
	Theme string `json:"theme"`
}

type appearanceSettings struct {
	path string
	mu   sync.Mutex
	mode string
}

func validTheme(mode string) bool {
	return mode == "system" || mode == "light" || mode == "dark"
}

func openAppearance(dir string) *appearanceSettings {
	s := &appearanceSettings{path: filepath.Join(dir, appearanceFile), mode: "system"}
	var saved appearancePreferences
	if data, err := os.ReadFile(s.path); err == nil && json.Unmarshal(data, &saved) == nil && validTheme(saved.Theme) {
		s.mode = saved.Theme
	}
	return s
}

func (s *appearanceSettings) get() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

func (s *appearanceSettings) save(mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(appearancePreferences{Theme: mode})
	if err != nil {
		return err
	}
	if err := atomicWrite(s.path, data); err != nil {
		return err
	}
	s.mode = mode
	return nil
}

func (a *application) appearanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/appearance", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, map[string]string{"system": systemAppearance()})
	})
	mux.HandleFunc("POST /api/appearance", func(w http.ResponseWriter, r *http.Request) {
		var prefs appearancePreferences
		if !requestJSON(w, r, 1024, &prefs) {
			return
		}
		if !validTheme(prefs.Theme) {
			problem(w, http.StatusBadRequest, "请选择可用的外观主题。")
			return
		}
		if err := a.appearance.save(prefs.Theme); err != nil {
			problem(w, http.StatusInternalServerError, "无法保存外观主题，请检查数据文件夹的写入权限。")
			return
		}
		respond(w, http.StatusOK, prefs)
	})
}
