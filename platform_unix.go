//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func legacyDataDirectory() (string, error) { return "", nil }

func tryLock(file *os.File) (bool, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

func replaceFile(source, target string) error { return os.Rename(source, target) }
func publishFile(source, target string) error { return os.Link(source, target) }

func openWindow(url string) error {
	cmd := exec.Command("xdg-open", url)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func startWindow(url, _ string) (<-chan struct{}, func(), error) { return nil, nil, openWindow(url) }

func reportStartupError(err error) { fmt.Fprintln(os.Stderr, err) }
