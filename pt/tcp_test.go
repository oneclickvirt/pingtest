package pt

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/oneclickvirt/pingtest/model"
)

func TestRunTCPProbeCalculatesMetrics(t *testing.T) {
	base := time.Unix(0, 0)
	times := []time.Time{
		base, base.Add(time.Millisecond),
		base, base.Add(2 * time.Millisecond),
		base, base.Add(3 * time.Millisecond),
		base, base.Add(4 * time.Millisecond),
	}
	var clockIndex int
	config := TCPProbeConfig{
		Attempts: 4,
		Timeout:  time.Second,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			_ = server.Close()
			return client, nil
		},
		Now: func() time.Time {
			current := times[clockIndex]
			clockIndex++
			return current
		},
	}

	result, err := RunTCPProbe(context.Background(), model.TCPTarget{Name: "test", Host: "example.test", Port: 443}, config)
	if err != nil {
		t.Fatalf("RunTCPProbe returned error: %v", err)
	}
	if result.Successful != 4 || result.Failed != 0 || result.SuccessRatePercent != 100 || result.LossPercent != 0 {
		t.Fatalf("unexpected counts: %+v", result)
	}
	if result.Min != time.Millisecond || result.Max != 4*time.Millisecond || result.Mean != 2500*time.Microsecond {
		t.Errorf("unexpected min/max/mean: %v/%v/%v", result.Min, result.Max, result.Mean)
	}
	if result.P50 != 2500*time.Microsecond || result.P95 != 3850*time.Microsecond {
		t.Errorf("unexpected percentiles: p50=%v p95=%v", result.P50, result.P95)
	}
	if len(result.Samples) != 4 {
		t.Fatalf("expected 4 samples, got %d", len(result.Samples))
	}
}

func TestRunTCPProbeClassifiesFailures(t *testing.T) {
	errorsByAttempt := []error{
		&net.DNSError{Err: "no such host", Name: "missing.test"},
		&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		timeoutError{},
		&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ENETUNREACH},
	}
	var index int
	result, err := RunTCPProbe(context.Background(), model.TCPTarget{Host: "example.test", Port: 443}, TCPProbeConfig{
		Attempts: 4,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			err := errorsByAttempt[index]
			index++
			return nil, err
		},
	})
	if err != nil {
		t.Fatalf("RunTCPProbe returned error: %v", err)
	}
	want := map[string]int{TCPErrorDNS: 1, TCPErrorRefused: 1, TCPErrorTimeout: 1, TCPErrorNetwork: 1}
	if result.Successful != 0 || result.Failed != 4 || result.SuccessRatePercent != 0 || result.LossPercent != 100 {
		t.Fatalf("unexpected counts: %+v", result)
	}
	for class, count := range want {
		if result.ErrorCounts[class] != count {
			t.Errorf("error class %q: got %d, want %d", class, result.ErrorCounts[class], count)
		}
	}
}

func TestTCPResultJSONIncludesSuccessRatePercent(t *testing.T) {
	result := TCPResult{Attempts: 4, Successful: 3, Failed: 1}
	result.finish()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["success_rate_percent"] != float64(75) {
		t.Fatalf("success_rate_percent = %#v, JSON = %s", payload["success_rate_percent"], encoded)
	}
}

func TestRunTCPProbeHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var dialed bool
	result, err := RunTCPProbe(ctx, model.TCPTarget{Host: "example.test", Port: 443}, TCPProbeConfig{
		Attempts: 3,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("unexpected dial")
		},
	})
	if err != nil {
		t.Fatalf("RunTCPProbe returned error: %v", err)
	}
	if dialed {
		t.Fatal("dialer called after context cancellation")
	}
	if result.Failed != 3 || result.ErrorCounts[TCPErrorCanceled] != 3 || result.LossPercent != 100 {
		t.Fatalf("unexpected cancellation result: %+v", result)
	}
}

func TestRunTCPProbesPreservesTargetOrder(t *testing.T) {
	targets := []model.TCPTarget{
		{Name: "one", Host: "one.test", Port: 443},
		{Name: "two", Host: "two.test", Port: 443},
		{Name: "three", Host: "three.test", Port: 443},
	}
	var mu sync.Mutex
	addresses := make([]string, 0, len(targets))
	results := RunTCPProbes(context.Background(), targets, TCPProbeConfig{
		Attempts:    1,
		Concurrency: 2,
		DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			mu.Lock()
			addresses = append(addresses, address)
			mu.Unlock()
			client, server := net.Pipe()
			_ = server.Close()
			return client, nil
		},
	})
	if len(addresses) != len(targets) {
		t.Fatalf("dialed %d targets, want %d", len(addresses), len(targets))
	}
	for index, target := range targets {
		if results[index].Target != target || results[index].Successful != 1 {
			t.Errorf("result %d does not match target: %+v", index, results[index])
		}
	}
}

func TestRunTCPRegistryUsesMergedProductionTargets(t *testing.T) {
	targets := model.AllTCPTargets()
	addresses := make(map[string]int, len(targets))
	var mu sync.Mutex
	results := RunTCPRegistry(context.Background(), TCPProbeConfig{
		Attempts:    1,
		Concurrency: 8,
		DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			mu.Lock()
			addresses[address]++
			mu.Unlock()
			client, server := net.Pipe()
			_ = server.Close()
			return client, nil
		},
	})
	if len(results) != len(targets) {
		t.Fatalf("registry returned %d results for %d targets", len(results), len(targets))
	}
	for _, address := range []string{"www.google.com:443", "vercel.com:443", "www.salesforce.com:443"} {
		if addresses[address] != 1 {
			t.Errorf("registry dial count for %q = %d, want 1", address, addresses[address])
		}
	}
}

func TestFormatTCPResultsSummarizesAndGroupsFailures(t *testing.T) {
	output := FormatTCPResults([]TCPResult{
		{
			Target:     model.TCPTarget{Name: "alpha", Category: "first"},
			Attempts:   4,
			Successful: 3,
			Failed:     1,
			Min:        time.Millisecond,
			Mean:       7 * time.Millisecond,
			P50:        2 * time.Millisecond,
			P95:        4 * time.Millisecond,
			Max:        4 * time.Millisecond,
			Samples: []TCPSample{
				{Attempt: 1, Duration: time.Millisecond, Success: true},
				{Attempt: 2, Duration: 2 * time.Millisecond, Success: true},
				{Attempt: 3, Duration: 4 * time.Millisecond, Success: true},
				{Attempt: 4, ErrorClass: TCPErrorTimeout},
			},
			ErrorCounts: map[string]int{TCPErrorTimeout: 1},
		},
		{
			Target:      model.TCPTarget{Name: "beta", Category: "second"},
			Attempts:    2,
			Failed:      2,
			ErrorCounts: map[string]int{TCPErrorDNS: 1, TCPErrorRefused: 1},
		},
		{
			Target:     model.TCPTarget{Name: "gamma", Category: "first"},
			Attempts:   3,
			Successful: 1,
			Failed:     2,
			Min:        8 * time.Millisecond,
			Mean:       8 * time.Millisecond,
			P50:        8 * time.Millisecond,
			P95:        8 * time.Millisecond,
			Max:        8 * time.Millisecond,
			Samples: []TCPSample{
				{Attempt: 1, Duration: 8 * time.Millisecond, Success: true},
				{Attempt: 2, ErrorClass: TCPErrorNetwork},
				{Attempt: 3, ErrorClass: TCPErrorCanceled},
			},
			ErrorCounts: map[string]int{TCPErrorNetwork: 1, TCPErrorCanceled: 1},
		},
	})
	for _, want := range []string{
		"汇总 目标:3",
		"握手:4/9",
		"成功率:44.4%",
		"失败:5",
		"Min/Avg/P50/P95/Max",
		"DNS:1  拒绝:1  超时:1  其他:2",
		"[first] 2个目标",
		"alpha",
		"beta",
		"0/0/1/0",
		"1/1/0/0",
		"0/0/0/2",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("formatted output missing %q: %q", want, output)
		}
	}
	for _, line := range strings.Split(output, "\n") {
		if width := runewidth.StringWidth(line); width > 80 {
			t.Fatalf("formatted line width %d exceeds 80: %q", width, line)
		}
		if strings.Contains(line, "alpha") && strings.Contains(line, "...") {
			t.Fatalf("latency detail was truncated: %q", line)
		}
	}
}

func TestFormatTCPResultsKeepsAggregateJSONFieldsUnchanged(t *testing.T) {
	result := TCPResult{Target: model.TCPTarget{Name: "fixture"}, Attempts: 2, Successful: 1, Failed: 1, Mean: time.Millisecond, ErrorCounts: map[string]int{TCPErrorDNS: 1}}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("TCPResult JSON compatibility changed: %v", err)
	}
}

func TestRunTCPProbeRejectsInvalidTarget(t *testing.T) {
	_, err := RunTCPProbe(context.Background(), model.TCPTarget{Host: "", Port: 443}, TCPProbeConfig{})
	if err == nil {
		t.Fatal("expected empty host error")
	}
	_, err = RunTCPProbe(context.Background(), model.TCPTarget{Host: "example.test", Port: 70000}, TCPProbeConfig{})
	if err == nil {
		t.Fatal("expected invalid port error")
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timed out" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
