# pingtest

[![Hits](https://hits.spiritlhl.net/pingtest.svg?action=hit&title=Hits&title_bg=%23555555&count_bg=%230eecf8&edge_flat=false)](https://hits.spiritlhl.net)

三网ICMP的PING值测试模块

## 说明

- [x] 基于[speedtest.net-爬虫](https://github.com/spiritLHLS/speedtest.net-CN-ID)、[speedtest.cn-爬虫](https://github.com/spiritLHLS/speedtest.cn-CN-ID)的数据
- [x] 调用```ping```测试
- [x] 主体逻辑借鉴了[ecsspeed](https://github.com/spiritLHLS/ecsspeed)
- [x] 使用官方的```pro-bing```包进行测试

## 使用

下载及安装

```
curl https://raw.githubusercontent.com/oneclickvirt/pingtest/main/pt_install.sh -sSf | bash
```

或

```
curl https://cdn.spiritlhl.net/https://raw.githubusercontent.com/oneclickvirt/pingtest/main/pt_install.sh -sSf | bash
```

使用

```
pt
```

或

```
./pt
```

进行测试

无环境依赖，理论上适配所有系统和主流架构，更多架构请查看 https://github.com/oneclickvirt/pingtest/releases/tag/output

```
Usage: pt [options]
  -h    Show help information
  -log
        Enable logging
  -v    Show version
```

## 卸载

```
rm -rf /root/pt
rm -rf /usr/bin/pt
```

## 在Golang中使用

```
go get github.com/oneclickvirt/pingtest@latest
```
