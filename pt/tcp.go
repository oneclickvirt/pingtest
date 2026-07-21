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
}

func DefaultTCPFormatOptions() TCPFormatOptions {
	return TCPFormatOptions{Format: TCPTextFormatCompact, MaxDetails: DefaultTCPCompactDetails}
}

// FormatTCPResults defaults to compact text for CLI and legacy embedding.
// API consumers should keep using TCPResult for complete per-target data.
func FormatTCPResults(results []TCPResult) string {
	return FormatTCPResultsWithOptions(results, DefaultTCPFormatOptions())
}

func FormatTCPResultsWithOptions(results []TCPResult, options TCPFormatOptions) string {
	if len(results) == 0 {
		return "暂无可用的 TCP 测试目标"
	}
	if options.Format != TCPTextFormatFull {
		options.Format = TCPTextFormatCompact
	}
	if options.MaxDetails <= 0 {
		options.MaxDetails = DefaultTCPCompactDetails
	}
	var output strings.Builder
	summary := summarizeTCPResults(results)
	writeTCPOverallSummary(&output, summary)
	groups := groupTCPResults(results)
	if options.Format == TCPTextFormatFull {
		writeTCPFullDetails(&output, groups, len(results))
	} else {
		writeTCPCompactDetails(&output, groups, results, options.MaxDetails)
	}
	return trimTCPOutput(output.String())
}

type tcpResultGroup struct {
	Name    string
	Results []TCPResult
	Summary tcpResultsSummary
}

func writeTCPOverallSummary(output *strings.Builder, summary tcpResultsSummary) {
	fmt.Fprintf(output, "汇总 目标:%d  握手:%d/%d  成功率:%.1f%%  失败:%d\n",
		summary.Targets, summary.Successful, summary.Attempts, summary.SuccessRate, summary.Failed)
	fmt.Fprintf(output, "延迟 Min/Avg/P50/P95/Max: %s\n", formatTCPDurationSet(summary.Min, summary.Mean, summary.P50, summary.P95, summary.Max))
	fmt.Fprintf(output, "失败 DNS:%d  拒绝:%d  超时:%d  其他:%d\n", summary.DNS, summary.Refused, summary.Timeout, summary.Other)
}

func groupTCPResults(results []TCPResult) []tcpResultGroup {
	groups := make([]tcpResultGroup, 0)
	indexes := make(map[string]int)
	for _, result := range results {
		category := strings.TrimSpace(result.Target.Category)
		if category == "" {
			category = "未分类"
		}
		index, exists := indexes[category]
		if !exists {
			index = len(groups)
			indexes[category] = index
			groups = append(groups, tcpResultGroup{Name: category})
		}
		groups[index].Results = append(groups[index].Results, result)
	}
	for index := range groups {
		groups[index].Summary = summarizeTCPResults(groups[index].Results)
	}
	return groups
}

func writeTCPCompactDetails(output *strings.Builder, groups []tcpResultGroup, results []TCPResult, maxDetails int) {
	const (
		categoryWidth  = 16
		targetsWidth   = 6
		handshakeWidth = 10
		rateWidth      = 8
		latencyWidth   = 19
		errorsWidth    = 11
	)
	output.WriteString("类别汇总\n")
	fmt.Fprintf(output, "%s  %s  %s  %s  %s  %s\n",
		padTCPCell("类别", categoryWidth), padTCPCell("目标", targetsWidth), padTCPCell("握手", handshakeWidth),
		padTCPCell("成功率", rateWidth), padTCPCell("Avg/P95/Max", latencyWidth), padTCPCell("D/R/T/O", errorsWidth))
	for _, group := range groups {
		summary := group.Summary
		fmt.Fprintf(output, "%s  %s  %s  %s  %s  %s\n",
			padTCPCell(tcpCategoryLabel(group.Name), categoryWidth), padTCPCell(strconv.Itoa(summary.Targets), targetsWidth),
			padTCPCell(fmt.Sprintf("%d/%d", summary.Successful, summary.Attempts), handshakeWidth),
			padTCPCell(fmt.Sprintf("%.1f%%", summary.SuccessRate), rateWidth),
			padTCPCell(formatTCPDurationSet(summary.Mean, summary.P95, summary.Max), latencyWidth),
			padTCPCell(fmt.Sprintf("%d/%d/%d/%d", summary.DNS, summary.Refused, summary.Timeout, summary.Other), errorsWidth))
	}

	selected := selectTCPDetailResults(results, maxDetails)
	fmt.Fprintf(output, "异常/最慢目标 %d/%d\n", len(selected), len(results))
	const (
		nameWidth           = 16
		detailCategoryWidth = 16
		successWidth        = 7
		lossWidth           = 7
		p95Width            = 10
		detailErrorsWidth   = 11
	)
	fmt.Fprintf(output, "%s  %s  %s  %s  %s  %s\n",
		padTCPCell("目标", nameWidth), padTCPCell("类别", detailCategoryWidth), padTCPCell("成功", successWidth),
		padTCPCell("丢包", lossWidth), padTCPCell("P95", p95Width), padTCPCell("D/R/T/O", detailErrorsWidth))
	for _, result := range selected {
		classes := classifyTCPResult(result)
		category := strings.TrimSpace(result.Target.Category)
		if category == "" {
			category = "未分类"
		}
		fmt.Fprintf(output, "%s  %s  %s  %s  %s  %s\n",
			padTCPCell(tcpResultName(result), nameWidth), padTCPCell(tcpCategoryLabel(category), detailCategoryWidth),
			padTCPCell(fmt.Sprintf("%d/%d", result.Successful, result.Attempts), successWidth),
			padTCPCell(fmt.Sprintf("%.1f%%", tcpLossPercent(result)), lossWidth),
			padTCPCell(formatTCPDurationSet(result.P95), p95Width),
			padTCPCell(fmt.Sprintf("%d/%d/%d/%d", classes.DNS, classes.Refused, classes.Timeout, classes.Other), detailErrorsWidth))
	}
}

func selectTCPDetailResults(results []TCPResult, limit int) []TCPResult {
	selected := append([]TCPResult(nil), results...)
	sort.SliceStable(selected, func(i, j int) bool {
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
	if len(selected) > limit {
		selected = selected[:limit]
	}
	return selected
}

func tcpErrorTotal(summary tcpErrorSummary) int {
	return summary.DNS + summary.Refused + summary.Timeout + summary.Other
}

func tcpCategoryLabel(category string) string {
	labels := map[string]string{
		"search": "搜索", "social": "社交", "video": "视频", "ai": "AI",
		"dev": "开发", "cloud": "云服务", "shopping": "购物", "tool": "工具",
		"gaming": "游戏", "tech": "科技", "news": "新闻", "global": "全球",
	}
	if label, ok := labels[strings.ToLower(strings.TrimSpace(category))]; ok {
		return label
	}
	return category
}

func writeTCPFullDetails(output *strings.Builder, groups []tcpResultGroup, total int) {
	const (
		nameWidth    = 16
		successWidth = 7
		lossWidth    = 7
		latencyWidth = 31
		errorsWidth  = 11
	)
	fmt.Fprintf(output, "完整目标 %d/%d\n", total, total)
	fmt.Fprintf(output, "%s  %s  %s  %s  %s\n", padTCPCell("目标", nameWidth), padTCPCell("成功", successWidth), padTCPCell("丢包", lossWidth), padTCPCell("Min/Avg/P50/P95/Max", latencyWidth), padTCPCell("D/R/T/O", errorsWidth))
	for _, group := range groups {
		fmt.Fprintf(output, "[%s] %d个目标\n", group.Name, len(group.Results))
		for _, result := range group.Results {
			classes := classifyTCPResult(result)
			fmt.Fprintf(output, "%s  %s  %s  %s  %s\n",
				padTCPCell(tcpResultName(result), nameWidth),
				padTCPCell(fmt.Sprintf("%d/%d", result.Successful, result.Attempts), successWidth),
				padTCPCell(fmt.Sprintf("%.1f%%", tcpLossPercent(result)), lossWidth),
				padTCPCell(formatTCPDurationSet(result.Min, result.Mean, result.P50, result.P95, result.Max), latencyWidth),
				padTCPCell(fmt.Sprintf("%d/%d/%d/%d", classes.DNS, classes.Refused, classes.Timeout, classes.Other), errorsWidth),
			)
		}
	}
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

func truncateTCPCell(value string, width int) string {
	if runewidth.StringWidth(value) <= width {
		return value
	}
	if width <= 3 {
		return runewidth.Truncate(value, width, "")
	}
	return runewidth.Truncate(value, width-3, "") + "..."
}

func formatTCPDurationSet(values ...time.Duration) string {
	max := time.Duration(0)
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	if max <= 0 {
		return "-"
	}
	unit := time.Microsecond
	suffix := "us"
	if max >= time.Second {
		unit, suffix = time.Second, "s"
	} else if max >= time.Millisecond {
		unit, suffix = time.Millisecond, "ms"
	}
	parts := make([]string, len(values))
	for index, value := range values {
		if value <= 0 {
			parts[index] = "-"
			continue
		}
		parts[index] = fmt.Sprintf("%.1f", float64(value)/float64(unit))
	}
	return strings.Join(parts, "/") + suffix
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
