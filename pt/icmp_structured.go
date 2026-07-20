package pt

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

type ICMPTarget struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	IPVersion string `json:"ip_version"`
}

type ICMPResult struct {
	Target      ICMPTarget    `json:"target"`
	Status      string        `json:"status"`
	Sent        int           `json:"sent"`
	Received    int           `json:"received"`
	LossPercent float64       `json:"loss_percent"`
	Min         time.Duration `json:"min"`
	Max         time.Duration `json:"max"`
	Mean        time.Duration `json:"mean"`
	P50         time.Duration `json:"p50"`
	P95         time.Duration `json:"p95"`
	Error       string        `json:"error,omitempty"`
}

type ICMPProbeConfig struct {
	Count       int
	Timeout     time.Duration
	Concurrency int
	Probe       func(context.Context, ICMPTarget, int, time.Duration) ICMPResult
}

func RunICMPProbes(ctx context.Context, targets []ICMPTarget, config ICMPProbeConfig) []ICMPResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if config.Count <= 0 {
		config.Count = 3
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if config.Concurrency <= 0 {
		config.Concurrency = 8
	}
	if config.Probe == nil {
		config.Probe = probeICMPTarget
	}
	results := make([]ICMPResult, len(targets))
	jobs := make(chan int)
	workers := min(config.Concurrency, len(targets))
	var wait sync.WaitGroup
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()
			for index := range jobs {
				results[index] = config.Probe(ctx, targets[index], config.Count, config.Timeout)
			}
		}()
	}
	for index := range targets {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			wait.Wait()
			markPendingICMP(results, targets, ctx.Err())
			return results
		}
	}
	close(jobs)
	wait.Wait()
	return results
}

func probeICMPTarget(ctx context.Context, target ICMPTarget, count int, timeout time.Duration) ICMPResult {
	result := ICMPResult{Target: target, Status: "unavailable", Sent: count, LossPercent: 100}
	if strings.TrimSpace(target.Host) == "" {
		result.Error = "missing host"
		return result
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	pinger, err := probing.NewPinger(target.Host)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	pinger.Count = count
	pinger.Timeout = timeout
	pinger.SetPrivileged(false)
	if strings.EqualFold(target.IPVersion, "ipv4") {
		pinger.SetNetwork("ip4")
	} else if strings.EqualFold(target.IPVersion, "ipv6") {
		pinger.SetNetwork("ip6")
	}
	if err := pinger.RunWithContext(probeCtx); err != nil && probeCtx.Err() != nil {
		result.Status = icmpContextStatus(probeCtx.Err())
		result.Error = probeCtx.Err().Error()
		return result
	} else if err != nil {
		result.Error = err.Error()
		return result
	}
	statistics := pinger.Statistics()
	result.Sent, result.Received = statistics.PacketsSent, statistics.PacketsRecv
	result.LossPercent = statistics.PacketLoss
	result.Min, result.Max, result.Mean = statistics.MinRtt, statistics.MaxRtt, statistics.AvgRtt
	rtts := append([]time.Duration(nil), statistics.Rtts...)
	sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })
	result.P50, result.P95 = durationPercentile(rtts, .50), durationPercentile(rtts, .95)
	if result.Received > 0 {
		result.Status = "ok"
		if result.Received < result.Sent {
			result.Status = "partial"
		}
	}
	return result
}

func markPendingICMP(results []ICMPResult, targets []ICMPTarget, err error) {
	for index := range results {
		if results[index].Target.Host != "" {
			continue
		}
		results[index] = ICMPResult{Target: targets[index], Status: icmpContextStatus(err), Error: err.Error()}
	}
}

func icmpContextStatus(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "unavailable"
}

func durationPercentile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	position := quantile * float64(len(values)-1)
	lower, upper := int(position), int(position+.999999999)
	if upper >= len(values) {
		upper = len(values) - 1
	}
	if lower == upper {
		return values[lower]
	}
	fraction := position - float64(lower)
	return values[lower] + time.Duration(math.Round(float64(values[upper]-values[lower])*fraction))
}
