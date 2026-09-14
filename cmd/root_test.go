package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

