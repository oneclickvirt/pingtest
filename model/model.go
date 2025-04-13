package model

import "time"

const PingTestVersion = "v0.0.7"

var EnableLoger = false
var MaxConcurrency = 100 // 并发量
var (
	IcmpTargets = "https://raw.githubusercontent.com/spiritLHLS/icmp_targets/refs/heads/main/nodes.json"
	NetCMCC     = "https://raw.githubusercontent.com/spiritLHLS/speedtest.net-CN-ID/main/CN_Mobile.csv"
	NetCT       = "https://raw.githubusercontent.com/spiritLHLS/speedtest.net-CN-ID/main/CN_Telecom.csv"
	NetCU       = "https://raw.githubusercontent.com/spiritLHLS/speedtest.net-CN-ID/main/CN_Unicom.csv"
	CnCMCC      = "https://raw.githubusercontent.com/spiritLHLS/speedtest.cn-CN-ID/main/mobile.csv"
	CnCT        = "https://raw.githubusercontent.com/spiritLHLS/speedtest.cn-CN-ID/main/telecom.csv"
	CnCU        = "https://raw.githubusercontent.com/spiritLHLS/speedtest.cn-CN-ID/main/unicom.csv"
	CdnList     = []string{
		"http://cdn1.spiritlhl.net/",
		"http://cdn2.spiritlhl.net/",
		"http://cdn3.spiritlhl.net/",
		"http://cdn4.spiritlhl.net/",
	}
)

type Server struct {
	Name       string
	IP         string
	Avg        time.Duration
	Tested     bool   // 标记是否已经测试过
	SourceType string // 记录来源类型
}

type IcmpTarget struct {
	Province  string `json:"province"`
	IspCode   string `json:"isp_code"`
	Isp       string `json:"isp"`
	IPVersion string `json:"ip_version"`
	IPs       string `json:"ips"`
}
