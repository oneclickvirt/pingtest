package pt

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
	probing "github.com/prometheus-community/pro-bing"
)

func pingServer(server *model.Server, wg *sync.WaitGroup) {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	defer wg.Done()
	target := server.IP
	pinger, err := probing.NewPinger(target)
	if err != nil {
		if model.EnableLoger {
			Logger.Info("cannot create Pinger: " + err.Error())
		}
		return
	}
	pinger.Count = 3
	pinger.Size = 24
	pinger.Interval = 500 * time.Millisecond
	pinger.Timeout = 3 * time.Second
	pinger.TTL = 64
	pinger.SetPrivileged(true)
	stats := pinger.Statistics() // 获取ping统计信息
	server.Avg = stats.AvgRtt
}

func PingTest() string {
	var result string
	servers1 := getServers("cu")
	servers2 := getServers("ct")
	servers3 := getServers("cmcc")
	process := func(servers []model.Server) []model.Server {
		var wg sync.WaitGroup
		for i := range servers {
			wg.Add(1)
			go pingServer(&servers[i], &wg)
		}
		wg.Wait()
		sort.Slice(servers, func(i, j int) bool {
			return servers[i].Avg < servers[j].Avg
		})
		return servers
	}
	var allServers []model.Server
	var wga sync.WaitGroup
	go func() {
		wga.Add(1)
		defer wga.Done()
		servers1 = process(servers1)
	}()
	go func() {
		wga.Add(1)
		defer wga.Done()
		servers2 = process(servers2)
	}()
	go func() {
		wga.Add(1)
		defer wga.Done()
		servers3 = process(servers3)
	}()
	wga.Wait()
	allServers = append(allServers, servers1...)
	allServers = append(allServers, servers2...)
	allServers = append(allServers, servers3...)
	var avgStr string
	for i, server := range allServers {
		if i > 0 && i%3 == 0 {
			result += "\n"
		}
		if server.Avg.String() == "0s" {
			avgStr = "N/A"
		} else {
			avgStr = fmt.Sprintf("%4d", server.Avg.Milliseconds())
		}
		name := server.Name
		nameWidth := runewidth.StringWidth(name)
		padding := 16 - nameWidth
		if padding < 0 {
			padding = 0
		}
		result += fmt.Sprintf("%s%s%4s | ", name, strings.Repeat(" ", padding), avgStr)
	}
	return result
}
