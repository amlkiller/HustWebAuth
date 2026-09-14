package cmd

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePosixTZ(t *testing.T) {
	testCases := []struct {
		input          string
		expectedOffset int
		expectedName   string
		expectNil      bool
	}{
		{
			input:          "Asia/Shanghai",
			expectedOffset: 8 * 3600,
			expectedName:   "Asia/Shanghai",
		},
		{
			input:          "CST-8",
			expectedOffset: 8 * 3600,
			expectedName:   "CST",
		},
		{
			input:          "CST-8CDT",
			expectedOffset: 8 * 3600,
			expectedName:   "CST",
		},
		{
			input:          "UTC+8",
			expectedOffset: 8 * 3600,
			expectedName:   "UTC+8",
		},
		{
			input:          "GMT+8",
			expectedOffset: 8 * 3600,
			expectedName:   "GMT+8",
		},
		{
			input:          "UTC-5",
			expectedOffset: -5 * 3600,
			expectedName:   "UTC-5",
		},
		{
			input:          "EST5",
			expectedOffset: -5 * 3600,
			expectedName:   "EST",
		},
		{
			input:          "EST5EDT",
			expectedOffset: -4 * 3600, // Daylight saving time active in September
			expectedName:   "EDT",
		},
		{
			input:          "UTC",
			expectedOffset: 0,
			expectedName:   "UTC",
		},
		{
			input:     "",
			expectNil: true,
		},
		{
			input:     "   ",
			expectNil: true,
		},
		{
			input:     "12345",
			expectNil: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			loc := parsePosixTZ(tc.input)
			if tc.expectNil {
				assert.Nil(t, loc)
				return
			}
			require.NotNil(t, loc)
			now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC).In(loc)
			_, offset := now.Zone()
			assert.Equal(t, tc.expectedOffset, offset, "timezone offset mismatch for %s", tc.input)
		})
	}
}

func TestInitTimezone_FromEnv(t *testing.T) {
	origTZ := os.Getenv("TZ")
	origLocal := time.Local
	defer func() {
		_ = os.Setenv("TZ", origTZ)
		time.Local = origLocal
	}()

	_ = os.Setenv("TZ", "CST-8")
	initTimezone()

	now := time.Now()
	_, offset := now.Zone()
	assert.Equal(t, 8*3600, offset)
}

func BenchmarkParsePosixTZ(b *testing.B) {
	testCases := []string{
		"Asia/Shanghai",
		"CST-8",
		"CST-8CDT",
		"UTC+8",
		"GMT+8",
		"EST5EDT",
	}

	for _, tc := range testCases {
		b.Run(tc, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = parsePosixTZ(tc)
			}
		})
	}
}

