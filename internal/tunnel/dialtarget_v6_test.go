package tunnel

import (
	"context"
	"net/netip"
	"testing"
)

// TestSortIPv6First 驗證 v6 優先排序：v6 全在前、v4 在後、同族保序。
func TestSortIPv6First(t *testing.T) {
	in := []netip.AddrPort{
		netip.MustParseAddrPort("1.1.1.1:80"),
		netip.MustParseAddrPort("[2606:4700::1]:80"),
		netip.MustParseAddrPort("8.8.8.8:80"),
		netip.MustParseAddrPort("[2a09:bac1::2]:80"),
	}
	got := sortIPv6First(in)
	want := []netip.AddrPort{
		netip.MustParseAddrPort("[2606:4700::1]:80"),
		netip.MustParseAddrPort("[2a09:bac1::2]:80"),
		netip.MustParseAddrPort("1.1.1.1:80"),
		netip.MustParseAddrPort("8.8.8.8:80"),
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestSortIPv6FirstV4Only v4-only 清單不變。
func TestSortIPv6FirstV4Only(t *testing.T) {
	in := []netip.AddrPort{
		netip.MustParseAddrPort("1.1.1.1:80"),
		netip.MustParseAddrPort("8.8.8.8:80"),
	}
	got := sortIPv6First(in)
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("[%d] = %v, want %v", i, got[i], in[i])
		}
	}
}

// TestResolveTargetPrefOrder 域名解析含兩族時，preferIPv6 決定候選順序。
func TestResolveTargetPrefOrder(t *testing.T) {
	c := newDNSCache(0, 8, func(ctx context.Context, host string) ([]string, error) {
		return []string{"1.1.1.1", "2606:4700::1111", "8.8.8.8", "2a09:bac1::3"}, nil
	})
	got, err := resolveTargetPref(context.Background(), c, "example.com:443", true)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Addr().Is6() || !got[1].Addr().Is6() {
		t.Fatalf("v6 未排前: %v", got)
	}
	if got[2].Addr().Is6() || got[3].Addr().Is6() {
		t.Fatalf("v4 未排後: %v", got)
	}

	// preferIPv6=false 應保持原始順序
	got2, err := resolveTargetPref(context.Background(), c, "example.com:443", false)
	if err != nil {
		t.Fatal(err)
	}
	if got2[0].Addr().String() != "1.1.1.1" {
		t.Fatalf("關閉時不應排序: %v", got2)
	}
}
