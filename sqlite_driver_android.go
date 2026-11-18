//go:build android

package main

// Use Android's libc entry points for SQLite's file operations.
import _ "github.com/mattn/go-sqlite3"

const sqliteDriverName = "sqlite3"
