package pt

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
	probing "github.com/prometheus-community/pro-bing"
)

// pingTelegramDCByGolang 使用golang的ping库测试Telegram DC (重复3次取平均)
func pingTelegramDCByGolang(dc *model.TelegramDC) {
	if model.EnableLoger {
		defer Logger.Sync()
	}
	
	var totalDuration time.Duration
	successCount := 0
	
	// 重复测试3次
	for attempt := 0; attempt < 3; attempt++ {
		pinger, err := probing.NewPinger(dc.IP)
		if err != nil {
			logError(fmt.Sprintf("无法创建 pinger (尝试 %d/3): %v", attempt+1, err))
			continue
		}
		pinger.Count = 1 // 每次只ping一次
		pinger.Timeout = timeout
		err = pinger.Run()
		if err != nil {
			logError(fmt.Sprintf("ping 失败 (尝试 %d/3): %v", attempt+1, err))
			continue
		}
		stats := pinger.Statistics()
		if stats.PacketsRecv > 0 {
			totalDuration += stats.AvgRtt
			successCount++
			logError(fmt.Sprintf("Ping %s 成功 (尝试 %d/3): %dms", dc.Name, attempt+1, stats.AvgRtt.Milliseconds()))
		}
	}
	
	if successCount > 0 {
		dc.Avg = totalDuration / time.Duration(successCount)
		dc.Tested = true
	} else {
		dc.Avg = 0
		dc.Tested = false
	}
}

// pingTelegramDCByCMD 使用系统ping命令测试Telegram DC (重复3次取平均)
func pingTelegramDCByCMD(dc *model.TelegramDC) {
	if model.EnableLoger {
		defer Logger.Sync()
	}
	
	var totalDuration time.Duration
	successCount := 0
	rootPerm := hasRootPermission()
	logError(fmt.Sprintf("Root权限检查: %v", rootPerm))
	
	// 重复测试3次
	for attempt := 0; attempt < 3; attempt++ {
		var cmd *exec.Cmd
		if rootPerm {
			cmd = exec.Command("sudo", "ping", "-c1", "-W3", dc.IP)
		} else {
			cmd = exec.Command("ping", "-c1", "-W3", dc.IP)
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			logError(fmt.Sprintf("无法ping (尝试 %d/3): %v", attempt+1, err))
			continue
		}
		if model.EnableLoger {
			logError(string(output))
		}
		// 解析输出结果
		if !strings.Contains(string(output), "time=") {
			logError(fmt.Sprintf("ping 失败，未找到 time= (尝试 %d/3)", attempt+1))
			continue
		}
		var avgTime float64
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "time=") {
				matches := strings.Split(line, "time=")
				if len(matches) >= 2 {
					avgTime, err = strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(matches[1], "ms", "")), 64)
					if err != nil {
						logError(fmt.Sprintf("无法解析平均延迟 (尝试 %d/3): %v", attempt+1, err))
						continue
					}
					totalDuration += time.Duration(avgTime * float64(time.Millisecond))
					successCount++
					logError(fmt.Sprintf("Ping %s 成功 (尝试 %d/3): %.2fms", dc.Name, attempt+1, avgTime))
					break
				}
			}
		}
	}
	
	if successCount > 0 {
		dc.Avg = totalDuration / time.Duration(successCount)
		dc.Tested = true
	} else {
		dc.Avg = 0
		dc.Tested = false
	}
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
	// 添加 defer recover 防止 panic
	defer func() {
		if r := recover(); r != nil {
			logError(fmt.Sprintf("TelegramDCTest panic 恢复: %v", r))
		}
	}()

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
			defer func() {
				if r := recover(); r != nil {
					logError(fmt.Sprintf("pingTelegramDCSimple panic 恢复: %v", r))
				}
			}()
			pingTelegramDCSimple(&datacenters[index])
		}(i)
	}
	wg.Wait()

	// 按延迟从小到大排序
	sort.Slice(datacenters, func(i, j int) bool {
		// 未测试成功的标记为 9999ms
		if !datacenters[i].Tested || datacenters[i].Avg.Milliseconds() == 0 {
			datacenters[i].Avg = 9999 * time.Millisecond
		}
		if !datacenters[j].Tested || datacenters[j].Avg.Milliseconds() == 0 {
			datacenters[j].Avg = 9999 * time.Millisecond
		}
		return datacenters[i].Avg < datacenters[j].Avg
	})

	// 格式化输出结果，参考三网延迟测试的格式
	var result string
	count := 0
	for _, dc := range datacenters {
		// 每三个数据中心换行一次
		if count > 0 && count%3 == 0 {
			result += "\n"
		}
		count++

		avgStr := fmt.Sprintf("%4d", dc.Avg.Milliseconds())
		// 使用 "DC名称-位置" 作为显示名称
		name := fmt.Sprintf("%s %s", dc.Name, dc.Location)
		// 计算需要的填充空格，使名称列宽度为20
		padding := 20 - runewidth.StringWidth(name)
		if padding < 0 {
			padding = 0
		}
		result += fmt.Sprintf("%s%s%4s | ", name, strings.Repeat(" ", padding), avgStr)
	}

	return result
}
