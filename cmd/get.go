/*
Copyright © 2022 a76yyyy q981331502@163.com

*/

package cmd

import (
	"fmt"
	"io"
	"log"
	urlutil "net/url"
	"strings"
	"time"

	ping "github.com/prometheus-community/pro-bing"
	"github.com/spf13/cobra"
)

// getCmd represents the get command
var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Get the login url from the redirect url",
	Long:  `If the specified IP fails to be pinged for more than the specified counts, get the login_url from the redirect_url`,
	Run: func(cmd *cobra.Command, args []string) {
		targetPingIP := ""
		if iface != "" {
			for _, ifc := range configuredInterfaces {
				if ifc.Iface == iface && ifc.PingIP != "" {
					targetPingIP = ifc.PingIP
					break
				}
			}
		}
		url, queryString, connected, err := GetLoginUrlWithInterface(iface, targetPingIP)
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

// GetLoginUrlWithInterface gets the login url from the redirect url, bound to the given interface.
func GetLoginUrlWithInterface(ifaceName string, targetPingIP ...string) (string, string, bool, error) {
	targetIP := pingIP
	if len(targetPingIP) > 0 && targetPingIP[0] != "" {
		targetIP = targetPingIP[0]
	}

	_, ip, err := ResolveInterface(ifaceName)
	if err != nil && ifaceName != "" {
		return "", "", false, fmt.Errorf("resolve interface %q failed: %w", ifaceName, err)
	}

	pinger, err := ping.NewPinger(targetIP)
	if err != nil {
		return "", "", false, err
	}
	pinger.Count = pingCount
	pinger.Timeout = pingTimeout
	pinger.SetPrivileged(pingPrivilege)
	if ip != nil {
		pinger.Source = ip.String()
	}

	if err = pinger.Run(); err != nil { // Blocks until finished.
		return "", "", false, err
	}
	if stats := pinger.Statistics(); stats.PacketLoss < 100.0 { // get send/receive/duplicate/rtt stats
		return "", "", true, nil
	}

	client, err := NewHTTPClient(ifaceName, 5*time.Second)
	if err != nil {
		return "", "", false, err
	}

	resp, err := client.Get(redirectURL)
	if err != nil {
		return "", "", false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", false, err
	}

	url, queryString, err := extractRedirectURL(string(body))
	if err != nil {
		return "", "", false, err
	}
	return url, queryString, false, nil
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

	urlParts := strings.SplitN(rawURL, "?", 2)
	if len(urlParts) < 2 {
		return rawURL, "", nil
	}

	return rawURL, urlutil.QueryEscape(urlParts[1]), nil
}
