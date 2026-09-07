package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountPool_CalculateCooldown(t *testing.T) {
	base := 10 * time.Minute
	max := 2 * time.Hour
	pool := NewAccountPool([]Account{
		{Account: "acc1", Password: "pwd"},
	}, base, max)

	// Exponential backoff check:
	// n=0: 10m * 1 = 10m
	assert.Equal(t, 10*time.Minute, pool.CalculateCooldown(0))
	// n=1: 10m * 2 = 20m
	assert.Equal(t, 20*time.Minute, pool.CalculateCooldown(1))
	// n=2: 10m * 4 = 40m
	assert.Equal(t, 40*time.Minute, pool.CalculateCooldown(2))
	// n=3: 10m * 8 = 80m
	assert.Equal(t, 80*time.Minute, pool.CalculateCooldown(3))
	// n=4: 10m * 16 = 160m -> capped at 120m (2h)
	assert.Equal(t, 2*time.Hour, pool.CalculateCooldown(4))
	// n=10: capped at 2h
	assert.Equal(t, 2*time.Hour, pool.CalculateCooldown(10))
	// n=-1: negative shift defense, should treat as 0
	assert.Equal(t, 10*time.Minute, pool.CalculateCooldown(-1))
	// n=50: overflow protection, capped at 2h
	assert.Equal(t, 2*time.Hour, pool.CalculateCooldown(50))
}

func TestAccountPool_RotationAndCooldown(t *testing.T) {
	accs := []Account{
		{Account: "userA", Password: "pA"},
		{Account: "userB", Password: "pB"},
		{Account: "userC", Password: "pC"},
	}
	base := 100 * time.Millisecond
	max := 1 * time.Second
	pool := NewAccountPool(accs, base, max)
	require.Equal(t, 3, pool.AccountsCount())

	// 1. Initial candidates should be in round-robin order
	cand1, err := pool.GetNextCandidate()
	require.NoError(t, err)
	assert.Equal(t, "userA", cand1.Account.Account)

	cand2, err := pool.GetNextCandidate()
	require.NoError(t, err)
	assert.Equal(t, "userB", cand2.Account.Account)

	cand3, err := pool.GetNextCandidate()
	require.NoError(t, err)
	assert.Equal(t, "userC", cand3.Account.Account)

	// 2. Mark userA as failed -> userA enters cooldown
	pool.MarkFailed(cand1, "password incorrect")
	assert.Equal(t, 1, cand1.ConsecutiveFails)
	assert.True(t, time.Now().Before(cand1.CooldownUntil))

	// 3. Next candidate should skip userA and give userB or userC
	candNext, err := pool.GetNextCandidate()
	require.NoError(t, err)
	assert.NotEqual(t, "userA", candNext.Account.Account)

	// 4. Mark userB and userC as failed as well
	pool.MarkFailed(cand2, "session limit")
	pool.MarkFailed(cand3, "system error")

	// 5. Now all accounts are in cooldown
	candBlocked, err := pool.GetNextCandidate()
	assert.Nil(t, candBlocked)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all 3 account(s) are in cooldown")

	// 6. Wait for cooldown to expire
	time.Sleep(120 * time.Millisecond)
	candRecovered, err := pool.GetNextCandidate()
	require.NoError(t, err)
	assert.NotNil(t, candRecovered)
}

func TestAccountPool_KickedOffline(t *testing.T) {
	accs := []Account{
		{Account: "user1", Password: "p1"},
		{Account: "user2", Password: "p2"},
	}
	pool := NewAccountPool(accs, 10*time.Minute, 2*time.Hour)

	cand, err := pool.GetNextCandidate()
	require.NoError(t, err)
	assert.Equal(t, "user1", cand.Account.Account)

	// Simulate successful login
	pool.MarkActive(cand)
	assert.True(t, pool.WasConnected())
	assert.Equal(t, cand, pool.ActiveAccount())
	assert.Equal(t, 0, cand.ConsecutiveFails)

	// Simulate dropped connection -> kicked offline
	pool.MarkKicked()
	assert.False(t, pool.WasConnected())
	assert.Nil(t, pool.ActiveAccount())
	assert.Equal(t, 1, cand.ConsecutiveFails)
	assert.True(t, time.Now().Before(cand.CooldownUntil))
	assert.Equal(t, "kicked offline / disconnected", cand.LastFailReason)

	// user1 is now in cooldown, next candidate MUST be user2
	nextCand, err := pool.GetNextCandidate()
	require.NoError(t, err)
	assert.Equal(t, "user2", nextCand.Account.Account)

	// user2 logs in successfully
	pool.MarkActive(nextCand)
	assert.True(t, pool.WasConnected())
	assert.Equal(t, nextCand, pool.ActiveAccount())

	// Confirm steady state resets fails
	pool.ConfirmConnected()
	assert.Equal(t, 0, nextCand.ConsecutiveFails)
}
