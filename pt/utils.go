package pt

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/imroc/req/v3"
	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
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
	case "cmccc":
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
					Name: head + record[3],
					IP:   record[4],
					Port: record[6],
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
					Name: name,
					IP:   ip,
					Port: parts[1],
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

func getServers(operator string) []*model.Server {
	netList := []string{model.NetCMCC, model.NetCT, model.NetCU}
	cnList := []string{model.CnCMCC, model.CnCT, model.CnCU}
	var servers []*model.Server
	var wg sync.WaitGroup
	dataCh := make(chan []*model.Server, 3) // 修改为3，因为我们增加了一个数据源
	// 定义一个函数来获取数据并解析
	fetchData := func(data string, dataType, operator string) {
		defer wg.Done()
		if data != "" {
			parsedData := parseCSVData(data, dataType, operator)
			dataCh <- parsedData
		}
	}
	fetchIcmpData := func(operator string) {
		defer wg.Done()
		icmpData := getData(model.IcmpTargets)
		if icmpData != "" {
			parsedData := parseIcmpTargets(icmpData)
			var icmpServers []*model.Server
			// 运营商映射
			ispCodeMap := map[string]string{
				"cm": "cmcc", // 移动
				"cu": "cu",   // 联通
				"ct": "ct",   // 电信
			}
			// 运营商名称映射
			ispNameMap := map[string]string{
				"cm": "移动",
				"cu": "联通",
				"ct": "电信",
			}
			// 检查当前请求的运营商是否与ICMP目标中的运营商匹配
			targetOperator := ispCodeMap[operator]
			if targetOperator == "" {
				dataCh <- icmpServers
				return
			}
			for _, target := range parsedData {
				// 只处理 IPv4 版本的地址且运营商匹配的条目
				if target.IPVersion == "v4" && target.IspCode == operator {
					ips := strings.Split(target.IPs, ",")
					for _, ip := range ips {
						ip = strings.TrimSpace(ip)
						if ip != "" {
							serverName := ispNameMap[target.IspCode] + target.Province
							icmpServers = append(icmpServers, &model.Server{
								Name: serverName,
								IP:   ip,
								Port: "80", // 默认使用80端口
							})
						}
					}
				}
			}
			dataCh <- icmpServers
		} else {
			dataCh <- []*model.Server{}
		}
	}
	appendData := func(data1, data2, operator string) {
		wg.Add(3) // 增加到3，因为我们增加了一个数据源
		go fetchData(data1, "net", operator)
		go fetchData(data2, "cn", operator)
		go fetchIcmpData(operator)
	}
	switch operator {
	case "cmcc":
		appendData(getData(netList[0]), getData(cnList[0]), "cm") // 注意这里传递"cm"给ICMP数据
	case "ct":
		appendData(getData(netList[1]), getData(cnList[1]), "ct")
	case "cu":
		appendData(getData(netList[2]), getData(cnList[2]), "cu")
	}
	go func() {
		wg.Wait()
		close(dataCh)
	}()
	for data := range dataCh {
		servers = append(servers, data...)
	}
	// 去重IP
	uniqueServers := make(map[string]*model.Server)
	for _, server := range servers {
		uniqueServers[server.IP] = server
	}
	servers = []*model.Server{}
	for _, server := range uniqueServers {
		servers = append(servers, server)
	}
	// 去重地址
	uniqueServers = make(map[string]*model.Server)
	for _, server := range servers {
		uniqueServers[server.Name] = server
	}
	servers = []*model.Server{}
	for _, server := range uniqueServers {
		servers = append(servers, server)
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
