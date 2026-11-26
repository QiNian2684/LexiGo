package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const shortcutsFile = "shortcuts.json"

type reviewBindings map[string]string

var shortcutActions = []string{"reveal", "forgot", "remember", "next", "undo"}
var shortcutLabels = map[string]string{
	"reveal": "展开中文释义", "forgot": "没记住", "remember": "记住了",
	"next": "下一单词", "undo": "撤销上一次作答",
}

var shortcutPattern = regexp.MustCompile(`^(Ctrl\+)?(Alt\+)?(Shift\+)?([A-Z0-9]|Enter|Space|Arrow(?:Up|Down|Left|Right)|F(?:[1-9]|1[0-2])|Numpad(?:[0-9]|Enter|Add|Subtract|Multiply|Divide|Decimal|Equal|Comma))$`)

func defaultReviewBindings() reviewBindings {
	return reviewBindings{"reveal": "Enter", "forgot": "1", "remember": "2", "next": "Enter", "undo": "Ctrl+Z"}
}

func reservedShortcut(parts []string) bool {
	key := parts[4]
	switch key {
	case "F1", "F3", "F5", "F6", "F7", "F10", "F11", "F12":
		return true
	}
	if parts[2] != "" {
		switch key {
		case "F4", "Space", "ArrowLeft", "ArrowRight":
			return true
		}
	}
	if parts[1] != "" && parts[2] == "" {
		if len(key) == 1 && strings.Contains("0ACDEFLNOPQRSTVWX", key) {
			return true
		}
		switch key {
		case "Numpad0", "NumpadAdd", "NumpadSubtract":
			return true
		}
	}
	return false
}

func reviewBindingError(keys reviewBindings) string {
	if len(keys) != len(shortcutActions) {
		return "快捷键配置不完整。"
	}
	used := make(map[string]string, len(keys))
	for _, action := range shortcutActions {
		chord := keys[action]
		parts := shortcutPattern.FindStringSubmatch(chord)
		if parts == nil {
			return "请选择支持的按键或组合键。"
		}
		if reservedShortcut(parts) {
			return "该按键由系统或窗口保留，请选择其他快捷键。"
		}
		if previous := used[chord]; previous != "" && (previous != "reveal" || action != "next") {
			return fmt.Sprintf("「%s」与「%s」使用了同一个快捷键，请选择其他按键。", shortcutLabels[previous], shortcutLabels[action])
		}
		used[chord] = action
	}
	return ""
}

type reviewShortcutSettings struct {
	path string
	mu   sync.Mutex
	keys reviewBindings
}

func openReviewShortcuts(dir string) *reviewShortcutSettings {
	s := &reviewShortcutSettings{path: filepath.Join(dir, shortcutsFile), keys: defaultReviewBindings()}
	var saved reviewBindings
	if data, err := os.ReadFile(s.path); err == nil && json.Unmarshal(data, &saved) == nil && reviewBindingError(saved) == "" {
		s.keys = saved
	}
	return s
}

func (s *reviewShortcutSettings) snapshot() reviewBindings {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A published map is immutable. Writers replace it after saving.
	return s.keys
}

func (s *reviewShortcutSettings) routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/shortcuts", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action  string `json:"action"`
			Binding string `json:"binding"`
			Reset   bool   `json:"reset"`
		}
		if !requestJSON(w, r, 1024, &req) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		keys := defaultReviewBindings()
		if !req.Reset {
			if _, ok := s.keys[req.Action]; !ok {
				problem(w, http.StatusBadRequest, "请选择要修改的复习操作。")
				return
			}
			keys = maps.Clone(s.keys)
			keys[req.Action] = req.Binding
		}
		if issue := reviewBindingError(keys); issue != "" {
			problem(w, http.StatusBadRequest, issue)
			return
		}
		data, err := json.Marshal(keys)
		if err == nil {
			err = atomicWrite(s.path, data)
		}
		if err != nil {
			problem(w, http.StatusInternalServerError, "无法保存快捷键，请检查本机数据目录的写入权限。")
			return
		}
		s.keys = keys
		respond(w, http.StatusOK, keys)
	})
}
