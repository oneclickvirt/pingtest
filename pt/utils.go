package pt

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/imroc/req/v3"
	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
)

var (
	icmpTargetsCache       []model.IcmpTarget
	icmpTargetsMutex       sync.Mutex
	icmpTargetsInitialized bool
)

func logError(msg string) {
	if model.EnableLoger && Logger != nil {
		Logger.Info(msg)
	}
}

// checkCDN 检查单个 CDN 是否可用，参考 shell 脚本的实现
// checkCDN 检查单个 CDN 是否可用，参考 shell 脚本的实现
// 通过访问测试 URL 并检查响应中是否包含 "success" 来验证
func checkCDN(cdnURL string) bool {
	client := req.C()
	client.SetTimeout(6 * time.Second) // 与 shell 脚本的 --max-time 6 保持一致

	// 测试 URL，与 shell 脚本中的 check_cdn_file 保持一致
	testURL := cdnURL + "https://raw.githubusercontent.com/spiritLHLS/ecs/main/back/test"

	resp, err := client.R().Get(testURL)
	if err != nil {
		logError(fmt.Sprintf("CDN 测试失败 %s: %v", cdnURL, err))
		return false
	}

	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
		b, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			logError(fmt.Sprintf("读取 CDN 测试响应失败 %s: %v", cdnURL, readErr))
			return false
		}

		bodyStr := string(b)
		// 与 shell 脚本一致，检查响应中是否包含 "success"
		if strings.Contains(bodyStr, "success") {
			logError(fmt.Sprintf("CDN 可用: %s", cdnURL))
			return true
		} else {
			logError(fmt.Sprintf("CDN 测试响应不包含 'success': %s", cdnURL))
			return false
		}
	}

	return false
}

// getData 获取目标地址的文本内容
func getData(endpoint string) string {
	// 添加 defer recover 防止 panic
	defer func() {
		if r := recover(); r != nil {
			logError(fmt.Sprintf("getData panic 恢复: %v", r))
		}
	}()

	client := req.C()
	client.SetTimeout(10 * time.Second) // 增加超时时间到10秒
	client.R().
		SetRetryCount(2).
		SetRetryBackoffInterval(1*time.Second, 3*time.Second).
		SetRetryFixedInterval(2 * time.Second)
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}

	// 创建一个带超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, baseUrl := range model.CdnList {
		// 检查 context 是否已超时
		select {
		case <-ctx.Done():
			logError("getData 总体超时")
			return ""
		default:
		}

		// 先测试 CDN 是否可用（参考 shell 脚本实现）
		if !checkCDN(baseUrl) {
			logError(fmt.Sprintf("CDN 不可用，跳过: %s", baseUrl))
			time.Sleep(500 * time.Millisecond) // 与 shell 脚本的 sleep 0.5 保持一致
			continue
		}

		url := baseUrl + endpoint
		resp, err := client.R().SetContext(ctx).Get(url)
		if err != nil {
			logError(fmt.Sprintf("获取 %s 失败: %v", url, err))
			continue
		}

		// 确保响应体被关闭
		if resp != nil && resp.Body != nil {
			defer resp.Body.Close()
			b, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				logError(fmt.Sprintf("读取响应体失败 %s: %v", url, readErr))
				continue
			}

			bodyStr := string(b)
			logError(fmt.Sprintf("成功从 %s 获取数据", url))
			return bodyStr
		}
	}
	logError(fmt.Sprintf("所有 CDN 尝试失败,endpoint: %s", endpoint))
	return ""
}

func resolveIP(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r := net.Resolver{}
	ips, err := r.LookupIP(ctx, "ip", name)
	if err != nil {
		return ""
	}
	if len(ips) > 0 {
		return ips[0].String()
	}
	logError(fmt.Sprintf("域名 %s 无法解析到IP地址", name))
	return ""
}

func parseCSVData(data, platform, operator string) []*model.Server {
	var servers []*model.Server
	r := csv.NewReader(strings.NewReader(data))
	records, err := r.ReadAll()
	if err != nil {
		if model.EnableLoger {
			Logger.Info(err.Error())
		}
		return servers
	}
	// 检查并去除 CSV 头部（header）
	if len(records) > 0 {
		if len(records[0]) > 6 && records[0][6] == "country_code" {
			records = records[1:]
		} else if len(records[0]) > 1 && records[0][1] == "country_code" {
			records = records[1:]
		}
	}
	var head string
	switch operator {
	case "cmcc":
		head = "移动"
	case "ct":
		head = "电信"
	case "cu":
		head = "联通"
	}
	if platform == "net" {
		for _, record := range records {
			// 确保记录至少包含8个字段
			if len(record) >= 8 {
				servers = append(servers, &model.Server{
					Name:       head + record[3],
					IP:         record[4],
					Tested:     false,
					SourceType: "net",
				})
			} else {
				if model.EnableLoger {
					Logger.Info(fmt.Sprintf("CSV 数据字段不足 (net): %d", len(record)))
				}
			}
		}
	} else if platform == "cn" {
		var name, ip string
		for _, record := range records {
			// 确保记录至少包含11个字段以便安全访问 record[10] 和 record[8]
			if len(record) >= 11 {
				parts := strings.Split(record[5], ":")
				if len(parts) < 2 {
					if model.EnableLoger {
						Logger.Info(fmt.Sprintf("record[5] 格式错误，缺少端口信息: %s", record[5]))
					}
					continue
				}
				ip = parts[0]
				if net.ParseIP(ip) == nil {
					ip = resolveIP(ip)
					if ip == "" {
						continue
					}
				}
				name = record[10] + record[8]
				if !strings.Contains(name, head) {
					name = head + name
				}
				servers = append(servers, &model.Server{
					Name:       name,
					IP:         ip,
					Tested:     false,
					SourceType: "cn",
				})
			} else {
				logError(fmt.Sprintf("CSV 数据字段不足 (cn): %d", len(record)))
			}
		}
	}
	logError(fmt.Sprintf("平台: %s, 运营商: %s 解析完成，获取服务器数量: %d", platform, operator, len(servers)))
	// 对同一运营商+省份的服务器进行去重
	return deduplicateServers(servers)
}

// 对服务器进行去重
func deduplicateServers(servers []*model.Server) []*model.Server {
	uniqueMap := make(map[string]*model.Server)
	for _, server := range servers {
		// 提取省份名称
		var province string
		if len(server.Name) > 2 {
			province = server.Name[2:]
		} else {
			province = server.Name
		}
		// 提取运营商
		var isp string
		if len(server.Name) >= 2 {
			isp = server.Name[:2]
		} else {
			isp = "未知"
		}
		// 唯一键 = 运营商 + 省份
		key := isp + "_" + province
		// 如果该组合不存在，则添加
		if _, exists := uniqueMap[key]; !exists {
			uniqueMap[key] = server
		}
	}
	// 转换回切片
	var uniqueServers []*model.Server
	for _, server := range uniqueMap {
		uniqueServers = append(uniqueServers, server)
	}
	return uniqueServers
}

// parseIcmpTargets 解析ICMP目标数据
func parseIcmpTargets(jsonData string) []model.IcmpTarget {
	var targets []model.IcmpTarget
	err := json.Unmarshal([]byte(jsonData), &targets)
	if err != nil {
		logError(fmt.Sprintf("解析ICMP目标失败: %s", err.Error()))
		return nil
	}
	return targets
}

// 加载ICMP目标数据，只在第一次调用时获取数据
func loadIcmpTargets() {
	icmpTargetsMutex.Lock()
	defer icmpTargetsMutex.Unlock()
	if !icmpTargetsInitialized {
		icmpData := getData(model.IcmpTargets)
		if icmpData != "" {
			icmpTargetsCache = parseIcmpTargets(icmpData)
			icmpTargetsInitialized = true
			logError(fmt.Sprintf("ICMP 目标数据初始化完成，共 %d 个目标", len(icmpTargetsCache)))
		}
	}
}

func cleanProvince(input string) string {
	suffixes := []string{"维吾尔自治区", "回族自治区", "自治区", "省", "市"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(input, suffix) {
			input = strings.TrimSuffix(input, suffix)
			break
		}
	}
	return input
}

// 获取ICMP目标服务器
func getIcmpServers(operator string) []*model.Server {
	// 确保ICMP目标数据已加载
	if !icmpTargetsInitialized {
		loadIcmpTargets()
	}
	var icmpServers []*model.Server
	// 运营商名称映射
	ispNameMap := map[string]string{
		"cm": "移动",
		"cu": "联通",
		"ct": "电信",
	}
	if len(icmpTargetsCache) > 0 {
		// 使用映射确保每个省份只添加一个IP
		provinceMap := make(map[string]bool)
		for _, target := range icmpTargetsCache {
			if target.IPVersion == "v4" && target.IspCode == operator {
				// 清理省份名称
				provinceName := cleanProvince(target.Province)
				// 如果该省份已经处理过，跳过
				if provinceMap[provinceName] {
					continue
				}
				ips := strings.Split(target.IPs, ",")
				if len(ips) > 0 {
					ip := strings.TrimSpace(ips[0]) // 只取第一个IP
					if ip != "" && net.ParseIP(ip) != nil {
						serverName := ispNameMap[target.IspCode] + provinceName
						icmpServers = append(icmpServers, &model.Server{
							Name:       serverName,
							IP:         ip,
							Tested:     false,
							SourceType: "icmp",
						})

						// 标记该省份已处理
						provinceMap[provinceName] = true
					}
				}
			}
		}
	}
	logError(fmt.Sprintf("获取 ICMP 服务器完成，共 %d 个服务器", len(icmpServers)))
	return icmpServers
}

// 对服务器按省份名称进行分组
// func groupServersByProvince(servers []*model.Server) map[string][]*model.Server {
// 	provinceMap := make(map[string][]*model.Server)
// 	for _, server := range servers {
// 		// 提取省份名称（假设名称格式为"运营商+省份"）
// 		var province string
// 		if len(server.Name) > 2 {
// 			province = server.Name[2:] // 跳过运营商名称（如"移动"）
// 		} else {
// 			province = server.Name
// 		}
// 		// 将服务器添加到对应省份的列表中
// 		provinceMap[province] = append(provinceMap[province], server)
// 	}
// 	return provinceMap
// }

func getServers(operator string) []*model.Server {
	// 添加 defer recover 防止 panic
	defer func() {
		if r := recover(); r != nil {
			logError(fmt.Sprintf("getServers panic 恢复: %v, operator: %s", r, operator))
		}
	}()

	netList := []string{model.NetCMCC, model.NetCT, model.NetCU}
	cnList := []string{model.CnCMCC, model.CnCT, model.CnCU}
	var servers []*model.Server
	var wg sync.WaitGroup
	dataCh := make(chan []*model.Server, 3)

	fetchData := func(data string, dataType, operator string) {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				logError(fmt.Sprintf("fetchData panic 恢复: %v", r))
				dataCh <- []*model.Server{}
			}
		}()

		if data != "" {
			parsedData := parseCSVData(data, dataType, operator)
			dataCh <- parsedData
		} else {
			dataCh <- []*model.Server{}
		}
	}

	var ispCode string
	var netIndex, cnIndex int
	switch operator {
	case "cmcc":
		ispCode = "cm"
		netIndex, cnIndex = 0, 0
	case "ct":
		ispCode = "ct"
		netIndex, cnIndex = 1, 1
	case "cu":
		ispCode = "cu"
		netIndex, cnIndex = 2, 2
	default:
		logError(fmt.Sprintf("未知的运营商: %s", operator))
		return []*model.Server{}
	}

	// 确保ICMP目标数据已加载
	if !icmpTargetsInitialized {
		loadIcmpTargets()
	}

	// 获取ICMP服务器并放入通道
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				logError(fmt.Sprintf("getIcmpServers panic 恢复: %v", r))
				dataCh <- []*model.Server{}
			}
		}()
		icmpServers := getIcmpServers(ispCode)
		dataCh <- icmpServers
	}()

	// 获取其他两种来源的数据
	wg.Add(2)
	go fetchData(getData(netList[netIndex]), "net", operator)
	go fetchData(getData(cnList[cnIndex]), "cn", operator)

	// 使用超时机制等待所有 goroutine 完成
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(dataCh)
		close(done)
	}()

	// 设置超时为 60 秒
	timeout := time.After(60 * time.Second)

	collecting := true
	for collecting {
		select {
		case data, ok := <-dataCh:
			if !ok {
				collecting = false
			} else {
				servers = append(servers, data...)
			}
		case <-timeout:
			logError(fmt.Sprintf("getServers 超时,operator: %s", operator))
			collecting = false
		}
	}

	// 等待清理完成或超时
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		logError("等待 goroutine 清理超时")
	}

	// 最终去重，确保每个运营商+省份组合只有一个服务器
	uniqueMap := make(map[string]*model.Server)
	for _, server := range servers {
		// 提取省份
		var province string
		if len(server.Name) > 2 {
			province = server.Name[2:]
		} else {
			province = server.Name
		}
		// 提取运营商
		var isp string
		if len(server.Name) >= 2 {
			isp = server.Name[:2]
		} else {
			isp = "未知"
		}
		// 创建唯一键
		key := isp + "_" + province
		// 根据来源优先级决定是否替换
		if existing, exists := uniqueMap[key]; !exists {
			// 如果不存在，直接添加
			uniqueMap[key] = server
		} else {
			// 如果已存在，根据来源类型决定是否替换
			// 优先级: icmp > net > cn
			if (server.SourceType == "icmp" && (existing.SourceType == "net" || existing.SourceType == "cn")) ||
				(server.SourceType == "net" && existing.SourceType == "cn") {
				uniqueMap[key] = server
			}
		}
	}
	// 转换回切片
	var result []*model.Server
	for _, server := range uniqueMap {
		result = append(result, server)
	}
	// 按照省份名称排序，确保稳定的输出顺序
	sort.Slice(result, func(i, j int) bool {
		var province1, province2 string
		if len(result[i].Name) > 2 {
			province1 = result[i].Name[2:]
		}
		if len(result[j].Name) > 2 {
			province2 = result[j].Name[2:]
		}
		return province1 < province2
	})
	logError(fmt.Sprintf("%s 运营商获取服务器完成，共整理 %d 个服务器", operator, len(result)))
	return result
}
