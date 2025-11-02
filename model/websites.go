package model

import "time"

// Website 网站配置
type Website struct {
	Name     string        // 网站名称
	URL      string        // 网站地址
	Avg      time.Duration // 平均延迟
	Tested   bool          // 是否已测试
	Category string        // 分类：search, social, video, ai, dev, shopping等
}

// PopularWebsites 流行网站列表
var PopularWebsites = []Website{
	// 搜索引擎
	{Name: "Google", URL: "https://www.google.com", Category: "search"},
	{Name: "Bing", URL: "https://www.bing.com", Category: "search"},
	
	// 社交媒体
	{Name: "Facebook", URL: "https://www.facebook.com", Category: "social"},
	{Name: "Twitter/X", URL: "https://www.twitter.com", Category: "social"},
	{Name: "Instagram", URL: "https://www.instagram.com", Category: "social"},
	{Name: "Reddit", URL: "https://www.reddit.com", Category: "social"},
	{Name: "TikTok", URL: "https://www.tiktok.com", Category: "social"},
	
	// 视频流媒体
	{Name: "YouTube", URL: "https://www.youtube.com", Category: "video"},
	{Name: "Netflix", URL: "https://www.netflix.com", Category: "video"},
	{Name: "DisneyPlus", URL: "https://www.disneyplus.com", Category: "video"},
	{Name: "PrimeVideo", URL: "https://www.primevideo.com", Category: "video"},
	{Name: "Spotify", URL: "https://www.spotify.com", Category: "video"},
	{Name: "Twitch", URL: "https://www.twitch.tv", Category: "video"},
	
	// AI 服务
	{Name: "OpenAI", URL: "https://chat.openai.com", Category: "ai"},
	{Name: "Claude", URL: "https://claude.ai", Category: "ai"},
	{Name: "Gemini", URL: "https://gemini.google.com", Category: "ai"},
	{Name: "Sora", URL: "https://sora.com", Category: "ai"},
	{Name: "MetaAI", URL: "https://www.meta.ai", Category: "ai"},
	
	// 开发平台
	{Name: "GitHub", URL: "https://www.github.com", Category: "dev"},
	{Name: "GitLab", URL: "https://gitlab.com", Category: "dev"},
	{Name: "StackOverflow", URL: "https://stackoverflow.com", Category: "dev"},
	{Name: "Docker Hub", URL: "https://hub.docker.com", Category: "dev"},
	
	// 云服务
	{Name: "AWS", URL: "https://aws.amazon.com", Category: "cloud"},
	{Name: "Azure", URL: "https://portal.azure.com", Category: "cloud"},
	{Name: "Google Cloud", URL: "https://console.cloud.google.com", Category: "cloud"},
	{Name: "DigitalOcean", URL: "https://www.digitalocean.com", Category: "cloud"},
	
	// 电商
	{Name: "Amazon", URL: "https://www.amazon.com", Category: "shopping"},
	{Name: "eBay", URL: "https://www.ebay.com", Category: "shopping"},
	{Name: "AliExpress", URL: "https://www.aliexpress.com", Category: "shopping"},
	
	// 工具
	{Name: "Wikipedia", URL: "https://www.wikipedia.org", Category: "tool"},
	{Name: "Steam", URL: "https://store.steampowered.com", Category: "gaming"},
	{Name: "Apple", URL: "https://www.apple.com", Category: "tech"},
	{Name: "Microsoft", URL: "https://www.microsoft.com", Category: "tech"},
	
	// 亚洲流媒体
	{Name: "Bilibili", URL: "https://www.bilibili.com", Category: "video"},
	{Name: "iQIYI", URL: "https://www.iq.com", Category: "video"},
	{Name: "ViuTV", URL: "https://www.viu.com", Category: "video"},
	{Name: "TVB Anywhere", URL: "https://www.tvbanywhere.com", Category: "video"},
	
	// 新闻
	{Name: "CNN", URL: "https://www.cnn.com", Category: "news"},
	{Name: "BBC", URL: "https://www.bbc.com", Category: "news"},
	{Name: "NYTimes", URL: "https://www.nytimes.com", Category: "news"},
}
