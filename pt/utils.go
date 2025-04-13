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
					continue
				}
				return string(b)
			}
		}
		if model.EnableLoger {
			Logger.Info(err.Error())
		}
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
				if model.EnableLoger {
					Logger.Info(fmt.Sprintf("CSV 数据字段不足 (cn): %d", len(record)))
				}
			}
		}
	}
	return servers
}

// parseIcmpTargets 解析ICMP目标数据
func parseIcmpTargets(jsonData string) []model.IcmpTarget {
	// 确保JSON数据格式正确，如果返回的是数组，需要添加[和]
	if !strings.HasPrefix(jsonData, "[") {
		jsonData = "[" + jsonData + "]"
	}
	// 如果JSON数据中的对象没有正确用逗号分隔，修复它
	jsonData = strings.ReplaceAll(jsonData, "}{", "},{")
	var targets []model.IcmpTarget
	err := json.Unmarshal([]byte(jsonData), &targets)
	if err != nil {
		if model.EnableLoger {
			Logger.Error(fmt.Sprintf("Failed to parse ICMP targets: %v", err))
		}
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
		}
	}
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
		for _, target := range icmpTargetsCache {
			// 只处理 IPv4 版本的地址且运营商匹配的条目
			if target.IPVersion == "v4" && target.IspCode == operator {
				ips := strings.Split(target.IPs, ",")
				for _, ip := range ips {
					ip = strings.TrimSpace(ip)
					if ip != "" && net.ParseIP(ip) != nil { // 确保IP有效
						serverName := ispNameMap[target.IspCode] + target.Province
						icmpServers = append(icmpServers, &model.Server{
							Name:       serverName,
							IP:         ip,
							Tested:     false,
							SourceType: "icmp",
						})
					}
				}
			}
		}
	}
	return icmpServers
}

// 对服务器按省份名称进行分组
func groupServersByProvince(servers []*model.Server) map[string][]*model.Server {
	provinceMap := make(map[string][]*model.Server)
	for _, server := range servers {
		// 提取省份名称（假设名称格式为"运营商+省份"）
		var province string
		if len(server.Name) > 2 {
			province = server.Name[2:] // 跳过运营商名称（如"移动"）
		} else {
			province = server.Name
		}

		// 将服务器添加到对应省份的列表中
		provinceMap[province] = append(provinceMap[province], server)
	}
	return provinceMap
}

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
	// 按省份对服务器进行分组
	provinceMap := groupServersByProvince(servers)
	// 按照一致的顺序整理服务器列表
	var result []*model.Server
	// 先处理所有不同的省份
	// 为了保持一致性，我们可以按省份名称排序
	var provinces []string
	for province := range provinceMap {
		provinces = append(provinces, province)
	}
	sort.Strings(provinces)
	// 对每个省份，先添加ICMP服务器，然后是net，最后是cn
	for _, province := range provinces {
		provinceServers := provinceMap[province]
		// 按照来源类型排序
		sort.SliceStable(provinceServers, func(i, j int) bool {
			sources := map[string]int{"icmp": 0, "net": 1, "cn": 2}
			return sources[provinceServers[i].SourceType] < sources[provinceServers[j].SourceType]
		})
		// 添加到最终结果
		result = append(result, provinceServers...)
	}
	return result
}
