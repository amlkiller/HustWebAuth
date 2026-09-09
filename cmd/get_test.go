package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractRedirectURL(t *testing.T) {
	// Case 1: Single quote attribute precedes the URL in single quotes
	htmlWithPrecedingQuote := `<script type='text/javascript'>location.href='http://1.2.3.4:8080/eportal/index.jsp?wlanuserip=10.0.0.1&wlanacname=hust';</script>`
	url, qs, err := extractRedirectURL(htmlWithPrecedingQuote)
	require.NoError(t, err)
	assert.Equal(t, "http://1.2.3.4:8080/eportal/index.jsp?wlanuserip=10.0.0.1&wlanacname=hust", url)
	assert.Equal(t, "wlanuserip%3D10.0.0.1%26wlanacname%3Dhust", qs)

	// Case 2: Double quoted URL
	htmlDoubleQuote := `<script type="text/javascript">window.location.href="https://auth.hust.edu.cn/eportal/index.jsp?param=val";</script>`
	url, qs, err = extractRedirectURL(htmlDoubleQuote)
	require.NoError(t, err)
	assert.Equal(t, "https://auth.hust.edu.cn/eportal/index.jsp?param=val", url)
	assert.Equal(t, "param%3Dval", qs)

	// Case 3: URL containing secondary question mark in query string
	htmlWithSecondaryQuestionMark := `<script>location.href='http://auth.hust.edu.cn/eportal/index.jsp?param1=val1?param2=val2';</script>`
	url, qs, err = extractRedirectURL(htmlWithSecondaryQuestionMark)
	require.NoError(t, err)
	assert.Equal(t, "http://auth.hust.edu.cn/eportal/index.jsp?param1=val1?param2=val2", url)
	assert.Equal(t, "param1%3Dval1%3Fparam2%3Dval2", qs)

	// Case 4: Fallback plain text token
	plainText := `Please authenticate at http://1.2.3.4/eportal/index.jsp?user=none immediately`
	url, qs, err = extractRedirectURL(plainText)
	require.NoError(t, err)
	assert.Equal(t, "http://1.2.3.4/eportal/index.jsp?user=none", url)
	assert.Equal(t, "user%3Dnone", qs)

	// Case 5: URL without query parameters
	noParams := `<script>location.href='http://1.2.3.4/eportal/index.jsp';</script>`
	url, qs, err = extractRedirectURL(noParams)
	require.NoError(t, err)
	assert.Equal(t, "http://1.2.3.4/eportal/index.jsp", url)
	assert.Empty(t, qs)

	// Case 6: No URL in body
	noURL := `<html><body>404 Not Found</body></html>`
	_, _, err = extractRedirectURL(noURL)
	assert.Error(t, err)
}

func TestGetLoginUrlWithInterface_Connected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	url, qs, connected, err := GetLoginUrlWithInterface("", ts.URL)
	require.NoError(t, err)
	assert.True(t, connected)
	assert.Empty(t, url)
	assert.Empty(t, qs)
}

func TestGetLoginUrlWithInterface_PortalRedirectLocation(t *testing.T) {
	targetURL := "http://172.18.2.3/eportal/index.jsp?wlanuserip=10.0.0.1&wlanacname=hust"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetURL, http.StatusFound)
	}))
	defer ts.Close()

	url, qs, connected, err := GetLoginUrlWithInterface("", ts.URL)
	require.NoError(t, err)
	assert.False(t, connected)
	assert.Equal(t, targetURL, url)
	assert.Equal(t, "wlanuserip%3D10.0.0.1%26wlanacname%3Dhust", qs)
}

func TestGetLoginUrlWithInterface_PortalHTMLBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<script>location.href='http://10.10.10.1/eportal/index.jsp?ip=1.1.1.1';</script>`)
	}))
	defer ts.Close()

	url, qs, connected, err := GetLoginUrlWithInterface("", ts.URL)
	require.NoError(t, err)
	assert.False(t, connected)
	assert.Equal(t, "http://10.10.10.1/eportal/index.jsp?ip=1.1.1.1", url)
	assert.Equal(t, "ip%3D1.1.1.1", qs)
}

func TestGetLoginUrlWithInterface_FallbackToRedirectURL(t *testing.T) {
	// Fallback server simulating 123.123.123.123
	fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<script>location.href='http://10.10.10.1/eportal/index.jsp?ip=1.1.1.1';</script>`)
	}))
	defer fallbackServer.Close()

	origRedirectURL := redirectURL
	origCheckTimeout := checkTimeout
	defer func() {
		redirectURL = origRedirectURL
		checkTimeout = origCheckTimeout
	}()
	redirectURL = fallbackServer.URL
	checkTimeout = 1 * time.Second

	// Closed server to simulate probe failure (e.g. DNS or connection timeout)
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedServer.Close() // Immediately close it

	url, qs, connected, err := GetLoginUrlWithInterface("", closedServer.URL)
	require.NoError(t, err)
	assert.False(t, connected)
	assert.Equal(t, "http://10.10.10.1/eportal/index.jsp?ip=1.1.1.1", url)
	assert.Equal(t, "ip%3D1.1.1.1", qs)
}

func TestInterfaceConfig_GetCheckURL(t *testing.T) {
	origCheckURL := checkURL
	defer func() {
		checkURL = origCheckURL
	}()
	checkURL = "http://connect.rom.miui.com/generate_204"

	// 1. Explicit CheckURL set
	cfg1 := InterfaceConfig{
		CheckURL: "http://custom.com/204",
		PingIP:   "202.114.0.131",
	}
	assert.Equal(t, "http://custom.com/204", cfg1.GetCheckURL())

	// 2. No CheckURL, but PingIP has http(s) URL
	cfg2 := InterfaceConfig{
		PingIP: "http://hicloud.com/generate_204",
	}
	assert.Equal(t, "http://hicloud.com/generate_204", cfg2.GetCheckURL())

	// 3. No CheckURL, and PingIP is legacy IP address -> fallback to global checkURL
	cfg3 := InterfaceConfig{
		PingIP: "202.114.0.131",
	}
	assert.Equal(t, "http://connect.rom.miui.com/generate_204", cfg3.GetCheckURL())

	// 4. Empty config -> fallback to global checkURL
	cfg4 := InterfaceConfig{}
	assert.Equal(t, "http://connect.rom.miui.com/generate_204", cfg4.GetCheckURL())
}
