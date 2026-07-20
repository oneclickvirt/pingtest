package main

import (
	"bytes"
	"context"
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
	for _, value := range []string{"目标", "成功", "fixture", "1/1", "P95", "1.00ms"} {
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
		tcp: func(context.Context) []pt.TCPResult {
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
			}}
		},
	}, &calls
}
