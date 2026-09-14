//go:build !windows && !plan9

package cmd

import (
	"io"
	"log"
	"log/syslog"
	"os"
	"path/filepath"
	"strings"
)

func initLog() {
	if isServiceControlCommand() {
		log.SetOutput(os.Stderr)
		return
	}

	logWriter := os.Stderr
	if logFile != "" {
		targetDir := logDir
		pattern := logFile
		if filepath.IsAbs(logFile) {
			targetDir = filepath.Dir(logFile)
			pattern = filepath.Base(logFile)
		} else if strings.ContainsRune(logFile, filepath.Separator) || strings.ContainsRune(logFile, '/') || strings.ContainsRune(logFile, '\\') {
			fullPath := filepath.Join(logDir, logFile)
			targetDir = filepath.Dir(fullPath)
			pattern = filepath.Base(fullPath)
		}

		var err error
		if err = os.MkdirAll(targetDir, 0755); err != nil {
			log.Fatal("Create log dir failed, err:", err)
		}
		if logRandom {
			logWriter, err = os.CreateTemp(targetDir, pattern)
		} else if logAppend {
			logWriter, err = os.OpenFile(filepath.Join(targetDir, pattern), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		} else {
			logWriter, err = os.OpenFile(filepath.Join(targetDir, pattern), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		}
		if err != nil {
			log.Fatal("Open log file failed, err:", err)
		}
		log.Println("Log file:", logWriter.Name())
	}

	if sysType != "windows" && sysLog {
		var err error
		sysLogWriter, err := syslog.New(syslog.LOG_INFO, "HustWebAuth")
		if err != nil {
			log.Fatal(err)
		}
		log.SetOutput(io.MultiWriter(logWriter, sysLogWriter))
	} else {
		log.SetOutput(logWriter)
	}
}
