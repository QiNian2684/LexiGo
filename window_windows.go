//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	webview "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

var (
	ole32           = windows.NewLazySystemDLL("ole32.dll")
	initCOM         = ole32.NewProc("CoInitializeEx")
	uninitCOM       = ole32.NewProc("CoUninitialize")
	comctl32        = windows.NewLazySystemDLL("comctl32.dll")
	setSubclass     = comctl32.NewProc("SetWindowSubclass")
	defSubclass     = comctl32.NewProc("DefSubclassProc")
	removeSubclass  = comctl32.NewProc("RemoveWindowSubclass")
	setDWMAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	setDPIContext   = windows.NewLazySystemDLL("user32.dll").NewProc("SetProcessDpiAwarenessContext")
)

func startWindow(link, dataDir string) (<-chan struct{}, func(), error) {
	profile := filepath.Join(dataDir, "WebView2")
	if err := os.MkdirAll(profile, 0700); err != nil {
		return nil, nil, fmt.Errorf("无法创建背词本的窗口配置：%w", err)
	}
	if setDPIContext.Find() == nil {
		setDPIContext.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2.
	}

	type result struct {
		stop func()
		err  error
	}
	started := make(chan result, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		status, _, _ := initCOM.Call(0, 2) // WebView2 requires a single-threaded COM apartment.
		if int32(status) < 0 {
			started <- result{err: fmt.Errorf("无法初始化 Windows 窗口（0x%08x）。", uint32(status))}
			return
		}
		defer uninitCOM.Call()

		version, err := webviewloader.GetInstalledVersion()
		if err != nil || version == "" {
			started <- result{err: errors.New("背词本需要 Microsoft Edge WebView2 Runtime。请安装或修复 Evergreen Runtime 后重试。\nhttps://developer.microsoft.com/microsoft-edge/webview2/")}
			return
		}
		// An isolated app profile persists across launches. Nil WebView2
		// environment options leave OS-account single sign-on disabled.
		window := webview.NewWithOptions(webview.WebViewOptions{
			DataPath: profile, AutoFocus: true,
			WindowOptions: webview.WindowOptions{Title: "背词本", Width: 1200, Height: 860, IconId: 1},
		})
		if window == nil {
			started <- result{err: errors.New("无法启动背词本的 WebView2 窗口。请检查 Data\\WebView2 的写入权限并重试。")}
			return
		}
		if err := installWindowCallbacks(window); err != nil {
			window.Destroy()
			started <- result{err: err}
			return
		}
		window.Navigate(link)
		started <- result{stop: func() {
			select {
			case <-done:
			default:
				window.Destroy()
			}
		}}
		window.Run()
	}()
	outcome := <-started
	return done, outcome.stop, outcome.err
}

func installWindowCallbacks(window webview.WebView) error {
	const closeMessage, ncDestroyMessage = 0x0010, 0x0082
	const subclassID = 1
	hwnd := uintptr(window.Window())
	var ready, saving, mayClose bool
	var callback uintptr
	callback = windows.NewCallback(func(handle, message, wparam, lparam, id, reference uintptr) uintptr {
		if message == closeMessage && ready && !mayClose {
			if !saving {
				saving = true
				window.Eval("window.wordReviewPrepareClose()")
			}
			return 0
		}
		if message == ncDestroyMessage {
			removeSubclass.Call(handle, callback, id)
		}
		result, _, _ := defSubclass.Call(handle, message, wparam, lparam)
		return result
	})
	if ok, _, err := setSubclass.Call(hwnd, callback, subclassID, 0); ok == 0 {
		return fmt.Errorf("无法初始化背词本的关闭保存回调：%w", err)
	}
	if err := window.Bind("wordReviewWindowReady", func() { ready = true }); err != nil {
		return err
	}
	if err := window.Bind("wordReviewFinishClose", func(saved bool) {
		if saving {
			saving = false
			if saved {
				mayClose = true
				window.Destroy()
			}
		}
	}); err != nil {
		return err
	}
	return window.Bind("wordReviewWindowAppearance", func(dark bool) {
		var enabled int32
		if dark {
			enabled = 1
		}
		if status, _, _ := setDWMAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&enabled)), 4); status != 0 {
			setDWMAttribute.Call(hwnd, 19, uintptr(unsafe.Pointer(&enabled)), 4)
		}
	})
}
