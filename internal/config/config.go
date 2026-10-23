// Package config 定義「多開CF代理」的單一 JSON 配置結構與讀寫。
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// HealthCheckConfig 健康檢查參數。
type HealthCheckConfig struct {
	IntervalSeconds       int     `json:"interval_seconds"`
	FailureThreshold      int     `json:"failure_threshold"`
	LatencyDiscardSeconds float64 `json:"latency_discard_seconds"` // 0 = 停用
	LatencyProbeSeconds   int     `json:"latency_probe_seconds"`   // 0 = 隨健康檢查；>0 = 獨立更高頻延遲探測
	ProbeBatch            int     `json:"probe_batch"`             // 每批併發探測條數；0 = 全量並發
	ProbeBatchGapMs       int     `json:"probe_batch_gap_ms"`      // 批間間隔毫秒；0 = 250
}

// Account 綁定在某個上游的入站帳密（用戶名:密碼）。
type Account struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Disabled bool   `json:"disabled"`
}

// Upstream 一個 Cloudflare WARP 隧道上游及其綁定帳號。
type Upstream struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	PrivateKey    string   `json:"private_key"`     // WireGuard 本端私鑰（base64）
	PeerPublicKey string   `json:"peer_public_key"` // 對端（WARP）公鑰
	Endpoint      string   `json:"endpoint"`        // host:port
	Addresses     []string `json:"addresses"`       // 本端隧道地址（含掩碼）
	Account       Account  `json:"account"`
}

// RoutingConfig 選路偏好參數。
type RoutingConfig struct {
	PreferLowestLatency bool `json:"prefer_lowest_latency"` // 全域延遲優先：所有帳號一律走當前延遲最低的健康出口（EMA 平滑後排序）
	// PreferIPv6Egress 撥號時優先使用 IPv6 目標地址。
	// WARP 的 IPv4 出口 IP 由 Cloudflare 按「源 IP」分配，與帳號無關（多帳號共用同一
	// 104.28.x.x 段）；而 IPv6 出口按帳號獨立分配（2606:4700:110:xxxx → 2a09:bacx:...）。
	// 因此只有當目標支持 IPv6 時，多帳號才構成真正的「多出口 IP 池」。
	// 開啟後：域名解析結果中 IPv6 地址排前，僅在 IPv6 全部撥號失敗時才回落 IPv4。
	PreferIPv6Egress bool `json:"prefer_ipv6_egress"`
}

// Config 頂層配置，持久化為單一 JSON 文件。
type Config struct {
	AdminPassword   string            `json:"admin_password"`
	ListenSocks5    string            `json:"listen_socks5"`
	ListenHTTP      string            `json:"listen_http"`
	ListenWeb       string            `json:"listen_web"`
	HealthCheck     HealthCheckConfig `json:"health_check"`
	DNSCacheSeconds int               `json:"dns_cache_seconds"` // 經隧道 DNS 結果的本機快取秒數；0 = 停用
	Routing         RoutingConfig     `json:"routing"`
	Upstreams       []Upstream        `json:"upstreams"`
}

// Default 返回默認配置。AdminPassword 留空，由首次啟動流程生成。
func Default() *Config {
	return &Config{
		ListenSocks5:    ":1080",
		ListenHTTP:      ":8080",
		ListenWeb:       ":8081",
		DNSCacheSeconds: 60,
		HealthCheck: HealthCheckConfig{
			IntervalSeconds:  30,
			FailureThreshold: 3,
		},
		Upstreams: []Upstream{},
	}
}

// FillDefaults 僅對空缺字段補默認值，不覆蓋明確設置的值。
func (c *Config) FillDefaults() {
	d := Default()
	if c.ListenSocks5 == "" {
		c.ListenSocks5 = d.ListenSocks5
	}
	if c.ListenHTTP == "" {
		c.ListenHTTP = d.ListenHTTP
	}
	if c.ListenWeb == "" {
		c.ListenWeb = d.ListenWeb
	}
	if c.HealthCheck.IntervalSeconds <= 0 {
		c.HealthCheck.IntervalSeconds = d.HealthCheck.IntervalSeconds
	}
	if c.HealthCheck.FailureThreshold <= 0 {
		c.HealthCheck.FailureThreshold = d.HealthCheck.FailureThreshold
	}
	if c.Upstreams == nil {
		c.Upstreams = []Upstream{}
	}
}

// Validate 檢查配置語義合法性；端口需可解析為 host:port。
func (c *Config) Validate() error {
	for _, p := range []struct{ name, val string }{
		{"listen_socks5", c.ListenSocks5},
		{"listen_http", c.ListenHTTP},
		{"listen_web", c.ListenWeb},
	} {
		if _, _, err := net.SplitHostPort(p.val); err != nil {
			return fmt.Errorf("%s 不是合法的 host:port（%q）: %w", p.name, p.val, err)
		}
	}
	if c.HealthCheck.IntervalSeconds <= 0 {
		return fmt.Errorf("health_check.interval_seconds 必須大於 0")
	}
	if c.HealthCheck.FailureThreshold <= 0 {
		return fmt.Errorf("health_check.failure_threshold 必須大於 0")
	}
	if c.HealthCheck.LatencyDiscardSeconds < 0 {
		return fmt.Errorf("health_check.latency_discard_seconds 不可小於 0")
	}
	if c.DNSCacheSeconds < 0 {
		return fmt.Errorf("dns_cache_seconds 不可小於 0")
	}
	if c.HealthCheck.LatencyProbeSeconds < 0 {
		return fmt.Errorf("health_check.latency_probe_seconds 不可小於 0")
	}
	// 選路互斥：全域延遲優先會把所有帳號收斂到同一最快出口，使多帳號的
	// 獨立 IPv6 出口 IP 池退化為單一 IP；與 IPv6 出口優先語意衝突，只能二選一。
	if c.Routing.PreferLowestLatency && c.Routing.PreferIPv6Egress {
		return fmt.Errorf("routing.prefer_lowest_latency 與 routing.prefer_ipv6_egress 互斥：延遲優先會把全部帳號擠到同一出口（IP 池退化為 1 個），請只開啟其中一個")
	}
	seen := map[string]bool{}
	for i, u := range c.Upstreams {
		if u.ID == "" {
			return fmt.Errorf("upstreams[%d].id 不可為空", i)
		}
		if seen[u.ID] {
			return fmt.Errorf("upstreams[%d].id 重複: %s", i, u.ID)
		}
		seen[u.ID] = true
		if !u.Enabled {
			continue // 停用的上游允許欄位不全，啟用時才校驗
		}
		if u.PrivateKey == "" || u.PeerPublicKey == "" {
			return fmt.Errorf("upstreams[%d]（%s）缺少 WireGuard 金鑰", i, u.Name)
		}
		if u.Endpoint == "" {
			return fmt.Errorf("upstreams[%d]（%s）缺少 endpoint", i, u.Name)
		}
		if len(u.Addresses) == 0 {
			return fmt.Errorf("upstreams[%d]（%s）缺少隧道地址", i, u.Name)
		}
		if u.Account.Username == "" || u.Account.Password == "" {
			return fmt.Errorf("upstreams[%d]（%s）缺少綁定帳號或密碼", i, u.Name)
		}
	}
	return nil
}

// Load 從 path 讀取配置；文件不存在時返回默認配置（首次啟動場景）。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		c := Default()
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("讀取配置 %s 失敗: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("解析配置 %s 失敗: %w", path, err)
	}
	// dns_cache_seconds：缺鍵（存量配置）補默認 60；顯式 0 = 停用。
	// int 零值無法區分兩者，故以指標探測原始 JSON 是否含該鍵。
	var probe struct {
		DNSCacheSeconds *int `json:"dns_cache_seconds"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil && probe.DNSCacheSeconds == nil {
		c.DNSCacheSeconds = 60
	}
	// routing.switch_margin_ms 已於 v1.6.9 移除；存量配置中的該鍵為未知鍵，靜默忽略。
	c.FillDefaults()
	// 啟動時即校驗：手改配置若含互斥/非法組合，寧可啟動失敗也不要靜默帶病運行
	// （例如同時開啟延遲優先與 IPv6 出口優先，會讓帳號池悄悄退化）。
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("配置 %s 校驗失敗: %w", path, err)
	}
	return &c, nil
}

// Save 以縮進 JSON 原子寫入（臨時文件 + rename），避免中途崩潰損壞配置。
func (c *Config) Save(path string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("建立配置目錄失敗: %w", err)
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失敗: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("寫入臨時配置失敗: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("替換配置文件失敗: %w", err)
	}
	return nil
}

// UpstreamByID 按 ID 查找上游。
func (c *Config) UpstreamByID(id string) (*Upstream, bool) {
	for i := range c.Upstreams {
		if c.Upstreams[i].ID == id {
			return &c.Upstreams[i], true
		}
	}
	return nil, false
}
