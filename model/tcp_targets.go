package model

import (
	"net/url"
	"strconv"
	"strings"
)

// TCPTarget describes an endpoint used by the TCP handshake probe.
type TCPTarget struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Category string `json:"category,omitempty"`
	Source   string `json:"source,omitempty"`
}

// tcpBenchTargets contains the public HTTPS endpoints used by TCPbench. It is
// kept as data so callers can replace or filter the registry without changing
// probe logic.
var tcpBenchTargets = []TCPTarget{
	{Name: "NYTimes", Host: "www.nytimes.com"},
	{Name: "TheGuardian", Host: "www.theguardian.com"},
	{Name: "CNN", Host: "www.cnn.com"},
	{Name: "Vercel", Host: "vercel.com"},
	{Name: "Reddit", Host: "www.reddit.com"},
	{Name: "BBC", Host: "www.bbc.com"},
	{Name: "Azure", Host: "azure.microsoft.com"},
	{Name: "Twitch", Host: "www.twitch.tv"},
	{Name: "Adobe", Host: "www.adobe.com"},
	{Name: "TikTok", Host: "www.tiktok.com"},
	{Name: "Apple", Host: "www.apple.com"},
	{Name: "PayPal", Host: "www.paypal.com"},
	{Name: "YahooMail", Host: "mail.yahoo.com"},
	{Name: "Steam", Host: "store.steampowered.com"},
	{Name: "Bing", Host: "www.bing.com"},
	{Name: "Discord", Host: "discord.com"},
	{Name: "Zoom", Host: "zoom.us"},
	{Name: "GitLab", Host: "gitlab.com"},
	{Name: "Twitter/X", Host: "twitter.com"},
	{Name: "Udemy", Host: "www.udemy.com"},
	{Name: "Cloudflare", Host: "www.cloudflare.com"},
	{Name: "Notion", Host: "www.notion.so"},
	{Name: "Midjourney", Host: "www.midjourney.com"},
	{Name: "Shopify", Host: "www.shopify.com"},
	{Name: "DeepL", Host: "www.deepl.com"},
	{Name: "Claude", Host: "claude.ai"},
	{Name: "ChatGPT", Host: "chatgpt.com"},
	{Name: "Perplexity", Host: "www.perplexity.ai"},
	{Name: "Medium", Host: "medium.com"},
	{Name: "Copilot", Host: "copilot.microsoft.com"},
	{Name: "Coinbase", Host: "www.coinbase.com"},
	{Name: "Microsoft", Host: "www.microsoft.com"},
	{Name: "Facebook", Host: "www.facebook.com"},
	{Name: "Instagram", Host: "www.instagram.com"},
	{Name: "WhatsApp", Host: "web.whatsapp.com"},
	{Name: "Dropbox", Host: "www.dropbox.com"},
	{Name: "Spotify", Host: "www.spotify.com"},
	{Name: "GoogleCloud", Host: "cloud.google.com"},
	{Name: "Slack", Host: "slack.com"},
	{Name: "Telegram", Host: "web.telegram.org"},
	{Name: "GitHub", Host: "github.com"},
	{Name: "Wikipedia", Host: "www.wikipedia.org"},
	{Name: "Gemini", Host: "gemini.google.com"},
	{Name: "eBay", Host: "www.ebay.com"},
	{Name: "Google", Host: "www.google.com"},
	{Name: "Stripe", Host: "stripe.com"},
	{Name: "Gmail", Host: "mail.google.com"},
	{Name: "Netflix", Host: "www.netflix.com"},
	{Name: "edX", Host: "www.edx.org"},
	{Name: "YouTube", Host: "www.youtube.com"},
	{Name: "AWS", Host: "aws.amazon.com"},
	{Name: "Coursera", Host: "www.coursera.org"},
	{Name: "Trello", Host: "trello.com"},
	{Name: "Canva", Host: "www.canva.com"},
	{Name: "ProtonMail", Host: "mail.proton.me"},
	{Name: "Figma", Host: "www.figma.com"},
	{Name: "KhanAcademy", Host: "www.khanacademy.org"},
	{Name: "EpicGames", Host: "www.epicgames.com"},
	{Name: "Amazon", Host: "www.amazon.com"},
	{Name: "Outlook", Host: "outlook.live.com"},
	{Name: "Xbox", Host: "www.xbox.com"},
	{Name: "Oracle", Host: "www.oracle.com"},
	{Name: "PlayStation", Host: "www.playstation.com"},
	{Name: "Salesforce", Host: "www.salesforce.com"},
}

// AllTCPTargets returns a copy of the registry formed from the existing
// website list and TCPbench endpoints. Existing website entries take
// precedence when their normalized host and port are already present.
func AllTCPTargets() []TCPTarget {
	result := make([]TCPTarget, 0, len(PopularWebsites)+len(tcpBenchTargets))
	seen := make(map[string]int, cap(result))

	for _, target := range WebsiteTCPTargets() {
		key := targetKey(target)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = len(result)
		result = append(result, target)
	}
	for _, target := range tcpBenchTargets {
		if target.Port == 0 {
			target.Port = 443
		}
		target.Source = "tcpbench"
		key := targetKey(target)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = len(result)
		result = append(result, target)
	}
	return result
}

// WebsiteTCPTargets returns a normalized copy of the existing popular website
// registry for context-aware TCP latency probes.
func WebsiteTCPTargets() []TCPTarget {
	result := make([]TCPTarget, 0, len(PopularWebsites))
	seen := make(map[string]struct{}, len(PopularWebsites))
	for _, website := range PopularWebsites {
		target, ok := websiteTarget(website)
		if !ok {
			continue
		}
		target.Source = "popular-websites"
		key := targetKey(target)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, target)
	}
	return result
}

func websiteTarget(website Website) (TCPTarget, bool) {
	parsed, err := url.Parse(strings.TrimSpace(website.URL))
	if err != nil || parsed.Hostname() == "" {
		return TCPTarget{}, false
	}
	port := 443
	if parsed.Port() != "" {
		parsedPort, err := strconv.Atoi(parsed.Port())
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return TCPTarget{}, false
		}
		port = parsedPort
	}
	return TCPTarget{
		Name:     website.Name,
		Host:     strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")),
		Port:     port,
		Category: website.Category,
	}, true
}

func targetKey(target TCPTarget) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(target.Host), ".")) + ":" + strconv.Itoa(target.Port)
}
