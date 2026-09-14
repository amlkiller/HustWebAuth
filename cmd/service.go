package cmd

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type program struct {
	// cmd  *cobra.Command
	// args []string
}

var customServiceName string

func newSVCConfig() *service.Config {
	var logOutput = false
	if logFile != "" {
		logOutput = true
	}

	name := customServiceName
	if name == "" {
		if iface != "" {
			name = "HustWebAuth_" + iface
		} else {
			name = "HustWebAuth"
		}
	}

	args := []string{"service", "run"}
	if cfgFile != "" {
		args = append(args, "-f", cfgFile)
	}
	if iface != "" {
		args = append(args, "-i", iface)
	}
	if customServiceName != "" {
		args = append(args, "--name", customServiceName)
	}
	if cycleEnable {
		args = append(args, "-c")
	}

	envVars := map[string]string{"HOME": homeDir}
	if tz := os.Getenv("TZ"); tz != "" {
		envVars["TZ"] = tz
	} else if time.Local != nil && time.Local != time.UTC {
		envVars["TZ"] = time.Local.String()
	}

	c := &service.Config{
		Name:        name,
		DisplayName: name,
		Description: "A service used to implement Ruijie web authentication.",
		Arguments:   args,
		EnvVars:     envVars,
		Option:      service.KeyValue{"LogOutput": logOutput, "LogDirectory": logDir},
	}

	// Start only once network is up on Linux/systemd.
	if sysType == "linux" {
		c.Dependencies = []string{
			"After=syslog.target network.target",
		}
	}

	// Use different scripts on OpenWrt and Linux sysv.
	if IsOpenWrt() {
		c.Option["SysvScript"] = openWrtScript
	} else if sysType == "linux" {
		c.Option["SysvScript"] = linuxSysvScript
	}

	return c
}

func newSVC(prg *program, conf *service.Config) (service.Service, error) {
	s, err := service.New(prg, conf)
	if err != nil {
		// log.Fatal(err)
		return nil, err
	}
	return s, nil
}

// serviceCmd represents the service command
var (
	serviceCmd = &cobra.Command{
		Use:   "service",
		Short: "System service related commands",
		Long:  `Use HustWebAuth as a system service: install, start, stop, uninstall, etc.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !service.Interactive() {
				if !rootCmd.PersistentFlags().Lookup("cycle").Changed && !viper.IsSet("cycle.enable") {
					cycleEnable = true
				}
				s, err := newSVC(&program{}, newSVCConfig())
				if err != nil {
					return err
				}
				return s.Run()
			}
			return cmd.Help()
		},
	}

	runCmd = &cobra.Command{
		Use:    "run",
		Short:  "Run HustWebAuth service worker (used by service manager)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !rootCmd.PersistentFlags().Lookup("cycle").Changed && !viper.IsSet("cycle.enable") {
				cycleEnable = true
			}
			s, err := newSVC(&program{}, newSVCConfig())
			if err != nil {
				return err
			}
			return s.Run()
		},
	}

	installCmd = &cobra.Command{
		Use:   "install",
		Short: "Install HustWebAuth service",
		Run: func(cmd *cobra.Command, args []string) {
			svcConfig := newSVCConfig()

			s, err := newSVC(&program{}, svcConfig)
			if err != nil {
				log.Fatal(err)
				return
			}

			err = svcAction(s, "install")
			if err != nil {
				log.Fatal(err)
				return
			}
			if IsOpenWrt() {
				// On OpenWrt it is important to run enable after the service
				// installation.  Otherwise, the service won't start on the system
				// startup.
				_, err = runInitdCommand(s.String(), "enable")
				if err != nil {
					log.Fatalf("service: running init enable: %s", err)
				}
			}
			log.Println("HustWebAuth service has been installed")

			// Save configuration to disk BEFORE starting the service if not already existing,
			// to avoid race conditions where the newly launched service reads a nonexistent file.
			if _, err := os.Stat(cfgFile); os.IsNotExist(err) || saveCfg {
				saveCfg = true
				saveConfig()
			}

			err = svcAction(s, "start")
			if err != nil {
				log.Fatal(err)
				return
			}
			log.Println("HustWebAuth service started.")
			if IsOpenWrt() || sysType == "linux" {
				log.Printf("Service stdout log: %s\n", filepath.Join(logDir, s.String()+".log"))
				log.Printf("Service stderr log: %s\n", filepath.Join(logDir, s.String()+".err"))
			}
		},
	}

	startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start HustWebAuth service",
		Run: func(cmd *cobra.Command, args []string) {
			s, err := newSVC(&program{}, newSVCConfig())
			if err != nil {
				log.Fatal(err)
				return
			}

			if IsOpenWrt() {
				confPath := "/etc/init.d/" + s.String()
				if _, err := os.Stat(confPath); os.IsNotExist(err) {
					log.Fatalf("HustWebAuth service is not installed (script %s not found). Please run '%s service install' first.", confPath, filenameWithSuffix)
					return
				}
			}

			err = svcAction(s, "start")
			if err != nil {
				log.Fatal(err)
				return
			}
			log.Println("HustWebAuth service started.")
			if IsOpenWrt() || sysType == "linux" {
				log.Printf("Service stdout log: %s\n", filepath.Join(logDir, s.String()+".log"))
				log.Printf("Service stderr log: %s\n", filepath.Join(logDir, s.String()+".err"))
			}
		},
	}

	statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Get HustWebAuth service status",
		Run: func(cmd *cobra.Command, args []string) {
			s, err := newSVC(&program{}, newSVCConfig())
			if err != nil {
				log.Fatal(err)
				return
			}

			status, err := svcStatus(s)
			if err != nil {
				log.Fatal(err)
				return
			}
			switch status {
			case service.StatusUnknown:
				log.Println("HustWebAuth service status is unable to be determined due to an error or it was not installed.")
			case service.StatusStopped:
				log.Println("HustWebAuth service is stopped.")
			case service.StatusRunning:
				pid := getServicePID(s.String())
				if pid != "" {
					log.Printf("HustWebAuth service is running (PID: %s).\n", pid)
				} else {
					log.Println("HustWebAuth service is running.")
				}
			}

			printRecentServiceLogs(s.String())
		},
	}

	stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop HustWebAuth service",
		Run: func(cmd *cobra.Command, args []string) {

			s, err := newSVC(&program{}, newSVCConfig())
			if err != nil {
				log.Fatal(err)
				return
			}

			if IsOpenWrt() {
				confPath := "/etc/init.d/" + s.String()
				if _, err := os.Stat(confPath); os.IsNotExist(err) {
					log.Fatalf("HustWebAuth service is not installed (script %s not found).", confPath)
					return
				}
			}

			err = svcAction(s, "stop")
			if err != nil {
				log.Fatal(err)
				return
			}
			log.Println("HustWebAuth service stopped.")
		},
	}

	restartCmd = &cobra.Command{
		Use:   "restart",
		Short: "Restart HustWebAuth service",
		Run: func(cmd *cobra.Command, args []string) {
			s, err := newSVC(&program{}, newSVCConfig())
			if err != nil {
				log.Fatal(err)
				return
			}

			if IsOpenWrt() {
				confPath := "/etc/init.d/" + s.String()
				if _, err := os.Stat(confPath); os.IsNotExist(err) {
					log.Fatalf("HustWebAuth service is not installed (script %s not found). Please run '%s service install' first.", confPath, filenameWithSuffix)
					return
				}
			}

			err = svcAction(s, "restart")
			if err != nil {
				log.Fatal(err)
				return
			}
			log.Println("HustWebAuth service has been restarted.")
			if IsOpenWrt() || sysType == "linux" {
				log.Printf("Service stdout log: %s\n", filepath.Join(logDir, s.String()+".log"))
				log.Printf("Service stderr log: %s\n", filepath.Join(logDir, s.String()+".err"))
			}
		},
	}

	uninstallCmd = &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall HustWebAuth service from system",
		Run: func(cmd *cobra.Command, args []string) {
			s, err := newSVC(&program{}, newSVCConfig())
			if err != nil {
				log.Fatal(err)
				return
			}

			if IsOpenWrt() {
				// On OpenWrt it is important to run disable command first
				// as it will remove the symlink
				_, err := runInitdCommand(s.String(), "disable")
				if err != nil {
					log.Fatalf("service: running init disable: %s", err)
				}
			}

			status, err := svcStatus(s)
			if err != nil {
				log.Fatal(err)
				return
			}
			if status == service.StatusRunning {
				err = svcAction(s, "stop")
				if err != nil {
					log.Println(err)
				}
			}

			err = svcAction(s, "uninstall")
			if err != nil {
				log.Fatal(err)
				return
			}
			log.Println("HustWebAuth service has been uninstalled")
		},
	}
)

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.PersistentFlags().StringVar(&customServiceName, "name", "", "Custom service name (default HustWebAuth or HustWebAuth_<iface>)")
	serviceCmd.AddCommand(installCmd, startCmd, statusCmd, stopCmd, restartCmd, uninstallCmd, runCmd)
}

// runInitdCommand runs init.d service command
// returns command code or error if any
func runInitdCommand(serviceName, action string) (int, error) {
	confPath := "/etc/init.d/" + serviceName
	// Pass the script and action as a single string argument.
	code, out, err := RunCommand("sh", "-c", confPath+" "+action)
	if err == nil && code != 0 {
		outStr := strings.TrimSpace(string(out))
		if outStr != "" {
			return code, fmt.Errorf("init.d %s failed with exit code %d: %s", action, code, outStr)
		}
		return code, fmt.Errorf("init.d %s failed with exit code %d", action, code)
	}

	return code, err
}

// svcAction performs the action on the service.
//
// On OpenWrt, the service utility may not exist.  We use our service script
// directly in this case.
func svcAction(s service.Service, action string) (err error) {
	if sysType == "darwin" && action == "start" {
		var exe string
		if exe, err = os.Executable(); err != nil {
			log.Println("Starting service error: getting executable path: ", err)
		} else if exe, err = filepath.EvalSymlinks(exe); err != nil {
			log.Println("Starting service error: evaluating executable symlinks: ", err)
		} else if !strings.HasPrefix(exe, "/Applications/") {
			log.Println("warning: service must be started from within the /Applications directory")
		}
	}

	err = service.Control(s, action)
	if err != nil && service.Platform() == "unix-systemv" &&
		(action == "start" || action == "stop" || action == "restart") {
		_, err = runInitdCommand(s.String(), action)

		return err
	}

	return err
}

// svcStatus returns the service's status.
//
// On OpenWrt, the service utility may not exist.  We use our service script
// directly in this case.
func svcStatus(s service.Service) (status service.Status, err error) {
	status, err = s.Status()
	if err != nil && service.Platform() == "unix-systemv" {
		var code int
		code, err = runInitdCommand(s.String(), "status")
		if err != nil || code != 0 {
			return service.StatusStopped, nil
		}

		return service.StatusRunning, nil
	}

	return status, err
}

// readTailLines reads the last n non-empty lines from a file without loading the entire file.
func readTailLines(filePath string, n int) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() == 0 {
		return nil, nil
	}

	const maxReadSize = 64 * 1024
	offset := int64(0)
	readSize := fi.Size()
	if readSize > maxReadSize {
		offset = readSize - maxReadSize
		readSize = maxReadSize
	}

	buf := make([]byte, readSize)
	_, err = f.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}

	rawLines := strings.Split(string(buf), "\n")
	if offset > 0 && len(rawLines) > 0 {
		rawLines = rawLines[1:]
	}

	var lines []string
	for _, l := range rawLines {
		trimmed := strings.TrimRight(l, "\r")
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// getServicePID returns the PID of the running service if available.
func getServicePID(serviceName string) string {
	pidFile := "/var/run/" + serviceName + ".pid"
	data, err := os.ReadFile(pidFile)
	if err == nil {
		pid := strings.TrimSpace(string(data))
		if pid != "" {
			return pid
		}
	}
	return ""
}

// printRecentServiceLogs displays recent log entries like systemctl status does.
func printRecentServiceLogs(serviceName string) {
	// 1. On systemd Linux systems, try journalctl
	if sysType == "linux" && service.Platform() == "linux-systemd" {
		code, out, err := RunCommand("journalctl", "-u", serviceName, "-n", "10", "--no-pager")
		if err == nil && code == 0 {
			outStr := strings.TrimSpace(string(out))
			if outStr != "" {
				fmt.Println("\nRecent journal logs (last 10 lines):")
				for _, line := range strings.Split(outStr, "\n") {
					fmt.Println("  " + line)
				}
				return
			}
		}
	}

	// 2. On OpenWrt, SysV, or file-logging environments, find relevant log files
	var candidates []string

	// Check configured log file
	if logFile != "" {
		targetPath := logFile
		if !filepath.IsAbs(targetPath) {
			targetPath = filepath.Join(logDir, targetPath)
		}
		if logRandom {
			matches, _ := filepath.Glob(targetPath + "*")
			var newestFile string
			var newestMod time.Time
			for _, m := range matches {
				if fi, err := os.Stat(m); err == nil && fi.ModTime().After(newestMod) {
					newestMod = fi.ModTime()
					newestFile = m
				}
			}
			if newestFile != "" {
				candidates = append(candidates, newestFile)
			}
		} else if _, err := os.Stat(targetPath); err == nil {
			candidates = append(candidates, targetPath)
		}
	}

	// Standard service stdout and stderr logs
	stdoutFile := filepath.Join(logDir, serviceName+".log")
	stderrFile := filepath.Join(logDir, serviceName+".err")

	for _, f := range []string{stdoutFile, stderrFile} {
		alreadyAdded := false
		for _, added := range candidates {
			if added == f {
				alreadyAdded = true
				break
			}
		}
		if !alreadyAdded {
			if fi, err := os.Stat(f); err == nil && fi.Size() > 0 {
				candidates = append(candidates, f)
			}
		}
	}

	if len(candidates) == 0 {
		return
	}

	for _, f := range candidates {
		lines, err := readTailLines(f, 10)
		if err == nil && len(lines) > 0 {
			fmt.Printf("\nRecent logs from %s (last %d lines):\n", f, len(lines))
			for _, line := range lines {
				fmt.Println("  " + line)
			}
		}
	}
}

// OpenWrt init script (compatible with OpenWrt, ImmortalWrt, and LEDE)
const openWrtScript = `#!/bin/sh /etc/rc.common

START=90
STOP=01

cmd='{{.Path|cmd}}{{range .Arguments}} {{.|cmd}}{{end}}'
name="{{.Name}}"
pid_file="/var/run/${name}.pid"
stdout_log="{{.LogDirectory}}/$name.log"
stderr_log="{{.LogDirectory}}/$name.err"

{{range $k, $v := .EnvVars -}}
export {{$k}}={{$v}}
{{end -}}

EXTRA_COMMANDS="status"
EXTRA_HELP="$(printf "\t%-16s%s\n" "status" "Print the service status")"

get_pid() {
    cat "${pid_file}"
}

is_running() {
    [ -f "${pid_file}" ] && [ -s "${pid_file}" ] && kill -0 "$(get_pid)" >/dev/null 2>&1
}

start() {
    if is_running; then
        echo "Already started"
    else
        echo "Starting $name"
        {{if .WorkingDirectory}}cd '{{.WorkingDirectory}}'{{end}}
        mkdir -p "{{.LogDirectory}}"
        [ -f /etc/TZ ] && export TZ="$(cat /etc/TZ)"
        eval "$cmd >> \"$stdout_log\" 2>> \"$stderr_log\" &"
        echo $! > "$pid_file"
        sleep 1
        if ! is_running; then
            echo "Unable to start, see $stdout_log and $stderr_log"
            exit 1
        fi
    fi
}

stop() {
    if is_running; then
        echo -n "Stopping $name.."
        kill $(get_pid) 2>/dev/null
        for i in 1 2 3 4 5 6 7 8 9 10
        do
            if ! is_running; then
                break
            fi
            echo -n "."
            sleep 1
        done
        echo
        if is_running; then
            echo "Not stopped; may still be shutting down or shutdown may have failed"
            exit 1
        else
            echo "Stopped"
            if [ -f "$pid_file" ]; then
                rm -f "$pid_file"
            fi
        fi
    else
        echo "Not running"
    fi
}

restart() {
    stop
    if is_running; then
        echo "Unable to stop, will not attempt to start"
        exit 1
    fi
    start
}

status() {
    if is_running; then
        echo "Running"
    else
        echo "Stopped"
        exit 1
    fi
}
`

// Generic Linux sysv init script with eval command execution fix
const linuxSysvScript = `#!/bin/sh
# For RedHat, Debian and cousins:
# chkconfig: - 99 01
# description: {{.Description}}
# processname: {{.Path}}

### BEGIN INIT INFO
# Provides:          {{.Path}}
# Required-Start:
# Required-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: {{.DisplayName}}
# Description:       {{.Description}}
### END INIT INFO

cmd='{{.Path|cmd}}{{range .Arguments}} {{.|cmd}}{{end}}'

name=$(basename $(readlink -f $0))
pid_file="/var/run/$name.pid"
stdout_log="{{.LogDirectory}}/$name.log"
stderr_log="{{.LogDirectory}}/$name.err"

{{range $k, $v := .EnvVars -}}
export {{$k}}={{$v}}
{{end -}}

[ -e /etc/sysconfig/$name ] && . /etc/sysconfig/$name

get_pid() {
    cat "$pid_file"
}

is_running() {
    [ -f "$pid_file" ] && cat /proc/$(get_pid)/stat > /dev/null 2>&1
}

case "$1" in
    start)
        if is_running; then
            echo "Already started"
        else
            echo "Starting $name"
            {{if .WorkingDirectory}}cd '{{.WorkingDirectory}}'{{end}}
            mkdir -p "{{.LogDirectory}}"
            [ -f /etc/TZ ] && export TZ="$(cat /etc/TZ)"
            eval "$cmd >> \"$stdout_log\" 2>> \"$stderr_log\" &"
            echo $! > "$pid_file"
            sleep 1
            if ! is_running; then
                echo "Unable to start, see $stdout_log and $stderr_log"
                exit 1
            fi
        fi
    ;;
    stop)
        if is_running; then
            echo -n "Stopping $name.."
            kill $(get_pid) 2>/dev/null
            for i in 1 2 3 4 5 6 7 8 9 10
            do
                if ! is_running; then
                    break
                fi
                echo -n "."
                sleep 1
            done
            echo
            if is_running; then
                echo "Not stopped; may still be shutting down or shutdown may have failed"
                exit 1
            else
                echo "Stopped"
                if [ -f "$pid_file" ]; then
                    rm -f "$pid_file"
                fi
            fi
        else
            echo "Not running"
        fi
    ;;
    restart)
        $0 stop
        if is_running; then
            echo "Unable to stop, will not attempt to start"
            exit 1
        fi
        $0 start
    ;;
    status)
        if is_running; then
            echo "Running"
        else
            echo "Stopped"
            exit 1
        fi
    ;;
    *)
    echo "Usage: $0 {start|stop|restart|status}"
    exit 1
    ;;
esac
exit 0
`
