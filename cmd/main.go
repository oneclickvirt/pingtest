package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
	"github.com/oneclickvirt/pingtest/pt"
)

type commandRunner struct {
	ping     func() string
	telegram func() string
	website  func() string
	tcp      func(context.Context) []pt.TCPResult
}

func productionCommandRunner() commandRunner {
	return commandRunner{
		ping:     pt.PingTest,
		telegram: pt.TelegramDCTest,
		website:  pt.WebsiteTest,
		tcp: func(ctx context.Context) []pt.TCPResult {
			return pt.RunTCPRegistry(ctx, pt.DefaultTCPProbeConfig())
		},
	}
}

func main() {
	go func() {
		http.Get("https://hits.spiritlhl.net/pingtest.svg?action=hit&title=Hits&title_bg=%23555555&count_bg=%230eecf8&edge_flat=false")
	}()
	fmt.Println("项目地址:", Blue("https://github.com/oneclickvirt/pingtest"))
	if exitCode := runCLI(context.Background(), os.Args[1:], os.Stdout, productionCommandRunner()); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func runCLI(ctx context.Context, args []string, output io.Writer, runner commandRunner) int {
	var showVersion, help bool
	var testMode string
	pingtestFlag := flag.NewFlagSet("pingtest", flag.ContinueOnError)
	pingtestFlag.SetOutput(output)
	pingtestFlag.BoolVar(&help, "h", false, "显示帮助信息")
	pingtestFlag.BoolVar(&showVersion, "v", false, "显示版本信息")
	pingtestFlag.BoolVar(&model.EnableLoger, "log", false, "启用日志记录")
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
		res = pt.FormatTCPResults(runner.tcp(ctx))
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
