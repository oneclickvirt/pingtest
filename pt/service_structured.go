package pt

import (
	"context"
	"strconv"

	"github.com/oneclickvirt/pingtest/model"
)

// InternationalICMPTargets returns a representative non-China target set for
// English and explicitly international runs.
func InternationalICMPTargets() []ICMPTarget {
	targets := []ICMPTarget{
		{ID: "cloudflare", Name: "Cloudflare", Host: "1.1.1.1", IPVersion: "ipv4"},
		{ID: "google", Name: "Google", Host: "8.8.8.8", IPVersion: "ipv4"},
		{ID: "quad9", Name: "Quad9", Host: "9.9.9.9", IPVersion: "ipv4"},
	}
	return append(targets, TelegramICMPTargets()...)
}

func RunInternationalICMPProbes(ctx context.Context, config ICMPProbeConfig) []ICMPResult {
	return RunICMPProbes(ctx, InternationalICMPTargets(), config)
}

// TelegramICMPTargets returns the existing Telegram DC registry in the
// structured ICMP format. The returned slice does not share mutable state with
// the legacy formatter.
func TelegramICMPTargets() []ICMPTarget {
	targets := make([]ICMPTarget, 0, len(model.TelegramDataCenters))
	for _, dc := range model.TelegramDataCenters {
		targets = append(targets, ICMPTarget{
			ID:        "telegram-dc-" + strconv.Itoa(dc.ID),
			Name:      dc.Name + " " + dc.Location,
			Host:      dc.IP,
			IPVersion: "ipv4",
		})
	}
	return targets
}

// RunTelegramICMPProbes performs the Telegram DC test through the pure Go,
// context-aware ICMP runner.
func RunTelegramICMPProbes(ctx context.Context, config ICMPProbeConfig) []ICMPResult {
	return RunICMPProbes(ctx, TelegramICMPTargets(), config)
}

// RunWebsiteTCPProbes performs the popular website latency test as bounded TCP
// handshakes, preserving DNS/refused/timeout classifications.
func RunWebsiteTCPProbes(ctx context.Context, config TCPProbeConfig) []TCPResult {
	return RunTCPProbes(ctx, model.WebsiteTCPTargets(), config)
}
