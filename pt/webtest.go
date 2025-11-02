package pt

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
	. "github.com/oneclickvirt/defaultset"
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
	// 添加 defer recover 防止 panic
	defer func() {
		if r := recover(); r != nil {
			logError(fmt.Sprintf("WebsiteTest panic 恢复: %v", r))
		}
	}()

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
				if r := recover(); r != nil {
					logError(fmt.Sprintf("testWebsite panic 恢复: %v", r))
				}
			}()
			testWebsite(&websites[index], 3) // 每个网站测试3次
		}(i)
	}
	wg.Wait()

	// 收集所有测试的网站（包括失败的，标记为 999ms）
	var allSites []model.Website
	for _, site := range websites {
		if !site.Tested || site.Avg.Milliseconds() == 0 {
			site.Avg = 999 * time.Millisecond
		}
		allSites = append(allSites, site)
	}

	// 按延迟从小到大排序
	sort.Slice(allSites, func(i, j int) bool {
		return allSites[i].Avg < allSites[j].Avg
	})

	// 格式化输出，参考三网延迟测试的格式
	var result string
	count := 0
	for _, site := range allSites {
		// 每三个网站换行一次
		if count > 0 && count%3 == 0 {
			result += "\n"
		}
		count++

		avgStr := fmt.Sprintf("%4d", site.Avg.Milliseconds())
		name := site.Name
		// 计算需要的填充空格，使名称列宽度为20
		padding := 20 - runewidth.StringWidth(name)
		if padding < 0 {
			padding = 0
		}
		result += fmt.Sprintf("%s%s%4s | ", name, strings.Repeat(" ", padding), avgStr)
	}

	return result
}
