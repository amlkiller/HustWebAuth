package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

var (
	reTZInformal = regexp.MustCompile(`^(?i)(UTC|GMT)\s*([+-])\s*(\d{1,2})(?::(\d{1,2}))?$`)
	reTZPosix    = regexp.MustCompile(`^([A-Za-z]{3,})([+-]?\d{1,2})(?::(\d{1,2}))?(?::(\d{1,2}))?`)
)

// parsePosixTZ parses a POSIX-style timezone string (e.g. "CST-8", "EST5EDT", "GMT-8", "UTC+8")
// or standard IANA location name (e.g. "Asia/Shanghai") and returns a *time.Location.
func parsePosixTZ(tz string) *time.Location {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return nil
	}

	// 1. Try standard LoadLocation first (works for IANA names like "Asia/Shanghai" thanks to embedded tzdata)
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}

	// 2. Handle informal "UTC+8", "UTC-8", "GMT+8", "GMT-8"
	if m := reTZInformal.FindStringSubmatch(tz); len(m) > 0 {
		name := strings.ToUpper(m[1])
		sign := m[2]
		hours, _ := strconv.Atoi(m[3])
		mins := 0
		if m[4] != "" {
			mins, _ = strconv.Atoi(m[4])
		}
		offsetSec := hours*3600 + mins*60
		if sign == "-" {
			offsetSec = -offsetSec
		}
		return time.FixedZone(fmt.Sprintf("%s%s%d", name, sign, hours), offsetSec)
	}

	// 3. POSIX standard: <name><offset>[<dst>[<offset>]...]
	// In POSIX: offset specifies the time you add to local time to get UTC.
	// Therefore: Local + offset = UTC  =>  Local = UTC - offset.
	// For instance, "CST-8" means offset is -8 hours, so Local is UTC - (-8h) = UTC + 8 hours (+28800s).
	// "EST5EDT" means offset is +5 hours, so Local is UTC - 5h (-18000s).
	if m := reTZPosix.FindStringSubmatch(tz); len(m) > 0 {
		name := m[1]
		hours, _ := strconv.Atoi(m[2])
		mins := 0
		if m[3] != "" {
			mins, _ = strconv.Atoi(m[3])
		}
		offsetSec := -(hours*3600 + mins*60)
		return time.FixedZone(name, offsetSec)
	}

	return nil
}

// detectSystemTimezone attempts to discover the host system's timezone name or POSIX string.
func detectSystemTimezone() string {
	// 1. Check TZ environment variable
	if tz := strings.TrimSpace(os.Getenv("TZ")); tz != "" {
		return tz
	}

	// 2. Check /etc/localtime symlink target
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		target = filepath.ToSlash(target)
		if idx := strings.Index(target, "zoneinfo/"); idx != -1 {
			return target[idx+len("zoneinfo/"):]
		}
	}

	// 3. Check /etc/TZ (standard OpenWrt timezone file)
	if data, err := os.ReadFile("/etc/TZ"); err == nil {
		if tz := strings.TrimSpace(string(data)); tz != "" {
			return tz
		}
	}

	// 4. Check /etc/timezone (Debian, Ubuntu, etc.)
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(data)); tz != "" {
			return tz
		}
	}

	// 5. Check OpenWrt /etc/config/system for zonename or timezone
	if data, err := os.ReadFile("/etc/config/system"); err == nil {
		for _, line := range bytes.Split(data, []byte("\n")) {
			lineStr := strings.TrimSpace(string(line))
			if strings.HasPrefix(lineStr, "option zonename") {
				fields := strings.Fields(lineStr)
				if len(fields) >= 3 {
					return strings.Trim(fields[2], "'\"")
				}
			}
			if strings.HasPrefix(lineStr, "option timezone") {
				fields := strings.Fields(lineStr)
				if len(fields) >= 3 {
					return strings.Trim(fields[2], "'\"")
				}
			}
		}
	}

	return ""
}

// initTimezone initializes the local timezone for logging and scheduling.
func initTimezone() {
	if sysType == "windows" {
		return
	}

	// If TZ is set in env, parse and apply it
	if envTZ := os.Getenv("TZ"); envTZ != "" {
		if loc := parsePosixTZ(envTZ); loc != nil {
			time.Local = loc
			return
		}
	}

	// Check if /etc/localtime exists as a regular file and Go loaded a valid local timezone
	if fi, err := os.Stat("/etc/localtime"); err == nil && !fi.IsDir() {
		// If /etc/localtime exists and Go loaded it as a non-UTC timezone, retain it
		if time.Local != nil && time.Local != time.UTC {
			return
		}
	}

	// Otherwise, detect timezone from OpenWrt / Linux system files
	detected := detectSystemTimezone()
	if detected != "" {
		if loc := parsePosixTZ(detected); loc != nil {
			time.Local = loc
			_ = os.Setenv("TZ", detected)
			return
		}
	}

	// If running on OpenWrt and still defaulting to UTC, fallback to China Standard Time (UTC+8)
	if IsOpenWrt() {
		if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
			time.Local = loc
		} else {
			time.Local = time.FixedZone("CST", 8*3600)
		}
		_ = os.Setenv("TZ", "CST-8")
	}
}
