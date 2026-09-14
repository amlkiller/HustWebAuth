package cmd

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"
)

// transportKey uniquely identifies a cached *http.Transport instance.
type transportKey struct {
	iface    string
	timeout  time.Duration
	insecure bool
}

var (
	transportPool = make(map[transportKey]*http.Transport)
	transportMu   sync.RWMutex
)

// allCipherSuites contains all supported cipher suites (both modern and legacy/insecure ones).
var allCipherSuites = func() []uint16 {
	var suites []uint16
	for _, cs := range tls.CipherSuites() {
		suites = append(suites, cs.ID)
	}
	for _, cs := range tls.InsecureCipherSuites() {
		suites = append(suites, cs.ID)
	}
	return suites
}()

// GetDefaultTLSConfig returns a tls.Config with broad compatibility for legacy captive portal servers.
func GetDefaultTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: insecure,
		MinVersion:         tls.VersionTLS10,
		CipherSuites:       allCipherSuites,
		Renegotiation:      tls.RenegotiateOnceAsClient,
	}
}

// ResolveInterface resolves an interface name or IP string into an interface name and IPv4 address.
func ResolveInterface(ifaceSpec string) (ifaceName string, ip net.IP, err error) {
	if ifaceSpec == "" {
		return "", nil, nil
	}

	// Case 1: Check if ifaceSpec is an IP address directly
	if parsedIP := net.ParseIP(ifaceSpec); parsedIP != nil {
		ip4 := parsedIP.To4()
		if ip4 != nil {
			ip = ip4
		} else {
			ip = parsedIP
		}

		// Try to find if any local interface owns this IP
		ifaces, err := net.Interfaces()
		if err == nil {
			for _, iface := range ifaces {
				addrs, err := iface.Addrs()
				if err != nil {
					continue
				}
				for _, addr := range addrs {
					var currIP net.IP
					switch v := addr.(type) {
					case *net.IPNet:
						currIP = v.IP
					case *net.IPAddr:
						currIP = v.IP
					}
					if currIP != nil && currIP.Equal(parsedIP) {
						ifaceName = iface.Name
						return ifaceName, ip, nil
					}
				}
			}
		}
		return "", ip, nil
	}

	// Case 2: ifaceSpec is an interface name (e.g. eth0, vwan1, macvlan0)
	iface, err := net.InterfaceByName(ifaceSpec)
	if err != nil {
		return "", nil, fmt.Errorf("interface %q not found: %w", ifaceSpec, err)
	}
	ifaceName = iface.Name

	addrs, err := iface.Addrs()
	if err == nil {
		for _, addr := range addrs {
			var currIP net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				currIP = v.IP
			case *net.IPAddr:
				currIP = v.IP
			}
			if currIP != nil && currIP.To4() != nil {
				ip = currIP.To4()
				break
			}
		}
	}

	return ifaceName, ip, nil
}

// GetOrCreateHTTPTransport retrieves an existing *http.Transport from the pool or creates a new one.
// The transport is thread-safe and reuses underlying TCP connections, TLS sessions, and socket bindings.
func GetOrCreateHTTPTransport(iface string, timeout time.Duration) (*http.Transport, error) {
	iface = strings.TrimSpace(iface)
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	key := transportKey{
		iface:    iface,
		timeout:  timeout,
		insecure: insecure,
	}

	transportMu.RLock()
	tr, ok := transportPool[key]
	transportMu.RUnlock()
	if ok {
		return tr, nil
	}

	transportMu.Lock()
	defer transportMu.Unlock()

	// Double-check after acquiring write lock
	if tr, ok := transportPool[key]; ok {
		return tr, nil
	}

	ifaceName, ip, err := ResolveInterface(iface)
	if err != nil && iface != "" {
		return nil, err
	}

	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}

	if ip != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: ip}
	}

	if ifaceName != "" {
		dialer.Control = func(network, address string, c syscall.RawConn) error {
			var controlErr error
			err := c.Control(func(fd uintptr) {
				controlErr = bindSocketToDevice(fd, ifaceName)
			})
			if err != nil {
				return err
			}
			if controlErr != nil {
				// If operation not permitted (e.g. non-root on linux), log warning and fall back to IP binding if available
				if errors.Is(controlErr, syscall.EPERM) && ip != nil {
					log.Printf("Warning: SO_BINDTODEVICE on %s requires CAP_NET_RAW/root: %v (falling back to LocalAddr %s)\n", ifaceName, controlErr, ip)
					return nil
				}
				return controlErr
			}
			return nil
		}
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       GetDefaultTLSConfig(),
	}

	transportPool[key] = transport
	return transport, nil
}

// GetHTTPClient returns an *http.Client configured with the specified interface, timeout,
// and redirect behavior, backed by a cached and reused *http.Transport connection pool.
func GetHTTPClient(iface string, timeout time.Duration, followRedirect bool) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	tr, err := GetOrCreateHTTPTransport(iface, timeout)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   timeout,
	}

	if !followRedirect {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	return client, nil
}

// NewHTTPClient returns an *http.Client bound to the specified interface and with the given timeout.
// It maintains backward compatibility while reusing the underlying *http.Transport connection pool.
func NewHTTPClient(iface string, timeout time.Duration) (*http.Client, error) {
	return GetHTTPClient(iface, timeout, true)
}

// CloseIdleHTTPConnections closes all idle connections in all cached transports.
func CloseIdleHTTPConnections() {
	transportMu.Lock()
	defer transportMu.Unlock()
	for _, tr := range transportPool {
		tr.CloseIdleConnections()
	}
}

// ResetHTTPTransportPool closes all idle connections and clears the transport pool (mainly for testing/cleanup).
func ResetHTTPTransportPool() {
	transportMu.Lock()
	defer transportMu.Unlock()
	for _, tr := range transportPool {
		tr.CloseIdleConnections()
	}
	transportPool = make(map[transportKey]*http.Transport)
}
