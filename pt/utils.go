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

// getData 获取目标地址的文本内容
func getData(endpoint string) string {
	client := req.C()
	client.SetTimeout(6 * time.Second)
	client.R().
		SetRetryCount(2).
		SetRetryBackoffInterval(1*time.Second, 3*time.Second).
		SetRetryFixedInterval(2 * time.Second)
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	for _, baseUrl := range model.CdnList {
		url := baseUrl + endpoint
		resp, err := client.R().Get(url)
		if err == nil {
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			if err == nil {
				if strings.Contains(string(b), "error") {
					logError(fmt.Sprintf("URL %s 返回错误响应", url))
					continue
				}
				logError(fmt.Sprintf("成功从 %s 获取数据", url))
				return string(b)
			}
		}
		logError(fmt.Sprintf("获取 %s 失败: %v", url, err))
	}
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
	netList := []string{model.NetCMCC, model.NetCT, model.NetCU}
	cnList := []string{model.CnCMCC, model.CnCT, model.CnCU}
	var servers []*model.Server
	var wg sync.WaitGroup
	dataCh := make(chan []*model.Server, 3)
	fetchData := func(data string, dataType, operator string) {
		defer wg.Done()
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
	}
	// 确保ICMP目标数据已加载
	if !icmpTargetsInitialized {
		loadIcmpTargets()
	}
	// 获取ICMP服务器并放入通道
	wg.Add(1)
	go func() {
		defer wg.Done()
		icmpServers := getIcmpServers(ispCode)
		dataCh <- icmpServers
	}()
	// 获取其他两种来源的数据
	wg.Add(2)
	go fetchData(getData(netList[netIndex]), "net", operator)
	go fetchData(getData(cnList[cnIndex]), "cn", operator)
	go func() {
		wg.Wait()
		close(dataCh)
	}()
	// 收集从通道中获取的数据
	for data := range dataCh {
		servers = append(servers, data...)
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
