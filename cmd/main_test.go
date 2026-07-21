package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oneclickvirt/pingtest/model"
	"github.com/oneclickvirt/pingtest/pt"
)

func TestRunCLIDefaultKeepsOriginalMode(t *testing.T) {
	runner, calls := offlineRunner()
	var output bytes.Buffer
	if exitCode := runCLI(context.Background(), nil, &output, runner); exitCode != 0 {
		t.Fatalf("runCLI exit code = %d", exitCode)
	}
	if got := strings.Join(*calls, ","); got != "ping" {
		t.Fatalf("default dispatch = %q, want ping", got)
	}
	if !strings.Contains(output.String(), "ping-result") {
		t.Fatalf("default output = %q", output.String())
	}
	if !strings.HasPrefix(output.String(), "项目地址:") {
		t.Fatalf("default output prefix changed: %q", output.String())
	}
}

func TestRunCLIRejectsJSONOutsideTCPMode(t *testing.T) {
	runner, calls := offlineRunner()
	var output bytes.Buffer
	if exitCode := runCLI(context.Background(), []string{"-json"}, &output, runner); exitCode == 0 {
		t.Fatal("JSON outside TCP mode returned success")
	}
	if len(*calls) != 0 || !strings.Contains(output.String(), "仅支持") {
		t.Fatalf("unexpected dispatch/output: calls=%v output=%q", *calls, output.String())
	}
}

func TestRunCLITCPModeUsesStructuredTCPRunner(t *testing.T) {
	runner, calls := offlineRunner()
	var output bytes.Buffer
	if exitCode := runCLI(context.Background(), []string{"-tm", "tcp"}, &output, runner); exitCode != 0 {
		t.Fatalf("runCLI exit code = %d", exitCode)
	}
	if got := strings.Join(*calls, ","); got != "tcp" {
		t.Fatalf("tcp dispatch = %q, want tcp", got)
	}
	for _, value := range []string{"汇总 目标:1", "握手:1/1", "成功率:100.0%", "失败 DNS:0", "类别汇总", "异常/最慢目标 1/1", "目标", "成功", "丢包", "fixture", "1/1"} {
		if !strings.Contains(output.String(), value) {
			t.Errorf("TCP output %q does not contain %q", output.String(), value)
		}
	}
	for _, forbidden := range []string{"success=", "loss=", "errors="} {
		if strings.Contains(output.String(), forbidden) {
			t.Errorf("TCP output retained debug field %q: %q", forbidden, output.String())
		}
	}
}

func TestRunCLITCPFullFormat(t *testing.T) {
	runner, calls := offlineRunner()
	var output bytes.Buffer
	if exitCode := runCLI(context.Background(), []string{"-tm", "tcp", "-tcp-format", "full"}, &output, runner); exitCode != 0 {
		t.Fatalf("runCLI exit code = %d, output=%q", exitCode, output.String())
	}
	if got := strings.Join(*calls, ","); got != "tcp" {
		t.Fatalf("tcp dispatch = %q, want tcp", got)
	}
	if !strings.Contains(output.String(), "完整目标 1/1") || strings.Contains(output.String(), "类别汇总") {
		t.Fatalf("full TCP format was not selected: %q", output.String())
	}
}

func TestRunCLITCPCompactDetailLimit(t *testing.T) {
	runner := commandRunner{tcp: func(_ context.Context, _ pt.TCPProbeConfig, _ string) ([]pt.TCPResult, error) {
		return []pt.TCPResult{
			{Target: model.TCPTarget{Name: "fast-a"}, Attempts: 1, Successful: 1, P95: time.Millisecond},
			{Target: model.TCPTarget{Name: "middle-b"}, Attempts: 1, Successful: 1, P95: 2 * time.Millisecond},
			{Target: model.TCPTarget{Name: "slow-c"}, Attempts: 1, Successful: 1, P95: 3 * time.Millisecond},
		}, nil
	}}
	var output bytes.Buffer
	if exitCode := runCLI(context.Background(), []string{"-tm", "tcp", "-tcp-details", "1"}, &output, runner); exitCode != 0 {
		t.Fatalf("runCLI exit code = %d, output=%q", exitCode, output.String())
	}
	if !strings.Contains(output.String(), "异常/最慢目标 1/3") || !strings.Contains(output.String(), "slow-c") || strings.Contains(output.String(), "fast-a") || strings.Contains(output.String(), "middle-b") {
		t.Fatalf("compact detail limit was not applied: %q", output.String())
	}
}

func TestRunCLIRejectsInvalidTCPTextOptionsBeforeRunning(t *testing.T) {
	for _, args := range [][]string{
		{"-tm", "tcp", "-tcp-format", "verbose"},
		{"-tm", "tcp", "-tcp-details", "0"},
	} {
		runner, calls := offlineRunner()
		var output bytes.Buffer
		if exitCode := runCLI(context.Background(), args, &output, runner); exitCode == 0 {
			t.Fatalf("invalid args %v returned success", args)
		}
		if len(*calls) != 0 || !strings.Contains(output.String(), "错误") {
			t.Fatalf("invalid args scheduled work: args=%v calls=%v output=%q", args, *calls, output.String())
		}
	}
}

func TestRunCLIRejectsUnknownModeWithoutScheduling(t *testing.T) {
	runner, calls := offlineRunner()
	var output bytes.Buffer
	if exitCode := runCLI(context.Background(), []string{"-tm", "missing"}, &output, runner); exitCode == 0 {
		t.Fatal("unknown mode returned success")
	}
	if len(*calls) != 0 {
		t.Fatalf("unknown mode scheduled calls: %v", *calls)
	}
	if !strings.Contains(output.String(), "tcp") {
		t.Fatalf("supported mode output does not mention tcp: %q", output.String())
	}
}

func offlineRunner() (commandRunner, *[]string) {
	calls := make([]string, 0, 2)
	recordText := func(name string) func() string {
		return func() string {
			calls = append(calls, name)
			return name + "-result"
		}
	}
	return commandRunner{
		ping:     recordText("ping"),
		telegram: recordText("telegram"),
		website:  recordText("website"),
		tcp: func(_ context.Context, _ pt.TCPProbeConfig, _ string) ([]pt.TCPResult, error) {
			calls = append(calls, "tcp")
			return []pt.TCPResult{{
				Target:      model.TCPTarget{Name: "fixture", Host: "fixture.test", Port: 443},
				Attempts:    1,
				Successful:  1,
				Min:         time.Millisecond,
				Mean:        time.Millisecond,
				P50:         time.Millisecond,
				P95:         time.Millisecond,
				Max:         time.Millisecond,
				LossPercent: 0,
			}}, nil
		},
	}, &calls
}

func TestRunCLITCPParametersReachRunnerAndJSONIsClean(t *testing.T) {
	var gotConfig pt.TCPProbeConfig
	var gotTarget string
	runner := commandRunner{
		tcp: func(_ context.Context, config pt.TCPProbeConfig, target string) ([]pt.TCPResult, error) {
			gotConfig, gotTarget = config, target
			return []pt.TCPResult{{Target: model.TCPTarget{Host: "fixture.test", Port: 8443}, Attempts: config.Attempts}}, nil
		},
	}
	var output bytes.Buffer
	args := []string{"-tm", "tcp", "-json", "-attempts", "5", "-timeout", "750ms", "-concurrency", "7", "-target", "fixture.test:8443"}
	if exitCode := runCLI(context.Background(), args, &output, runner); exitCode != 0 {
		t.Fatalf("runCLI exit code = %d, output=%q", exitCode, output.String())
	}
	if gotConfig.Attempts != 5 || gotConfig.Timeout != 750*time.Millisecond || gotConfig.Concurrency != 7 || gotTarget != "fixture.test:8443" {
		t.Fatalf("TCP options not forwarded: config=%+v target=%q", gotConfig, gotTarget)
	}
	var results []pt.TCPResult
	if err := json.Unmarshal(output.Bytes(), &results); err != nil {
		t.Fatalf("stdout is not clean JSON: %v: %q", err, output.String())
	}
	if len(results) != 1 || results[0].Attempts != 5 {
		t.Fatalf("unexpected JSON results: %+v", results)
	}
}

func TestParseTCPTargetDefaultsPortAndAcceptsIPv6(t *testing.T) {
	for input, wantHost := range map[string]string{"example.test": "example.test", "2001:db8::1": "2001:db8::1"} {
		target, err := parseTCPTarget(input)
		if err != nil || target.Host != wantHost || target.Port != 443 {
			t.Fatalf("parseTCPTarget(%q) = %+v, %v", input, target, err)
		}
	}
}
