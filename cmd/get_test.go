package cmd

import (
	"testing"

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
