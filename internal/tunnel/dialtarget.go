package tunnel

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// resolveTarget 把撥號目標轉為候選 AddrPort 清單（依序嘗試，保留故障轉移）。
// IP 字面值直連（不觸發解析）；域名經 cache（TTL 內免 DNS 往返）。
func resolveTarget(ctx context.Context, cache *dnsCache, addr string) ([]netip.AddrPort, error) {
	return resolveTargetPref(ctx, cache, addr, false)
}

// resolveTargetPref 是 resolveTarget 的可指定 IPv6 偏好版本。
// preferIPv6=true 時：候選清單中 IPv6 地址全部排到 IPv4 之前（各自保持原始順序），
// 讓呼叫方的「依序嘗試」邏輯天然形成「先 v6、全失敗才回落 v4」的語意——
// 這正是多帳號構成獨立出口 IP 池的前提（WARP 的 v4 出口與帳號無關、v6 出口按帳號獨立）。
func resolveTargetPref(ctx context.Context, cache *dnsCache, addr string, preferIPv6 bool) ([]netip.AddrPort, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("目標地址 %q 無效: %w", addr, err)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || port == 0 {
		return nil, fmt.Errorf("目標端口無效 %q", addr)
	}

	if ip, perr := netip.ParseAddr(host); perr == nil {
		return []netip.AddrPort{netip.AddrPortFrom(ip.Unmap(), uint16(port))}, nil
	}

	if cache == nil {
		return nil, fmt.Errorf("目標 %q 是域名但無 DNS 快取可用", addr)
	}
	ips, rerr := cache.resolveHost(ctx, host)
	if rerr != nil {
		return nil, fmt.Errorf("解析 %s 失敗: %w", host, rerr)
	}
	out := make([]netip.AddrPort, 0, len(ips))
	for _, s := range ips {
		ip, perr := netip.ParseAddr(s)
		if perr != nil {
			continue
		}
		out = append(out, netip.AddrPortFrom(ip.Unmap(), uint16(port)))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("解析 %s 無可用位址", host)
	}
	if preferIPv6 {
		out = sortIPv6First(out)
	}
	return out, nil
}

// sortIPv6First 穩定排序：IPv6 候選在前，IPv4 在後（同族內保持原順序）。
func sortIPv6First(in []netip.AddrPort) []netip.AddrPort {
	out := make([]netip.AddrPort, 0, len(in))
	for _, ap := range in {
		if ap.Addr().Is6() && !ap.Addr().Is4In6() {
			out = append(out, ap)
		}
	}
	for _, ap := range in {
		if !(ap.Addr().Is6() && !ap.Addr().Is4In6()) {
			out = append(out, ap)
		}
	}
	return out
}
