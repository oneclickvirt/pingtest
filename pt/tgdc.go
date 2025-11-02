package pt

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
	probing "github.com/prometheus-community/pro-bing"
)

// pingTelegramDCByGolang 使用golang的ping库测试Telegram DC
func pingTelegramDCByGolang(dc *model.TelegramDC) {
	if model.EnableLoger {
		defer Logger.Sync()
	}
	pinger, err := probing.NewPinger(dc.IP)
	if err != nil {
		logError("无法创建 pinger: " + err.Error())
		return
	}
	pinger.Count = pingCount
	pinger.Timeout = timeout
	err = pinger.Run()
	if err != nil {
		logError("ping 失败: " + err.Error())
		return
	}
	stats := pinger.Statistics()
	if stats.PacketsRecv > 0 {
		dc.Avg = stats.AvgRtt
		dc.Tested = true
	} else {
		dc.Avg = 0
	}
}

// pingTelegramDCByCMD 使用系统ping命令测试Telegram DC
func pingTelegramDCByCMD(dc *model.TelegramDC) {
	if model.EnableLoger {
		defer Logger.Sync()
	}
	var cmd *exec.Cmd
	rootPerm := hasRootPermission()
	logError(fmt.Sprintf("Root权限检查: %v", rootPerm))
	if rootPerm {
		cmd = exec.Command("sudo", "ping", "-c1", "-W3", dc.IP)
	} else {
		cmd = exec.Command("ping", "-c1", "-W3", dc.IP)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		logError("无法ping: " + err.Error())
		return
	}
	if model.EnableLoger {
		logError(string(output))
	}
	// 解析输出结果
	if !strings.Contains(string(output), "time=") {
		logError("ping 失败，未找到 time=")
		return
	}
	var avgTime float64
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "time=") {
			matches := strings.Split(line, "time=")
			if len(matches) >= 2 {
				avgTime, err = strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(matches[1], "ms", "")), 64)
				if err != nil {
					logError("无法解析平均延迟: " + err.Error())
					return
				}
				break
			}
		}
	}
	dc.Avg = time.Duration(avgTime * float64(time.Millisecond))
	dc.Tested = true
}

// pingTelegramDCSimple 简化版的ping函数，用于测试单个Telegram DC
func pingTelegramDCSimple(dc *model.TelegramDC) {
	var cmd *exec.Cmd
	rootPerm := hasRootPermission()
	logError(fmt.Sprintf("Root权限检查: %v", rootPerm))
	if rootPerm {
		cmd = exec.Command("sudo", "ping", "-h")
	} else {
		cmd = exec.Command("ping", "-h")
	}
	output, err := cmd.CombinedOutput()
	if err != nil || (!strings.Contains(string(output), "Usage") && strings.Contains(string(output), "err")) {
		pingTelegramDCByGolang(dc)
	} else {
		pingTelegramDCByCMD(dc)
	}
	if dc.Tested {
		logError(fmt.Sprintf("Ping %s (%s) 成功，延迟: %dms", dc.Name, dc.IP, dc.Avg.Milliseconds()))
	} else {
		logError(fmt.Sprintf("Ping %s (%s) 失败", dc.Name, dc.IP))
	}
}

// TelegramDCTest 测试所有Telegram数据中心
func TelegramDCTest() string {
	if model.EnableLoger {
		InitLogger()
	}
	
	// 复制数据中心配置，避免修改原始数据
	datacenters := make([]model.TelegramDC, len(model.TelegramDataCenters))
	copy(datacenters, model.TelegramDataCenters)
	
	// 使用并发测试所有数据中心
	var wg sync.WaitGroup
	for i := range datacenters {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			pingTelegramDCSimple(&datacenters[index])
		}(i)
	}
	wg.Wait()
	
	// 按延迟从小到大排序
	sort.Slice(datacenters, func(i, j int) bool {
		// 未测试成功的放到最后
		if !datacenters[i].Tested || datacenters[i].Avg.Milliseconds() == 0 {
			return false
		}
		if !datacenters[j].Tested || datacenters[j].Avg.Milliseconds() == 0 {
			return true
		}
		return datacenters[i].Avg < datacenters[j].Avg
	})
	
	// 格式化输出结果
	var result string
	for _, dc := range datacenters {
		var latency string
		if dc.Tested && dc.Avg.Milliseconds() > 0 {
			latency = fmt.Sprintf("%d ms", dc.Avg.Milliseconds())
		} else {
			latency = "超时/失败"
		}
		result += fmt.Sprintf("%-5s %-30s %s\n", dc.Name, dc.Location, latency)
	}
	
	return result
}
