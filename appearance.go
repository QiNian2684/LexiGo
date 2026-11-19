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
	Theme     string `json:"theme"`
	HideGuide bool   `json:"hideGuide,omitempty"`
}

type appearanceSettings struct {
	path           string
	mu             sync.Mutex
	prefs          appearancePreferences
	guidePresented bool
}

func validTheme(mode string) bool {
	return mode == "system" || mode == "light" || mode == "dark"
}

func openAppearance(dir string) *appearanceSettings {
	s := &appearanceSettings{path: filepath.Join(dir, appearanceFile), prefs: appearancePreferences{Theme: "system"}}
	var saved appearancePreferences
	if data, err := os.ReadFile(s.path); err == nil && json.Unmarshal(data, &saved) == nil && validTheme(saved.Theme) {
		s.prefs = saved
	}
	return s
}

func (s *appearanceSettings) windowPreferences() (appearancePreferences, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	showGuide := !s.guidePresented && !s.prefs.HideGuide
	s.guidePresented = true
	return s.prefs, showGuide
}

func (s *appearanceSettings) saveLocked(prefs appearancePreferences) error {
	data, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	if err := atomicWrite(s.path, data); err != nil {
		return err
	}
	s.prefs = prefs
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
		s := a.appearance
		s.mu.Lock()
		defer s.mu.Unlock()
		prefs.HideGuide = s.prefs.HideGuide
		if err := s.saveLocked(prefs); err != nil {
			problem(w, http.StatusInternalServerError, "无法保存外观主题，请检查数据文件夹的写入权限。")
			return
		}
		respond(w, http.StatusOK, prefs)
	})
	mux.HandleFunc("POST /api/guide", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Hidden bool `json:"hidden"`
		}
		if !requestJSON(w, r, 1024, &req) {
			return
		}
		s := a.appearance
		s.mu.Lock()
		defer s.mu.Unlock()
		prefs := s.prefs
		prefs.HideGuide = req.Hidden
		if err := s.saveLocked(prefs); err != nil {
			problem(w, http.StatusInternalServerError, "无法保存提示设置，请检查数据文件夹的写入权限。")
			return
		}
		respond(w, http.StatusOK, req)
	})
}
