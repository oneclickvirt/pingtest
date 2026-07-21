package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
	"github.com/oneclickvirt/pingtest/pt"
)

type commandRunner struct {
	ping     func() string
	telegram func() string
	website  func() string
	tcp      func(context.Context, pt.TCPProbeConfig, string) ([]pt.TCPResult, error)
}

func productionCommandRunner() commandRunner {
	return commandRunner{
		ping:     pt.PingTest,
		telegram: pt.TelegramDCTest,
		website:  pt.WebsiteTest,
		tcp: func(ctx context.Context, config pt.TCPProbeConfig, target string) ([]pt.TCPResult, error) {
			if strings.TrimSpace(target) == "" {
				results, _, err := pt.RunLoadedTCPRegistry(ctx, config)
				return results, err
			}
			parsed, err := parseTCPTarget(target)
			if err != nil {
				return nil, err
			}
			return pt.RunTCPProbes(ctx, []model.TCPTarget{parsed}, config), nil
		},
	}
}

func main() {
	go func() {
		http.Get("https://hits.spiritlhl.net/pingtest.svg?action=hit&title=Hits&title_bg=%23555555&count_bg=%230eecf8&edge_flat=false")
	}()
	if exitCode := runCLI(context.Background(), os.Args[1:], os.Stdout, productionCommandRunner()); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func runCLI(ctx context.Context, args []string, output io.Writer, runner commandRunner) int {
	var showVersion, help, jsonOutput bool
	var testMode, target, tcpFormat string
	var attempts, concurrency, tcpDetails int
	var timeout time.Duration
	pingtestFlag := flag.NewFlagSet("pingtest", flag.ContinueOnError)
	pingtestFlag.SetOutput(output)
	pingtestFlag.BoolVar(&help, "h", false, "显示帮助信息")
	pingtestFlag.BoolVar(&showVersion, "v", false, "显示版本信息")
	pingtestFlag.BoolVar(&model.EnableLoger, "log", false, "启用日志记录")
	pingtestFlag.BoolVar(&jsonOutput, "json", false, "TCP 模式输出结构化 JSON")
	pingtestFlag.IntVar(&attempts, "attempts", 3, "TCP 模式每个目标的尝试次数")
	pingtestFlag.DurationVar(&timeout, "timeout", 5*time.Second, "TCP 模式单次握手超时")
	pingtestFlag.IntVar(&concurrency, "concurrency", 16, "TCP 模式最大并发数")
	pingtestFlag.StringVar(&target, "target", "", "TCP 模式仅测试一个 host[:port] 目标")
	pingtestFlag.StringVar(&tcpFormat, "tcp-format", string(pt.TCPTextFormatCompact), "TCP 文本输出格式: compact 或 full")
	pingtestFlag.IntVar(&tcpDetails, "tcp-details", pt.DefaultTCPCompactDetails, "TCP compact 模式最多显示的异常/最慢目标数")
	pingtestFlag.StringVar(&testMode, "tm", "ori", "测试模式:\n"+
		"  ori    - 国内三网延迟测试（默认）\n"+
		"  tgdc   - Telegram 数据中心连通性测试\n"+
		"  web    - 流行网站连通性测试\n"+
		"  tcp    - TCP 握手延迟与可用性测试\n"+
		"  china  - 国内三网 + TG + 网站全测试\n"+
		"  global - 全球测试（TG + 网站，不含三网）")
	if err := pingtestFlag.Parse(args); err != nil {
		return 2
	}
	if jsonOutput && testMode != "tcp" {
		fmt.Fprintln(output, "错误: -json 仅支持 -tm tcp")
		return 2
	}
	if !jsonOutput {
		fmt.Fprintln(output, "项目地址:", Blue("https://github.com/oneclickvirt/pingtest"))
	}

	if help {
		fmt.Fprintln(output, "用法: pingtest [选项]")
		fmt.Fprintln(output, "\n选项:")
		pingtestFlag.PrintDefaults()
		fmt.Fprintln(output, "\n示例:")
		fmt.Fprintln(output, "  pingtest              # 默认模式: 测试国内三网延迟")
		fmt.Fprintln(output, "  pingtest -tm ori      # 测试国内三网延迟（默认）")
		fmt.Fprintln(output, "  pingtest -tm tgdc     # 测试 Telegram 数据中心")
		fmt.Fprintln(output, "  pingtest -tm web      # 测试流行网站连通性")
		fmt.Fprintln(output, "  pingtest -tm tcp      # 测试合并目标集的 TCP 握手")
		fmt.Fprintln(output, "  pingtest -tm china    # 测试国内三网 + TG + 网站")
		fmt.Fprintln(output, "  pingtest -tm global   # 测试 TG + 网站（不含三网）")
		fmt.Fprintln(output, "  pingtest -log         # 启用详细日志")
		return 0
	}

	if showVersion {
		fmt.Fprintln(output, model.PingTestVersion)
		return 0
	}

	// 根据测试模式执行不同的测试
	var res string
	switch testMode {
	case "ori", "": // ori 或空都是默认三网测试
		res = runner.ping()
	case "tgdc":
		res = runner.telegram()
	case "web":
		res = runner.website()
	case "tcp":
		format := pt.TCPTextFormat(strings.ToLower(strings.TrimSpace(tcpFormat)))
		if attempts < 1 || concurrency < 1 || timeout <= 0 || tcpDetails < 1 {
			fmt.Fprintln(output, "错误: attempts、timeout、concurrency 和 tcp-details 必须大于 0")
			return 2
		}
		if format != pt.TCPTextFormatCompact && format != pt.TCPTextFormatFull {
			fmt.Fprintln(output, "错误: tcp-format 仅支持 compact 或 full")
			return 2
		}
		results, err := runner.tcp(ctx, pt.TCPProbeConfig{Attempts: attempts, Timeout: timeout, Concurrency: concurrency}, target)
		if err != nil {
			fmt.Fprintf(output, "错误: %v\n", err)
			return 2
		}
		if jsonOutput {
			encoder := json.NewEncoder(output)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(results); err != nil {
				return 1
			}
			return 0
		}
		res = pt.FormatTCPResultsWithOptions(results, pt.TCPFormatOptions{Format: format, MaxDetails: tcpDetails})
	case "china":
		// 国内三网
		res = runner.ping()
	case "global":
		// TG + 网站（不含三网）
		res1 := runner.telegram()
		res2 := runner.website()
		res = res1 + "\n" + res2
	default:
		fmt.Fprintf(output, "错误: 未知的测试模式 '%s'\n", testMode)
		fmt.Fprintln(output, "支持的模式: ori, tgdc, web, tcp, china, global")
		return 2
	}
	fmt.Fprintln(output, res)
	return 0
}

func parseTCPTarget(value string) (model.TCPTarget, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return model.TCPTarget{}, fmt.Errorf("TCP target is empty")
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		if ip := net.ParseIP(strings.Trim(value, "[]")); ip != nil {
			host, portText = ip.String(), "443"
		} else if !strings.Contains(value, ":") {
			host, portText = value, "443"
		} else {
			return model.TCPTarget{}, fmt.Errorf("invalid TCP target %q", value)
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || strings.TrimSpace(host) == "" {
		return model.TCPTarget{}, fmt.Errorf("invalid TCP target %q", value)
	}
	return model.TCPTarget{Name: value, Host: strings.Trim(host, "[]"), Port: port, Category: "custom", Source: "cli"}, nil
}
