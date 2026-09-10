package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCommand(t *testing.T) {
	// 1. Success command
	code, out, err := RunCommand("echo", "hello")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, string(out), "hello")

	// 2. Command exiting with non-zero and output on stdout
	code, out, err = RunCommand("sh", "-c", "echo 'failed on stdout' && exit 2")
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	assert.Contains(t, string(out), "failed on stdout")

	// 3. Command exiting with non-zero and output on stderr
	code, out, err = RunCommand("sh", "-c", "echo 'failed on stderr' >&2 && exit 3")
	require.NoError(t, err)
	assert.Equal(t, 3, code)
	assert.Contains(t, string(out), "failed on stderr")
}
