//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32    = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx  = kernel32.NewProc("LockFileEx")
	moveFileEx  = kernel32.NewProc("MoveFileExW")
	messageBox  = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	findWindow  = syscall.NewLazyDLL("user32.dll").NewProc("FindWindowW")
	showWindow  = syscall.NewLazyDLL("user32.dll").NewProc("ShowWindowAsync")
	focusWindow = syscall.NewLazyDLL("user32.dll").NewProc("SetForegroundWindow")
)

func legacyDataDirectory() (string, error) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir != "" {
		return filepath.Join(dir, "WordReview"), nil
	}
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "WordReview"), err
}

func tryLock(file *os.File) (bool, error) {
	const failImmediately, exclusive = 1, 2
	var overlap syscall.Overlapped
	r, _, err := lockFileEx.Call(file.Fd(), failImmediately|exclusive, 0, 1, 0, uintptr(unsafe.Pointer(&overlap)))
	if r != 0 {
		return true, nil
	}
	if errors.Is(err, syscall.Errno(33)) {
		return false, nil
	}
	return false, err
}

func replaceFile(source, target string) error {
	return moveWindowsFile(source, target, 1|8) // Replace existing, write through.
}

func publishFile(source, target string) error {
	return moveWindowsFile(source, target, 8) // Write through without replacement.
}

func moveWindowsFile(source, target string, flags uintptr) error {
	from, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r, _, err := moveFileEx.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), flags)
	if r == 0 {
		return err
	}
	return nil
}

func openWindow(link string) error {
	title, _ := syscall.UTF16PtrFromString("背词本")
	hwnd, _, _ := findWindow.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd != 0 {
		showWindow.Call(hwnd, 9)
		focusWindow.Call(hwnd)
	}
	return nil
}

func reportStartupError(err error) {
	msg, _ := syscall.UTF16PtrFromString(err.Error())
	title, _ := syscall.UTF16PtrFromString("背词本")
	messageBox.Call(0, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(title)), 0x10)
}
