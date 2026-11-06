import type { Member } from "./types";

export function presenceLabel(member: Member): string {
  if (member.online) return "在线";
  if (!member.presenceState || member.presenceState === "offline") return "离线";
  if (member.probe?.status === "pending") return "检测中";
  if (member.probe?.status === "no_response") return "未响应";
  return "未观测";
}

export function presenceClass(member: Member): string {
  if (member.online) return "is-online";
  return ["离线", "未响应"].includes(presenceLabel(member))
    ? "is-offline"
    : "is-unknown";
}

export function presenceTitle(member: Member): string {
  if (member.presenceSource === "probe" && member.online) {
    return `近期确认托管 IP ${member.probe?.lastSuccessAddress || member.probe?.address || ""} 可达，不代表与控制器直连`;
  }
  if (member.online) return "检测到近期原生 ZeroTier 活动";
  if (member.probe?.status === "pending") return "正在从服务器检测托管 IP 可达性";
  if (member.probe?.status === "no_response") {
    return "暂无近期原生活动，ICMP 未响应；可能是防火墙或路由限制，不代表确定离线";
  }
  if (member.probe?.status === "unavailable") return "服务器 ICMP 检测不可用，无法确定设备是否离线";
  if (member.probe?.status === "disabled") return "服务器已停用 ICMP 检测，暂无近期原生活动";
  return "控制器暂无近期活动记录，不代表设备确定离线";
}

export function probeDescription(member: Member): string {
  const probe = member.probe;
  if (!probe || probe.status === "unprobed") return "尚无 IP 检测记录";
  if (probe.status === "disabled") return "服务器已停用 ICMP 检测";
  if (probe.status === "pending") return "正在检测托管 IP";
  if (probe.status === "unavailable") return "ICMP 检测不可用（工具、权限或运行平台限制）";
  if (probe.status === "reachable") return `${probe.address} 可达，响应耗时 ${probe.latency} ms`;
  return `${probe.address} 未响应（也可能被防火墙拦截或无可用路由）`;
}

export function connectionStatus(member: Member) {
  if (member.connection === "direct" || member.connection === "relay") {
    return {
      kind: member.connection,
      label: member.connection === "direct" ? "直连" : "中继",
      latency: member.latency >= 0 ? member.latency : null,
      address: member.paths[0] || "无活动路径",
      title: member.paths.join("\n"),
      cached: false,
    };
  }
  const probe = member.probe;
  if (member.online && member.presenceSource === "probe" && probe?.lastSuccess) {
    const latency = probe.lastSuccessLatency ?? (probe.status === "reachable" ? probe.latency : -1);
    return {
      kind: "ping",
      label: "Ping 检测",
      latency: latency >= 0 ? latency : null,
      address: probe.lastSuccessAddress || probe.address,
      title: `服务器 ICMP 检测响应耗时（含命令开销），不是原生链路延迟。最近成功：${new Date(probe.lastSuccess).toLocaleString("zh-CN", { hour12: false })}`,
      cached: probe.status !== "reachable",
    };
  }
  return { kind: "unknown", label: "—", latency: null, address: "无活动路径", title: "暂无可用连接记录", cached: false };
}
