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
	pingtestFlag := flag.NewFlagSet("cputest", flag.ContinueOnError)
	pingtestFlag.BoolVar(&help, "h", false, "Show help information")
	pingtestFlag.BoolVar(&showVersion, "v", false, "Show version")
	pingtestFlag.BoolVar(&model.EnableLoger, "log", false, "Enable logging")
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
	fmt.Println(res)
}
