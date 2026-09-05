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
		url, queryString, connected, err := GetLoginUrlWithInterface(iface)
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
func GetLoginUrlWithInterface(ifaceName string) (string, string, bool, error) {
	_, ip, err := ResolveInterface(ifaceName)
	if err != nil && ifaceName != "" {
		return "", "", false, fmt.Errorf("resolve interface %q failed: %w", ifaceName, err)
	}

	pinger, err := ping.NewPinger(pingIP)
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
	res := string(body)

	// Safe URL extraction without crashing on missing single quotes
	var rawURL string
	if parts := strings.Split(res, "'"); len(parts) >= 3 {
		rawURL = parts[1]
	} else if partsQuote := strings.Split(res, "\""); len(partsQuote) >= 3 {
		for _, p := range partsQuote {
			if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
				rawURL = p
				break
			}
		}
	}

	if rawURL == "" {
		// Fallback: check if the body directly starts with or contains http
		if strings.Contains(res, "http://") || strings.Contains(res, "https://") {
			for _, token := range strings.Fields(res) {
				if strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://") {
					rawURL = strings.Trim(token, `'"<>;`)
					break
				}
			}
		}
	}

	if rawURL == "" {
		return "", "", false, fmt.Errorf("unable to extract login redirect URL from response: %s", res)
	}

	urlParts := strings.Split(rawURL, "?")
	if len(urlParts) < 2 {
		return rawURL, "", false, nil
	}

	url := rawURL
	queryString := urlutil.QueryEscape(urlParts[1])
	return url, queryString, false, nil
}
