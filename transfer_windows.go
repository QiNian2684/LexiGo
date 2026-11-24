//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var transferDialog sync.Mutex

type openFilename struct {
	size                    uint32
	owner, instance         uintptr
	filter, customFilter    *uint16
	maxCustom, filterIndex  uint32
	file                    *uint16
	maxFile                 uint32
	fileTitle               *uint16
	maxFileTitle            uint32
	initialDirectory, title *uint16
	flags                   uint32
	fileOffset, fileExt     uint16
	defaultExt              *uint16
	customData, hook        uintptr
	templateName            *uint16
	reserved                uintptr
	reservedFlags, flagsEx  uint32
}

func chooseDataFile(save bool, suggested, extension string) (string, error) {
	transferDialog.Lock()
	defer transferDialog.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	dialog := syscall.NewLazyDLL("comdlg32.dll")
	procedure, title := "GetOpenFileNameW", "导入完整学习数据"
	description := "LexiGo 学习数据"
	flags := uint32(0x00080000 | 0x00000800 | 0x00000008) // Explorer, existing path, unchanged working directory.
	file := make([]uint16, 32768)
	if save {
		procedure, title = "GetSaveFileNameW", "导出完整学习数据"
		if extension == ".csv" {
			title, description = "导出词库 CSV", "CSV 词库"
		}
		flags |= 0x00000002 // Confirm overwriting.
		copy(file, utf16.Encode([]rune(suggested)))
	} else {
		flags |= 0x00001000 // Existing file.
	}
	filter := utf16.Encode([]rune(description + " (*" + extension + ")\x00*" + extension + "\x00\x00"))
	name, _ := syscall.UTF16PtrFromString(title)
	var ext *uint16
	if save {
		ext, _ = syscall.UTF16PtrFromString(extension[1:])
	}
	foreground := syscall.NewLazyDLL("user32.dll").NewProc("GetForegroundWindow")
	owner, _, _ := foreground.Call()
	options := openFilename{
		owner: owner, filter: &filter[0], filterIndex: 1,
		file: &file[0], maxFile: uint32(len(file)), title: name,
		flags: flags, defaultExt: ext,
	}
	options.size = uint32(unsafe.Sizeof(options))
	ok, _, _ := dialog.NewProc(procedure).Call(uintptr(unsafe.Pointer(&options)))
	runtime.KeepAlive(options)
	if ok == 0 {
		code, _, _ := dialog.NewProc("CommDlgExtendedError").Call()
		if code != 0 {
			return "", fmt.Errorf("Windows 文件对话框失败（%#x）。", code)
		}
		return "", nil
	}
	path := syscall.UTF16ToString(file)
	if !strings.EqualFold(filepath.Ext(path), extension) {
		return "", fmt.Errorf("请选择扩展名为 %s 的文件。", extension)
	}
	return path, nil
}
