package cmd

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/kardianos/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceScript_EvalExecution(t *testing.T) {
	// Verify that openWrtScript contains eval execution, sleep check, name template, and POSIX is_running check
	assert.Contains(t, openWrtScript, "eval \"$cmd >> \\\"$stdout_log\\\" 2>> \\\"$stderr_log\\\" &\"")
	assert.Contains(t, openWrtScript, "cmd='{{.Path|cmd}}{{range .Arguments}} {{.|cmd}}{{end}}'")
	assert.Contains(t, openWrtScript, "sleep 1")
	assert.Contains(t, openWrtScript, `name="{{.Name}}"`)
	assert.Contains(t, openWrtScript, `[ -f "${pid_file}" ] && [ -s "${pid_file}" ] && kill -0 "$(get_pid)" >/dev/null 2>&1`)

	// Verify that linuxSysvScript contains eval execution, sleep check, name template, and POSIX is_running check
	assert.Contains(t, linuxSysvScript, "eval \"$cmd >> \\\"$stdout_log\\\" 2>> \\\"$stderr_log\\\" &\"")
	assert.Contains(t, linuxSysvScript, "cmd='{{.Path|cmd}}{{range .Arguments}} {{.|cmd}}{{end}}'")
	assert.Contains(t, linuxSysvScript, "sleep 1")
	assert.Contains(t, linuxSysvScript, `name="{{.Name}}"`)
	assert.Contains(t, linuxSysvScript, `[ -f "${pid_file}" ] && [ -s "${pid_file}" ] && kill -0 "$(get_pid)" >/dev/null 2>&1`)

	// Verify that linuxSysvScript avoids readlink and /proc dependency
	assert.NotContains(t, linuxSysvScript, "readlink")
	assert.NotContains(t, linuxSysvScript, "/proc/")
}

func TestServiceScript_TemplateRendering(t *testing.T) {
	data := struct {
		Path             string
		Arguments        []string
		Name             string
		LogDirectory     string
		WorkingDirectory string
		EnvVars          map[string]string
		Description      string
		DisplayName      string
	}{
		Path:             "/root/HustWebAuth_linux_arm64",
		Arguments:        []string{"service", "-f", "/root/HustWebAuth.yaml"},
		Name:             "HustWebAuth",
		LogDirectory:     "/tmp/HustWebAuth",
		WorkingDirectory: "",
		EnvVars:          map[string]string{"HOME": "/root"},
		Description:      "A service used to implement Ruijie web authentication.",
		DisplayName:      "HustWebAuth",
	}

	tmplFuncs := template.FuncMap{
		"cmd": func(s string) string {
			return `"` + strings.Replace(s, `"`, `\"`, -1) + `"`
		},
		"cmdEscape": func(s string) string {
			return strings.Replace(s, " ", `\x20`, -1)
		},
	}

	// Render openWrtScript
	tOWrt, err := template.New("openwrt").Funcs(tmplFuncs).Parse(openWrtScript)
	require.NoError(t, err)
	var bufOWrt bytes.Buffer
	err = tOWrt.Execute(&bufOWrt, data)
	require.NoError(t, err)
	renderedOWrt := bufOWrt.String()

	assert.Contains(t, renderedOWrt, `cmd='"/root/HustWebAuth_linux_arm64" "service" "-f" "/root/HustWebAuth.yaml"'`)
	assert.Contains(t, renderedOWrt, `eval "$cmd >> \"$stdout_log\" 2>> \"$stderr_log\" &"`)
	assert.Contains(t, renderedOWrt, `name="HustWebAuth"`)
	assert.Contains(t, renderedOWrt, `stdout_log="/tmp/HustWebAuth/$name.log"`)
	assert.Contains(t, renderedOWrt, `kill -0 "$(get_pid)"`)

	// Render linuxSysvScript
	tSysv, err := template.New("sysv").Funcs(tmplFuncs).Parse(linuxSysvScript)
	require.NoError(t, err)
	var bufSysv bytes.Buffer
	err = tSysv.Execute(&bufSysv, data)
	require.NoError(t, err)
	renderedSysv := bufSysv.String()

	assert.Contains(t, renderedSysv, `cmd='"/root/HustWebAuth_linux_arm64" "service" "-f" "/root/HustWebAuth.yaml"'`)
	assert.Contains(t, renderedSysv, `eval "$cmd >> \"$stdout_log\" 2>> \"$stderr_log\" &"`)
	assert.Contains(t, renderedSysv, `name="HustWebAuth"`)
	assert.Contains(t, renderedSysv, `stdout_log="/tmp/HustWebAuth/$name.log"`)
	assert.Contains(t, renderedSysv, `kill -0 "$(get_pid)"`)
	assert.NotContains(t, renderedSysv, "readlink")
	assert.NotContains(t, renderedSysv, "/proc/")

	// Test multi-WAN / custom service name rendering with suffix
	dataMulti := data
	dataMulti.Name = "HustWebAuth_eth0"
	var bufSysvMulti bytes.Buffer
	err = tSysv.Execute(&bufSysvMulti, dataMulti)
	require.NoError(t, err)
	renderedSysvMulti := bufSysvMulti.String()

	assert.Contains(t, renderedSysvMulti, `name="HustWebAuth_eth0"`)
	assert.Contains(t, renderedSysvMulti, `pid_file="/var/run/$name.pid"`)
	assert.NotContains(t, renderedSysvMulti, "readlink")
}

func TestServiceScript_IsRunningPOSIX(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "test.pid")

	shScript := `
pid_file="` + pidFile + `"
get_pid() {
    cat "$pid_file"
}
is_running() {
    [ -f "${pid_file}" ] && [ -s "${pid_file}" ] && kill -0 "$(get_pid)" >/dev/null 2>&1
}
if is_running; then
    exit 0
else
    exit 1
fi
`

	// Case 1: PID file does not exist -> exit code 1 (not running)
	code, _, _ := RunCommand("sh", "-c", shScript)
	assert.Equal(t, 1, code)

	// Case 2: PID file exists but is empty -> exit code 1 (not running)
	err := os.WriteFile(pidFile, []byte(""), 0644)
	require.NoError(t, err)
	code, _, _ = RunCommand("sh", "-c", shScript)
	assert.Equal(t, 1, code)

	// Case 3: PID file exists with non-existent process PID -> exit code 1 (not running)
	err = os.WriteFile(pidFile, []byte("99999999"), 0644)
	require.NoError(t, err)
	code, _, _ = RunCommand("sh", "-c", shScript)
	assert.Equal(t, 1, code)

	// Case 4: PID file contains current running process PID -> exit code 0 (running)
	err = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	require.NoError(t, err)
	code, _, _ = RunCommand("sh", "-c", shScript)
	assert.Equal(t, 0, code)
}

func TestIsServiceControlCommand(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "service start",
			args:     []string{"HustWebAuth", "service", "start"},
			expected: true,
		},
		{
			name:     "service stop",
			args:     []string{"HustWebAuth", "service", "stop"},
			expected: true,
		},
		{
			name:     "service status",
			args:     []string{"HustWebAuth", "service", "status"},
			expected: true,
		},
		{
			name:     "service restart",
			args:     []string{"HustWebAuth", "service", "restart"},
			expected: true,
		},
		{
			name:     "service install",
			args:     []string{"HustWebAuth", "service", "install"},
			expected: true,
		},
		{
			name:     "service uninstall",
			args:     []string{"HustWebAuth", "service", "uninstall"},
			expected: true,
		},
		{
			name:     "service daemon run (background service worker)",
			args:     []string{"HustWebAuth", "service", "-f", "/root/HustWebAuth.yaml"},
			expected: false,
		},
		{
			name:     "root command run",
			args:     []string{"HustWebAuth", "-c"},
			expected: false,
		},
		{
			name:     "login command run",
			args:     []string{"HustWebAuth", "login"},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.Args = tc.args
			assert.Equal(t, tc.expected, isServiceControlCommand())
		})
	}
}

func TestNewSVCConfig_Options(t *testing.T) {
	origCustomServiceName := customServiceName
	origIface := iface
	origLogDir := logDir
	defer func() {
		customServiceName = origCustomServiceName
		iface = origIface
		logDir = origLogDir
	}()

	customServiceName = "CustomAuth"
	iface = "eth0"
	logDir = "/tmp/testlog"

	conf := newSVCConfig()
	assert.Equal(t, "CustomAuth", conf.Name)
	assert.Equal(t, "CustomAuth", conf.DisplayName)
	assert.Contains(t, conf.Arguments, "service")
	assert.Contains(t, conf.Arguments, "--name")
	assert.Contains(t, conf.Arguments, "CustomAuth")

	if sysType == "linux" {
		assert.NotEmpty(t, conf.Option["SysvScript"])
		scriptStr, ok := conf.Option["SysvScript"].(string)
		assert.True(t, ok)
		assert.Contains(t, scriptStr, "eval \"$cmd")
	}
}

func TestServiceCmd_InteractiveHelp(t *testing.T) {
	// Verify runCmd exists and is hidden
	foundRun := false
	for _, sub := range serviceCmd.Commands() {
		if sub.Name() == "run" {
			foundRun = true
			assert.True(t, sub.Hidden)
			break
		}
	}
	assert.True(t, foundRun, "service run subcommand should be registered")

	// Verify executing serviceCmd directly in test (interactive environment) returns nil
	var buf bytes.Buffer
	serviceCmd.SetOut(&buf)
	err := serviceCmd.RunE(serviceCmd, []string{})
	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Available Commands:")
	assert.Contains(t, output, "install")
	assert.Contains(t, output, "start")
	assert.Contains(t, output, "status")
	assert.NotContains(t, output, "\n  run ") // run subcommand is hidden from available commands
}

func TestReadTailLines(t *testing.T) {
	tempDir := t.TempDir()
	logPath := tempDir + "/test_tail.log"

	// 1. Non-existent file
	lines, err := readTailLines(tempDir+"/not_found.log", 10)
	assert.Error(t, err)
	assert.Nil(t, lines)

	// 2. Empty file
	err = os.WriteFile(logPath, []byte(""), 0644)
	require.NoError(t, err)
	lines, err = readTailLines(logPath, 10)
	require.NoError(t, err)
	assert.Empty(t, lines)

	// 3. File with 5 lines, ask for 3
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	err = os.WriteFile(logPath, []byte(content), 0644)
	require.NoError(t, err)

	lines, err = readTailLines(logPath, 3)
	require.NoError(t, err)
	assert.Equal(t, []string{"line 3", "line 4", "line 5"}, lines)

	// 4. File with 5 lines, ask for 10
	lines, err = readTailLines(logPath, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"line 1", "line 2", "line 3", "line 4", "line 5"}, lines)
}

func TestGetServicePID(t *testing.T) {
	// 1. Non-existent service PID
	pid := getServicePID("non_existent_service_12345")
	assert.Empty(t, pid)
}

func TestServiceUninstall_OpenWrt_ScriptNotExist(t *testing.T) {
	origIsOpenWrt := isOpenWrtFunc
	origCustomServiceName := customServiceName
	origStatusFunc := svcStatusFunc
	origActionFunc := svcActionFunc
	defer func() {
		isOpenWrtFunc = origIsOpenWrt
		customServiceName = origCustomServiceName
		svcStatusFunc = origStatusFunc
		svcActionFunc = origActionFunc
	}()

	isOpenWrtFunc = func() bool { return true }
	customServiceName = "NonExistentService_12345"
	_, err := os.Stat("/etc/init.d/" + customServiceName)
	require.True(t, os.IsNotExist(err))

	var actionsCalled []string
	svcStatusFunc = func(s service.Service) (service.Status, error) {
		actionsCalled = append(actionsCalled, "status")
		return service.StatusStopped, nil
	}
	svcActionFunc = func(s service.Service, action string) error {
		actionsCalled = append(actionsCalled, action)
		return nil
	}

	var logBuf bytes.Buffer
	origLogOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origLogOut)

	uninstallCmd.Run(uninstallCmd, []string{})

	output := logBuf.String()
	assert.NotContains(t, output, "service: running init disable:")
	assert.Contains(t, output, "HustWebAuth service has been uninstalled")
	assert.Equal(t, []string{"status", "uninstall"}, actionsCalled)
}

func TestServiceUninstall_OpenWrt_DisableFailure_NoFatal(t *testing.T) {
	// Look for an existing init.d script on linux (e.g. cron)
	entries, err := os.ReadDir("/etc/init.d")
	if err != nil || len(entries) == 0 {
		t.Skip("skipping test: no /etc/init.d scripts found")
	}

	var existingScriptName string
	for _, entry := range entries {
		if !entry.IsDir() {
			code, _, _ := RunCommand("sh", "-c", "/etc/init.d/"+entry.Name()+" disable")
			if code != 0 {
				existingScriptName = entry.Name()
				break
			}
		}
	}
	if existingScriptName == "" {
		t.Skip("skipping test: no files found in /etc/init.d that fail on disable")
	}

	origIsOpenWrt := isOpenWrtFunc
	origCustomServiceName := customServiceName
	origStatusFunc := svcStatusFunc
	origActionFunc := svcActionFunc
	defer func() {
		isOpenWrtFunc = origIsOpenWrt
		customServiceName = origCustomServiceName
		svcStatusFunc = origStatusFunc
		svcActionFunc = origActionFunc
	}()

	isOpenWrtFunc = func() bool { return true }
	customServiceName = existingScriptName

	// Ensure the script actually exists
	_, err = os.Stat("/etc/init.d/" + customServiceName)
	require.NoError(t, err)

	var actionsCalled []string
	svcStatusFunc = func(s service.Service) (service.Status, error) {
		actionsCalled = append(actionsCalled, "status")
		return service.StatusRunning, nil
	}
	svcActionFunc = func(s service.Service, action string) error {
		actionsCalled = append(actionsCalled, action)
		return nil
	}

	var logBuf bytes.Buffer
	origLogOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origLogOut)

	uninstallCmd.Run(uninstallCmd, []string{})

	output := logBuf.String()
	// Since the script in /etc/init.d does not support OpenWrt "disable" action,
	// runInitdCommand fails, but it should log a warning via log.Printf instead of terminating with log.Fatalf
	assert.Contains(t, output, "service: running init disable:")
	assert.Contains(t, output, "HustWebAuth service has been uninstalled")
	assert.Equal(t, []string{"status", "stop", "uninstall"}, actionsCalled)
}

func TestServiceUninstall_NonOpenWrt(t *testing.T) {
	origIsOpenWrt := isOpenWrtFunc
	origCustomServiceName := customServiceName
	origStatusFunc := svcStatusFunc
	origActionFunc := svcActionFunc
	defer func() {
		isOpenWrtFunc = origIsOpenWrt
		customServiceName = origCustomServiceName
		svcStatusFunc = origStatusFunc
		svcActionFunc = origActionFunc
	}()

	isOpenWrtFunc = func() bool { return false }
	customServiceName = "NonOpenWrtService_123"

	var actionsCalled []string
	svcStatusFunc = func(s service.Service) (service.Status, error) {
		actionsCalled = append(actionsCalled, "status")
		return service.StatusStopped, nil
	}
	svcActionFunc = func(s service.Service, action string) error {
		actionsCalled = append(actionsCalled, action)
		return nil
	}

	var logBuf bytes.Buffer
	origLogOut := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origLogOut)

	uninstallCmd.Run(uninstallCmd, []string{})

	output := logBuf.String()
	assert.NotContains(t, output, "service: running init disable:")
	assert.Contains(t, output, "HustWebAuth service has been uninstalled")
	assert.Equal(t, []string{"status", "uninstall"}, actionsCalled)
}

func TestRunInitdCommand_NonExistentScript(t *testing.T) {
	code, err := runInitdCommand("non_existent_service_script_12345", "disable")
	assert.Error(t, err)
	assert.Equal(t, 127, code)
	assert.Contains(t, err.Error(), "exit code 127")
}

func TestProgram_StartStopLifecycle(t *testing.T) {
	origCycleEnable := cycleEnable
	origCycleDuration := cycleDuration
	origCheckURL := checkURL
	origIface := iface
	origConfiguredInterfaces := configuredInterfaces
	defer func() {
		cycleEnable = origCycleEnable
		cycleDuration = origCycleDuration
		checkURL = origCheckURL
		iface = origIface
		configuredInterfaces = origConfiguredInterfaces
	}()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	cycleEnable = true
	cycleDuration = 1 * time.Hour
	checkURL = ts.URL
	iface = ""
	configuredInterfaces = nil

	prg := &program{}
	err := prg.Start(nil)
	require.NoError(t, err)
	require.NotNil(t, prg.ctx)
	require.NotNil(t, prg.cancel)
	assert.NoError(t, prg.ctx.Err())

	// Wait briefly to allow worker goroutine to start and enter ticker loop
	time.Sleep(50 * time.Millisecond)

	stopDone := make(chan struct{})
	go func() {
		stopErr := prg.Stop(nil)
		assert.NoError(t, stopErr)
		close(stopDone)
	}()

	select {
	case <-stopDone:
		// Graceful stop completed
	case <-time.After(2 * time.Second):
		t.Fatal("program.Stop timed out; worker goroutine did not exit gracefully")
	}

	assert.ErrorIs(t, prg.ctx.Err(), context.Canceled)
}

func TestProgram_StopWithoutStart(t *testing.T) {
	prg := &program{}
	err := prg.Stop(nil)
	assert.NoError(t, err)
}

func TestProgram_StartStop_MultiWorker(t *testing.T) {
	origCycleEnable := cycleEnable
	origCycleDuration := cycleDuration
	origCheckURL := checkURL
	origIface := iface
	origConfiguredInterfaces := configuredInterfaces
	defer func() {
		cycleEnable = origCycleEnable
		cycleDuration = origCycleDuration
		checkURL = origCheckURL
		iface = origIface
		configuredInterfaces = origConfiguredInterfaces
	}()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	cycleEnable = true
	cycleDuration = 1 * time.Hour
	checkURL = ts.URL
	iface = ""
	configuredInterfaces = []InterfaceConfig{
		{
			Iface:    "",
			CheckURL: ts.URL,
			Accounts: []Account{{Account: "user1", Password: "pwd"}},
		},
		{
			Iface:    "",
			CheckURL: ts.URL,
			Accounts: []Account{{Account: "user2", Password: "pwd"}},
		},
	}

	prg := &program{}
	err := prg.Start(nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	stopDone := make(chan struct{})
	go func() {
		stopErr := prg.Stop(nil)
		assert.NoError(t, stopErr)
		close(stopDone)
	}()

	select {
	case <-stopDone:
		// All concurrent workers stopped gracefully
	case <-time.After(2 * time.Second):
		t.Fatal("program.Stop timed out waiting for multiple workers to exit")
	}

	assert.ErrorIs(t, prg.ctx.Err(), context.Canceled)
}

