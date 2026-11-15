# Panorama VPK 底包

完整发布包使用 [BabelTower Community v1.0.1](https://github.com/c1375rick/babeltower-community/releases/tag/v1.0.1) 的 [pak03_dir.vpk](https://github.com/c1375rick/babeltower-community/releases/download/v1.0.1/pak03_dir.vpk) 作为构建输入。下载后从仓库根目录运行以下命令。Linux 上使用 `python3`。

```sh
python scripts/build_release.py --base-vpk "路径/pak03_dir.vpk"
```

上游底包 SHA-256 为 `5df514abd1a66edc1aaaa41a841b8b7b4b219cd349b44c648961caa539a2779d`。GitHub Actions 从同一固定地址下载底包，构建时由 `scripts/build_vpk.py` 校验。

本地底包可以放在 Git 忽略的 `local/vpk/`。脚本默认使用 `local/vpk/community-v1.0.1-local.vpk`，即上一版 Deadlock AI Translator 的底包，SHA-256 为 `8e23728cf20861e85da93ba93dcb5ce8e7541c42effa4e79c328f11f3ae7aefd`。使用其他路径时传入 `--base-vpk`。

打包脚本替换底包中的桥接协议及聊天脚本，并生成 `dist/pak23_dir.vpk`。修改 XML、CSS 或其他 Panorama 脚本时，需要用 Source 2 资源编译工具重新生成编译资源。
