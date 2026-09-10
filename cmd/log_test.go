package cmd

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitLog_Truncate(t *testing.T) {
	origLogDir := logDir
	origLogFile := logFile
	origLogRandom := logRandom
	origLogAppend := logAppend
	defer func() {
		logDir = origLogDir
		logFile = origLogFile
		logRandom = origLogRandom
		logAppend = origLogAppend
		log.SetOutput(os.Stderr)
	}()

	tempDir := t.TempDir()
	logDir = tempDir
	logFile = "test.log"
	logRandom = false
	logAppend = false

	// Write initial dummy data that is long
	filePath := filepath.Join(tempDir, logFile)
	err := os.WriteFile(filePath, []byte("old initial content that is very long and should be truncated"), 0644)
	require.NoError(t, err)

	initLog()
	log.Print("new")

	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "old initial content")
	assert.Contains(t, string(content), "new")
}

func TestInitLog_AbsolutePathRandom(t *testing.T) {
	origLogDir := logDir
	origLogFile := logFile
	origLogRandom := logRandom
	origLogAppend := logAppend
	defer func() {
		logDir = origLogDir
		logFile = origLogFile
		logRandom = origLogRandom
		logAppend = origLogAppend
		log.SetOutput(os.Stderr)
	}()

	tempDir := t.TempDir()
	logDir = "/some/other/unused/dir"
	logFile = filepath.Join(tempDir, "abs_test.log")
	logRandom = true

	initLog()
	log.Print("hello abs random")

	// Find files in tempDir
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	found := false
	for _, e := range entries {
		if !e.IsDir() {
			data, err := os.ReadFile(filepath.Join(tempDir, e.Name()))
			require.NoError(t, err)
			if string(data) != "" {
				assert.Contains(t, string(data), "hello abs random")
				found = true
				break
			}
		}
	}
	assert.True(t, found, "expected log file created in absolute path directory")
}

func TestInitLog_SubdirRelativeRandom(t *testing.T) {
	origLogDir := logDir
	origLogFile := logFile
	origLogRandom := logRandom
	origLogAppend := logAppend
	defer func() {
		logDir = origLogDir
		logFile = origLogFile
		logRandom = origLogRandom
		logAppend = origLogAppend
		log.SetOutput(os.Stderr)
	}()

	tempDir := t.TempDir()
	logDir = tempDir
	logFile = filepath.Join("subdir", "nested_test.log")
	logRandom = true

	initLog()
	log.Print("hello nested random")

	nestedDir := filepath.Join(tempDir, "subdir")
	entries, err := os.ReadDir(nestedDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	found := false
	for _, e := range entries {
		if !e.IsDir() {
			data, err := os.ReadFile(filepath.Join(nestedDir, e.Name()))
			require.NoError(t, err)
			if string(data) != "" {
				assert.Contains(t, string(data), "hello nested random")
				found = true
				break
			}
		}
	}
	assert.True(t, found, "expected log file created in nested directory")
}

func TestInitLog_AbsolutePathNoRandom(t *testing.T) {
	origLogDir := logDir
	origLogFile := logFile
	origLogRandom := logRandom
	origLogAppend := logAppend
	defer func() {
		logDir = origLogDir
		logFile = origLogFile
		logRandom = origLogRandom
		logAppend = origLogAppend
		log.SetOutput(os.Stderr)
	}()

	tempDir := t.TempDir()
	absPath := filepath.Join(tempDir, "abs_fixed.log")
	logFile = absPath
	logRandom = false
	logAppend = true

	initLog()
	log.Print("hello abs fixed")

	data, err := os.ReadFile(absPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "hello abs fixed")
}

