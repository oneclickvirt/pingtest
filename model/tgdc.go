package model

import "time"

// TelegramDC Telegram 数据中心配置
type TelegramDC struct {
	ID       int           // DC ID
	Name     string        // DC 名称
	Location string        // 地理位置
	IP       string        // IP 地址
	Avg      time.Duration // 平均延迟
	Tested   bool          // 是否已测试
}

// TelegramDataCenters 定义 Telegram 的 5 个数据中心
// 参考 https://github.com/OctoGramApp/octogramapp.github.io
var TelegramDataCenters = []TelegramDC{
	{
		ID:       1,
		Name:     "TG-DC1",
		Location: "MIA USA",
		IP:       "149.154.175.50",
		Tested:   false,
	},
	{
		ID:       2,
		Name:     "TG-DC2",
		Location: "AMS NL",
		IP:       "149.154.167.50",
		Tested:   false,
	},
	{
		ID:       3,
		Name:     "TG-DC3",
		Location: "MIA USA",
		IP:       "149.154.175.100",
		Tested:   false,
	},
	{
		ID:       4,
		Name:     "TG-DC4",
		Location: "AMS NL",
		IP:       "149.154.167.91",
		Tested:   false,
	},
	{
		ID:       5,
		Name:     "TG-DC5",
		Location: "Singapore",
		IP:       "91.108.56.100",
		Tested:   false,
	},
}
