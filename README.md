# pingtest

[![Hits](https://hits.seeyoufarm.com/api/count/incr/badge.svg?url=https%3A%2F%2Fgithub.com%2Foneclickvirt%2Fpingtest&count_bg=%232EFFF8&title_bg=%23555555&icon=&icon_color=%23E7E7E7&title=hits&edge_flat=false)](https://www.spiritlhl.net)

三网ICMP的PING值测试模块

## 说明

- [x] 基于[speedtest.net-爬虫](https://github.com/spiritLHLS/speedtest.net-CN-ID)、[speedtest.cn-爬虫](https://github.com/spiritLHLS/speedtest.cn-CN-ID)的数据
- [x] 主体逻辑借鉴了[ecsspeed](https://github.com/spiritLHLS/ecsspeed)

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