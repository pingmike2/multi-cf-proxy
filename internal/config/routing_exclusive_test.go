package config

import (
	"strings"
	"testing"
)

// TestValidateRoutingExclusive 兩個選路開關同時開啟必須被 Validate 拒絕。
func TestValidateRoutingExclusive(t *testing.T) {
	base := func() *Config {
		return &Config{
			ListenSocks5: "127.0.0.1:1080",
			ListenHTTP:   "127.0.0.1:8080",
			ListenWeb:    "127.0.0.1:9090",
			HealthCheck: HealthCheckConfig{
				IntervalSeconds:  30,
				FailureThreshold: 3,
			},
		}
	}

	t.Run("兩者同時開啟被拒", func(t *testing.T) {
		c := base()
		c.Routing.PreferLowestLatency = true
		c.Routing.PreferIPv6Egress = true
		err := c.Validate()
		if err == nil {
			t.Fatal("期望互斥錯誤，實際通過")
		}
		if !strings.Contains(err.Error(), "互斥") {
			t.Fatalf("錯誤訊息應說明互斥，實際: %v", err)
		}
	})

	t.Run("只開延遲優先通過", func(t *testing.T) {
		c := base()
		c.Routing.PreferLowestLatency = true
		if err := c.Validate(); err != nil {
			t.Fatalf("不應報錯: %v", err)
		}
	})

	t.Run("只開IPv6優先通過", func(t *testing.T) {
		c := base()
		c.Routing.PreferIPv6Egress = true
		if err := c.Validate(); err != nil {
			t.Fatalf("不應報錯: %v", err)
		}
	})

	t.Run("兩者皆關通過", func(t *testing.T) {
		if err := base().Validate(); err != nil {
			t.Fatalf("不應報錯: %v", err)
		}
	})
}
