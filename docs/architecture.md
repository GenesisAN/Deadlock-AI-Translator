# Deadlock 翻译桥的 Go 架构

## 组件和依赖

| 组件 | 职责 | 依赖方向 |
| --- | --- | --- |
| `cmd/deadlock-translator` | 启动参数、数据目录、两个回环监听器、优雅关闭 | 组装其余模块 |
| `internal/server` | HTTP 路由、输入校验、Community v3 兼容响应、网页和游戏设置 | settings、translate |
| `internal/translate` | 整句字典、术语索引、前缀规则、请求合并、缓存、回退 | settings 的只读快照、可替换的翻译接口 |
| `internal/provider` | OpenAI Chat Completions、Anthropic、传统翻译协议、预设与连接池 | Go 标准库 |
| `internal/settings` | 配置版本、旧版导入、凭据及原子写、并发更新 | 平台的文件锁、Windows DPAPI |
| `internal/localcert` | localhost 根与叶证书的创建、校验、续期 | Go 标准库 |
| Panorama / CEF | 游戏内收发、显示和命令拦截。沿用 Community v3 的 fragment / HTMLTitle 帧 | 固定 localhost HTTPS |

## 网络和资源控制

- Go 标准库 `net/http` 和 `crypto/tls` 提供两个独立入口。同一个进程直接调用应用层，不需要本地反向代理。网页入口默认使用 8791，游戏按现有 VPK 合约使用 8792。两者均只绑定 IPv4/IPv6 回环，核对 Host、Origin 和浏览器写请求的自定义头。游戏入口只暴露翻译和游戏偏好。
- 游戏批次最多八条、总长 2100 UTF-16 单元，整个桥收到的 JSON 最大 64 KiB。桥为游戏请求使用最多 6.2 秒翻译预算，给 CEF 留出返回时间。各服务商共享有界连接池。用户显式选择备用服务商后才会尝试，整个链路共享一个截止时间。
- 默认并发八个模型调用，其中一个槽留给出站。队列满时立即返回繁忙。等价请求共用一次调用，最后一个等待方取消时终止上游请求。主服务商成功结果进入 512 条、五分钟的内存缓存，配置版本改变后自然失效。Web 试译和仅预览均不写入缓存。
- 日常路径使用内存配置和预编译术语索引。无游戏目录扫描、进程轮询或常驻辅助进程。游戏最近探测来自桥页和设置读写。性能需要按目标机记录空闲工作集、本地处理 P50/P95、模型耗时和超时率，避免把模型往返时间归到本地桥。

## 翻译和词库

首先做整句匹配。优先级为设置页自定义词库、外置 `dictionary.json` 的 user 区、Deadlock 内置词库、通用词库、导入的 learned 区。随后按当前句子扫描专名，首字符索引限制候选，多个命中时优先较长词条。英文匹配检查词边界，中文别名可以嵌在句子里。明确词条可用占位符保护，译后逐个确认完整性。歧义词只作为模型提示。例如 BKB 对应 Unstoppable，卡牌在击杀或集火语境指 Wraith，技能语境可能指 Card Trick。

全局前置指令和第一条匹配的前缀规则只附给 AI 服务商。规则可以按收发方向和目标语言过滤。游戏、设置页预览共用这套流程。新增 OpenAI 兼容服务一般只需一条预设；新增协议通过 provider 包接入。词库和启动入口可以单独维护。Community VPK 遇到翻译失败沿用游戏端原文发送逻辑。

## 配置和迁移

`settings.json` 使用带版本的 JSON，便于迁移及查看。HTTP 更新采用摘要版本检查，网页和游戏写入前都检查各自的版本。更新通过单写者锁和同目录临时文件替换，落盘后发布新的只读配置。凭据单独保存并与服务商的源地址绑定。Windows 使用当前用户 DPAPI。Linux 使用 0700 状态目录和 0600 凭据文件。外部接口地址改变后，需重新输入该服务商的 Key。

Windows 默认使用 `%LOCALAPPDATA%\DeadlockAITranslator`，Linux 使用 `$XDG_CONFIG_HOME/DeadlockAITranslator` 或 `~/.config/DeadlockAITranslator`。首次启动可从旧 `config/config.json` 导入现有设置和 Key。独立 Go 状态文件让旧版可继续用于回退。新版本安装包随附的 VPK 会在游戏侧空闲时同步网页设置。使用旧 VPK 也可以翻译，网页修改的游戏偏好在重启游戏后读取。旧版 CEF localStorage 的独立偏好未导入，以旧 config.json 为迁移来源。

Windows 默认在启动时准备 localhost 证书。叶证书剩余 30 天以内时用同一根证书续签。程序检查当前用户根证书库中是否已有这张根证书，缺少时调用 Windows 证书安装流程，安装后再核对一次。用户取消或安装失败时仍启动设置页。外部 PEM 参数和 `--no-auto-cert` 可由用户自行管理信任。

## 平台交付

发布目标为 `windows-amd64` 和 `linux-amd64`，两者均为 x86-64。每个平台运行设置页、模型调用和字典。Windows 本地检查确认 HTTPS、游戏批次 API、DPAPI 凭据、配置冲突和术语预览，单次验证后的工作集约 19 MiB。Linux / Steam Deck 的游戏内支持取决于 Proton/CEF 能否读取本机证书信任和加载 VPK，需要在目标机实测。游戏端使用随附的 Community v3 VPK。原版协议说明见 [community-v3-protocol.md](community-v3-protocol.md)，本项目将其中的翻译和游戏偏好接到本机 HTTPS。
