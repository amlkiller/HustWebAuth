package cmd

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// Account represents credentials and auth configuration for an account.
type Account struct {
	Account     string `json:"account" yaml:"account" mapstructure:"account"`
	Password    string `json:"password" yaml:"password" mapstructure:"password"`
	ServiceType string `json:"serviceType,omitempty" yaml:"serviceType,omitempty" mapstructure:"serviceType"`
	Encrypt     *bool  `json:"encrypt,omitempty" yaml:"encrypt,omitempty" mapstructure:"encrypt"`
}

// ManagedAccount wraps Account with runtime cooldown and status metrics.
type ManagedAccount struct {
	Account          Account
	CooldownUntil    time.Time
	LastLoginTime    time.Time
	LastFailReason   string
	ConsecutiveFails int
}

// AccountPool manages multi-account rotation and exponential backoff cooldown.
type AccountPool struct {
	mu            sync.Mutex
	accounts      []*ManagedAccount
	currentIndex  int
	baseCooldown  time.Duration
	maxCooldown   time.Duration
	activeAccount *ManagedAccount
	wasConnected  bool
	rotation      bool
}

// NewAccountPool creates an AccountPool.
func NewAccountPool(accounts []Account, baseCooldown, maxCooldown time.Duration) *AccountPool {
	if baseCooldown <= 0 {
		baseCooldown = 4*time.Minute + 59*time.Second
	}
	if maxCooldown <= 0 {
		maxCooldown = 2 * time.Hour
	}
	if maxCooldown < baseCooldown {
		maxCooldown = baseCooldown
	}

	managed := make([]*ManagedAccount, 0, len(accounts))
	for _, acc := range accounts {
		if acc.Account == "" {
			continue
		}
		managed = append(managed, &ManagedAccount{
			Account: acc,
		})
	}

	return &AccountPool{
		accounts:     managed,
		baseCooldown: baseCooldown,
		maxCooldown:  maxCooldown,
		rotation:     true,
	}
}

// SetRotation sets whether account rotation is enabled.
func (p *AccountPool) SetRotation(enable bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rotation = enable
}

// Rotation returns whether account rotation is enabled.
func (p *AccountPool) Rotation() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rotation
}

// CalculateCooldown computes the exponential backoff cooldown duration for a given fail count.
func (p *AccountPool) CalculateCooldown(consecutiveFails int) time.Duration {
	if p.baseCooldown <= 0 {
		return 0
	}
	shift := consecutiveFails
	if shift < 0 {
		shift = 0
	} else if shift > 10 {
		shift = 10 // avoid integer overflow
	}
	multiplier := time.Duration(1 << shift)
	cd := p.baseCooldown * multiplier
	if p.maxCooldown > 0 && (cd > p.maxCooldown || cd <= 0) {
		cd = p.maxCooldown
	}
	return cd
}

// GetNextCandidate selects the next non-cooldown account in round-robin order.
func (p *AccountPool) GetNextCandidate() (*ManagedAccount, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.accounts)
	if n == 0 {
		return nil, fmt.Errorf("no accounts configured in pool")
	}

	if !p.rotation || n == 1 {
		acc := p.accounts[0]
		now := time.Now()
		if now.Before(acc.CooldownUntil) {
			remaining := acc.CooldownUntil.Sub(now).Round(time.Second)
			return nil, fmt.Errorf("primary account %q is in cooldown, earliest available in %s (until %s)",
				acc.Account.Account, remaining, acc.CooldownUntil.Format("15:04:05"))
		}
		return acc, nil
	}

	now := time.Now()
	var earliestCooldown time.Time
	var earliestAcc *ManagedAccount

	for i := 0; i < n; i++ {
		idx := (p.currentIndex + i) % n
		acc := p.accounts[idx]

		if now.After(acc.CooldownUntil) || now.Equal(acc.CooldownUntil) {
			p.currentIndex = (idx + 1) % n
			return acc, nil
		}

		if earliestAcc == nil || acc.CooldownUntil.Before(earliestCooldown) {
			earliestCooldown = acc.CooldownUntil
			earliestAcc = acc
		}
	}

	remaining := earliestCooldown.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	return nil, fmt.Errorf("all %d account(s) are in cooldown, earliest available in %s (%s, until %s)",
		n, remaining.Round(time.Second), earliestAcc.Account.Account, earliestCooldown.Format("15:04:05"))
}

// MarkActive marks the account as successfully logged in and resets its failure count.
func (p *AccountPool) MarkActive(acc *ManagedAccount) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.activeAccount = acc
	p.wasConnected = true
	acc.LastLoginTime = time.Now()
	acc.ConsecutiveFails = 0
	acc.CooldownUntil = time.Time{}
	acc.LastFailReason = ""
}

// MarkKicked handles network disconnection / kick-out of the previously active account.
func (p *AccountPool) MarkKicked() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.wasConnected && p.activeAccount != nil {
		acc := p.activeAccount
		if len(p.accounts) > 1 && p.rotation {
			cd := p.CalculateCooldown(acc.ConsecutiveFails)
			acc.CooldownUntil = time.Now().Add(cd)
			acc.ConsecutiveFails++
			log.Printf("[AccountPool] Account %q was kicked offline. Entering exponential cooldown (%s, count=%d) until %s\n",
				acc.Account.Account, cd, acc.ConsecutiveFails, acc.CooldownUntil.Format("15:04:05"))
		} else {
			acc.ConsecutiveFails++
			log.Printf("[AccountPool] Account %q was kicked offline / disconnected. Immediate reconnection will be attempted (single account or rotation disabled)\n",
				acc.Account.Account)
		}
		acc.LastFailReason = "kicked offline / disconnected"
		p.activeAccount = nil
	}
	p.wasConnected = false
}

// MarkFailed puts an account into exponential cooldown after a login failure.
func (p *AccountPool) MarkFailed(acc *ManagedAccount, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.accounts) > 1 && p.rotation {
		cd := p.CalculateCooldown(acc.ConsecutiveFails)
		acc.CooldownUntil = time.Now().Add(cd)
		acc.ConsecutiveFails++
		log.Printf("[AccountPool] Account %q failed (%s). Entering exponential cooldown (%s, count=%d) until %s\n",
			acc.Account.Account, reason, cd, acc.ConsecutiveFails, acc.CooldownUntil.Format("15:04:05"))
	} else {
		acc.ConsecutiveFails++
		log.Printf("[AccountPool] Account %q failed (%s). Fail count: %d\n",
			acc.Account.Account, reason, acc.ConsecutiveFails)
	}
	acc.LastFailReason = reason

	if p.activeAccount == acc {
		p.activeAccount = nil
	}
}

// ConfirmConnected marks the state as connected and maintains stable online state.
func (p *AccountPool) ConfirmConnected() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.wasConnected = true
	if p.activeAccount != nil {
		p.activeAccount.ConsecutiveFails = 0
	}
}

// WasConnected returns whether the network was connected in the previous check.
func (p *AccountPool) WasConnected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.wasConnected
}

// SetWasConnected updates the connection state tracking.
func (p *AccountPool) SetWasConnected(connected bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.wasConnected = connected
}

// ActiveAccount returns the currently active account.
func (p *AccountPool) ActiveAccount() *ManagedAccount {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activeAccount
}

// AccountsCount returns the number of accounts managed.
func (p *AccountPool) AccountsCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.accounts)
}
