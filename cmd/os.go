package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"

	"github.com/AdguardTeam/golibs/mathutil"
)

// MaxCmdOutputSize is the maximum length of performed shell command output in
// bytes.
const MaxCmdOutputSize = 64 * 1024

// RunCommand runs shell command.
func RunCommand(command string, arguments ...string) (code int, output []byte, err error) {
	cmd := exec.Command(command, arguments...)
	out, err := cmd.Output()

	out = out[:mathutil.Min(len(out), MaxCmdOutputSize)]

	if err != nil {
		if eerr := new(exec.ExitError); errors.As(err, &eerr) {
			outBytes := eerr.Stderr
			if len(outBytes) == 0 {
				outBytes = out
			}
			outBytes = outBytes[:mathutil.Min(len(outBytes), MaxCmdOutputSize)]
			return eerr.ExitCode(), outBytes, nil
		}

		return 1, nil, fmt.Errorf("command %q failed: %w: %s", command, err, out)
	}

	return cmd.ProcessState.ExitCode(), out, nil
}

var isOpenWrtFunc = isOpenWrt

// IsOpenWrt returns true if host OS is OpenWrt.
func IsOpenWrt() (ok bool) {
	return isOpenWrtFunc()
}

// RootDirFS returns the [fs.FS] rooted at the operating system's root.  On
// Windows it returns the fs.FS rooted at the volume of the system directory
// (usually, C:).
func RootDirFS() (fsys fs.FS) {
	return rootDirFS()
}

// isServiceControlCommand checks if the current process was invoked
// for a transient service management action (install, start, stop, etc.)
func isServiceControlCommand() bool {
	if len(os.Args) < 2 {
		return false
	}
	for i, arg := range os.Args {
		if arg == "service" && i+1 < len(os.Args) {
			action := os.Args[i+1]
			switch action {
			case "start", "stop", "restart", "status", "install", "uninstall":
				return true
			}
		}
	}
	return false
}

