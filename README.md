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
- `Build Console`：构建和测试新版可执行包。
- `Build Debian Packages`：从对应发行标签构建 amd64 / arm64 DEB 并测试。

主分支已移除旧版 Node / Pug 界面、旧打包脚本及历史截图；历史版本仍可通过 Git 历史与
历史 Releases 找回。自动构建不会部署到服务器。

项目许可证见 [LICENSE](LICENSE)，第三方组件许可证见
[`console/web/public/licenses`](console/web/public/licenses)。
