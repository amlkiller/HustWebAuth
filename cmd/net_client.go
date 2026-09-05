package cmd

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"syscall"
	"time"
)

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

// NewHTTPClient returns an *http.Client bound to the specified interface and with the given timeout.
func NewHTTPClient(iface string, timeout time.Duration) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
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
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}
