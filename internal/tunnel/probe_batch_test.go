package tunnel

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"multi-cf-proxy/internal/config"
)

// probeCounter 記錄一輪探測的併發峰值與總調用數。
type probeCounter struct {
	inFlight atomic.Int32
	peak     atomic.Int32
	calls    atomic.Int32
	delay    time.Duration
}

func (p *probeCounter) fn() ProbeFunc {
	return func(ctx context.Context, d Dialer) (time.Duration, error) {
		cur := p.inFlight.Add(1)
		defer p.inFlight.Add(-1)
		for {
			prev := p.peak.Load()
			if cur <= prev || p.peak.CompareAndSwap(prev, cur) {
				break
			}
		}
		p.calls.Add(1)
		time.Sleep(p.delay)
		return time.Millisecond, nil
	}
}

func syncN(t *testing.T, m *Manager, n int) {
	t.Helper()
	ups := make([]*config.Upstream, 0, n)
	for i := 0; i < n; i++ {
		ups = append(ups, mkUpstream(fmt.Sprintf("u%02d", i), true))
	}
	if err := m.Sync(context.Background(), ups); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

// TestProbeBatchLimitsConcurrency 分批後同瞬間探測數不得超過 batch，且每條隧道都要被探到。
func TestProbeBatchLimitsConcurrency(t *testing.T) {
	const n, batch = 20, 4
	var created []*fakeTunnel
	pc := &probeCounter{delay: 3 * time.Millisecond}
	m := NewManager(fakeFactory(&created), pc.fn(), time.Hour, 3)
	m.SetProbeBatch(batch, time.Millisecond)
	syncN(t, m, n)

	m.probeAll(context.Background())

	if got := pc.peak.Load(); got > batch {
		t.Fatalf("併發峰值 %d 超過批次上限 %d", got, batch)
	}
	if got := pc.calls.Load(); got != n {
		t.Fatalf("探測次數 %d，期望 %d", got, n)
	}
}

// TestProbeBatchZeroKeepsFullConcurrency batch<=0 時維持上游的全量並發行為。
func TestProbeBatchZeroKeepsFullConcurrency(t *testing.T) {
	const n = 12
	var created []*fakeTunnel
	pc := &probeCounter{delay: 5 * time.Millisecond}
	m := NewManager(fakeFactory(&created), pc.fn(), time.Hour, 3)
	m.SetProbeBatch(0, 0)
	syncN(t, m, n)

	m.probeAll(context.Background())

	if got := pc.peak.Load(); got != n {
		t.Fatalf("全量並發峰值 %d，期望 %d", got, n)
	}
}

// TestProbeBatchContextCancel 分批等待期間 ctx 取消應立即停止後續批次。
func TestProbeBatchContextCancel(t *testing.T) {
	const n, batch = 40, 2
	var created []*fakeTunnel
	pc := &probeCounter{delay: 2 * time.Millisecond}
	m := NewManager(fakeFactory(&created), pc.fn(), time.Hour, 3)
	m.SetProbeBatch(batch, 20*time.Millisecond)
	syncN(t, m, n)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(6*time.Millisecond, cancel)
	m.probeAll(ctx)

	if got := pc.calls.Load(); got >= n {
		t.Fatalf("取消後仍在跑完全部 %d 條（實際 %d）", n, got)
	}
}
