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
