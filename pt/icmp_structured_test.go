package pt

import (
	"context"
	"testing"
	"time"
)

func TestRunICMPProbesPreservesOrderAndMetrics(t *testing.T) {
	targets := []ICMPTarget{{ID: "a", Host: "a.test"}, {ID: "b", Host: "b.test"}}
	results := RunICMPProbes(context.Background(), targets, ICMPProbeConfig{
		Count: 3, Concurrency: 2,
		Probe: func(_ context.Context, target ICMPTarget, count int, _ time.Duration) ICMPResult {
			return ICMPResult{Target: target, Status: "ok", Sent: count, Received: count, P50: time.Millisecond, P95: 2 * time.Millisecond}
		},
	})
	if len(results) != 2 || results[0].Target.ID != "a" || results[1].Target.ID != "b" || results[0].P95 != 2*time.Millisecond {
		t.Fatalf("unexpected ICMP results: %+v", results)
	}
}

func TestRunICMPProbesHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := RunICMPProbes(ctx, []ICMPTarget{{ID: "a", Host: "a.test"}}, ICMPProbeConfig{})
	if len(results) != 1 || results[0].Status != "canceled" {
		t.Fatalf("unexpected canceled ICMP results: %+v", results)
	}
}

func TestDurationPercentile(t *testing.T) {
	values := []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}
	if got := durationPercentile(values, .50); got != 2*time.Millisecond {
		t.Fatalf("p50 = %s", got)
	}
	if got := durationPercentile(values, .95); got != 3800*time.Microsecond {
		t.Fatalf("p95 = %s", got)
	}
}
