package cmd

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAccountList(t *testing.T) {
	// Comma separated accounts with matching passwords
	accs := parseAccountList("user1, user2", "pass1, pass2", "internet", false)
	assert.Len(t, accs, 2)
	assert.Equal(t, "user1", accs[0].Account)
	assert.Equal(t, "pass1", accs[0].Password)
	assert.Equal(t, "user2", accs[1].Account)
	assert.Equal(t, "pass2", accs[1].Password)

	// Single password reused for multiple accounts
	accsReused := parseAccountList("u1, u2, u3", "common_pwd", "local", true)
	assert.Len(t, accsReused, 3)
	assert.Equal(t, "common_pwd", accsReused[0].Password)
	assert.Equal(t, "common_pwd", accsReused[1].Password)
	assert.Equal(t, "common_pwd", accsReused[2].Password)
	assert.True(t, *accsReused[0].Encrypt)

	// Single account with comma in password
	accsCommaPwd := parseAccountList("singleUser", "my,secret,pass", "internet", false)
	assert.Len(t, accsCommaPwd, 1)
	assert.Equal(t, "singleUser", accsCommaPwd[0].Account)
	assert.Equal(t, "my,secret,pass", accsCommaPwd[0].Password)
}

func TestGetEffectiveAccountsPrecedence(t *testing.T) {
	// Backup original state
	origAccount := account
	origPassword := password
	origConfiguredAccounts := configuredAccounts
	defer func() {
		account = origAccount
		password = origPassword
		configuredAccounts = origConfiguredAccounts
	}()

	// Case 1: YAML has both auth.account and auth.accounts -> auth.accounts must take priority
	account = "singleFromYaml"
	password = "pwd"
	configuredAccounts = []Account{
		{Account: "pool1", Password: "p1"},
		{Account: "pool2", Password: "p2"},
	}
	accs := getEffectiveAccounts()
	assert.Len(t, accs, 2)
	assert.Equal(t, "pool1", accs[0].Account)
	assert.Equal(t, "pool2", accs[1].Account)

	// Case 2: Only single account is configured
	configuredAccounts = nil
	accsSingle := getEffectiveAccounts()
	assert.Len(t, accsSingle, 1)
	assert.Equal(t, "singleFromYaml", accsSingle[0].Account)

	// Case 3: Comma-separated accounts configured in auth.account
	account = "multiUser1, multiUser2"
	password = "pwd1, pwd2"
	configuredAccounts = parseAccountList(account, password, serviceType, encrypt)
	accsMulti := getEffectiveAccounts()
	assert.Len(t, accsMulti, 2)
	assert.Equal(t, "multiUser1", accsMulti[0].Account)
	assert.Equal(t, "pwd1", accsMulti[0].Password)
	assert.Equal(t, "multiUser2", accsMulti[1].Account)
	assert.Equal(t, "pwd2", accsMulti[1].Password)
}

func TestInterfaceAccountsCliPrecedence(t *testing.T) {
	origIface := iface
	origConfiguredInterfaces := configuredInterfaces
	origAccount := account
	defer func() {
		iface = origIface
		configuredInterfaces = origConfiguredInterfaces
		account = origAccount
		rootCmd.PersistentFlags().Lookup("account").Changed = false
	}()

	iface = "eth0"
	configuredInterfaces = []InterfaceConfig{
		{
			Iface: "eth0",
			Accounts: []Account{
				{Account: "yamlIfaceUser", Password: "pwd"},
			},
		},
	}

	// 1. When CLI flag -a was NOT changed, interface accounts should be used
	rootCmd.PersistentFlags().Lookup("account").Changed = false
	accs := getEffectiveAccounts()
	if !rootCmd.PersistentFlags().Lookup("account").Changed && len(configuredInterfaces[0].Accounts) > 0 {
		accs = configuredInterfaces[0].Accounts
	}
	assert.Equal(t, "yamlIfaceUser", accs[0].Account)

	// 2. When CLI flag -a WAS changed, CLI accounts must take precedence
	rootCmd.PersistentFlags().Lookup("account").Changed = true
	account = "cliUser"
	accs = getEffectiveAccounts()
	if !rootCmd.PersistentFlags().Lookup("account").Changed && len(configuredInterfaces[0].Accounts) > 0 {
		accs = configuredInterfaces[0].Accounts
	}
	assert.Equal(t, "cliUser", accs[0].Account)
}

func TestSaveConfig_DirectoryCreationAndReset(t *testing.T) {
	origCfgFile := cfgFile
	origSaveCfg := saveCfg
	defer func() {
		cfgFile = origCfgFile
		saveCfg = origSaveCfg
	}()

	tempDir := t.TempDir()
	nestedFile := filepath.Join(tempDir, "nested", "subdir", "HustWebAuth.yaml")
	cfgFile = nestedFile
	saveCfg = true

	viper.Set("auth.account", "saveConfigTestUser")

	// Call saveConfig
	saveConfig()

	// saveCfg must be reset to false to avoid duplicate writing
	assert.False(t, saveCfg)

	// File must exist in the created subdirectory
	content, err := os.ReadFile(nestedFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "saveConfigTestUser")

	// Second call with saveCfg=false must be safe no-op
	saveConfig()
	assert.False(t, saveCfg)
}

func TestRunSingleWorker_ContextCancel(t *testing.T) {
	origCycleEnable := cycleEnable
	origCycleDuration := cycleDuration
	origCheckURL := checkURL
	defer func() {
		cycleEnable = origCycleEnable
		cycleDuration = origCycleDuration
		checkURL = origCheckURL
	}()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	cycleEnable = true
	cycleDuration = 1 * time.Hour
	checkURL = ts.URL

	cfg := InterfaceConfig{
		Iface:    "",
		CheckURL: ts.URL,
		Accounts: []Account{
			{Account: "testUser", Password: "testPassword"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		runSingleWorker(ctx, cfg, false)
		close(done)
	}()

	// Wait briefly to ensure worker executed initial check and entered ticker loop
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Exited gracefully
	case <-time.After(2 * time.Second):
		t.Fatal("runSingleWorker did not exit within timeout after context cancel")
	}
}

func TestRunSingleWorker_PreCanceledContext(t *testing.T) {
	origCycleEnable := cycleEnable
	defer func() {
		cycleEnable = origCycleEnable
	}()
	cycleEnable = true

	cfg := InterfaceConfig{
		Iface:    "",
		Accounts: []Account{{Account: "testUser", Password: "pwd"}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling worker

	done := make(chan struct{})
	go func() {
		runSingleWorker(ctx, cfg, false)
		close(done)
	}()

	select {
	case <-done:
		// Succeeded immediately without blocking
	case <-time.After(1 * time.Second):
		t.Fatal("runSingleWorker blocked on pre-canceled context")
	}
}

func TestRunCycleWithContext_MultiWorker_ContextCancel(t *testing.T) {
	origCycleEnable := cycleEnable
	origCycleDuration := cycleDuration
	origIface := iface
	origConfiguredInterfaces := configuredInterfaces
	defer func() {
		cycleEnable = origCycleEnable
		cycleDuration = origCycleDuration
		iface = origIface
		configuredInterfaces = origConfiguredInterfaces
	}()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	cycleEnable = true
	cycleDuration = 1 * time.Hour
	iface = ""
	configuredInterfaces = []InterfaceConfig{
		{
			Iface:    "",
			CheckURL: ts.URL,
			Accounts: []Account{{Account: "userA", Password: "pwd"}},
		},
		{
			Iface:    "",
			CheckURL: ts.URL,
			Accounts: []Account{{Account: "userB", Password: "pwd"}},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		runCycleWithContext(ctx)
		close(done)
	}()

	// Wait briefly to ensure concurrent workers started and entered ticker loop
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// All concurrent workers exited gracefully
	case <-time.After(2 * time.Second):
		t.Fatal("runCycleWithContext did not return within timeout after cancel")
	}
}

func TestExampleYamlConfigParsing(t *testing.T) {
	// 1. Locate example.yaml in repo root
	examplePath := filepath.Join("..", "example.yaml")
	require.FileExists(t, examplePath, "example.yaml must exist in the project root")

	// 2. Validate parsing with an isolated Viper instance
	v := viper.New()
	v.SetConfigFile(examplePath)
	v.SetConfigType("yaml")
	err := v.ReadInConfig()
	require.NoError(t, err, "Viper should parse example.yaml without error")

	// Verify auth module
	assert.Equal(t, "U202300001", v.GetString("auth.account"))
	assert.Equal(t, "YourPasswordHere", v.GetString("auth.password"))
	assert.Equal(t, "internet", v.GetString("auth.serviceType"))
	assert.False(t, v.GetBool("auth.encrypt"))
	assert.Equal(t, 4*time.Minute+59*time.Second, v.GetDuration("auth.cooldown"))
	assert.Equal(t, 2*time.Hour, v.GetDuration("auth.maxCooldown"))
	assert.True(t, v.GetBool("auth.rotation"))

	var accs []Account
	err = v.UnmarshalKey("auth.accounts", &accs)
	require.NoError(t, err)
	require.Len(t, accs, 2)
	assert.Equal(t, "U202300001", accs[0].Account)
	assert.Equal(t, "Password1", accs[0].Password)
	assert.Equal(t, "internet", accs[0].ServiceType)
	require.NotNil(t, accs[0].Encrypt)
	assert.False(t, *accs[0].Encrypt)
	assert.Equal(t, "U202300002", accs[1].Account)
	assert.Equal(t, "Password2", accs[1].Password)

	// Verify net module
	assert.Equal(t, "", v.GetString("net.iface"))
	assert.True(t, v.GetBool("net.insecure"))

	// Verify interfaces list (Scenario 2)
	var ifcs []InterfaceConfig
	err = v.UnmarshalKey("interfaces", &ifcs)
	require.NoError(t, err)
	require.Len(t, ifcs, 2)
	assert.Equal(t, "vwan1", ifcs[0].Iface)
	assert.Equal(t, "http://connect.rom.miui.com/generate_204", ifcs[0].CheckURL)
	assert.Equal(t, 4*time.Minute+59*time.Second, ifcs[0].Cooldown)
	assert.Equal(t, 2*time.Hour, ifcs[0].MaxCooldown)
	require.Len(t, ifcs[0].Accounts, 2)
	assert.Equal(t, "vwan1_user1", ifcs[0].Accounts[0].Account)
	assert.Equal(t, "vwan1_pass1", ifcs[0].Accounts[0].Password)

	assert.Equal(t, "vwan2", ifcs[1].Iface)
	assert.Equal(t, "http://connect.rom.miui.com/generate_204", ifcs[1].CheckURL)
	assert.Equal(t, 4*time.Minute+59*time.Second, ifcs[1].Cooldown)
	assert.Equal(t, 2*time.Hour, ifcs[1].MaxCooldown)
	require.Len(t, ifcs[1].Accounts, 1)
	assert.Equal(t, "vwan2_user1", ifcs[1].Accounts[0].Account)

	// Verify check module
	assert.Equal(t, "http://connect.rom.miui.com/generate_204", v.GetString("check.url"))
	assert.Equal(t, 5*time.Second, v.GetDuration("check.timeout"))

	// Verify redirect module
	assert.Equal(t, "http://123.123.123.123", v.GetString("redirect.url"))

	// Verify cycle module
	assert.True(t, v.GetBool("cycle.enable"))
	assert.Equal(t, 5*time.Minute, v.GetDuration("cycle.duration"))
	assert.Equal(t, 3, v.GetInt("cycle.retry"))

	// Verify daemon module
	assert.False(t, v.GetBool("daemon.enable"))
	assert.Equal(t, "/var/run/HustWebAuth_daemon.pid", v.GetString("daemon.pidFile"))

	// Verify log module
	assert.Equal(t, "/tmp/HustWebAuth", v.GetString("log.dir"))
	assert.Equal(t, "HustWebAuth.log", v.GetString("log.file"))
	assert.False(t, v.GetBool("log.random"))
	assert.True(t, v.GetBool("log.append"))
	assert.False(t, v.GetBool("log.connected"))
	assert.False(t, v.GetBool("log.syslog"))

	// Verify ping compatibility module
	assert.Equal(t, "202.114.0.131", v.GetString("ping.ip"))
	assert.Equal(t, 3, v.GetInt("ping.count"))
	assert.Equal(t, 3*time.Second, v.GetDuration("ping.timeout"))
	assert.True(t, v.GetBool("ping.privilege"))
}

func TestRunSingleWorker_CooldownLogSuppression(t *testing.T) {
	origCycleEnable := cycleEnable
	origCycleDuration := cycleDuration
	origCheckURL := checkURL
	defer func() {
		cycleEnable = origCycleEnable
		cycleDuration = origCycleDuration
		checkURL = origCheckURL
		log.SetOutput(os.Stderr)
	}()

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)

	// Server returns HTTP 200 with redirect to login portal
	portalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `<script>location.href="http://%s/eportal/index.jsp?wlanuserip=1.1.1.1";</script>`, r.Host)
	}))
	defer portalServer.Close()

	cycleEnable = true
	cycleDuration = 15 * time.Millisecond
	checkURL = portalServer.URL

	// Configure an account that enters cooldown on failure
	cfg := InterfaceConfig{
		Iface:       "",
		CheckURL:    portalServer.URL,
		Cooldown:    10 * time.Minute,
		MaxCooldown: 10 * time.Minute,
		Accounts: []Account{
			{Account: "cooldownUser", Password: "wrongPassword"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		runSingleWorker(ctx, cfg, false)
		close(done)
	}()

	// Wait for multiple ticks (initial failure + several cooldown ticks)
	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done

	out := logBuf.String()
	// Count occurrences of "waiting for cooldown to expire..."
	count := strings.Count(out, "waiting for cooldown to expire...")
	assert.Equal(t, 1, count, "cooldown message should be logged exactly once during the cooldown period, got: %d\nLog:\n%s", count, out)
}


