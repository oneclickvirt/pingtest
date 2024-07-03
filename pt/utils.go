package pt

import (
	"encoding/csv"
	"io"
	"strings"
	"time"

	"github.com/imroc/req/v3"
	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/pingtest/model"
)

func getData(endpoint string) string {
	client := req.C()
	client.SetTimeout(6 * time.Second)
	client.R().
		SetRetryCount(2).
		SetRetryBackoffInterval(1*time.Second, 5*time.Second).
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
				return string(b)
			}
		}
		if model.EnableLoger {
			Logger.Info(err.Error())
		}
	}
	return ""
}

func parseCSVData(data, platform string) []model.Server {
	var servers []model.Server
	r := csv.NewReader(strings.NewReader(data))
	records, err := r.ReadAll()
	if err != nil {
		if model.EnableLoger {
			Logger.Info(err.Error())
		}
		return servers
	}
	if len(records) > 0 && (records[0][6] == "country_code" || records[0][1] == "country_code") {
		records = records[1:]
	}
	if platform == "net" {
		for _, record := range records {
			if len(record) >= 8 {
				servers = append(servers, model.Server{
					Name: record[3],
					IP:   record[4],
					Port: record[6],
				})
			}
		}
	} else if platform == "cn" {
		for _, record := range records {
			if len(record) >= 8 {
				servers = append(servers, model.Server{
					Name: record[10] + record[8],
					IP:   strings.Split(record[5], ":")[0],
					Port: strings.Split(record[5], ":")[1],
				})
			}
		}
	}
	return servers
}

func getServers(operator string) []model.Server {
    netList := []string{model.NetCMCC, model.NetCT, model.NetCU}
    cnList := []string{model.CnCMCC, model.CnCT, model.CnCU}
    var servers []model.Server
    // 定义一个函数来获取数据并解析
    appendData := func(data1, data2 string) {
        if data1 != "" {
            servers = append(servers, parseCSVData(data1, "net")...)
        }
        if data2 != "" {
            servers = append(servers, parseCSVData(data2, "cn")...)
        }
    }
    switch operator {
    case "cmcc":
        appendData(getData(netList[0]), getData(cnList[0]))
    case "ct":
        appendData(getData(netList[1]), getData(cnList[1]))
    case "cu":
        appendData(getData(netList[2]), getData(cnList[2]))
    }
    return servers
}

