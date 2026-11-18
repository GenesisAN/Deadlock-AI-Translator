# Deadlock AI Translator

《Deadlock》游戏内双向聊天翻译，支持双语发送和自定义词库。游戏 Mod 将聊天交给本机 Go 翻译桥，由翻译桥查词库或调用你配置的翻译服务。

本项目基于 BabelTower Community 的游戏模组和桥接页面开发。 [上游仓库](https://github.com/c1375rick/babeltower-community)

## 安装

1. 从本仓库的 **Releases** 下载并解压对应平台的安装包。Windows 使用 `deadlock-translator-windows-amd64.zip`，Linux 使用 `deadlock-translator-linux-amd64.zip`。每个包都包含 `pak23_dir.vpk`。
2. 启动翻译桥（Windows 和 Linux 均为 x86-64）。
   - Windows 双击 `deadlock-translator.exe`。首次启动时信任 **Deadlock Translator Go Local Root**，然后完全退出并重启 Steam。
   - Linux 在解压目录先运行 `./deadlock-translator --init-tls`，让 Steam 信任生成的根证书，再运行 `./deadlock-translator`。
3. 在设置页选择翻译服务，按服务要求填写 API Key。 [打开本地设置页](http://127.0.0.1:8791/ui)
4. 用 Mod Manager 安装 `pak23_dir.vpk`，或将它复制到 `Deadlock/game/citadel/addons/`。游玩时保持翻译桥运行。

## 游戏内用法

- 收到的消息会在聊天中显示译文。发送模式可选原文、译文或双语。
- 游戏内 AI 翻译会参考最近 10 条收发原文，帮助理解连续对话中的简称和指代。
- 默认将成功的游戏内 AI 翻译样本保存到本机配置目录的 `translation-samples/`：包含原文、实际提示词和最终译文，不自动上传。启动时传入 `--collect-samples=false` 可关闭。
- `/t` 切换发送前翻译，`/r 我去守家` 让当前这条消息直发原文。
- 在设置页的「词库」中查看分类词条，或添加自己的英雄名、装备名等译法。

## 构建

- **编译翻译桥**（Go 1.25.12+）运行 `go build -o dist/local/ ./cmd/deadlock-translator`。Windows 可用根目录的 `Start-Go-Bridge.cmd` 启动。
- **生成平台发布包**（Python 3.9+）按[底包说明](docs/vpk.md)下载上游 VPK，再运行 `python scripts/build_release.py --base-vpk "路径/pak03_dir.vpk"`。Linux 上使用 `python3`，两个 ZIP 均位于 `dist/`。

## 发布

分支推送和 PR 会运行 Windows 和 Linux CI。需要发布时，在目标提交上创建并推送 `v1.2.9` 这类 `vMAJOR.MINOR.PATCH` 格式的 Tag。CI 通过后，GitHub Actions 会按 Tag 的版本打包并发布。

## 项目目录

| 目录 | 内容 |
| --- | --- |
| `cmd/deadlock-translator/`、`internal/` | Go 翻译桥入口、实现和内嵌资源 |
| `panorama/` | 游戏模组源码 |
| `scripts/`、`.github/workflows/` | 构建、测试和发布 |
| `docs/`、`examples/` | 文档和示例 |
| `local/vpk/`、`dist/` | 本地 VPK 底包和构建产物，均由 Git 忽略 |

[快速上手](docs/QUICKSTART.zh-CN.md) · [架构文档](docs/architecture.md) · [词库示例](examples/dictionary.example.json) · [许可证与资源来源](NOTICE.md)
