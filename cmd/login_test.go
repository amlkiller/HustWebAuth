package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCookieWithClient(t *testing.T) {
	// Server 1: Returns a cookie
	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "testcookie123"})
		w.WriteHeader(http.StatusOK)
	}))
	defer s1.Close()

	client := s1.Client()
	cookie, err := GetCookieWithClient(client, s1.URL)
	require.NoError(t, err)
	require.NotNil(t, cookie)
	assert.Equal(t, "JSESSIONID", cookie.Name)
	assert.Equal(t, "testcookie123", cookie.Value)

	// Server 2: Returns NO cookie (must not panic!)
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer s2.Close()

	cookie2, err := GetCookieWithClient(client, s2.URL)
	require.NoError(t, err)
	assert.Nil(t, cookie2) // Safe nil return without panic
}

func TestLoginWithClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/InterFace.do"))
		assert.Equal(t, "method=login", r.URL.RawQuery)

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		bodyStr := string(body)

		if strings.Contains(bodyStr, "userId=userSuccess") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"result":"success","userIndex":"idx12345"}`)
		} else {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"result":"fail","message":"account already online"}`)
		}
	}))
	defer server.Close()

	client := server.Client()
	cookie := &http.Cookie{Name: "JSESSIONID", Value: "testsession"}

	// 1. Success case
	res1, err := loginWithClient(client, server.URL+"/eportal/index.jsp", "wlanuserip=1.2.3.4", Account{
		Account:     "userSuccess",
		Password:    "pwd123",
		ServiceType: "internet",
	}, cookie)
	require.NoError(t, err)
	assert.Contains(t, res1, `"result":"success"`)

	// 2. Fail case
	res2, err := loginWithClient(client, server.URL+"/eportal/index.jsp", "wlanuserip=1.2.3.4", Account{
		Account:     "userFail",
		Password:    "pwd123",
		ServiceType: "internet",
	}, cookie)
	require.NoError(t, err)
	assert.Contains(t, res2, `"result":"fail"`)
}

func TestMultiAccountFailover(t *testing.T) {
	// Mock ePortal server:
	// "user1" fails with session limit
	// "user2" succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)

		if strings.Contains(bodyStr, "userId=user1") {
			fmt.Fprint(w, `{"result":"fail","message":"max sessions exceeded"}`)
		} else if strings.Contains(bodyStr, "userId=user2") {
			fmt.Fprint(w, `{"result":"success","userIndex":"idx_user2"}`)
		} else {
			fmt.Fprint(w, `{"result":"fail","message":"unknown account"}`)
		}
	}))
	defer server.Close()

	client := server.Client()
	pool := NewAccountPool([]Account{
		{Account: "user1", Password: "p1"},
		{Account: "user2", Password: "p2"},
	}, 10*time.Minute, 2*time.Hour)

	url := server.URL + "/eportal/index.jsp"
	queryString := "param=val"

	// Simulate failover logic
	var loggedInAccount string
	total := pool.AccountsCount()
	for i := 0; i < total; i++ {
		cand, err := pool.GetNextCandidate()
		require.NoError(t, err)

		loginRes, err := loginWithClient(client, url, queryString, cand.Account, nil)
		require.NoError(t, err)

		if strings.Contains(loginRes, `"result":"success"`) {
			pool.MarkActive(cand)
			loggedInAccount = cand.Account.Account
			break
		} else {
			pool.MarkFailed(cand, "login failed")
		}
	}

	// Verify user1 failed and is in cooldown
	assert.Equal(t, "user2", loggedInAccount)
	assert.Equal(t, "user2", pool.ActiveAccount().Account.Account)

	// user1 should be in cooldown
	user1Candidate, err := pool.GetNextCandidate()
	require.NoError(t, err)
	// Since user1 is in cooldown, the next candidate must be user2
	assert.Equal(t, "user2", user1Candidate.Account.Account)
}
