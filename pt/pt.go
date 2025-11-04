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

// pingServerByGolang 使用golang的ping库进行测试 (重复3次取平均)
func pingServerByGolang(server *model.Server) {
	if model.EnableLoger {
		defer Logger.Sync()
	}
	
	var totalDuration time.Duration
	successCount := 0
	
	// 重复测试3次
	for attempt := 0; attempt < 3; attempt++ {
		pinger, err := probing.NewPinger(server.IP)
		if err != nil {
			logError(fmt.Sprintf("cannot create pinger (尝试 %d/3): %v", attempt+1, err))
			continue
		}
		pinger.Count = 1 // 每次只ping一次
		pinger.Timeout = timeout
		err = pinger.Run()
		if err != nil {
			logError(fmt.Sprintf("ping failed (尝试 %d/3): %v", attempt+1, err))
			continue
		}
		stats := pinger.Statistics()
		if stats.PacketsRecv > 0 {
			totalDuration += stats.AvgRtt
			successCount++
			logError(fmt.Sprintf("Ping %s 成功 (尝试 %d/3): %dms", server.Name, attempt+1, stats.AvgRtt.Milliseconds()))
		}
	}
	
	if successCount > 0 {
		server.Avg = totalDuration / time.Duration(successCount)
		server.Tested = true
	} else {
		server.Avg = 0
		server.Tested = false
	}
}

// pingServerSimple 简化版的ping函数，不需要WaitGroup
func pingServerSimple(server *model.Server) {
	var cmd *exec.Cmd
	rootPerm := hasRootPermission()
	logError(fmt.Sprintf("Root permission check: %v", rootPerm))
	if rootPerm {
		cmd = exec.Command("sudo", "ping", "-h")
	} else {
		cmd = exec.Command("ping", "-h")
	}
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
	
	var totalDuration time.Duration
	successCount := 0
	rootPerm := hasRootPermission()
	logError(fmt.Sprintf("Root permission check: %v", rootPerm))
	
	// 重复测试3次
	for attempt := 0; attempt < 3; attempt++ {
		// 执行 ping 命令
		var cmd *exec.Cmd
		if rootPerm {
			cmd = exec.Command("sudo", "ping", "-c1", "-W3", server.IP)
		} else {
			cmd = exec.Command("ping", "-c1", "-W3", server.IP)
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			logError(fmt.Sprintf("cannot ping (尝试 %d/3): %v", attempt+1, err))
			continue
		}
		if model.EnableLoger {
			logError(string(output))
		}
		// 解析输出结果
		if !strings.Contains(string(output), "time=") {
			logError(fmt.Sprintf("ping failed without time= (尝试 %d/3)", attempt+1))
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
						logError(fmt.Sprintf("cannot parse avgTime (尝试 %d/3): %v", attempt+1, err))
						continue
					}
					totalDuration += time.Duration(avgTime * float64(time.Millisecond))
					successCount++
					logError(fmt.Sprintf("Ping %s 成功 (尝试 %d/3): %.2fms", server.Name, attempt+1, avgTime))
					break
				}
			}
		}
	}
	
	if successCount > 0 {
		server.Avg = totalDuration / time.Duration(successCount)
		server.Tested = true
	} else {
		server.Avg = 0
		server.Tested = false
	}
}

// 预处理服务器列表，确保每个运营商+省份组合只有一个服务器
func preprocessServers(servers []*model.Server) []*model.Server {
	// 使用map来跟踪每个运营商+省份组合
	uniqueMap := make(map[string]*model.Server)
	for _, server := range servers {
		// 提取运营商
		var isp string
		if len(server.Name) >= 2 {
			isp = server.Name[:2]
		} else {
			isp = "未知"
		}
		// 提取省份
		var province string
		if len(server.Name) > 2 {
			province = server.Name[2:]
		} else {
			province = "未知"
		}
		// 生成唯一键
		key := isp + "_" + province
		// 根据来源类型优先级排序添加
		if _, exists := uniqueMap[key]; !exists {
			// 如果不存在，直接添加
			uniqueMap[key] = server
		} else {
			// 如果已存在，则根据来源类型决定是否替换
			existingType := uniqueMap[key].SourceType
			newType := server.SourceType
			// 优先级: icmp > net > cn
			if (newType == "icmp" && (existingType == "net" || existingType == "cn")) ||
				(newType == "net" && existingType == "cn") {
				uniqueMap[key] = server
			}
		}
	}
	// 将去重后的服务器转换回切片
	var uniqueServers []*model.Server
	for _, server := range uniqueMap {
		uniqueServers = append(uniqueServers, server)
	}
	return uniqueServers
}

// 使用有限并发工作池执行ping测试
func processWithLimitedConcurrency(servers []*model.Server, concurrency int) []*model.Server {
	// 先预处理服务器列表，确保每个运营商+省份组合只有一个服务器
	uniqueServers := preprocessServers(servers)
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i := range uniqueServers {
		wg.Add(1)
		sem <- struct{}{}
		go func(index int) {
			defer func() {
				<-sem
				wg.Done()
			}()
			pingServerSimple(uniqueServers[index])
		}(i)
	}
	wg.Wait()
	var testedServers []*model.Server
	for _, server := range uniqueServers {
		// 所有服务器都保留，失败的标记为 9999ms
		if !server.Tested || server.Avg.Milliseconds() == 0 {
			server.Avg = 9999 * time.Millisecond
		}
		testedServers = append(testedServers, server)
	}
	return testedServers
}

func PingTest() string {
	// 添加 defer recover 防止 panic
	defer func() {
		if r := recover(); r != nil {
			logError(fmt.Sprintf("PingTest panic 恢复: %v", r))
		}
	}()

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
		defer func() {
			if r := recover(); r != nil {
				logError(fmt.Sprintf("processWithLimitedConcurrency panic 恢复: %v", r))
				resultChan <- []*model.Server{}
			}
		}()
		resultChan <- processWithLimitedConcurrency(servers1, model.MaxConcurrency)
	}()
	go func() {
		defer wga.Done()
		defer func() {
			if r := recover(); r != nil {
				logError(fmt.Sprintf("processWithLimitedConcurrency panic 恢复: %v", r))
				resultChan <- []*model.Server{}
			}
		}()
		resultChan <- processWithLimitedConcurrency(servers2, model.MaxConcurrency)
	}()
	go func() {
		defer wga.Done()
		defer func() {
			if r := recover(); r != nil {
				logError(fmt.Sprintf("processWithLimitedConcurrency panic 恢复: %v", r))
				resultChan <- []*model.Server{}
			}
		}()
		resultChan <- processWithLimitedConcurrency(servers3, model.MaxConcurrency)
	}()
	go func() {
		wga.Wait()
		close(resultChan)
	}()
	for servers := range resultChan {
		allServers = append(allServers, servers...)
	}
	// 首先按运营商分组，然后按延迟排序
	sort.Slice(allServers, func(i, j int) bool {
		// 获取运营商名称前缀（前两个字符）
		isp1 := allServers[i].Name[:2]
		isp2 := allServers[j].Name[:2]
		// 先按运营商分组
		if isp1 != isp2 {
			return isp1 < isp2
		}
		// 相同运营商则按延迟排序
		return allServers[i].Avg < allServers[j].Avg
	})
	// 优化输出格式，按运营商分组显示
	var currentISP string
	var count int
	for _, server := range allServers {
		// 提取运营商
		isp := server.Name[:2]
		// 如果运营商变了，输出分隔符
		if isp != currentISP {
			if currentISP != "" {
				// 在运营商之间添加空行
				result += "\n"
			}
			currentISP = isp
			count = 0
		}
		// 每三个服务器换行一次
		if count > 0 && count%3 == 0 {
			result += "\n"
		}
		count++
		avgStr := fmt.Sprintf("%4d", server.Avg.Milliseconds())
		name := server.Name
		padding := 20 - runewidth.StringWidth(name)
		if padding < 0 {
			padding = 0
		}
		result += fmt.Sprintf("%s%s%4s | ", name, strings.Repeat(" ", padding), avgStr)
	}
	return result
}
