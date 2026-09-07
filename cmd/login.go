/*
Copyright © 2022 a76yyyy q981331502@163.com
*/

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var register bool

// loginCmd represents the login command
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Hust web auth only once",
	Long:  `Hust web auth only once.`,
	Run: func(cmd *cobra.Command, args []string) {
		res, err := Login()
		if err != nil {
			log.Fatal(err)
		}
		if res != "" {
			log.Println(res)
		}
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
	loginCmd.PersistentFlags().BoolVarP(&register, "register", "r", false, "Register Mac address")
}

func ifaceTag(ifaceName string) string {
	if ifaceName == "" {
		return "Default"
	}
	return ifaceName
}

// GetCookie gets cookie of the auth page using the default client.
func GetCookie(url string) (*http.Cookie, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	return GetCookieWithClient(client, url)
}

// GetCookieWithClient gets cookie of the auth page using the specified client.
func GetCookieWithClient(client *http.Client, url string) (*http.Cookie, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return nil, nil
	}
	return cookies[0], nil
}

// login performs the HTTP post request to eportal login interface.
func loginWithClient(client *http.Client, url string, queryString string, acc Account, cookie *http.Cookie) (string, error) {
	trueurl := strings.Split(url, "/eportal/")[0] + "/eportal/InterFace.do?method=login"

	var passwordEncrypt string
	isEncrypt := encrypt
	if acc.Encrypt != nil {
		isEncrypt = *acc.Encrypt
	}
	if isEncrypt {
		passwordEncrypt = "true"
	} else {
		passwordEncrypt = "false"
	}

	svcType := serviceType
	if acc.ServiceType != "" {
		svcType = acc.ServiceType
	}

	data := "userId=" + acc.Account +
		"&password=" + acc.Password +
		"&service=" + svcType +
		"&queryString=" + queryString +
		"&operatorPwd=&operatorUserId=&validcode=&passwordEncrypt=" + passwordEncrypt

	req, err := http.NewRequest("POST", trueurl, strings.NewReader(data))
	if err != nil {
		return "", err
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/98.0.4758.102 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

// RegisterMAC registers the mac address, only for the first time.
func RegisterMAC(url string, userIndex string, cookie *http.Cookie) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	return RegisterMACWithClient(client, url, userIndex, cookie)
}

// RegisterMACWithClient registers the mac address using the specified client.
func RegisterMACWithClient(client *http.Client, url string, userIndex string, cookie *http.Cookie) (string, error) {
	trueurl := strings.Split(url, "/eportal/")[0] + "/eportal/InterFace.do?method=registerMac"
	data := "mac=&userIndex=" + userIndex
	req, err := http.NewRequest("POST", trueurl, strings.NewReader(data))
	if err != nil {
		return "", err
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/98.0.4758.102 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

// Login performs Ruijie web auth once using default pool and interface.
func Login() (res string, err error) {
	pool := getDefaultAccountPool()
	return LoginWithInterface(iface, pool, register)
}

// LoginWithInterface handles ping detection, kicked-out exponential backoff, and multi-account rotation on a specific interface.
func LoginWithInterface(ifaceName string, pool *AccountPool, doRegister bool, targetPingIP ...string) (res string, err error) {
	url, queryString, connected, err := GetLoginUrlWithInterface(ifaceName, targetPingIP...)
	if err != nil {
		return "", err
	}

	if connected {
		pool.ConfirmConnected()
		if logConnected {
			return "The network is connected, no authentication required", nil
		}
		return "", nil
	}

	// If network was previously connected, but now disconnected -> kicked offline!
	if pool.WasConnected() {
		pool.MarkKicked()
	}

	client, err := NewHTTPClient(ifaceName, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("[%s] failed to create HTTP client: %w", ifaceTag(ifaceName), err)
	}

	cookie, err := GetCookieWithClient(client, url)
	if err != nil {
		return "", fmt.Errorf("[%s] failed to get cookie: %w", ifaceTag(ifaceName), err)
	}

	totalAccounts := pool.AccountsCount()
	if totalAccounts == 0 {
		return "", errors.New("no accounts available for authentication")
	}

	maxAttempts := totalAccounts
	if !rotationEnable && maxAttempts > 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		candidate, err := pool.GetNextCandidate()
		if err != nil {
			return "", err // all accounts in cooldown or empty
		}

		log.Printf("[%s] Attempting login with account: %s (fail count: %d)\n",
			ifaceTag(ifaceName), candidate.Account.Account, candidate.ConsecutiveFails)

		loginRes, err := loginWithClient(client, url, queryString, candidate.Account, cookie)
		if err != nil {
			pool.MarkFailed(candidate, err.Error())
			lastErr = err
			continue
		}

		if strings.Contains(loginRes, "\"result\":\"success\"") {
			pool.MarkActive(candidate)
			log.Printf("[%s] Account %s login success!\n", ifaceTag(ifaceName), candidate.Account.Account)
			res = "Login success!"

			if doRegister {
				var resJson map[string]interface{}
				if err := json.Unmarshal([]byte(loginRes), &resJson); err == nil {
					if userIndex, ok := resJson["userIndex"].(string); ok {
						regRes, err := RegisterMACWithClient(client, url, userIndex, cookie)
						if err != nil {
							return "", err
						}
						return regRes, nil
					}
				}
				return "Unsupport register service. ", nil
			}
			return res, nil
		}

		// Failed login response from Ruijie
		failMsg := loginRes
		var resJson map[string]interface{}
		if err := json.Unmarshal([]byte(loginRes), &resJson); err == nil {
			if msg, ok := resJson["message"].(string); ok && msg != "" {
				failMsg = msg
			}
		}

		pool.MarkFailed(candidate, failMsg)
		lastErr = fmt.Errorf("login fail (%s): %s", candidate.Account.Account, failMsg)
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("all candidate accounts failed or entered cooldown")
}
