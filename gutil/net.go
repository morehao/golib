package gutil

import (
	"encoding/binary"
	"errors"
	"net"
	"time"
)

// LocalIP 返回本机的首选（全局单播）IPv4/IPv6 地址，找不到时返回 nil。
// 用于日志、服务注册等需要标识「本机从哪个地址对外」的场景。
func LocalIP() net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.IsGlobalUnicast() {
			return ipNet.IP
		}
	}
	return nil
}

// MustLocalIP 同 LocalIP，但在拿不到地址时回退到 127.0.0.1，保证调用方总能拿到一个非 nil 地址。
func MustLocalIP() net.IP {
	if ip := LocalIP(); ip != nil {
		return ip
	}
	return net.ParseIP("127.0.0.1")
}

// IPToUint32 将 IPv4 地址字符串转换为 uint32（大端序）。
// 传入非 IPv4（包括 IPv6）时返回错误。
func IPToUint32(ip string) (uint32, error) {
	addr := net.ParseIP(ip)
	if addr == nil {
		return 0, errors.New("gutil: invalid ip address")
	}
	v4 := addr.To4()
	if v4 == nil {
		return 0, errors.New("gutil: not an ipv4 address")
	}
	return binary.BigEndian.Uint32(v4), nil
}

// MustIPToUint32 同 IPToUint32，解析失败时返回 0。
func MustIPToUint32(ip string) uint32 {
	n, _ := IPToUint32(ip)
	return n
}

// Uint32ToIP 将 uint32（大端序）转换为 IPv4 地址字符串。
func Uint32ToIP(n uint32) string {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, n)
	return net.IP(b).String()
}

// CheckPort 检测指定的 host:port 是否可建立 TCP 连接（端口是否开放），
// 使用 1 秒的连接超时。返回 true 表示连接成功。
func CheckPort(addr string) bool {
	return CheckPortTimeout(addr, time.Second)
}

// CheckPortTimeout 同 CheckPort，但可指定连接超时；timeout <= 0 时使用 1 秒。
func CheckPortTimeout(addr string, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = time.Second
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
