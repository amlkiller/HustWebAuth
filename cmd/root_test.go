package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
}
