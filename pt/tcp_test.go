package pt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

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

func TestPercentileRoundsToNearestNanosecond(t *testing.T) {
	values := []time.Duration{0, time.Nanosecond}
	if got := percentile(values, 0.50); got != time.Nanosecond {
		t.Fatalf("p50 = %s, want %s", got, time.Nanosecond)
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

func TestFormatTCPResultsUsesOneCompleteRowPerPlatform(t *testing.T) {
	output := FormatTCPResults([]TCPResult{
		{
			Target: model.TCPTarget{Name: "alpha", Category: "first"}, Attempts: 4, Successful: 3, Failed: 1,
			Min: time.Millisecond, Mean: 7 * time.Millisecond, P50: 2 * time.Millisecond, P95: 4 * time.Millisecond, Max: 4 * time.Millisecond,
			Samples: []TCPSample{
				{Attempt: 1, Duration: time.Millisecond, Success: true},
				{Attempt: 2, Duration: 2 * time.Millisecond, Success: true},
				{Attempt: 3, Duration: 4 * time.Millisecond, Success: true},
				{Attempt: 4, ErrorClass: TCPErrorTimeout},
			},
			ErrorCounts: map[string]int{TCPErrorTimeout: 1},
		},
		{Target: model.TCPTarget{Name: "beta", Category: "second"}, Attempts: 2, Failed: 2, ErrorCounts: map[string]int{TCPErrorDNS: 1, TCPErrorRefused: 1}},
		{
			Target: model.TCPTarget{Name: "gamma", Category: "first"}, Attempts: 3, Successful: 1, Failed: 2,
			Min: 8 * time.Millisecond, Mean: 8 * time.Millisecond, P50: 8 * time.Millisecond, P95: 8 * time.Millisecond, Max: 8 * time.Millisecond,
			Samples: []TCPSample{
				{Attempt: 1, Duration: 8 * time.Millisecond, Success: true},
				{Attempt: 2, ErrorClass: TCPErrorNetwork},
				{Attempt: 3, ErrorClass: TCPErrorCanceled},
			},
			ErrorCounts: map[string]int{TCPErrorNetwork: 1, TCPErrorCanceled: 1},
		},
	})
	for _, want := range []string{
		"汇总 目标:3", "握手:4/9", "成功率:44.4%", "失败:5",
		"DNS:1", "拒绝:1", "超时:1", "其他:2",
		"平台", "成功/尝试", "丢包", "Min/Avg/P50/P95/Max",
		"alpha", "beta", "gamma", "0/0/1/0", "1/1/0/0", "0/0/0/2",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("formatted output missing %q: %q", want, output)
		}
	}
	lines := strings.Split(output, "\n")
	if len(lines) != 5 {
		t.Fatalf("output has %d lines, want summary + header + 3 rows:\n%s", len(lines), output)
	}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		rows := 0
		for _, line := range lines {
			if strings.Contains(line, name) {
				rows++
			}
		}
		if rows != 1 {
			t.Fatalf("platform %q appears on %d rows:\n%s", name, rows, output)
		}
	}
	for _, forbidden := range []string{"延迟 ", "失败 DNS", "完整目标", "类别汇总", "/first", "/second"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output retained obsolete text %q:\n%s", forbidden, output)
		}
	}
}

func TestTCPResultsShowAllPlatformsAndSupportRequestedOrder(t *testing.T) {
	results := make([]TCPResult, 0, 13)
	for index := 0; index < 12; index++ {
		latency := time.Duration(index+1) * time.Millisecond
		results = append(results, TCPResult{
			Target:   model.TCPTarget{Name: fmt.Sprintf("normal-%02d", index), Category: "global"},
			Attempts: 1, Successful: 1, Min: latency, Mean: latency, P50: latency, P95: latency, Max: latency,
		})
	}
	results = append(results, TCPResult{
		Target: model.TCPTarget{Name: "failed", Category: "global"}, Attempts: 3, Failed: 3,
		ErrorCounts: map[string]int{TCPErrorDNS: 3},
	})

	compact := FormatTCPResultsWithOptions(results, TCPFormatOptions{Format: TCPTextFormatCompact, MaxDetails: 3, Sort: model.TCPSortLatency})
	for _, want := range []string{"failed", "normal-11", "normal-10", "normal-00"} {
		if !strings.Contains(compact, want) {
			t.Errorf("compact output missing %q:\n%s", want, compact)
		}
	}
	if strings.Count(compact, "normal-") != 12 {
		t.Fatalf("compact output omitted platforms:\n%s", compact)
	}
	if strings.Contains(compact, "/全球") || strings.Contains(compact, "完整目标") {
		t.Fatalf("compact output retained a category suffix or marker:\n%s", compact)
	}
	if !(strings.Index(compact, "failed") < strings.Index(compact, "normal-11") && strings.Index(compact, "normal-11") < strings.Index(compact, "normal-10")) {
		t.Fatalf("explicit latency ordering was not retained:\n%s", compact)
	}

	full := FormatTCPResultsWithOptions(results, TCPFormatOptions{Format: TCPTextFormatFull})
	for _, result := range results {
		if !strings.Contains(full, result.Target.Name) {
			t.Errorf("full output omitted %q", result.Target.Name)
		}
	}
	if !(strings.Index(full, "failed") < strings.Index(full, "normal-00") && strings.Index(full, "normal-00") < strings.Index(full, "normal-01")) {
		t.Fatalf("default platform-name ordering is unstable:\n%s", full)
	}
}

func TestTCPDetailKeepsSlowLatencyAndErrorCountersComplete(t *testing.T) {
	result := TCPResult{
		Target:   model.TCPTarget{Name: "ProtonMail", Category: "global"},
		Attempts: 3, Successful: 3,
		Min: 237100 * time.Microsecond, Mean: 251100 * time.Microsecond,
		P50: 245800 * time.Microsecond, P95: 268000 * time.Microsecond,
		Max: 270500 * time.Microsecond,
	}
	output := FormatTCPResults([]TCPResult{result})
	if strings.Contains(output, "...") {
		t.Fatalf("slow TCP detail was truncated:\n%s", output)
	}
	for _, want := range []string{"237/251/246/268/271ms", "0/0/0/0"} {
		if !strings.Contains(output, want) {
			t.Fatalf("slow TCP detail missing %q:\n%s", want, output)
		}
	}
	if lines := strings.Split(output, "\n"); len(lines) != 3 || !strings.Contains(lines[2], "ProtonMail") || !strings.Contains(lines[2], "237/251/246/268/271ms") {
		t.Fatalf("platform did not stay on one complete row:\n%s", output)
	}
}

func TestDefaultRegistryOutputIncludesEveryPlatformSortedByName(t *testing.T) {
	targets := model.AllTCPTargets()
	results := make([]TCPResult, 0, len(targets))
	for index, target := range targets {
		latency := time.Duration(index+1) * time.Millisecond
		results = append(results, TCPResult{
			Target: target, Attempts: 3, Successful: 3,
			Min: latency, Mean: latency, P50: latency, P95: latency, Max: latency,
		})
	}
	output := FormatTCPResults(results)
	lines := strings.Split(output, "\n")
	if len(lines) != len(results)+2 {
		t.Fatalf("output has %d lines for %d platforms:\n%s", len(lines), len(results), output)
	}
	for _, result := range results {
		if !strings.Contains(output, tcpResultName(result)) {
			t.Fatalf("default output omitted %q", tcpResultName(result))
		}
	}
	ordered := sortTCPResults(results, model.TCPSortName)
	for index, result := range ordered {
		if !strings.Contains(lines[index+2], tcpResultName(result)) {
			t.Fatalf("row %d is not sorted by platform name: %q", index, lines[index+2])
		}
	}
}

func TestTCPResultsTranslateEnglishAndOmitZeroFailureClasses(t *testing.T) {
	result := TCPResult{
		Target:   model.TCPTarget{Name: "A very long platform name that must remain complete", Category: "shopping"},
		Attempts: 2, Successful: 2, Min: time.Millisecond, Mean: 2 * time.Millisecond,
		P50: 2 * time.Millisecond, P95: 3 * time.Millisecond, Max: 3 * time.Millisecond,
	}
	output := FormatTCPResultsWithOptions([]TCPResult{result}, TCPFormatOptions{Language: "en"})
	for _, want := range []string{"Summary Targets:1", "Handshakes:2/2", "Success rate:100.0%", "Failed:0", "Platform", "Success/Attempts", "Loss", result.Target.Name} {
		if !strings.Contains(output, want) {
			t.Fatalf("English output missing %q:\n%s", want, output)
		}
	}
	for _, forbidden := range []string{"DNS:0", "Refused:0", "Timeout:0", "Other:0", "/shopping", "/购物", "..."} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("English output retained %q:\n%s", forbidden, output)
		}
	}
	lines := strings.Split(output, "\n")
	if len(lines) != 3 {
		t.Fatalf("English output has %d lines, want 3:\n%s", len(lines), output)
	}
	for header, value := range map[string]string{
		"Success/Attempts":             "2/2",
		"Loss":                         "0.0%",
		"Min/Avg/P50/P95/Max; D/R/T/O": "1.0/2.0/2.0/3.0/3.0ms; 0/0/0/0",
	} {
		if strings.Index(lines[1], header) != strings.Index(lines[2], value) {
			t.Fatalf("column %q is not aligned:\n%s", header, output)
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
