package cmd

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveInterface(t *testing.T) {
	// Empty specifier should return empty without error
	name, ip, err := ResolveInterface("")
	require.NoError(t, err)
	assert.Empty(t, name)
	assert.Nil(t, ip)

	// Local loopback IP
	name, ip, err = ResolveInterface("127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, net.ParseIP("127.0.0.1").To4(), ip.To4())
	_ = name

	// Non-existent interface name should return error
	_, _, err = ResolveInterface("non_existent_iface_999")
	assert.Error(t, err)
}

func TestNewHTTPClient(t *testing.T) {
	// Default client with no interface
	client, err := NewHTTPClient("", 3*time.Second)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, 3*time.Second, client.Timeout)

	// Client with loopback IP
	client, err = NewHTTPClient("127.0.0.1", 2*time.Second)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, 2*time.Second, client.Timeout)
}

func TestTLSCompatibility_RSAKeyExchange(t *testing.T) {
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	ts.TLS = &tls.Config{
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
		MaxVersion:   tls.VersionTLS12,
	}
	ts.StartTLS()
	defer ts.Close()

	client, err := NewHTTPClient("", 3*time.Second)
	require.NoError(t, err)

	resp, err := client.Get(ts.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestTLSCompatibility_LegacyVersions(t *testing.T) {
	for _, ver := range []uint16{tls.VersionTLS10, tls.VersionTLS11, tls.VersionTLS12} {
		t.Run(fmt.Sprintf("TLS_%x", ver), func(t *testing.T) {
			ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			}))
			ts.TLS = &tls.Config{
				CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
				MinVersion:   ver,
				MaxVersion:   ver,
			}
			ts.StartTLS()
			defer ts.Close()

			client, err := NewHTTPClient("", 3*time.Second)
			require.NoError(t, err)

			resp, err := client.Get(ts.URL)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

func TestTLSCompatibility_InsecureVerifyFlag(t *testing.T) {
	origInsecure := insecure
	defer func() {
		insecure = origInsecure
	}()

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// 1. When insecure = true (default), self-signed cert is accepted
	insecure = true
	client1, err := NewHTTPClient("", 3*time.Second)
	require.NoError(t, err)
	resp1, err := client1.Get(ts.URL)
	require.NoError(t, err)
	resp1.Body.Close()

	// 2. When insecure = false, self-signed cert is rejected
	insecure = false
	client2, err := NewHTTPClient("", 3*time.Second)
	require.NoError(t, err)
	_, err = client2.Get(ts.URL)
	assert.Error(t, err)
}

