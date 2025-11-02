# pingtest

[![Hits](https://hits.spiritlhl.net/pingtest.svg?action=hit&title=Hits&title_bg=%23555555&count_bg=%230eecf8&edge_flat=false)](https://hits.spiritlhl.net)

全方位的网络连通性测试工具

## 功能特性

- [x] **三网延迟测试** - 基于[speedtestnet](https://github.com/spiritLHLS/speedtest.net-CN-ID)、[speedtestcn](https://github.com/spiritLHLS/speedtest.cn-CN-ID)、[icmp_targets](https://github.com/spiritLHLS/icmp_targets)的数据
- [x] **Telegram DC 检测** - 测试所有 Telegram 数据中心的连通性和延迟（参考 [OctoGramApp](https://github.com/OctoGramApp/octogramapp.github.io)）
- [x] **流行网站测试** - 测试 Google、YouTube、Netflix、OpenAI 等主流网站的连通性 [UnlockTests](https://github.com/oneclickvirt/UnlockTests)
- [x] 支持调用本机```ping```进行测试
- [x] 支持使用官方```pro-bing```库进行测试
- [x] 主体逻辑借鉴了[ecsspeed](https://github.com/spiritLHLS/ecsspeed)

## 使用

### 下载及安装

```bash
curl https://raw.githubusercontent.com/oneclickvirt/pingtest/main/pt_install.sh -sSf | bash
```

或

```bash
curl https://cdn.spiritlhl.net/https://raw.githubusercontent.com/oneclickvirt/pingtest/main/pt_install.sh -sSf | bash
```

### 基本使用

```bash
pt              # 默认模式: 测试国内三网延迟
pt -tm ori      # 测试国内三网延迟（与默认相同）
pt -tm tgdc     # 测试 Telegram 数据中心
pt -tm web      # 测试流行网站连通性
pt -tm china    # 测试国内三网 + TG + 网站
pt -tm global   # 测试 TG + 网站（不含三网）
pt -log         # 启用详细日志
```

## 测试模式说明

### 1. ori - 国内三网延迟测试（默认）

测试国内移动、联通、电信三大运营商各省份节点的 ICMP 延迟

```bash
pt
# 或
pt -tm ori
```

**注意**: 测试失败的节点将显示延迟为 999ms

### 2. tgdc - Telegram DC 测试

测试 Telegram 5个数据中心的连通性和延迟：
- DC1 - Miami, USA
- DC2 - Amsterdam, Netherlands  
- DC3 - Miami, USA
- DC4 - Amsterdam, Netherlands
- DC5 - Singapore

```bash
pt -tm tgdc
```

**注意**: 测试失败的数据中心将显示延迟为 999ms

### 3. web - 流行网站测试

测试以下类别的网站连通性和响应时间：

- **搜索引擎**: Google, Bing
- **社交媒体**: Facebook, Twitter, Instagram, Reddit, TikTok
- **视频流媒体**: YouTube, Netflix, Disney+, Prime Video, Spotify, Twitch, Bilibili, iQIYI
- **AI 服务**: OpenAI, Claude, Gemini, Sora, Meta AI
- **开发平台**: GitHub, GitLab, Stack Overflow, Docker Hub
- **云服务**: AWS, Azure, Google Cloud, DigitalOcean
- **电商平台**: Amazon, eBay, AliExpress
- **游戏平台**: Steam
- **新闻媒体**: CNN, BBC, NYTimes
- **科技公司**: Apple, Microsoft
- **工具网站**: Wikipedia

```bash
pt -tm web
```

**注意**: 测试失败的网站将显示延迟为 999ms

### 4. china - 国内全面测试

依次运行国内三网 + Telegram DC + 网站测试

```bash
pt -tm china
```

### 5. global - 全球测试

仅运行 Telegram DC + 网站测试（不含国内三网）

```bash
pt -tm global
```

## 命令行参数

```
用法: pt [选项]

选项:
  -h           显示帮助信息
  -v           显示版本信息
  -log         启用日志记录
  -tm string   测试模式:
                 ori    - 国内三网延迟测试（默认）
                 tgdc   - Telegram 数据中心连通性测试
                 web    - 流行网站连通性测试
                 china  - 国内三网 + TG + 网站全测试
                 global - 全球测试（TG + 网站，不含三网）

示例:
  pt              # 默认模式: 测试国内三网延迟
  pt -tm ori      # 测试国内三网延迟（与默认相同）
  pt -tm tgdc     # 测试 Telegram 数据中心
  pt -tm web      # 测试流行网站连通性
  pt -tm china    # 测试国内三网 + TG + 网站
  pt -tm global   # 测试 TG + 网站（不含三网）
  pt -log         # 启用详细日志
```

## 系统兼容性

无环境依赖，理论上适配所有系统和主流架构

更多架构请查看: https://github.com/oneclickvirt/pingtest/releases/tag/output

## 卸载

```bash
rm -rf /root/pt
rm -rf /usr/bin/pt
```

## 在 Golang 中使用

```bash
go get github.com/oneclickvirt/pingtest@v
```