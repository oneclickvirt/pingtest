package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
	"github.com/oneclickvirt/pingtest/pt"
)

func main() {
	go func() {
		http.Get("https://hits.spiritlhl.net/pingtest.svg?action=hit&title=Hits&title_bg=%23555555&count_bg=%230eecf8&edge_flat=false")
	}()
	fmt.Println("项目地址:", Blue("https://github.com/oneclickvirt/pingtest"))
	var showVersion, help bool
	var testMode string
	pingtestFlag := flag.NewFlagSet("pingtest", flag.ContinueOnError)
	pingtestFlag.BoolVar(&help, "h", false, "显示帮助信息")
	pingtestFlag.BoolVar(&showVersion, "v", false, "显示版本信息")
	pingtestFlag.BoolVar(&model.EnableLoger, "log", false, "启用日志记录")
	pingtestFlag.StringVar(&testMode, "tm", "ori", "测试模式:\n"+
		"  ori    - 国内三网延迟测试（默认）\n"+
		"  tgdc   - Telegram 数据中心连通性测试\n"+
		"  web    - 流行网站连通性测试\n"+
		"  china  - 国内三网 + TG + 网站全测试\n"+
		"  global - 全球测试（TG + 网站，不含三网）")
	pingtestFlag.Parse(os.Args[1:])

	if help {
		fmt.Printf("用法: %s [选项]\n\n", os.Args[0])
		fmt.Println("选项:")
		pingtestFlag.PrintDefaults()
		fmt.Println("\n示例:")
		fmt.Printf("  %s              # 默认模式: 测试国内三网延迟\n", os.Args[0])
		fmt.Printf("  %s -tm ori      # 测试国内三网延迟（默认）\n", os.Args[0])
		fmt.Printf("  %s -tm tgdc     # 测试 Telegram 数据中心\n", os.Args[0])
		fmt.Printf("  %s -tm web      # 测试流行网站连通性\n", os.Args[0])
		fmt.Printf("  %s -tm china    # 测试国内三网 + TG + 网站\n", os.Args[0])
		fmt.Printf("  %s -tm global   # 测试 TG + 网站（不含三网）\n", os.Args[0])
		fmt.Printf("  %s -log         # 启用详细日志\n", os.Args[0])
		return
	}

	if showVersion {
		fmt.Println(model.PingTestVersion)
		return
	}

	// 根据测试模式执行不同的测试
	var res string
	switch testMode {
	case "ori", "": // ori 或空都是默认三网测试
		res = pt.PingTest()
	case "tgdc":
		res = pt.TelegramDCTest()
	case "web":
		res = pt.WebsiteTest()
	case "china":
		// 国内三网 + TG + 网站
		res1 := pt.PingTest()
		res2 := pt.TelegramDCTest()
		res3 := pt.WebsiteTest()
		res = res1 + "\n" + res2 + "\n" + res3
	case "global":
		// TG + 网站（不含三网）
		res1 := pt.TelegramDCTest()
		res2 := pt.WebsiteTest()
		res = res1 + "\n" + res2
	default:
		fmt.Printf("错误: 未知的测试模式 '%s'\n", testMode)
		fmt.Println("支持的模式: ori, tgdc, web, china, global")
		return
	}
	fmt.Println(res)
}
