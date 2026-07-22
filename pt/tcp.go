package pt

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
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

type TCPTextFormat string

const (
	TCPTextFormatCompact     TCPTextFormat = "compact"
	TCPTextFormatFull        TCPTextFormat = "full"
	DefaultTCPCompactDetails               = 8
)

type TCPFormatOptions struct {
	Format     TCPTextFormat
	MaxDetails int
	Sort       model.TCPSort
	Language   string
}

func DefaultTCPFormatOptions() TCPFormatOptions {
	return TCPFormatOptions{Format: TCPTextFormatFull, MaxDetails: DefaultTCPCompactDetails, Sort: model.TCPSortName}
}

// FormatTCPResults renders every target as one stable table row. API consumers
// should keep using TCPResult when they need individual samples.
func FormatTCPResults(results []TCPResult) string {
	return FormatTCPResultsWithOptions(results, DefaultTCPFormatOptions())
}

func FormatTCPResultsWithOptions(results []TCPResult, options TCPFormatOptions) string {
	labels := tcpLabelsForLanguage(options.Language)
	if len(results) == 0 {
		return labels.noTargets
	}
	var output strings.Builder
	summary := summarizeTCPResults(results)
	writeTCPOverallSummary(&output, summary, labels)
	if options.Sort != model.TCPSortLatency {
		options.Sort = model.TCPSortName
	}
	writeTCPResultTable(&output, results, options.Sort, labels)
	return trimTCPOutput(output.String())
}

type tcpTextLabels struct {
	noTargets       string
	summary         string
	targets         string
	handshakes      string
	successRate     string
	failed          string
	dns             string
	refused         string
	timeout         string
	other           string
	platform        string
	successAttempts string
	loss            string
}

func tcpLabelsForLanguage(language string) tcpTextLabels {
	if strings.EqualFold(strings.TrimSpace(language), "en") {
		return tcpTextLabels{
			noTargets: "No TCP targets available", summary: "Summary", targets: "Targets",
			handshakes: "Handshakes", successRate: "Success rate", failed: "Failed",
			dns: "DNS", refused: "Refused", timeout: "Timeout", other: "Other",
			platform: "Platform", successAttempts: "Success/Attempts", loss: "Loss",
		}
	}
	return tcpTextLabels{
		noTargets: "暂无可用的 TCP 测试目标", summary: "汇总", targets: "目标",
		handshakes: "握手", successRate: "成功率", failed: "失败",
		dns: "DNS", refused: "拒绝", timeout: "超时", other: "其他",
		platform: "平台", successAttempts: "成功/尝试", loss: "丢包",
	}
}

func writeTCPOverallSummary(output *strings.Builder, summary tcpResultsSummary, labels tcpTextLabels) {
	fmt.Fprintf(output, "%s %s:%d  %s:%d/%d  %s:%.1f%%  %s:%d",
		labels.summary, labels.targets, summary.Targets, labels.handshakes, summary.Successful, summary.Attempts,
		labels.successRate, summary.SuccessRate, labels.failed, summary.Failed)
	for _, failure := range []struct {
		label string
		count int
	}{
		{labels.dns, summary.DNS},
		{labels.refused, summary.Refused},
		{labels.timeout, summary.Timeout},
		{labels.other, summary.Other},
	} {
		if failure.count > 0 {
			fmt.Fprintf(output, "  %s:%d", failure.label, failure.count)
		}
	}
	output.WriteByte('\n')
}

func sortTCPResults(results []TCPResult, order model.TCPSort) []TCPResult {
	selected := append([]TCPResult(nil), results...)
	sort.SliceStable(selected, func(i, j int) bool {
		if order == model.TCPSortName {
			left, right := strings.ToLower(tcpResultName(selected[i])), strings.ToLower(tcpResultName(selected[j]))
			if left != right {
				return left < right
			}
			leftKey := strings.ToLower(strings.TrimSpace(selected[i].Target.Host)) + ":" + strconv.Itoa(selected[i].Target.Port)
			rightKey := strings.ToLower(strings.TrimSpace(selected[j].Target.Host)) + ":" + strconv.Itoa(selected[j].Target.Port)
			return leftKey < rightKey
		}
		leftErrors := tcpErrorTotal(classifyTCPResult(selected[i]))
		rightErrors := tcpErrorTotal(classifyTCPResult(selected[j]))
		if (leftErrors > 0) != (rightErrors > 0) {
			return leftErrors > 0
		}
		if leftErrors != rightErrors {
			return leftErrors > rightErrors
		}
		leftLoss, rightLoss := tcpLossPercent(selected[i]), tcpLossPercent(selected[j])
		if leftLoss != rightLoss {
			return leftLoss > rightLoss
		}
		if selected[i].P95 != selected[j].P95 {
			return selected[i].P95 > selected[j].P95
		}
		if selected[i].Max != selected[j].Max {
			return selected[i].Max > selected[j].Max
		}
		return tcpResultName(selected[i]) < tcpResultName(selected[j])
	})
	return selected
}

func tcpErrorTotal(summary tcpErrorSummary) int {
	return summary.DNS + summary.Refused + summary.Timeout + summary.Other
}

func writeTCPResultTable(output *strings.Builder, results []TCPResult, order model.TCPSort, labels tcpTextLabels) {
	type row struct {
		cells []string
	}
	headings := []string{
		labels.platform, labels.successAttempts, labels.loss,
		"Min", "Avg", "P50", "P95", "Max", "D", "R", "T", "O",
	}
	rows := make([]row, 0, len(results))
	widths := make([]int, len(headings))
	for index, heading := range headings {
		widths[index] = runewidth.StringWidth(heading)
		if index >= 3 {
			widths[index] = max(widths[index], 3)
		}
	}
	for _, result := range sortTCPResults(results, order) {
		classes := classifyTCPResult(result)
		current := row{cells: []string{
			tcpResultName(result),
			fmt.Sprintf("%d/%d", result.Successful, result.Attempts),
			fmt.Sprintf("%.1f%%", tcpLossPercent(result)),
			formatTCPMilliseconds(result.Min),
			formatTCPMilliseconds(result.Mean),
			formatTCPMilliseconds(result.P50),
			formatTCPMilliseconds(result.P95),
			formatTCPMilliseconds(result.Max),
			strconv.Itoa(classes.DNS),
			strconv.Itoa(classes.Refused),
			strconv.Itoa(classes.Timeout),
			strconv.Itoa(classes.Other),
		}}
		for index, value := range current.cells {
			widths[index] = max(widths[index], runewidth.StringWidth(value))
		}
		rows = append(rows, current)
	}
	writeTCPTableRow(output, headings, widths)
	for _, current := range rows {
		writeTCPTableRow(output, current.cells, widths)
	}
}

func writeTCPTableRow(output *strings.Builder, cells []string, widths []int) {
	for index, cell := range cells {
		if index > 0 {
			output.WriteString("  ")
		}
		if index == 0 {
			output.WriteString(padTCPCell(cell, widths[index]))
		} else {
			output.WriteString(padTCPCellLeft(cell, widths[index]))
		}
	}
	output.WriteByte('\n')
}

func trimTCPOutput(value string) string {
	lines := strings.Split(strings.TrimSuffix(value, "\n"), "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " ")
	}
	return strings.Join(lines, "\n")
}

type tcpErrorSummary struct {
	DNS, Refused, Timeout, Other int
}

type tcpResultsSummary struct {
	Targets, Attempts, Successful, Failed int
	SuccessRate                           float64
	tcpErrorSummary
	Min, Mean, P50, P95, Max time.Duration
}

func summarizeTCPResults(results []TCPResult) tcpResultsSummary {
	summary := tcpResultsSummary{Targets: len(results)}
	latencies := make([]time.Duration, 0)
	allSamples := true
	for _, result := range results {
		attempts := result.Attempts
		if attempts < result.Successful {
			attempts = result.Successful
		}
		summary.Attempts += attempts
		summary.Successful += result.Successful
		classes := classifyTCPResult(result)
		summary.DNS += classes.DNS
		summary.Refused += classes.Refused
		summary.Timeout += classes.Timeout
		summary.Other += classes.Other
		successSamples := 0
		for _, sample := range result.Samples {
			if sample.Success {
				latencies = append(latencies, sample.Duration)
				successSamples++
			}
		}
		if successSamples < result.Successful {
			allSamples = false
		}
	}
	summary.Failed = summary.DNS + summary.Refused + summary.Timeout + summary.Other
	if summary.Attempts > 0 {
		summary.SuccessRate = float64(summary.Successful) * 100 / float64(summary.Attempts)
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		summary.Min = latencies[0]
		summary.Max = latencies[len(latencies)-1]
		var total time.Duration
		for _, latency := range latencies {
			total += latency
		}
		summary.Mean = total / time.Duration(len(latencies))
		summary.P50 = percentile(latencies, 0.50)
		summary.P95 = percentile(latencies, 0.95)
	}
	if !allSamples {
		// A caller may provide aggregate TCPResult values without Samples. Keep
		// those values visible instead of pretending the derived percentiles are exact.
		summary.Min, summary.P50, summary.P95, summary.Max = 0, 0, 0, 0
		weighted := func(selectValue func(TCPResult) time.Duration) time.Duration {
			var total time.Duration
			var count int
			for _, result := range results {
				if result.Successful <= 0 {
					continue
				}
				total += selectValue(result) * time.Duration(result.Successful)
				count += result.Successful
			}
			if count == 0 {
				return 0
			}
			return total / time.Duration(count)
		}
		for _, result := range results {
			if result.Successful <= 0 {
				continue
			}
			if result.Min > 0 && (summary.Min == 0 || result.Min < summary.Min) {
				summary.Min = result.Min
			}
			if result.Max > summary.Max {
				summary.Max = result.Max
			}
		}
		summary.Mean = weighted(func(result TCPResult) time.Duration { return result.Mean })
		summary.P50 = weighted(func(result TCPResult) time.Duration { return result.P50 })
		summary.P95 = weighted(func(result TCPResult) time.Duration { return result.P95 })
	}
	return summary
}

func classifyTCPResult(result TCPResult) tcpErrorSummary {
	classes := tcpErrorSummary{
		DNS:     result.ErrorCounts[TCPErrorDNS],
		Refused: result.ErrorCounts[TCPErrorRefused],
		Timeout: result.ErrorCounts[TCPErrorTimeout],
	}
	classified := classes.DNS + classes.Refused + classes.Timeout
	for class, count := range result.ErrorCounts {
		if class != TCPErrorDNS && class != TCPErrorRefused && class != TCPErrorTimeout {
			classes.Other += count
		}
	}
	expectedFailures := result.Attempts - result.Successful
	if expectedFailures < result.Failed {
		expectedFailures = result.Failed
	}
	if expectedFailures > classified+classes.Other {
		classes.Other += expectedFailures - classified - classes.Other
	}
	return classes
}

func tcpResultName(result TCPResult) string {
	name := strings.TrimSpace(result.Target.Name)
	if name == "" {
		name = net.JoinHostPort(result.Target.Host, fmt.Sprintf("%d", result.Target.Port))
	}
	return name
}

func tcpLossPercent(result TCPResult) float64 {
	if result.Attempts <= 0 {
		return 0
	}
	return float64(result.Attempts-result.Successful) * 100 / float64(result.Attempts)
}

func padTCPCell(value string, width int) string {
	value = truncateTCPCell(strings.Join(strings.Fields(value), " "), width)
	padding := width - runewidth.StringWidth(value)
	if padding < 0 {
		padding = 0
	}
	return value + strings.Repeat(" ", padding)
}

func padTCPCellLeft(value string, width int) string {
	value = truncateTCPCell(strings.Join(strings.Fields(value), " "), width)
	padding := width - runewidth.StringWidth(value)
	if padding < 0 {
		padding = 0
	}
	return strings.Repeat(" ", padding) + value
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

func formatTCPMilliseconds(value time.Duration) string {
	if value <= 0 {
		return "-"
	}
	milliseconds := float64(value) / float64(time.Millisecond)
	if milliseconds >= 100 {
		return strconv.FormatFloat(math.Round(milliseconds), 'f', 0, 64)
	}
	return strconv.FormatFloat(milliseconds, 'f', 1, 64)
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
	return time.Duration(math.Round(float64(values[lower])*(1-weight) + float64(values[upper])*weight))
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
