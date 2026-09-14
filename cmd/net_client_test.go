package cmd

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync"
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

func TestTransportReuse(t *testing.T) {
	defer ResetHTTPTransportPool()

	// 1. Same parameters return the exact same *http.Transport instance
	tr1, err := GetOrCreateHTTPTransport("", 3*time.Second)
	require.NoError(t, err)
	tr2, err := GetOrCreateHTTPTransport("", 3*time.Second)
	require.NoError(t, err)
	assert.Same(t, tr1, tr2, "GetOrCreateHTTPTransport must return the same *http.Transport for identical keys")

	// 2. NewHTTPClient and GetHTTPClient share the same *http.Transport
	client1, err := NewHTTPClient("", 3*time.Second)
	require.NoError(t, err)
	client2, err := GetHTTPClient("", 3*time.Second, true)
	require.NoError(t, err)
	client3, err := GetHTTPClient("", 3*time.Second, false)
	require.NoError(t, err)

	assert.Same(t, tr1, client1.Transport)
	assert.Same(t, tr1, client2.Transport)
	assert.Same(t, tr1, client3.Transport)

	// 3. Verify that CheckRedirect is independent across client wrappers
	assert.Nil(t, client1.CheckRedirect, "NewHTTPClient should not have custom CheckRedirect by default")
	assert.Nil(t, client2.CheckRedirect, "GetHTTPClient(..., true) should not have custom CheckRedirect")
	require.NotNil(t, client3.CheckRedirect, "GetHTTPClient(..., false) must have custom CheckRedirect")
	assert.Equal(t, http.ErrUseLastResponse, client3.CheckRedirect(nil, nil))

	// 4. Verify actual TCP connection reuse using httptrace
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	var reusedFirst, reusedSecond bool
	traceFirst := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			reusedFirst = connInfo.Reused
		},
	}
	req1, err := http.NewRequest("GET", ts.URL, nil)
	require.NoError(t, err)
	req1 = req1.WithContext(httptrace.WithClientTrace(req1.Context(), traceFirst))
	resp1, err := client1.Do(req1)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()
	assert.False(t, reusedFirst, "First HTTP request should establish a brand new connection")

	traceSecond := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			reusedSecond = connInfo.Reused
		},
	}
	req2, err := http.NewRequest("GET", ts.URL, nil)
	require.NoError(t, err)
	req2 = req2.WithContext(httptrace.WithClientTrace(req2.Context(), traceSecond))
	resp2, err := client3.Do(req2)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	assert.True(t, reusedSecond, "Second HTTP request via shared transport must reuse keep-alive connection")
}

func TestCloseIdleHTTPConnections(t *testing.T) {
	defer ResetHTTPTransportPool()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	client, err := NewHTTPClient("", 3*time.Second)
	require.NoError(t, err)

	// First request creates connection
	resp1, err := client.Get(ts.URL)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()

	// Second request reuses connection
	var reusedBeforeClose bool
	trace1 := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			reusedBeforeClose = connInfo.Reused
		},
	}
	req1, err := http.NewRequest("GET", ts.URL, nil)
	require.NoError(t, err)
	req1 = req1.WithContext(httptrace.WithClientTrace(req1.Context(), trace1))
	resp2, err := client.Do(req1)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	assert.True(t, reusedBeforeClose, "Connection should be reused before CloseIdleHTTPConnections")

	// Close all idle connections
	CloseIdleHTTPConnections()

	// Third request must dial a new connection because idle connections were closed
	var reusedAfterClose bool
	trace2 := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			reusedAfterClose = connInfo.Reused
		},
	}
	req2, err := http.NewRequest("GET", ts.URL, nil)
	require.NoError(t, err)
	req2 = req2.WithContext(httptrace.WithClientTrace(req2.Context(), trace2))
	resp3, err := client.Do(req2)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp3.Body)
	resp3.Body.Close()
	assert.False(t, reusedAfterClose, "Connection should NOT be reused immediately after CloseIdleHTTPConnections")

	// Fourth request should reuse the connection established in third request
	var reusedFourth bool
	trace3 := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			reusedFourth = connInfo.Reused
		},
	}
	req3, err := http.NewRequest("GET", ts.URL, nil)
	require.NoError(t, err)
	req3 = req3.WithContext(httptrace.WithClientTrace(req3.Context(), trace3))
	resp4, err := client.Do(req3)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp4.Body)
	resp4.Body.Close()
	assert.True(t, reusedFourth, "Subsequent request should reuse the newly established keep-alive connection")
}

func TestTransportConcurrency(t *testing.T) {
	defer ResetHTTPTransportPool()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("concurrency ok"))
	}))
	defer ts.Close()

	const workers = 30
	const iterations = 10

	var wg sync.WaitGroup
	errCh := make(chan error, workers*iterations)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Alternately test NewHTTPClient, GetHTTPClient follow, and GetHTTPClient no-follow
				var client *http.Client
				var err error
				switch j % 3 {
				case 0:
					client, err = NewHTTPClient("", 3*time.Second)
				case 1:
					client, err = GetHTTPClient("", 3*time.Second, true)
				case 2:
					client, err = GetHTTPClient("", 3*time.Second, false)
				}
				if err != nil {
					errCh <- fmt.Errorf("worker %d iter %d: %w", workerID, j, err)
					return
				}

				resp, err := client.Get(ts.URL)
				if err != nil {
					errCh <- fmt.Errorf("worker %d iter %d request: %w", workerID, j, err)
					return
				}
				resp.Body.Close()

				if j == 5 && workerID == 0 {
					CloseIdleHTTPConnections()
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		assert.NoError(t, err)
	}
}

func TestGetHTTPClient_RedirectPolicy(t *testing.T) {
	defer ResetHTTPTransportPool()

	targetURL := "/target"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, targetURL, http.StatusFound)
			return
		}
		if r.URL.Path == targetURL {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("reached target"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// Client with followRedirect = false should receive the 302 redirect response directly
	clientNoFollow, err := GetHTTPClient("", 3*time.Second, false)
	require.NoError(t, err)
	resp1, err := clientNoFollow.Get(ts.URL + "/redirect")
	require.NoError(t, err)
	defer resp1.Body.Close()
	assert.Equal(t, http.StatusFound, resp1.StatusCode)
	assert.Equal(t, targetURL, resp1.Header.Get("Location"))

	// Client with followRedirect = true should follow the redirect to /target and get 200 OK
	clientFollow, err := GetHTTPClient("", 3*time.Second, true)
	require.NoError(t, err)
	resp2, err := clientFollow.Get(ts.URL + "/redirect")
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	// Both clients must share the exact same underlying *http.Transport
	assert.Same(t, clientNoFollow.Transport, clientFollow.Transport)
}

func TestGetOrCreateHTTPTransport_InvalidInterface(t *testing.T) {
	defer ResetHTTPTransportPool()

	tr, err := GetOrCreateHTTPTransport("non_existent_iface_999", 3*time.Second)
	assert.Error(t, err)
	assert.Nil(t, tr)

	// Ensure invalid interface was not cached
	transportMu.RLock()
	_, exists := transportPool[transportKey{iface: "non_existent_iface_999", timeout: 3 * time.Second, insecure: insecure}]
	transportMu.RUnlock()
	assert.False(t, exists, "Invalid interface should not be cached in transportPool")
}

func TestResetHTTPTransportPool(t *testing.T) {
	defer ResetHTTPTransportPool()

	tr1, err := GetOrCreateHTTPTransport("", 3*time.Second)
	require.NoError(t, err)
	require.NotNil(t, tr1)

	ResetHTTPTransportPool()

	tr2, err := GetOrCreateHTTPTransport("", 3*time.Second)
	require.NoError(t, err)
	require.NotNil(t, tr2)

	assert.NotSame(t, tr1, tr2, "After ResetHTTPTransportPool, a new *http.Transport should be created")
}

