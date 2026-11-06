# 安装与维护

## 系统要求

支持 Debian/Ubuntu、systemd 和 amd64/arm64，使用 root 或 sudo。
一键命令需要 curl；脚本会按需安装 Python 3、util-linux、HTTPS 根证书和运行依赖。
需要能访问 GitHub Releases 和系统 APT 软件源。下载公开发行包无需 Token。

从仓库根目录查看参数，或仅更新 Console：

```sh
bash scripts/install.sh --help
sudo bash scripts/install.sh
sudo bash scripts/install.sh --version 1.0.0
```

默认版本为脚本中的 `VERSION`，不会自动选取其他版本。仓库 master 安装脚本会随维护更新，
执行前可审阅源码；需要固定脚本版本时，可把下载 URL 中的 master 换成经过核对的提交 SHA。

## 本地安装包

将对应架构的 DEB 和同名 `.sha256` 文件放在同一目录：

```sh
sudo bash scripts/install.sh --deb /path/to/ztncui-console_1.0.0_amd64.deb
```

也可以通过 `--sha256` 指定可信来源的 64 位校验值。
脚本验证包名、版本、架构和 SHA-256，校验成功后才暂停界面服务备份。
SHA-256 用于完整性检查，不等于独立的发布者签名验证。

## 配置与备份

全新安装会先保持 `127.0.0.1:3000`，提示输入并确认初始 `admin` 密码（10 至 256 字节）。
本机初始化完成后，自动改为 `0.0.0.0:3000` 并重启 Console，访问 `http://服务器IP:3000`。
密码不写入服务配置或日志；初始化失败不开放监听。重启或就绪验证失败则恢复原监听地址。
`0.0.0.0` 包括所有 IPv4 接口，不仅局域网；请使用防火墙限制来源，公网另配 HTTPS。

无交互终端可传入 `--admin-password-file /path/to/password`：文件须为普通文件，
由 root 或当前 sudo 用户所有，权限为 `400`/`600`，内容为初始密码，不支持符号链接。
密码文件由你管理，使用后请妥善处理；脚本不会自动删除文件。

仅本机访问可使用 `--local-only`，此时不会自动初始化管理员或开放监听，通过 SSH 隧道访问：

```sh
ssh -L 3000:127.0.0.1:3000 root@SERVER
```

已有软件包、管理数据目录、配置目录或自定义服务文件均按已有安装处理，
**保留原来的监听地址、端口和管理员，不执行新的初始化**。
已有安装如果只监听本机，需明确修改 `/etc/ztncui-console/console.conf` 的 `LISTEN_ADDR`，
再执行 `sudo systemctl restart ztncui-console`，脚本不会擅自更改访问范围。
已有 `/etc/systemd/system` 中的自定义服务配置优先，不会被脚本覆盖。

默认保留管理员、数据库、planet、节点身份和配置，不自动清理系统软件。
仅缺少 ZeroTier One 时调用官方安装器，已有核心不会自动升级或降级。
旧 Node 界面不会自动停止或导入，请按[迁移说明](../console/README.md#data-ownership-and-migration)操作。

备份位于 `/var/backups/ztncui-console/<UTC时间>-installer/`，包含已有管理数据、
ZeroTier 目录、服务配置和 planet 签名密钥目录。备份含私钥和账号信息，不要公开。
管理数据库备份期间短暂停止界面服务；ZeroTier 目录为在线归档，不是完整停机一致性快照。
安装后重启 Console 并检查 HTTP 接口；失败时保留备份并报错，不自动还原数据库或软件包。
工作目录也会保留，便于排错；后续清理前请确认内容与备份。

## 可选私有 Planet

**普通安装或升级不需要生成 planet。** 明确需要创建或更换根节点时，才使用：

```sh
sudo bash scripts/install.sh --generate-planet YOUR_PUBLIC_IPV4
```

将 `YOUR_PUBLIC_IPV4` 替换为服务器公网 IPv4。该操作会替换本机 planet、重启 ZeroTier，
并生成 `/root/planet` 供客户端手动导入。必须核对公网地址、NAT 映射和 UDP 9993 可达性，
脚本不会自动开放防火墙。自建根节点不保证业务节点一定直连。

签名密钥保存在 `/var/lib/ztncui-planet-keys/`，后续生成时复用，**不能分发给客户端**。
迁移其他部署时应先妥善迁移原签名密钥，重新生成密钥不是无缝更新。

生成器固定使用官方 1.14.2 源码和提交校验，只编译工具，不安装该版本核心。
官方 1.16.2 已删除工具目录，不能简单替换源码版本。
[隔离测试](https://github.com/xuanranran/ztncui/actions/runs/42226024594)通过了 Ubuntu 24.04 amd64 上的
工具编译、planet 生成，以及关闭 SSO 的 1.16.2 节点对 PLANET 的识别、连接、上线和重启重连。
未覆盖 ARM64、公网 NAT、业务路由、流规则及完整安装脚本的端到端安装流程。
