# ZeroTierOne 功能覆盖审计

审计日期：2026-11-06。范围是新版 `console/`，不是已经部署的旧界面。
源码基准是 upstream `dev` 提交
[`899352e38405968516bb12a770f0ac02f6058fa8`](https://github.com/zerotier/ZeroTierOne/tree/899352e38405968516bb12a770f0ac02f6058fa8)。
这不是对所有已发布版本的兼容承诺，正式部署前仍须针对实际 ZeroTier 版本联调。

## 已有控制器功能

| 分类 | 已覆盖内容 |
| --- | --- |
| 网络 | 创建、列表、名称、备注、私有/公开、删除、复制 ID、加入二维码 |
| IPv4 | 子网、私有地址前缀、随机生成、手动范围、多地址池、自动分配开关 |
| IPv6 | 自定义子网/地址池、自动分配、RFC4193、6PLANE |
| 托管路由 | IPv4/IPv6 CIDR、本地路由/成员网关、新增/删除、范围校验 |
| DNS | 搜索域、多服务器、IPv4/IPv6 服务器校验 |
| 二层 | 广播、组播接收上限、MTU、成员主动桥接 |
| 成员 | 手动添加、授权/撤销、批量操作、手动 IP、禁用自动分配、删除 |
| 本地管理 | 名称、文字标签、置顶、IP 数值排序与恢复原序、搜索、筛选、CSV |
| 状态 | 共享采样、SSE、重连、失败保留旧数据、直连/中继/延迟和最后活动 |
| 管理安全 | 单管理员、密码迁移和变更、会话、CSRF、登录限速、审计 |

来源：上游 [网络写入接口](https://github.com/zerotier/ZeroTierOne/blob/899352e38405968516bb12a770f0ac02f6058fa8/nonfree/controller/EmbeddedNetworkController.cpp#L779)、
[成员写入接口](https://github.com/zerotier/ZeroTierOne/blob/899352e38405968516bb12a770f0ac02f6058fa8/nonfree/controller/EmbeddedNetworkController.cpp#L1423)。
备注、文字标签和置顶属于控制台元数据，不是 ZeroTier 原生策略字段。

## 本轮补齐

- 流规则增加模板、上游规则语言和原生 JSON 三种编辑方式。
- 模板支持二层协议过滤、常用服务白名单及自定义 TCP/UDP 端口范围。
- 使用上游 MPL-2.0 规则编译器，保留行列错误定位；没有自行编写解析器。
- 模板保留 ARP 和 IPv6 邻居发现，放行服务的源/目标端口；它是无状态过滤，
  不等价于连接跟踪防火墙。需要更严策略时使用自定义规则及主机防火墙。
- 限制流量但未选择任何服务时禁止保存，避免误操作断网；有意全部阻断可用 `drop;`。
- 应用规则前确认。模板和 JSON 只修改基础 `rules`；自定义源码中的标签、能力定义按 ID 合并，保留未在源码中出现的已有定义。
- 模板增加设备豁免搜索、授权状态、分页和逐设备开关。豁免规则同时匹配
  `ztsrc/ztdest`，在协议过滤前求值，仅在启用流量限制时生效，不改变成员授权。
- “恢复默认规则”有独立确认并立即保存：基础规则恢复 `accept;`，清除模板限制、
  自定义端口和豁免列表；不改变网络、路由、地址池、DNS 或成员数据。
- 规则源码/模板单独存入 SQLite，并与编译结果绑定。其他工具修改规则后，
  不再用过期模板解释新规则，而是回到当前真实 JSON。
- 设备原生名称不再被缺失的本地备注覆盖，离线版本可从 `vMajor/vMinor/vRev` 回填。
- 原生成员数值策略标签保留为 `policyTags`，不与本地 `tags` 文字标签混淆。
- 原生标签管理支持 ID、可选默认值、枚举值及名称；能力管理支持默认分配和最多 64 条原生/编译规则。
- 成员标签矩阵和能力矩阵支持搜索、分页、逐成员或多成员保存及撤销，原生赋值与本地文字标签分开保存。
- 成员策略写入同时校验成员策略版本和网络版本；有成员赋值或规则引用的定义不能直接删除。
- 能力默认分配仅针对控制器新建的成员；标签默认值会补充缺失的值，不覆盖已有显式赋值。
- 系统设置统一全宽布局，增加状态刷新、零层地址复制、操作记录搜索与实时更新。导出过滤加入令牌和 SSO/成员认证凭据。

编译器来源：[rule-compiler.js](https://github.com/zerotier/ZeroTierOne/blob/899352e38405968516bb12a770f0ac02f6058fa8/rule-compiler/rule-compiler.js)。
行为说明：[官方 Rules Engine 文档](https://docs.zerotier.com/rules/)。

## 尚未提供的选项

| 优先级 | 字段/能力 | 当前边界与后续建议 |
| --- | --- | --- |
| P2 | 网络/成员 `remoteTraceTarget`、`remoteTraceLevel` | 适合作为可折叠诊断项。目标为 ZeroTier 节点，不是任意 IP；需校验权限、实际版本和追踪接收端。 |
| P2 | `authTokens` 自动授权及过期时间 | 属于网络加入授权，不是服务 API 的 `authtoken.secret`。需一次显示、撤销、过期与审计，暂不提供编辑入口。 |
| P2 | `authenticationExpiryTime`、`authenticationURL` | 底层有成员字段，但并不能单凭两个输入框实现完整 SSO；应与认证提供方一同设计。 |
| P3 | `relays` | 源码存在解析路径；先验证目标版本实际行为，再考虑开放，不能与 Moon/Planet 混为一谈。 |
| P3 | 更多只读信息 | 身份公钥、原生授权时间/方式、协议版本、完整 peer 路径和诊断事件，可放入设备详情的高级区。 |

对应源码：
[原生策略定义](https://github.com/zerotier/ZeroTierOne/blob/899352e38405968516bb12a770f0ac02f6058fa8/nonfree/controller/EmbeddedNetworkController.cpp#L951)、
[成员策略赋值](https://github.com/zerotier/ZeroTierOne/blob/899352e38405968516bb12a770f0ac02f6058fa8/nonfree/controller/EmbeddedNetworkController.cpp#L1476)、
[默认字段与 SSO 配置](https://github.com/zerotier/ZeroTierOne/blob/899352e38405968516bb12a770f0ac02f6058fa8/nonfree/controller/DB.cpp#L26)。

## 不应误放为全网络开关的内容

- `allowManaged`、`allowGlobal`、`allowDefault`、`allowDNS` 是每台客户端的本地选择。
  在控制器设置默认路由或 DNS，并不能代替客户端同意使用它们。
- UDP 监听端口、端口映射、接口黑名单、Moon/Planet、TCP fallback 是节点运行配置。
  不能通过普通成员接口远程改变所有设备；后续可增加只读诊断和客户端配置导出。
- Linux IP forwarding、NAT、防火墙以及 LAN 回程路由不属于 ZeroTier 网络控制器。
  主动桥接不等于已建立跨网段三层转发。
- New Central 的组织、订阅、网络分组和团队权限不是 ZeroTierOne 嵌入控制器 API。
  本项目保持单管理员，不引入付费提示或额外管理员入口。

来源：[客户端本地设置](https://github.com/zerotier/ZeroTierOne/blob/899352e38405968516bb12a770f0ac02f6058fa8/service/OneService.cpp#L623)、
[官方控制器指南](https://docs.zerotier.com/controller/)。

## New Central 交互对照

已在获得授权的登录会话中只读查看，未保存或更改云端网络配置。
二维码弹窗使用居中标题、260px 二维码、名称/ID 信息条及整行复制按钮。
共享弹窗采用 32% 黑色遮罩、225ms 缩放淡入、125ms 关闭，支持减少动态效果、
Escape、焦点约束和关闭后焦点恢复。对话框实色背景，暗色保持 `#242424`。
网络页面使用参考站的全宽布局、32px 桌面边距、20px 标题、16px 正文、14px 按钮及 16px 圆角，
不再受到 1600px 页面和 1000px 流规则双重限宽，系统设置采用同一套尺寸和配色。
移动端为 16px 页面边距，并保留纯黑背景和 `#242424` 实色工具区域。
目前不是整个 New Central 的逐像素克隆，也不会复制其订阅限制。

## 验证边界

自动测试涵盖服务模板编译、双向设备豁免、端口/ID 边界、规则错误定位、源码单独存储、
恢复默认的并发保护、原生策略定义/赋值、删除引用保护、成员并发冲突和导出敏感字段过滤。
浏览器验证覆盖隔离演示库及 `192.168.2.190` 的真实成员列表、登录、实时采样和界面迁移。
尚未在真实客户端验证限制性规则转发，正式启用前应先在测试网络联调。

在线状态与授权状态独立显示。原生 peer 接收时间优先，缺少活动记录时使用已授权成员的
托管私有 IP 进行有界、低频 ICMP 补充检测；支持列表与单设备手动触发。
IP 可达不等于控制器直连，未响应也不等于确定离线，工具/权限不可用单独标识。
检测不修改控制器配置，不提升服务权限；预览模式不发送真实探测。
