package gutil

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIPUint32RoundTrip(t *testing.T) {
	cases := []string{"0.0.0.0", "127.0.0.1", "192.168.1.1", "255.255.255.255", "8.8.8.8"}
	for _, ip := range cases {
		n, err := IPToUint32(ip)
		require.NoError(t, err, ip)
		require.Equal(t, ip, Uint32ToIP(n), ip)
	}
}

func TestIPToUint32Invalid(t *testing.T) {
	_, err := IPToUint32("not-an-ip")
	require.Error(t, err)
	_, err = IPToUint32("::1") // IPv6 不支持
	require.Error(t, err)
}

func TestMustIPToUint32Fallback(t *testing.T) {
	require.Equal(t, uint32(0), MustIPToUint32("bad"))
	require.Equal(t, uint32(0x7f000001), MustIPToUint32("127.0.0.1"))
}

func TestLocalIP(t *testing.T) {
	ip := LocalIP()
	// 多数环境至少有一个全局单播地址；允许为 nil，但 MustLocalIP 必须非 nil
	if ip != nil {
		require.NotNil(t, ip)
	}
	require.NotNil(t, MustLocalIP())
}

func TestCheckPort(t *testing.T) {
	// 启动一个临时监听端口，确认可探测为开放
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	defer ln.Close()

	require.True(t, CheckPort(addr))

	// 关闭后不应再可连（端口大概率不可复用）
	ln.Close()
	require.False(t, CheckPort(addr))
}
