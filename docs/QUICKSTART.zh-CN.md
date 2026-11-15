# Deadlock AI Translator 快速上手

## Windows x86-64 安装

1. **解压 ZIP。** 如果正在使用旧版翻译桥，先退出旧桥和 Deadlock。
2. **启动翻译桥。** 双击解压目录里的 `deadlock-translator.exe`，游玩时保持终端窗口打开。首次出现 Windows 证书信任提示时，核对名称 **Deadlock Translator Go Local Root** 后确认，再完全退出并重启 Steam（包括系统托盘里的 Steam）。
3. **配置翻译服务。** 在浏览器打开 [本地设置页](http://127.0.0.1:8791/ui)，选择 DeepSeek 或其他服务商，填写所需的 API Key 并保存。如果之前已配置 Go 版，启动时会读取原有设置。
4. **安装游戏 Mod。** 用 Mod Manager 安装或替换解压目录里的 `pak23_dir.vpk`。手动安装时，在 Steam 的 Deadlock 菜单中选择「管理 → 浏览本地文件」，打开 `game\citadel\addons` 并复制 VPK。升级时替换旧的翻译 Mod，确保只加载一份。然后启动游戏。

## 游戏内用法

| 聊天框输入 | 效果 |
| --- | --- |
| `我去守家` | 按当前设置处理这条消息。 |
| `/t` | 开关发送前翻译，不影响接收翻译。 |
| `/r 我去守家` | 仅这条消息直发「我去守家」。`/raw` 也可以使用。 |

Go 桥接终端里的 `[聊天][收]` 和 `[聊天][发]` 分别显示收到和发出的消息，包括原文、译文以及直发或翻译失败等状态。可在网页设置里切换「在终端显示聊天原文和译文」。

如果 `/r` 被当成普通聊天发出，请确认游戏加载的是这个发布包里的 VPK。如果设置页能打开但游戏桥一直显示离线，请确认桥接终端仍开着，并在首次信任证书后重启 Steam。

## 更新

Go 桥启动时会在终端显示版本和程序的 SHA-256。本地设置页打开时会自动检查更新，也可以点击「检查更新」重试。发现新版后点击「安装更新」，完成后关闭并重新启动 Go 桥。若发布说明提到游戏 Mod 更新，从 GitHub Releases 下载对应平台的安装包，使用其中的 `pak23_dir.vpk` 替换游戏里的旧版。

## Linux x86-64 / Steam Deck

在解压目录运行 `./deadlock-translator --init-tls`，让 Steam 信任生成的根证书，再运行 `./deadlock-translator`。打开本地设置页，并用同一目录的 `pak23_dir.vpk` 安装游戏 Mod。Proton 下的游戏接入待实测。

## 来源和许可证

Go 桥保留 BabelTower 的 GPL-3.0 许可。游戏界面和 CEF 桥页派生自 BabelTower Community v1.0.1，按 AGPL-3.0-or-later 提供。游戏素材版权归 Valve 所有。[BabelTower](https://github.com/Thirt927/BabelTower) · [BabelTower Community](https://github.com/c1375rick/babeltower-community) · [GPL-3.0](https://www.gnu.org/licenses/gpl-3.0.html) · [AGPL-3.0](https://www.gnu.org/licenses/agpl-3.0.html)
