# Deadlock AI Translator

《Deadlock》游戏内双向聊天翻译，支持双语发送和自定义词库。游戏 Mod 将聊天交给本机 Go 翻译桥，由翻译桥查词库或调用你配置的翻译服务。

本项目基于 BabelTower Community 的游戏模组和桥接页面开发。 [上游仓库](https://github.com/c1375rick/babeltower-community)

## 安装

1. 从本仓库的 **Releases** 下载并解压 `Deadlock-AI-Translator-Go.zip`。
2. 启动翻译桥（Windows 和 Linux 均为 x86-64）。
   - Windows 双击 `deadlock-translator.exe`。首次启动时信任 **Deadlock Translator Go Local Root**，然后完全退出并重启 Steam。
   - Linux 进入 `linux-amd64`，先运行 `./deadlock-translator --init-tls`，让 Steam 信任生成的根证书，再运行 `./deadlock-translator`。
3. 在设置页选择翻译服务，按服务要求填写 API Key。 [打开本地设置页](http://127.0.0.1:8791/ui)
4. 用 Mod Manager 安装 `pak23_dir.vpk`，或将它复制到 `Deadlock/game/citadel/addons/`。游玩时保持翻译桥运行。

## 游戏内用法

- 收到的消息会在聊天中显示译文。发送模式可选原文、译文或双语。
- `/t` 切换发送前翻译，`/r 我去守家` 让当前这条消息直发原文。
- 在设置页的「游戏语境」中添加英雄名、装备名等自定义术语。

## 构建

- **编译翻译桥**（Go 1.25.12+）运行 `go build ./cmd/deadlock-translator`。
- **生成完整发布包**（Python 3.9+）运行 `python scripts/build_release.py`，Linux 上使用 `python3`。产物位于 `dist/`。

[架构文档](docs/architecture.md) · [许可证与资源来源](NOTICE.md)
# Deadlock-AI-Translator
