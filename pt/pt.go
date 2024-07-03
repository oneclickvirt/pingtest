package pt

import (
	"fmt"
	"runtime"
	"time"

	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
	"github.com/prometheus-community/pro-bing"
)

func PingTest() string {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	var result string
	// 要ping的目标主机
	target := "www.google.com"
	// 创建一个新的pinger
	pinger, err := probing.NewPinger(target)
	if err != nil {
		if model.EnableLoger {
			Logger.Info("cannot create Pinger: " + err.Error())
		}
	}
	// 设置ping的次数
	pinger.Count = 3
	pinger.Size = 24
	pinger.Interval = time.Second
	pinger.Timeout = time.Second * 3
	pinger.TTL = 64
	if runtime.GOOS != "windows" {
		pinger.SetPrivileged(false)
	} else {
		pinger.SetPrivileged(true)
	}
	// 开始ping操作
	err = pinger.Run() // 阻塞
	if err != nil {
		if model.EnableLoger {
			Logger.Info("ping failed: " + err.Error())
		}
		pinger.SetPrivileged(true) // 无特权模式操作失败，切换特权模式
		err = pinger.Run() // 阻塞
		if err != nil {
			if model.EnableLoger {
				Logger.Info("ping failed: " + err.Error())
			}
		}
	}
	// 获取ping统计信息
	stats := pinger.Statistics()
	// 打印ping统计信息
	result += fmt.Sprintf("\n--- %s ping statistics ---\n", stats.Addr)
	result += fmt.Sprintf("%d packets transmitted, %d packets received, %v%% packet loss\n",
		stats.PacketsSent, stats.PacketsRecv, stats.PacketLoss)
	result += fmt.Sprintf("round-trip min/avg/max/stddev = %v/%v/%v/%v\n",
		stats.MinRtt, stats.AvgRtt, stats.MaxRtt, stats.StdDevRtt)
	return result
}
