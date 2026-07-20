package pt

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/oneclickvirt/pingtest/model"
)

const (
	TCPErrorDNS      = "dns"
	TCPErrorTimeout  = "timeout"
	TCPErrorRefused  = "refused"
	TCPErrorCanceled = "canceled"
	TCPErrorNetwork  = "network"
	TCPErrorUnknown  = "unknown"
)

// TCPDialFunc is injectable so callers and tests can use a custom resolver,
// source address, proxy, or an offline dialer without changing probe logic.
type TCPDialFunc func(context.Context, string, string) (net.Conn, error)

// TCPProbeConfig controls a TCP handshake run. Zero values use the defaults.
type TCPProbeConfig struct {
	Attempts    int
	Timeout     time.Duration
	Concurrency int
	DialContext TCPDialFunc
	Now         func() time.Time
}

// TCPSample records one connection attempt. Duration is zero for failed
// attempts because no successful handshake latency exists to measure.
type TCPSample struct {
	Attempt    int           `json:"attempt"`
	Duration   time.Duration `json:"duration"`
	Success    bool          `json:"success"`
	ErrorClass string        `json:"error_class,omitempty"`
}

// TCPResult is the structured result for one endpoint.
type TCPResult struct {
	Target             model.TCPTarget `json:"target"`
	Attempts           int             `json:"attempts"`
	Successful         int             `json:"successful"`
	Failed             int             `json:"failed"`
	SuccessRatePercent float64         `json:"success_rate_percent"`
	LossPercent        float64         `json:"loss_percent"`
	Min                time.Duration   `json:"min"`
	Max                time.Duration   `json:"max"`
	Mean               time.Duration   `json:"mean"`
	P50                time.Duration   `json:"p50"`
	P95                time.Duration   `json:"p95"`
	Samples            []TCPSample     `json:"samples"`
	ErrorCounts        map[string]int  `json:"error_counts,omitempty"`
}

// DefaultTCPProbeConfig returns the standard low-cost TCP probe settings.
func DefaultTCPProbeConfig() TCPProbeConfig {
	return TCPProbeConfig{
		Attempts:    3,
		Timeout:     5 * time.Second,
		Concurrency: 16,
		DialContext: (&net.Dialer{}).DialContext,
		Now:         time.Now,
	}
}

func (config TCPProbeConfig) withDefaults() TCPProbeConfig {
	defaults := DefaultTCPProbeConfig()
	if config.Attempts <= 0 {
		config.Attempts = defaults.Attempts
	}
	if config.Timeout <= 0 {
		config.Timeout = defaults.Timeout
	}
	if config.Concurrency <= 0 {
		config.Concurrency = defaults.Concurrency
	}
	if config.DialContext == nil {
		config.DialContext = defaults.DialContext
	}
	if config.Now == nil {
		config.Now = defaults.Now
	}
	return config
}

// RunTCPProbe performs repeated TCP handshakes against target. It records
// failed attempts and their stable error classes rather than returning early,
// allowing callers to render partial results. An invalid target is returned
// as an error; cancellation and network failures remain in the result.
func RunTCPProbe(ctx context.Context, target model.TCPTarget, config TCPProbeConfig) (TCPResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(target.Host) == "" {
		return TCPResult{}, errors.New("tcp target host is empty")
	}
	if target.Port < 1 || target.Port > 65535 {
		return TCPResult{}, fmt.Errorf("tcp target port %d is invalid", target.Port)
	}
	config = config.withDefaults()
	result := TCPResult{
		Target:      target,
		Attempts:    config.Attempts,
		Samples:     make([]TCPSample, 0, config.Attempts),
		ErrorCounts: make(map[string]int),
	}
	address := net.JoinHostPort(target.Host, fmt.Sprintf("%d", target.Port))
	for attempt := 1; attempt <= config.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			result.recordFailure(attempt, classifyTCPError(err))
			for remaining := attempt + 1; remaining <= config.Attempts; remaining++ {
				result.recordFailure(remaining, TCPErrorCanceled)
			}
			break
		}
		attemptCtx, cancel := context.WithTimeout(ctx, config.Timeout)
		started := config.Now()
		conn, err := config.DialContext(attemptCtx, "tcp", address)
		elapsed := config.Now().Sub(started)
		cancel()
		if err != nil {
			result.recordFailure(attempt, classifyTCPError(err))
			continue
		}
		if conn != nil {
			_ = conn.Close()
		}
		if elapsed < 0 {
			elapsed = 0
		}
		result.recordSuccess(attempt, elapsed)
	}
	result.finish()
	return result, nil
}

// RunTCPProbes runs targets in bounded parallelism while preserving target
// order in the returned slice. A failed individual target is represented in
// its TCPResult; only invalid target errors are returned by RunTCPProbe and
// therefore do not abort the complete batch.
func RunTCPProbes(ctx context.Context, targets []model.TCPTarget, config TCPProbeConfig) []TCPResult {
	if ctx == nil {
		ctx = context.Background()
	}
	config = config.withDefaults()
	results := make([]TCPResult, len(targets))
	jobs := make(chan int)
	workers := config.Concurrency
	if workers > len(targets) {
		workers = len(targets)
	}
	if workers == 0 {
		return results
	}
	var workerWG sync.WaitGroup
	workerWG.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer workerWG.Done()
			for index := range jobs {
				result, err := RunTCPProbe(ctx, targets[index], config)
				if err != nil {
					result = TCPResult{Target: targets[index], Attempts: config.Attempts, ErrorCounts: map[string]int{TCPErrorUnknown: 1}}
				}
				results[index] = result
			}
		}()
	}
	for index := range targets {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			workerWG.Wait()
			return results
		}
	}
	close(jobs)
	workerWG.Wait()
	return results
}

// RunTCPRegistry probes the production registry formed from the existing
// website targets and the additional TCPbench targets. Callers that already
// have a data-driven target list should use RunTCPProbes directly.
func RunTCPRegistry(ctx context.Context, config TCPProbeConfig) []TCPResult {
	return RunTCPProbes(ctx, model.AllTCPTargets(), config)
}

// RunLoadedTCPRegistry resolves the repository-owned remote registry with an
// embedded fallback before probing it. The load result lets API callers report
// the actual data source without parsing target metadata.
func RunLoadedTCPRegistry(ctx context.Context, config TCPProbeConfig) ([]TCPResult, model.TCPTargetRegistryLoadResult, error) {
	loaded, err := model.LoadMergedTCPTargets(ctx, nil, model.DefaultTCPTargetRegistrySources(), 10)
	if err != nil {
		return nil, model.TCPTargetRegistryLoadResult{}, err
	}
	return RunTCPProbes(ctx, loaded.Targets, config), loaded, nil
}

// FormatTCPResults renders structured TCP results for the standalone CLI.
// API consumers should keep using TCPResult so durations and error classes do
// not need to be parsed back from terminal text.
func FormatTCPResults(results []TCPResult) string {
	if len(results) == 0 {
		return "暂无可用的 TCP 测试目标"
	}
	var output strings.Builder
	const (
		nameWidth = 28
		okWidth   = 9
		lossWidth = 9
		meanWidth = 11
		p95Width  = 11
	)
	fmt.Fprintf(&output, "%s  %s  %s  %s  %s\n",
		padTCPCell("目标", nameWidth), padTCPCell("成功", okWidth), padTCPCell("丢包", lossWidth),
		padTCPCell("平均", meanWidth), padTCPCell("P95", p95Width))
	for _, result := range results {
		name := strings.TrimSpace(result.Target.Name)
		if name == "" {
			name = net.JoinHostPort(result.Target.Host, fmt.Sprintf("%d", result.Target.Port))
		}
		fmt.Fprintf(&output, "%s  %s  %s  %s  %s\n",
			padTCPCell(name, nameWidth), padTCPCell(fmt.Sprintf("%d/%d", result.Successful, result.Attempts), okWidth),
			padTCPCell(fmt.Sprintf("%.1f%%", result.LossPercent), lossWidth), padTCPCell(formatTCPDuration(result.Mean), meanWidth),
			padTCPCell(formatTCPDuration(result.P95), p95Width),
		)
		classes := make([]string, 0, len(result.ErrorCounts))
		for class := range result.ErrorCounts {
			classes = append(classes, class)
		}
		sort.Strings(classes)
		errorsText := "-"
		var errorsBuilder strings.Builder
		for classIndex, class := range classes {
			if classIndex > 0 {
				errorsBuilder.WriteByte(' ')
			}
			fmt.Fprintf(&errorsBuilder, "%s:%d", class, result.ErrorCounts[class])
		}
		if errorsBuilder.Len() > 0 {
			errorsText = errorsBuilder.String()
		}
		detail := fmt.Sprintf("  范围: %s - %s  P50: %s  错误: %s",
			formatTCPDuration(result.Min), formatTCPDuration(result.Max), formatTCPDuration(result.P50), errorsText)
		output.WriteString(truncateTCPCell(detail, 80))
		output.WriteByte('\n')
	}
	return strings.TrimSuffix(output.String(), "\n")
}

func padTCPCell(value string, width int) string {
	value = truncateTCPCell(strings.Join(strings.Fields(value), " "), width)
	padding := width - runewidth.StringWidth(value)
	if padding < 0 {
		padding = 0
	}
	return value + strings.Repeat(" ", padding)
}

func truncateTCPCell(value string, width int) string {
	if runewidth.StringWidth(value) <= width {
		return value
	}
	if width <= 3 {
		return runewidth.Truncate(value, width, "")
	}
	return runewidth.Truncate(value, width-3, "") + "..."
}

func formatTCPDuration(value time.Duration) string {
	if value <= 0 {
		return "-"
	}
	if value >= time.Second {
		return fmt.Sprintf("%.2fs", value.Seconds())
	}
	if value >= time.Millisecond {
		return fmt.Sprintf("%.2fms", float64(value)/float64(time.Millisecond))
	}
	return fmt.Sprintf("%.2fus", float64(value)/float64(time.Microsecond))
}

func (result *TCPResult) recordSuccess(attempt int, duration time.Duration) {
	result.Successful++
	result.Samples = append(result.Samples, TCPSample{Attempt: attempt, Duration: duration, Success: true})
}

func (result *TCPResult) recordFailure(attempt int, errorClass string) {
	result.Failed++
	result.ErrorCounts[errorClass]++
	result.Samples = append(result.Samples, TCPSample{Attempt: attempt, ErrorClass: errorClass})
}

func (result *TCPResult) finish() {
	result.SuccessRatePercent = 0
	result.LossPercent = 0
	if result.Attempts > 0 {
		result.SuccessRatePercent = float64(result.Successful) * 100 / float64(result.Attempts)
		result.LossPercent = float64(result.Failed) * 100 / float64(result.Attempts)
	}
	latencies := make([]time.Duration, 0, result.Successful)
	for _, sample := range result.Samples {
		if sample.Success {
			latencies = append(latencies, sample.Duration)
		}
	}
	if len(latencies) == 0 {
		return
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	result.Min = latencies[0]
	result.Max = latencies[len(latencies)-1]
	var total time.Duration
	for _, latency := range latencies {
		total += latency
	}
	result.Mean = total / time.Duration(len(latencies))
	result.P50 = percentile(latencies, 0.50)
	result.P95 = percentile(latencies, 0.95)
}

func percentile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	position := quantile * float64(len(values)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return values[lower]
	}
	weight := position - float64(lower)
	return time.Duration(float64(values[lower])*(1-weight) + float64(values[upper])*weight)
}

func classifyTCPError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return TCPErrorCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return TCPErrorTimeout
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return TCPErrorDNS
	}
	if errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(strings.ToLower(err.Error()), "connection refused") {
		return TCPErrorRefused
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return TCPErrorTimeout
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return TCPErrorNetwork
	}
	return TCPErrorUnknown
}
