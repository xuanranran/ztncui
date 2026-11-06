# ztncui Console

自托管的 ZeroTier 网络管理界面，采用 Go + Vue 3，当前版本 **1.0.0**。
仅保留一个管理员账号，支持电脑、平板、折叠屏及普通手机。

## 功能

- 网络与成员管理、批量授权、设备置顶、托管 IP 排序和图标选择。
- IPv4 / IPv6 地址分配、托管路由、DNS、组播、广播和 MTU。
- 原生流规则、设备豁免、策略标签及能力管理。
- 实时刷新、在线状态、直连 / 中继信息及 Ping 补充检测。
- 可调整 Ping 间隔与超时，区分检测耗时和原生链路延迟。
- 网络二维码、管理数据导出、密码修改和操作记录。
- 亮色、跟随系统和 OLED 暗色模式，暗色底色为 `#000000`。

## 下载与安装

[GitHub Releases](https://github.com/xuanranran/ztncui/releases) 提供构建产物及 SHA-256 校验文件。
Linux 可执行包已内置前端，无需在运行服务器安装 Node.js。
DEB 提供 amd64、arm64 两个架构，通过隔离安装测试后发布。

安装前先配置 ZeroTier One，并通过 `dpkg --print-architecture` 确认架构。

```sh
sudo apt install ./ztncui-console_1.0.0_amd64.deb
# ARM64：sudo apt install ./ztncui-console_1.0.0_arm64.deb
```

DEB 默认仅监听 `127.0.0.1:3000`。使用本机浏览器或 SSH 隧道初始化管理员后，
可在 `/etc/ztncui-console/console.conf` 修改监听地址，再重启 `ztncui-console` 服务。
不要向公网暴露未初始化的管理员设置页面。

### 安装脚本

仓库提供中文提示的 [Debian/Ubuntu 安装脚本](scripts/install.sh)，支持 amd64、arm64。
需要 root、systemd、Python 3、util-linux，以及可用的 APT 软件源。
以下命令均在仓库根目录执行：

```sh
bash scripts/install.sh --help
# 推荐：将 DEB 和同名 .sha256 文件放在同一目录，避免私有仓库下载认证问题。
sudo bash scripts/install.sh --deb /path/to/ztncui-console_1.0.0_amd64.deb
# 或显式提供可信来源的 SHA-256：
sudo bash scripts/install.sh --deb /path/to/package.deb --sha256 <64位校验值>
```

未指定 `--deb` 时，从本仓库 Releases 下载安装包和校验文件。
私有仓库需要具有仓库内容读取权限的 `GITHUB_TOKEN`，不要把令牌写进脚本或命令历史：

```bash
read -rsp 'GitHub 只读令牌：' GITHUB_TOKEN; echo
export GITHUB_TOKEN
sudo --preserve-env=GITHUB_TOKEN bash scripts/install.sh
unset GITHUB_TOKEN
```

脚本会校验包名、版本、架构与 SHA-256；检查通过后，先备份再安装。
备份保存在 `/var/backups/ztncui-console/<UTC时间>-installer/`，包含已有管理数据库、
ZeroTier 目录、自定义服务配置和 planet 签名密钥目录。管理数据库备份期间短暂停止界面服务；
ZeroTier 运行目录为在线归档，不等于完全停机的一致性快照。备份含私钥和账号数据，请严格保密。

默认保留现有 planet、节点身份、管理员及配置，不执行系统软件自动清理。
仅在未安装 ZeroTier One 时调用官方安装器，已有核心不会自动升级或降级。
Console 安装后会重启并检查 HTTP 接口；失败时保留备份并报错，**不会自动还原数据库或软件包**。
已有 `/etc/systemd/system` 自定义服务配置仍然优先，端口不强制改为默认值。
旧版 Node 界面不会自动停止或导入；迁移须按下文链接操作，并避免监听端口冲突。

首次安装先建立 SSH 隧道，再打开 `http://127.0.0.1:3000` 创建管理员：

```sh
ssh -L 3000:127.0.0.1:3000 root@SERVER
```

### 可选私有 Planet

**普通安装和升级不需要重建 planet。** 只有明确要更换根节点时才执行：

```sh
sudo bash scripts/install.sh --deb /path/to/package.deb --generate-planet <服务器公网IPv4>
```

此选项使用 `9993/UDP`，会替换本机 planet 并重启 ZeroTier；客户端需要手动导入生成的
`/root/planet`。还需确认公网地址、NAT 映射和防火墙可达性，脚本不会替你开放端口。
签名密钥保存在 `/var/lib/ztncui-planet-keys/` 并在后续生成时复用，**不得分发给客户端**。
从其他部署迁移时，应先妥善迁移原签名密钥，不能把重新生成密钥当成无缝更新。

工具固定使用官方 1.14.2 源码及提交校验，只编译 planet 生成器，不安装该版本核心。
官方 1.16.2 已删除该生成器目录，因此不能简单替换源码版本号。
[隔离实测](https://github.com/xuanranran/ztncui/actions/runs/42226024594)已确认：Ubuntu 24.04 amd64 中，
1.14.2 生成的 planet 可被关闭 SSO 的 1.16.2 测试构建识别，建立活动路径、上线并在重启后重连。
该测试未覆盖 ARM64、公网 NAT、业务路由和流规则，也不代表完整安装脚本已经通过端到端安装测试。

## 数据与升级

ZeroTier 保存网络、成员及身份数据；管理界面使用独立 SQLite 数据库保存账号、名称、
置顶、图标、策略编辑信息和系统设置，默认位于 `/var/lib/ztncui-console/console.sqlite`。

升级前请备份管理数据库及完整 ZeroTier 数据目录。旧版用户可通过旧 `etc` 目录的
**离线副本**导入 `admin` 账号与元数据，其他管理员不会导入。
新旧界面不能占用同一端口，迁移不会自动删除旧版数据。

管理数据 JSON 导出不是完整控制器备份。Ping 未响应可能受到防火墙或路由限制，
不代表确定离线；限制性流规则请先在测试网络验证。

详细说明：[构建、部署与迁移文档](console/README.md)。

## 本地开发

需要 Go 1.26+、Node.js 24+ 和 npm。

```sh
cd console/web
npm ci
npm test
npm run build
cd ..
go test ./...
go run ./cmd/ztncui --demo --addr 127.0.0.1:3101 --db data/preview.sqlite
```

打开 `http://127.0.0.1:3101`，演示账号 `admin`，密码 `Preview123456!`。
演示模式使用隔离示例数据，不会访问或修改真实控制器。

## 仓库结构与构建

- `console/cmd`、`console/internal`：Go 服务及测试。
- `console/web`：Vue 前端、内置图标和字体、第三方许可。
- `console/packaging`：DEB 构建、systemd 配置和隔离安装测试。
- `scripts/install.sh`：保留数据和配置的安装器，以及显式启用的 planet 生成选项。
- `Build Console`：构建和测试新版可执行包。
- `Build Debian Packages`：从对应发行标签构建 amd64 / arm64 DEB 并测试。

主分支已移除旧版 Node / Pug 界面、旧打包脚本及历史截图；历史版本仍可通过 Git 历史与
历史 Releases 找回。自动构建不会部署到服务器。

项目许可证见 [LICENSE](LICENSE)，第三方组件许可证见
[`console/web/public/licenses`](console/web/public/licenses)。
