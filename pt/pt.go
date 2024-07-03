package pt

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
	probing "github.com/prometheus-community/pro-bing"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const ICMPProtocolICMP = 1

func pingServerByProbing(server *model.Server, wg *sync.WaitGroup) {
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
	fmt.Println(stats.MinRtt, stats.AvgRtt, stats.MaxRtt)
	server.Avg = stats.AvgRtt
}

func pingServerByGolang(server *model.Server, wg *sync.WaitGroup) {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	defer wg.Done()
	conn, err := icmp.ListenPacket("ip4:icmp", "")
	if err != nil {
		if model.EnableLoger {
			Logger.Info("cannot listen for ICMP packets: " + err.Error())
		}
		return
	}
	defer conn.Close()
	target := server.IP
	dst, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		if model.EnableLoger {
			Logger.Info("cannot resolve IP address: " + err.Error())
		}
		return
	}
	var totalRtt time.Duration
	pingCount := 3
	for i := 0; i < pingCount; i++ {
		msg := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   i,
				Seq:  i,
				Data: []byte("ping"),
			},
		}
		msgBytes, err := msg.Marshal(nil)
		if err != nil {
			if model.EnableLoger {
				Logger.Info("cannot marshal ICMP message: " + err.Error())
			}
			return
		}
		start := time.Now()
		_, err = conn.WriteTo(msgBytes, dst)
		if err != nil {
			if model.EnableLoger {
				Logger.Info("cannot send ICMP message: " + err.Error())
			}
			return
		}
		reply := make([]byte, 1500)
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := conn.ReadFrom(reply)
		if err != nil {
			if model.EnableLoger {
				Logger.Info("cannot receive ICMP reply: " + err.Error())
			}
			return
		}
		duration := time.Since(start)
		rm, err := icmp.ParseMessage(ICMPProtocolICMP, reply[:n])
		if err != nil {
			if model.EnableLoger {
				Logger.Info("cannot parse ICMP reply: " + err.Error())
			}
			return
		}
		switch rm.Type {
		case ipv4.ICMPTypeEchoReply:
			totalRtt += duration
		}
	}
	fmt.Println(totalRtt.Milliseconds())
	server.Avg = totalRtt / time.Duration(pingCount)
}

func pingServerByCMD(server *model.Server, wg *sync.WaitGroup) {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	defer wg.Done()
	// 执行 ping 命令
	cmd := exec.Command("ping", "-c1", "-W3", server.IP)
	output, err := cmd.Output()
	if err != nil {
		if model.EnableLoger {
			Logger.Info("cannot ping: " + err.Error())
		}
		pingServerByGolang(server, wg)
		return
	}
	// 解析输出结果
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	var avgTime float64
	rttRegex := regexp.MustCompile(`time=(\d+\.\d+) ms`)
	for scanner.Scan() {
		line := scanner.Text()
		matches := rttRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			avgTime, err = strconv.ParseFloat(matches[1], 64)
			if err != nil {
				if model.EnableLoger {
					Logger.Info("cannot parse avgTime: " + err.Error())
				}
				pingServerByGolang(server, wg)
				return
			}
		}
	}
	if avgTime == 0 {
		if model.EnableLoger {
			Logger.Info("avgTime is 0.")
		}
		pingServerByGolang(server, wg)
		return
	}
	if err := scanner.Err(); err != nil {
		if model.EnableLoger {
			Logger.Info("scanner error: " + err.Error())
		}
		pingServerByGolang(server, wg)
		return
	}
}

func pingServer(server *model.Server, wg *sync.WaitGroup) {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	defer wg.Done()
	cmd := exec.Command("ping", "-h")
	output, err := cmd.Output()
	if err != nil {
		if model.EnableLoger {
			Logger.Info("cannot ping: " + err.Error())
		}
		pingServerByGolang(server, wg)
		return
	} else if !strings.Contains(string(output), "Usage") {
		if model.EnableLoger {
			Logger.Info("cannot match ping command.")
		}
		pingServerByGolang(server, wg)
		return
	} else {
		pingServerByCMD(server, wg)
		return
	}
}

func PingTest() string {
	var result string
	servers1 := getServers("cu")
	servers2 := getServers("ct")
	servers3 := getServers("cmcc")
	process := func(servers []*model.Server) []*model.Server {
		var wg sync.WaitGroup
		for i := range servers {
			wg.Add(1)
			go pingServerByProbing(servers[i], &wg)
		}
		wg.Wait()
		sort.Slice(servers, func(i, j int) bool {
			return servers[i].Avg < servers[j].Avg
		})
		return servers
	}
	var allServers []*model.Server
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
		if server.Avg == 0 {
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
