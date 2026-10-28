package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitLoggerAtPathIsIdempotent(t *testing.T) {
	oldLogger, oldLogFile, oldLogPath := logger, logFile, logPath
	logger = log.New(os.Stdout, "", 0)
	logFile = nil
	logPath = ""
	logFilePath := filepath.Join(t.TempDir(), "ytdown.log")
	t.Cleanup(func() {
		CloseLogger()
		logger = oldLogger
		logFile = oldLogFile
		logPath = oldLogPath
	})

	if err := initLoggerAtPath(logFilePath); err != nil {
		t.Fatalf("first initLoggerAtPath() error = %v", err)
	}
	LogInfo("first startup marker")
	if err := initLoggerAtPath(logFilePath); err != nil {
		t.Fatalf("second initLoggerAtPath() error = %v", err)
	}
	LogInfo("second startup marker")

	assertLogContains(t, logFilePath, "first startup marker", "second startup marker")
}

func TestInitLoggerAppendsExistingSessionLog(t *testing.T) {
	oldLogger, oldLogFile, oldLogPath := logger, logFile, logPath
	logger = log.New(os.Stdout, "", 0)
	logFile = nil
	logPath = ""
	logFilePath := filepath.Join(t.TempDir(), "ytdown.log")
	t.Cleanup(func() {
		CloseLogger()
		logger = oldLogger
		logFile = oldLogFile
		logPath = oldLogPath
	})

	if err := os.WriteFile(logFilePath, []byte("previous session marker\\n"), 0o600); err != nil {
		t.Fatalf("seed existing log: %v", err)
	}
	if err := initLoggerAtPath(logFilePath); err != nil {
		t.Fatalf("initLoggerAtPath() error = %v", err)
	}
	LogInfo("current session marker")
	assertLogContains(t, logFilePath, "previous session marker", "current session marker")
}

func TestSanitizeLogMessageRedactsJSONSecrets(t *testing.T) {
	message := `{"web_session":"json-session-secret","manual_header":"Cookie: json-cookie-secret","access_token":"json-token-secret"}`
	got := sanitizeLogMessage(message)
	for _, secret := range []string{"json-session-secret", "json-cookie-secret", "json-token-secret"} {
		if strings.Contains(got, secret) {
			t.Errorf("sanitizeLogMessage() leaked %q: %q", secret, got)
		}
	}
}

func TestCleanupLogFileClosesButPreservesLog(t *testing.T) {
	oldLogger, oldLogFile, oldLogPath := logger, logFile, logPath
	logger = log.New(os.Stdout, "", 0)
	logFile = nil
	logPath = ""
	logFilePath := filepath.Join(t.TempDir(), "ytdown.log")
	t.Cleanup(func() {
		CloseLogger()
		logger = oldLogger
		logFile = oldLogFile
		logPath = oldLogPath
	})

	if err := initLoggerAtPath(logFilePath); err != nil {
		t.Fatalf("initLoggerAtPath() error = %v", err)
	}
	LogInfo("post-shutdown diagnostic marker")
	CleanupLogFile()

	assertLogContains(t, logFilePath, "post-shutdown diagnostic marker")
}

func TestLogTimestampUsesVietnamTime(t *testing.T) {
	location, err := vietnamTimeLocation()
	if err != nil {
		t.Fatalf("vietnamTimeLocation() error = %v", err)
	}
	if location.String() != "Asia/Ho_Chi_Minh" {
		t.Fatalf("Vietnam location = %q, want Asia/Ho_Chi_Minh", location)
	}
	got := formatLogAt(time.Date(2026, time.October, 29, 1, 2, 3, 0, time.UTC), location, "INFO", "marker")
	if !strings.HasPrefix(got, "[2026-10-29 08:02:03 +0700 +07] [INFO]") {
		t.Fatalf("formatLogAt() = %q, want Vietnam local timestamp", got)
	}
}

func TestSanitizeLogMessageRedactsCookieAndURLSecrets(t *testing.T) {
	message := "yt-dlp failed: https://example.test/watch?token=token-secret web_session=session-secret\t.xiaohongshu.com\tTRUE\t/\tTRUE\t2147483647\tweb_session\tnetscape-secret"
	got := sanitizeLogMessage(message)
	for _, secret := range []string{"token-secret", "session-secret", "netscape-secret"} {
		if strings.Contains(got, secret) {
			t.Errorf("sanitizeLogMessage() leaked %q: %q", secret, got)
		}
	}
	for _, marker := range []string{"[REDACTED]", "watch?token="} {
		if !strings.Contains(got, marker) {
			t.Errorf("sanitizeLogMessage() = %q, missing %q", got, marker)
		}
	}
}

func TestClearLogFileTruncatesAndKeepsLoggerWritable(t *testing.T) {
	oldLogger, oldLogFile, oldLogPath := logger, logFile, logPath
	logger = log.New(os.Stdout, "", 0)
	logFile = nil
	logPath = ""
	logFilePath := filepath.Join(t.TempDir(), "ytdown.log")
	t.Cleanup(func() {
		CloseLogger()
		logger = oldLogger
		logFile = oldLogFile
		logPath = oldLogPath
	})

	if err := initLoggerAtPath(logFilePath); err != nil {
		t.Fatalf("initLoggerAtPath() error = %v", err)
	}
	LogInfo("remove this marker")
	if err := ClearLogFile(); err != nil {
		t.Fatalf("ClearLogFile() error = %v", err)
	}
	LogInfo("write after clear")
	assertLogContains(t, logFilePath, "write after clear")
	contents, err := os.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("read cleared log: %v", err)
	}
	if strings.Contains(string(contents), "remove this marker") {
		t.Fatalf("ClearLogFile() retained old entry: %q", contents)
	}
}

func TestFinalizeLoggerPreservesOrDeletesLog(t *testing.T) {
	for _, clearOnExit := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserve", true: "delete"}[clearOnExit], func(t *testing.T) {
			oldLogger, oldLogFile, oldLogPath := logger, logFile, logPath
			logger = log.New(os.Stdout, "", 0)
			logFile = nil
			logPath = ""
			logFilePath := filepath.Join(t.TempDir(), "ytdown.log")
			t.Cleanup(func() {
				CloseLogger()
				logger = oldLogger
				logFile = oldLogFile
				logPath = oldLogPath
			})

			if err := initLoggerAtPath(logFilePath); err != nil {
				t.Fatalf("initLoggerAtPath() error = %v", err)
			}
			LogInfo("shutdown marker")
			if err := FinalizeLogger(clearOnExit); err != nil {
				t.Fatalf("FinalizeLogger() error = %v", err)
			}
			_, err := os.Stat(logFilePath)
			if clearOnExit && !os.IsNotExist(err) {
				t.Fatalf("FinalizeLogger(true) should remove log, stat error = %v", err)
			}
			if !clearOnExit {
				if err != nil {
					t.Fatalf("FinalizeLogger(false) should preserve log: %v", err)
				}
				assertLogContains(t, logFilePath, "shutdown marker")
			}
		})
	}
}

func assertLogContains(t *testing.T, path string, markers ...string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read logger output: %v", err)
	}
	for _, marker := range markers {
		if !strings.Contains(string(contents), marker) {
			t.Errorf("logger output %q is missing %q", contents, marker)
		}
	}
}
