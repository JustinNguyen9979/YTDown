package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"time"
)

var (
	logger  = log.New(os.Stdout, "", 0)
	logFile *os.File
	logPath string
)

// InitLogger khởi tạo logger ghi ra cả stdout và file
func InitLogger() error {
	usr, err := user.Current()
	if err != nil {
		return nil
	}
	logDir := filepath.Join(usr.HomeDir, ".config", "ytdown", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil
	}
	logPath = filepath.Join(logDir, "ytdown.log")

	// O_TRUNC: mỗi lần mở app là tạo file log mới (không append)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil
	}
	logFile = f

	// Ghi đồng thời ra stdout VÀ file
	mw := io.MultiWriter(os.Stdout, f)
	logger = log.New(mw, "", 0)
	return nil
}

// CloseLogger đóng file log
func CloseLogger() {
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
}

// CleanupLogFile đóng và xóa file log
func CleanupLogFile() {
	CloseLogger()
	if logPath != "" {
		os.Remove(logPath)
		logPath = ""
	}
}

func formatLog(level, message string) string {
	timestamp := time.Now().Format("15:04:05")
	return fmt.Sprintf("[%s] [%s] %s", timestamp, level, message)
}

func LogInfo(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logger.Println(formatLog("INFO", msg))
}

func LogError(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logger.Println(formatLog("ERROR", msg))
}

func LogWarning(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logger.Println(formatLog("WARNING", msg))
}

var debugMode = false // hoặc đọc từ env variable

func LogDebug(format string, v ...interface{}) {
	if !debugMode {
		return // ✅ Skip hoàn toàn trong production
	}
	msg := fmt.Sprintf(format, v...)
	logger.Println(formatLog("DEBUG", msg))
}
