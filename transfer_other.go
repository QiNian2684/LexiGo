//go:build !windows

package main

import "errors"

func chooseDataFile(bool, string, string) (string, error) {
	return "", errors.New("请在当前应用中使用系统文件选择入口。")
}
