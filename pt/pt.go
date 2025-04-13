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

// pingServerByGolang 使用golang的ping库进行测试
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

// pingServerSimple 简化版的ping函数，不需要WaitGroup
func pingServerSimple(server *model.Server) {
	cmd := exec.Command("sudo", "ping", "-h")
	output, err := cmd.CombinedOutput()
	if err != nil || (!strings.Contains(string(output), "Usage") && strings.Contains(string(output), "err")) {
		pingServerByGolang(server)
	} else {
		pingServerByCMD(server)
	}
	if server.Tested {
		logError(fmt.Sprintf("Ping %s (%s) 成功，延迟: %dms", server.Name, server.IP, server.Avg.Milliseconds()))
	} else {
		logError(fmt.Sprintf("Ping %s (%s) 失败", server.Name, server.IP))
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
	if model.EnableLoger {
		logError(string(output))
	}
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
	sem := make(chan struct{}, concurrency)
	testedProvinces := make(map[string]bool)
	for i := range servers {
		var province string
		if len(servers[i].Name) > 2 {
			province = servers[i].Name[2:]
		} else {
			province = servers[i].Name
		}
		if testedProvinces[province] {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(index int) {
			defer func() {
				<-sem
				wg.Done()
			}()
			pingServerSimple(servers[index])
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
	wg.Wait()
	var testedServers []*model.Server
	for _, server := range servers {
		if server.Tested && server.Avg.Milliseconds() > 0 {
			testedServers = append(testedServers, server)
		}
	}
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
	servers1 := getServers("cu")
	servers2 := getServers("ct")
	servers3 := getServers("cmcc")
	var allServers []*model.Server
	resultChan := make(chan []*model.Server, 3)
	var wga sync.WaitGroup
	wga.Add(3)
	go func() {
		defer wga.Done()
		resultChan <- processWithLimitedConcurrency(servers1, model.MaxConcurrency)
	}()
	go func() {
		defer wga.Done()
		resultChan <- processWithLimitedConcurrency(servers2, model.MaxConcurrency)
	}()
	go func() {
		defer wga.Done()
		resultChan <- processWithLimitedConcurrency(servers3, model.MaxConcurrency)
	}()
	go func() {
		wga.Wait()
		close(resultChan)
	}()
	for servers := range resultChan {
		allServers = append(allServers, servers...)
	}
	sort.Slice(allServers, func(i, j int) bool {
		return allServers[i].Avg < allServers[j].Avg
	})
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
