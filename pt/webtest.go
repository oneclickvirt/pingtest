package pt

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oneclickvirt/pingtest/model"
)

// testWebsite 测试单个网站的连通性和响应时间
func testWebsite(website *model.Website, attempts int) {
	if model.EnableLoger {
		defer Logger.Sync()
	}
	
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 允许重定向，但不超过10次
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
	}
	
	var totalDuration time.Duration
	successCount := 0
	
	for i := 0; i < attempts; i++ {
		start := time.Now()
		resp, err := client.Get(website.URL)
		duration := time.Since(start)
		
		if err != nil {
			logError(fmt.Sprintf("测试 %s 失败 (尝试 %d/%d): %v", website.Name, i+1, attempts, err))
			continue
		}
		
		if resp != nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				// 只要不是5xx错误，都算成功
				totalDuration += duration
				successCount++
				logError(fmt.Sprintf("测试 %s 成功 (尝试 %d/%d): %d ms, 状态码: %d", 
					website.Name, i+1, attempts, duration.Milliseconds(), resp.StatusCode))
			} else {
				logError(fmt.Sprintf("测试 %s 失败 (尝试 %d/%d): 服务器错误 %d", 
					website.Name, i+1, attempts, resp.StatusCode))
			}
		}
	}
	
	if successCount > 0 {
		website.Avg = totalDuration / time.Duration(successCount)
		website.Tested = true
	} else {
		website.Avg = 0
		website.Tested = false
	}
}

// WebsiteTest 测试所有网站的连通性
func WebsiteTest() string {
	if model.EnableLoger {
		InitLogger()
	}
	
	// 复制网站配置
	websites := make([]model.Website, len(model.PopularWebsites))
	copy(websites, model.PopularWebsites)
	
	// 并发测试所有网站
	var wg sync.WaitGroup
	// 使用信号量限制并发数
	sem := make(chan struct{}, 10) // 最多10个并发请求
	
	for i := range websites {
		wg.Add(1)
		sem <- struct{}{}
		go func(index int) {
			defer func() {
				<-sem
				wg.Done()
			}()
			testWebsite(&websites[index], 3) // 每个网站测试3次
		}(i)
	}
	wg.Wait()
	
	// 按分类分组网站
	categoryMap := make(map[string][]model.Website)
	for _, site := range websites {
		if site.Tested {
			categoryMap[site.Category] = append(categoryMap[site.Category], site)
		}
	}
	
	// 对每个分类内的网站按延迟排序
	for category := range categoryMap {
		sort.Slice(categoryMap[category], func(i, j int) bool {
			return categoryMap[category][i].Avg < categoryMap[category][j].Avg
		})
	}
	
	// 格式化输出
	var result strings.Builder
	
	// 定义分类顺序和中文名称
	categoryOrder := []struct {
		key  string
		name string
	}{
		{"search", "搜索引擎"},
		{"social", "社交媒体"},
		{"video", "视频流媒体"},
		{"ai", "AI 服务"},
		{"dev", "开发平台"},
		{"cloud", "云服务"},
		{"shopping", "电商平台"},
		{"gaming", "游戏平台"},
		{"news", "新闻媒体"},
		{"tech", "科技公司"},
		{"tool", "工具网站"},
	}
	
	// 统计信息
	totalTested := 0
	totalSuccess := 0
	
	for _, cat := range categoryOrder {
		sites, exists := categoryMap[cat.key]
		if !exists || len(sites) == 0 {
			continue
		}
		
		result.WriteString(fmt.Sprintf("%s\n", cat.name))
		result.WriteString(strings.Repeat("-", 70) + "\n")
		
		for _, site := range sites {
			totalTested++
			var status string
			if site.Tested && site.Avg.Milliseconds() > 0 {
				totalSuccess++
				latency := site.Avg.Milliseconds()
				// 根据延迟添加颜色标记
				if latency < 100 {
					status = fmt.Sprintf("✓ %4d ms  [极快]", latency)
				} else if latency < 300 {
					status = fmt.Sprintf("✓ %4d ms  [良好]", latency)
				} else if latency < 1000 {
					status = fmt.Sprintf("✓ %4d ms  [一般]", latency)
				} else {
					status = fmt.Sprintf("✓ %4d ms  [较慢]", latency)
				}
			} else {
				status = "✗ 超时/失败"
			}
			
			result.WriteString(fmt.Sprintf("%-20s %s\n", site.Name, status))
		}
		result.WriteString("\n")
	}
	
	result.WriteString(strings.Repeat("=", 70) + "\n")
	result.WriteString(fmt.Sprintf("总计: 测试 %d 个网站, 成功 %d 个, 失败 %d 个\n", 
		totalTested, totalSuccess, totalTested-totalSuccess))
	
	// 找出最快的5个网站
	var allSuccessWebsites []model.Website
	for _, sites := range categoryMap {
		for _, site := range sites {
			if site.Tested && site.Avg.Milliseconds() > 0 {
				allSuccessWebsites = append(allSuccessWebsites, site)
			}
		}
	}
	
	if len(allSuccessWebsites) > 0 {
		sort.Slice(allSuccessWebsites, func(i, j int) bool {
			return allSuccessWebsites[i].Avg < allSuccessWebsites[j].Avg
		})
		
		result.WriteString("\n响应最快的网站:\n")
		count := 5
		if len(allSuccessWebsites) < count {
			count = len(allSuccessWebsites)
		}
		for i := 0; i < count; i++ {
			site := allSuccessWebsites[i]
			result.WriteString(fmt.Sprintf("  %d. %-20s %4d ms\n", 
				i+1, site.Name, site.Avg.Milliseconds()))
		}
	}
	
	return result.String()
}
