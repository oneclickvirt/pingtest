package pt

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunTelegramICMPProbesUsesStructuredRunner(t *testing.T) {
	var called atomic.Int64
	results := RunTelegramICMPProbes(context.Background(), ICMPProbeConfig{
		Count: 2, Concurrency: 2,
		Probe: func(_ context.Context, target ICMPTarget, count int, _ time.Duration) ICMPResult {
			called.Add(1)
			return ICMPResult{Target: target, Status: "ok", Sent: count, Received: count}
		},
	})
	callCount := int(called.Load())
	if callCount != len(TelegramICMPTargets()) || len(results) != callCount {
		t.Fatalf("unexpected Telegram probe count: called=%d results=%d", callCount, len(results))
	}
	for _, result := range results {
		if result.Target.IPVersion != "ipv4" || result.Status != "ok" {
			t.Fatalf("unexpected Telegram result: %#v", result)
		}
	}
}

func TestRunWebsiteTCPProbesUsesWebsiteRegistry(t *testing.T) {
	results := RunWebsiteTCPProbes(context.Background(), TCPProbeConfig{
		Attempts: 1, Concurrency: 8,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			_ = server.Close()
			return client, nil
		},
	})
	if len(results) == 0 {
		t.Fatal("website probe returned no results")
	}
	for _, result := range results {
		if result.Target.Source != "popular-websites" || result.Successful != 1 {
			t.Fatalf("unexpected website result: %#v", result)
		}
	}
}
