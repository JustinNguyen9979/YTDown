package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "time/tzdata"
)

var (
	logger  = log.New(os.Stdout, "", 0)
	logFile *os.File
	logPath string
	logMu   sync.Mutex
)

const vietnamTimeZone = "Asia/Ho_Chi_Minh"

var logRedactors = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(["'](?:web_session|cookie|set-cookie|authorization|manual_header|access_token|xsec_token|xsec_source|token|auth|signature|sign|session)["']\s*:\s*")([^"]*)(")`),
	regexp.MustCompile(`(?i)(["']?web_session["']?\s*[:=]\s*["']?)([^"'\s,}]+)`),
	regexp.MustCompile(`(?i)(["']?(?:cookie|set-cookie|authorization|manual_header|access_token|xsec_token|xsec_source|token|auth|signature|sign|session)["']?\s*[:=]\s*["']?)([^"'\s,}]+)`),
	regexp.MustCompile(`(?i)([?&](?:xsec_token|xsec_source|token|access_token|auth|signature|sign|session|web_session)=)[^&#\s]+`),
	regexp.MustCompile(`(?i)(\b(?:cookie|set-cookie|authorization):\s*)[^\r\n]*`),
	regexp.MustCompile(`(?im)((?:^|\t)[^\t]+\t(?:TRUE|FALSE)\t[^\t]*\t(?:TRUE|FALSE)\t\d+\t[^\t]+\t)[^\t\r\n]+`),
}

func sanitizeLogMessage(message string) string {
	for _, redactor := range logRedactors {
		message = redactor.ReplaceAllString(message, "${1}[REDACTED]")
	}
	return message
}

func sanitizeDownloadErrorForLog(message string) string {
	return sanitizeLogMessage(message)
}

func vietnamTimeLocation() (*time.Location, error) {
	return time.LoadLocation(vietnamTimeZone)
}

func formatLogAt(at time.Time, location *time.Location, level, message string) string {
	return fmt.Sprintf("[%s] [%s] %s", at.In(location).Format("2006-01-02 15:04:05 -0700 MST"), level, sanitizeLogMessage(message))
}

func writeLog(level, message string) {
	logMu.Lock()
	defer logMu.Unlock()
	location, err := vietnamTimeLocation()
	if err != nil {
		location = time.FixedZone("ICT", 7*60*60)
	}
	logger.Println(formatLogAt(time.Now(), location, level, message))
}

// LogFrontendMessage receives redacted browser-console entries from the frontend.
func writeFrontendLog(level, message string) {
	level = strings.ToUpper(strings.TrimSpace(level))
	switch level {
	case "ERROR", "WARN", "WARNING", "INFO", "DEBUG", "LOG":
		writeLog("FE/"+level, message)
	default:
		writeLog("FE/LOG", message)
	}
}

// LogProcessOutput stores one redacted line emitted by a child process.
func LogProcessOutput(process, line string) {
	writeLog("PROC/"+process, line)
}

func LogDownloadError(format string, v ...interface{}) {
	writeLog("ERROR", "[DL] "+fmt.Sprintf(format, v...))
}

// InitLogger initializes file and stdout logging.
func InitLogger() error {
	usr, err := user.Current()
	if err != nil {
		return fmt.Errorf("resolve current user for log directory: %w", err)
	}
	logDir := filepath.Join(usr.HomeDir, ".config", "ytdown", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	return initLoggerAtPath(filepath.Join(logDir, "ytdown.log"))
}

func initLoggerAtPath(path string) error {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	logPath = path
	logFile = f
	logger = log.New(io.MultiWriter(os.Stdout, f), "", 0)
	return nil
}

// ClearLogFile truncates the log and leaves logging active.
func ClearLogFile() error {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile == nil {
		if logPath == "" {
			return fmt.Errorf("log file is not initialized")
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("clear log file: %w", err)
		}
		logFile = f
		logger = log.New(io.MultiWriter(os.Stdout, f), "", 0)
		return nil
	}
	if err := logFile.Truncate(0); err != nil {
		return fmt.Errorf("truncate log file: %w", err)
	}
	if _, err := logFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek log file: %w", err)
	}
	return nil
}

// FinalizeLogger closes the log and removes it only when requested.
func FinalizeLogger(clearOnExit bool) error {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		if err := logFile.Close(); err != nil {
			logFile = nil
			return fmt.Errorf("close log file: %w", err)
		}
		logFile = nil
		logger = log.New(os.Stdout, "", 0)
	}
	if clearOnExit && logPath != "" {
		if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove log file: %w", err)
		}
	}
	return nil
}

// CloseLogger closes the file logger while preserving its contents.
func CloseLogger() {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
		logger = log.New(os.Stdout, "", 0)
	}
}

// CleanupLogFile closes the file logger while preserving it for diagnosis.
func CleanupLogFile() {
	CloseLogger()
}

func formatLog(level, message string) string {
	location, err := vietnamTimeLocation()
	if err != nil {
		location = time.FixedZone("ICT", 7*60*60)
	}
	return formatLogAt(time.Now(), location, level, message)
}

func LogInfo(format string, v ...interface{}) {
	writeLog("INFO", fmt.Sprintf(format, v...))
}

func LogError(format string, v ...interface{}) {
	writeLog("ERROR", fmt.Sprintf(format, v...))
}

func LogWarning(format string, v ...interface{}) {
	writeLog("WARNING", fmt.Sprintf(format, v...))
}

func LogDebug(format string, v ...interface{}) {
	writeLog("DEBUG", fmt.Sprintf(format, v...))
}
