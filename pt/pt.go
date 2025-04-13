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

const (
	ICMPProtocolICMP = 1
	pingCount        = 3
	timeout          = 3 * time.Second
)

func pingServerByGolang(server *model.Server) {
	if model.EnableLoger {
		defer Logger.Sync()
	}
	pinger, err := probing.NewPinger(server.IP)
	if err != nil {
		logError("cannot create pinger: " + err.Error())
		return
	}
	pinger.Count = pingCount
	pinger.Timeout = timeout
	err = pinger.Run()
	if err != nil {
		logError("ping failed: " + err.Error())
		return
	}
	stats := pinger.Statistics()
	if stats.PacketsRecv > 0 {
		server.Avg = stats.AvgRtt
		server.Tested = true
	} else {
		server.Avg = 0
	}
}

func pingServer(server *model.Server, wg *sync.WaitGroup) {
	defer wg.Done()
	cmd := exec.Command("sudo", "ping", "-h")
	output, err := cmd.CombinedOutput()
	if err != nil || (!strings.Contains(string(output), "Usage") && strings.Contains(string(output), "err")) {
		pingServerByGolang(server)
	} else {
		pingServerByCMD(server)
	}
}

func pingServerByCMD(server *model.Server) {
	if model.EnableLoger {
		defer Logger.Sync()
	}
	// 执行 ping 命令
	cmd := exec.Command("sudo", "ping", "-c1", "-W3", server.IP)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logError("cannot ping: " + err.Error())
		return
	}
	logError(string(output))
	// 解析输出结果
	if !strings.Contains(string(output), "time=") {
		logError("ping failed without time=")
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
					logError("cannot parse avgTime: " + err.Error())
					return
				}
				break
			}
		}
	}
	server.Avg = time.Duration(avgTime * float64(time.Millisecond))
	server.Tested = true
}

// 使用有限并发工作池执行ping测试
func processWithLimitedConcurrency(servers []*model.Server, concurrency int) []*model.Server {
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency) // 信号量控制并发
	// 创建已测试省份的映射，避免重复测试同一省份
	testedProvinces := make(map[string]bool)
	for i := range servers {
		// 提取省份名称（假设名称格式为"运营商+省份"）
		var province string
		if len(servers[i].Name) > 2 {
			province = servers[i].Name[2:] // 跳过运营商名称（如"移动"）
		} else {
			province = servers[i].Name
		}
		// 如果该省份已经测试通过，跳过此服务器
		if testedProvinces[province] {
			continue
		}
		wg.Add(1)
		sem <- struct{}{} // 获取信号量
		go func(index int) {
			defer func() {
				<-sem // 释放信号量
				wg.Done()
			}()
			pingServer(servers[index], &sync.WaitGroup{})
			// 如果测试成功，标记该省份已测试
			if servers[index].Tested && servers[index].Avg.Milliseconds() > 0 {
				var province string
				if len(servers[index].Name) > 2 {
					province = servers[index].Name[2:]
				} else {
					province = servers[index].Name
				}
				testedProvinces[province] = true
			}
		}(i)
	}
	wg.Wait() // 等待所有任务完成
	// 只保留成功测试的服务器
	var testedServers []*model.Server
	for _, server := range servers {
		if server.Tested && server.Avg.Milliseconds() > 0 {
			testedServers = append(testedServers, server)
		}
	}
	// 对测试过的服务器按延迟排序
	sort.Slice(testedServers, func(i, j int) bool {
		return testedServers[i].Avg < testedServers[j].Avg
	})
	return testedServers
}

func PingTest() string {
	if model.EnableLoger {
		InitLogger()
	}
	var result string
	// 分别获取各运营商的服务器列表
	servers1 := getServers("cu")
	servers2 := getServers("ct")
	servers3 := getServers("cmcc")
	// 使用可配置的并发数进行测试
	var allServers []*model.Server
	var wga sync.WaitGroup
	wga.Add(3)
	// 为每个运营商分配独立的goroutine进行处理
	go func() {
		defer wga.Done()
		testedServers := processWithLimitedConcurrency(servers1, model.MaxConcurrency)
		allServers = append(allServers, testedServers...)
	}()
	go func() {
		defer wga.Done()
		testedServers := processWithLimitedConcurrency(servers2, model.MaxConcurrency)
		allServers = append(allServers, testedServers...)
	}()
	go func() {
		defer wga.Done()
		testedServers := processWithLimitedConcurrency(servers3, model.MaxConcurrency)
		allServers = append(allServers, testedServers...)
	}()
	wga.Wait()
	// 最终排序
	sort.Slice(allServers, func(i, j int) bool {
		return allServers[i].Avg < allServers[j].Avg
	})
	// 格式化输出结果
	var count int
	for _, server := range allServers {
		if server.Avg.Milliseconds() == 0 {
			continue
		}
		if count > 0 && count%3 == 0 {
			result += "\n"
		}
		count++
		avgStr := fmt.Sprintf("%4d", server.Avg.Milliseconds())
		name := server.Name
		padding := 16 - runewidth.StringWidth(name)
		if padding < 0 {
			padding = 0
		}
		result += fmt.Sprintf("%s%s%4s | ", name, strings.Repeat(" ", padding), avgStr)
	}
	return result
}
