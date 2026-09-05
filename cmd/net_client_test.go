package cmd

import (
	"net"
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
