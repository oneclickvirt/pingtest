package model

import "time"

const PingTestVersion = "v0.0.6"

var EnableLoger = false

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
	Name string
	IP   string
	Port string
	Avg  time.Duration
}

type IcmpTarget struct {
	Province  string `json:"province"`
	IspCode   string `json:"isp_code"`
	Isp       string `json:"isp"`
	IPVersion string `json:"ip_version"`
	IPs       string `json:"ips"`
}
