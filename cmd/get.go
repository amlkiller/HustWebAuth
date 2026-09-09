/*
Copyright © 2022 a76yyyy q981331502@163.com

*/

package cmd

import (
	"fmt"
	"io"
	"log"
	"net/http"
	urlutil "net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// getCmd represents the get command
var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Get the login url from the redirect url",
	Long:  `Check connectivity via HTTP 204 endpoint and get the login_url from the redirect_url if unauthenticated`,
	Run: func(cmd *cobra.Command, args []string) {
		targetCheckURL := ""
		if iface != "" {
			for _, ifc := range configuredInterfaces {
				if ifc.Iface == iface {
					targetCheckURL = ifc.GetCheckURL()
					break
				}
			}
		}
		url, queryString, connected, err := GetLoginUrlWithInterface(iface, targetCheckURL)
		if err != nil {
			log.Fatal(err.Error())
		}
		if connected {
			log.Println("The network is connected, no authentication required")
		} else {
			log.Println("The login url is: ", url)
			log.Println("The query string is: ", queryString)
		}
	},
}

func init() {
	rootCmd.AddCommand(getCmd)
}

// GetLoginUrl gets the login url from the redirect url using the default interface.
func GetLoginUrl() (string, string, bool, error) {
	return GetLoginUrlWithInterface(iface)
}

// parseRedirectURL extracts the base URL and query-escaped params from a full URL string.
func parseRedirectURL(rawURL string) (string, string) {
	urlParts := strings.SplitN(rawURL, "?", 2)
	if len(urlParts) < 2 {
		return rawURL, ""
	}
	return rawURL, urlutil.QueryEscape(urlParts[1])
}

// GetLoginUrlWithInterface checks network connectivity and extracts the login url bound to the specified interface.
func GetLoginUrlWithInterface(ifaceName string, targetCheckURL ...string) (string, string, bool, error) {
	endpoint := checkURL
	if len(targetCheckURL) > 0 && targetCheckURL[0] != "" {
		endpoint = targetCheckURL[0]
	}
	if endpoint == "" {
		endpoint = "http://connect.rom.miui.com/generate_204"
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}

	timeout := checkTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	client, err := NewHTTPClient(ifaceName, timeout)
	if err != nil {
		return "", "", false, err
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// 1. Probe connectivity using HTTP 204 endpoint bound to the interface
	resp, probeErr := client.Get(endpoint)
	if probeErr == nil {
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNoContent {
			return "", "", true, nil
		}

		// If redirected or hijacked by captive portal, attempt to extract login URL
		if loc := resp.Header.Get("Location"); loc != "" {
			if parsedLoc, parseErr := urlutil.Parse(loc); parseErr == nil {
				if !parsedLoc.IsAbs() && resp.Request != nil && resp.Request.URL != nil {
					parsedLoc = resp.Request.URL.ResolveReference(parsedLoc)
				}
				u, qs := parseRedirectURL(parsedLoc.String())
				return u, qs, false, nil
			}
		}

		body, readErr := io.ReadAll(resp.Body)
		if readErr == nil && len(body) > 0 {
			if u, qs, extractErr := extractRedirectURL(string(body)); extractErr == nil && u != "" {
				return u, qs, false, nil
			}
		}
	}

	// 2. Fallback: if probe failed (e.g. DNS resolution failure before login) or didn't provide login URL,
	// query redirectURL (http://123.123.123.123) which bypasses DNS
	respFallback, errFallback := client.Get(redirectURL)
	if errFallback != nil {
		if probeErr != nil {
			return "", "", false, fmt.Errorf("connectivity check (%s) failed: %v; fallback to redirectURL (%s) failed: %w", endpoint, probeErr, redirectURL, errFallback)
		}
		return "", "", false, fmt.Errorf("probe did not return 204 and fallback to redirectURL (%s) failed: %w", redirectURL, errFallback)
	}
	defer respFallback.Body.Close()

	if respFallback.StatusCode == http.StatusNoContent {
		return "", "", true, nil
	}

	if loc := respFallback.Header.Get("Location"); loc != "" {
		if parsedLoc, parseErr := urlutil.Parse(loc); parseErr == nil {
			if !parsedLoc.IsAbs() && respFallback.Request != nil && respFallback.Request.URL != nil {
				parsedLoc = respFallback.Request.URL.ResolveReference(parsedLoc)
			}
			u, qs := parseRedirectURL(parsedLoc.String())
			return u, qs, false, nil
		}
	}

	bodyFallback, errFallback := io.ReadAll(respFallback.Body)
	if errFallback != nil {
		return "", "", false, errFallback
	}

	u, qs, errExtract := extractRedirectURL(string(bodyFallback))
	if errExtract != nil {
		return "", "", false, errExtract
	}
	return u, qs, false, nil
}

// extractRedirectURL extracts the redirect URL and escaped query string from the raw response body.
func extractRedirectURL(res string) (url string, queryString string, err error) {
	var rawURL string

	// 1. Search in single quotes
	if strings.Contains(res, "'") {
		for _, p := range strings.Split(res, "'") {
			if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
				rawURL = p
				break
			}
		}
	}

	// 2. Search in double quotes
	if rawURL == "" && strings.Contains(res, "\"") {
		for _, p := range strings.Split(res, "\"") {
			if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
				rawURL = p
				break
			}
		}
	}

	// 3. Fallback: search in space-separated tokens
	if rawURL == "" && (strings.Contains(res, "http://") || strings.Contains(res, "https://")) {
		for _, token := range strings.Fields(res) {
			cleanToken := strings.Trim(token, `'"<>;=()`)
			if strings.HasPrefix(cleanToken, "http://") || strings.HasPrefix(cleanToken, "https://") {
				rawURL = cleanToken
				break
			}
		}
	}

	if rawURL == "" {
		return "", "", fmt.Errorf("unable to extract login redirect URL from response: %s", res)
	}

	u, qs := parseRedirectURL(rawURL)
	return u, qs, nil
}
