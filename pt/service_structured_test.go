package pt

import (
	"context"
	"net"
	"strings"
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

func TestInternationalICMPTargetsExcludeChinaAndRemainRepresentative(t *testing.T) {
	targets := InternationalICMPTargets()
	if len(targets) < 6 {
		t.Fatalf("international target count = %d, want representative set", len(targets))
	}
	for _, target := range targets {
		text := strings.ToLower(target.ID + " " + target.Name + " " + target.Host)
		if strings.Contains(text, "china") || strings.Contains(text, "中国") {
			t.Fatalf("international targets contain China entry: %+v", target)
		}
	}
}
