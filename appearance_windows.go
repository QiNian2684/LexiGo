//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

func systemAppearance() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		if os.IsNotExist(err) {
			return "light"
		}
		return ""
	}
	defer key.Close()
	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		if os.IsNotExist(err) {
			return "light"
		}
		return ""
	}
	if value == 0 {
		return "dark"
	}
	return "light"
}
