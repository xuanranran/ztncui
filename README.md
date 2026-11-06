# ztncui Console

自托管 ZeroTier 管理界面，采用 Go + Vue 3，当前版本 **1.0.0**。
保留官方 ZeroTier 核心，仅支持一个管理员，适配电脑、平板与手机。

## 功能

- 网络、成员、批量授权、设备置顶、图标和 IP 排序。
- IPv4/IPv6 分配、托管路由、DNS、组播及原生流规则。
- 设备豁免、策略标签、能力管理和二维码分享。
- 实时刷新、直连/中继状态、可配置的 Ping 补充检测。
- 亮色、跟随系统与 OLED 纯黑模式。

## 一键安装或更新

支持使用 systemd 的 Debian/Ubuntu，自动识别 amd64/arm64，**不需要 Token**。
需要预先安装 `curl` 和 `sudo`；root 用户可去掉命令中的 `sudo`。

```sh
installer=$(mktemp) && curl -fsSL https://raw.githubusercontent.com/xuanranran/ztncui/master/scripts/install.sh -o "$installer" && sudo bash "$installer"
```

默认下载 1.0.0 DEB 并校验 SHA-256，**先备份，保留现有数据、管理员密码、配置和 planet**。
已有官方核心不会自动升级或降级；仅缺少 ZeroTier One 时安装官方版本。
Console 备份和更新会短暂停止或重启管理界面，默认不重启 ZeroTier。

全新安装时，脚本会提示设置 `admin` 密码，完成本机初始化后自动监听 `0.0.0.0:3000`，
使用 **`http://服务器IP:3000`** 访问，无需再手动修改监听地址。
无交互终端时可用 `--admin-password-file`，需要仅本机访问时可用 `--local-only`。
初始化失败会保持本机监听，不开放未初始化的页面。

**已有安装不会改变原来的监听地址或端口**，也不会重置账号。
配置位于 `/etc/ztncui-console/console.conf`，已有自定义 systemd 配置优先。
`0.0.0.0` 包含所有 IPv4 接口，请用防火墙限制来源；公网访问应另配 HTTPS。

也可从 [Releases](https://github.com/xuanranran/ztncui/releases) 下载 DEB 离线安装；
[安装详解](docs/INSTALL.md)包含参数、备份和可选私有 Planet 操作。

## 数据与升级

- 管理数据库：`/var/lib/ztncui-console/console.sqlite`。
- ZeroTier 身份和控制器数据：`/var/lib/zerotier-one/`。
- 脚本备份：`/var/backups/ztncui-console/<UTC时间>-installer/`，含敏感信息，请妥善保管。
- 旧版 Node 界面不会自动停止或迁移，需[离线导入](console/README.md#data-ownership-and-migration)，避免端口冲突。
- 安装失败保留备份，不自动回滚；管理数据 JSON 导出不是完整控制器备份。

DEB 已通过隔离安装测试；planet 兼容性测试范围见[安装详解](docs/INSTALL.md)。
完整安装脚本尚未完成端到端安装验证。Ping 无响应不等于离线。

## 开发与许可

构建、测试和本地演示见 [开发文档](console/README.md)。
历史版本保留在 Git 历史和 Releases 中，Actions 不会自动部署服务器。

[项目许可证](LICENSE) · [第三方许可](console/web/public/licenses)
