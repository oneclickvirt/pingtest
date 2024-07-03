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
		http.Get("https://hits.seeyoufarm.com/api/count/incr/badge.svg?url=https%3A%2F%2Fgithub.com%2Foneclickvirt%2Fpingtest&count_bg=%2323E01C&title_bg=%23555555&icon=sonarcloud.svg&icon_color=%23E7E7E7&title=hits&edge_flat=false")
	}()
	fmt.Println("项目地址:", Blue("https://github.com/oneclickvirt/pingtest"))
	var showVersion, help bool
	var language string
	pingtestFlag := flag.NewFlagSet("cputest", flag.ContinueOnError)
	pingtestFlag.BoolVar(&help, "h", false, "Show help information")
	pingtestFlag.BoolVar(&showVersion, "v", false, "Show version")
	pingtestFlag.StringVar(&language, "l", "", "Language parameter (en or zh)")
	pingtestFlag.Parse(os.Args[1:])
	if help {
		fmt.Printf("Usage: %s [options]\n", os.Args[0])
		pingtestFlag.PrintDefaults()
		return
	}
	if showVersion {
		fmt.Println(model.PingTestVersion)
		return
	}
	res := pt.PingTest()
	fmt.Printf(res)
}
