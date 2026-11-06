import { compile } from "./vendor/zerotier-rule-compiler.js";
import type { Capability, PolicyMetadata, PolicyTag, Rule } from "./types";

export type Template = {
  layer2: boolean;
  restrict: boolean;
  services: string[];
  ports: { protocol: "tcp" | "udp"; start: number; end: number }[];
  excluded: string[];
};
export const services = [
  { id: "ping", name: "Ping 与路由追踪", detail: "ICMP / ICMPv6", ports: [] },
  {
    id: "smb",
    name: "Windows 文件共享",
    detail: "137、138/UDP；139、445/TCP",
    ports: [
      ["udp", 137],
      ["udp", 138],
      ["tcp", 139],
      ["tcp", 445],
    ],
  },
  {
    id: "dns",
    name: "DNS",
    detail: "53 / TCP、UDP",
    ports: [
      ["tcp", 53],
      ["udp", 53],
    ],
  },
  { id: "http", name: "HTTP", detail: "80 / TCP", ports: [["tcp", 80]] },
  { id: "https", name: "HTTPS", detail: "443 / TCP", ports: [["tcp", 443]] },
  { id: "ssh", name: "SSH", detail: "22 / TCP", ports: [["tcp", 22]] },
  {
    id: "rdp",
    name: "远程桌面",
    detail: "3389 / TCP、UDP",
    ports: [
      ["tcp", 3389],
      ["udp", 3389],
    ],
  },
] as const;
export function defaultTemplate(): Template {
  return {
    layer2: false,
    restrict: false,
    services: [],
    ports: [],
    excluded: [],
  };
}
export function compilePolicy(source: string) {
  const rules: Rule[] = [],
    capabilities: Record<
      string,
      { id: number; default: boolean; rules: Rule[] }
    > = {},
    tags: Record<
      string,
      { id: number; default: number | null; enums: Record<string, number> }
    > = {};
  const error = compile(source, rules, capabilities, tags);
  if (error)
    throw new Error(`第 ${error[0]} 行，第 ${error[1]} 列：${error[2]}`);
  if (!rules.length || rules.length > 1024)
    throw new Error("规则数量必须为 1 至 1024 条");
  const metadata: PolicyMetadata = { tags: {}, capabilities: {} };
  const tagList: PolicyTag[] = [],
    capList: Capability[] = [];
  for (const [name, tag] of Object.entries(tags)) {
    if (tagList.some((t) => t.id === tag.id)) throw new Error("标签 ID 重复");
    tagList.push({ id: tag.id, default: tag.default });
    metadata.tags[tag.id] = {
      name,
      values: Object.fromEntries(
        Object.entries(tag.enums || {}).map(([label, value]) => [
          String(value),
          label,
        ]),
      ),
    };
  }
  for (const [name, cap] of Object.entries(capabilities)) {
    if (capList.some((c) => c.id === cap.id)) throw new Error("能力 ID 重复");
    if (cap.rules.length > 64) throw new Error("每项能力最多 64 条规则");
    capList.push({ id: cap.id, default: !!cap.default, rules: cap.rules });
    metadata.capabilities[cap.id] = { name };
  }
  if (tagList.length > 128 || capList.length > 128)
    throw new Error("策略定义最多 128 项");
  return { rules, tags: tagList, capabilities: capList, metadata };
}
export function compileRules(source: string) {
  const policy = compilePolicy(source);
  if (policy.tags.length || policy.capabilities.length)
    throw new Error("能力规则正文不能嵌套 capability/tag 定义");
  return policy.rules;
}
export function mergeDefinitions<T extends { id: number }>(
  current: T[],
  updates: T[],
): T[] {
  return [
    ...new Map(
      [...current, ...updates].map((value) => [value.id, value]),
    ).values(),
  ].sort((a, b) => a.id - b.id);
}
export function templateSource(value: Template) {
  const lines: string[] = [];
  const excluded = value.excluded ?? [];
  if (
    excluded.length > 128 ||
    excluded.some((id) => !/^[0-9a-f]{10}$/i.test(id))
  )
    throw new Error("豁免设备 ID 必须为 10 位十六进制，最多 128 台设备");
  // Evaluate both directions before protocol restrictions, on every endpoint.
  if (value.restrict) {
    for (const id of new Set(excluded.map((id) => id.toLowerCase())))
      lines.push(`accept ztsrc ${id} or ztdest ${id};`);
  }
  if (value.layer2)
    lines.push(
      "drop not ethertype ipv4 and not ethertype arp and not ethertype ipv6;",
    );
  if (!value.restrict) return [...lines, "accept;"].join("\n");
  if (!value.services.length && !value.ports.length)
    throw new Error("至少选择一种允许的服务或添加自定义端口");
  if (value.services.some((id) => !services.some((s) => s.id === id)))
    throw new Error("未知服务");
  // Keep address resolution available even when service traffic is restricted.
  lines.push(
    "accept ethertype arp;",
    "accept ipprotocol icmp6 and icmp 135 -1;",
    "accept ipprotocol icmp6 and icmp 136 -1;",
  );
  for (const service of services.filter((s) => value.services.includes(s.id))) {
    if (service.id === "ping")
      lines.push("accept ipprotocol 1;", "accept ipprotocol 58;");
    for (const [protocol, port] of service.ports)
      lines.push(...portRules(protocol, port, port));
  }
  for (const p of value.ports)
    lines.push(...portRules(p.protocol, p.start, p.end));
  return [...lines, "drop;"].join("\n");
}
function portRules(protocol: string, start: number, end: number) {
  if (
    !["tcp", "udp"].includes(protocol) ||
    !Number.isInteger(start) ||
    !Number.isInteger(end) ||
    start < 1 ||
    end > 65535 ||
    start > end
  )
    throw new Error("端口范围须为 1 至 65535，且起始端口不大于结束端口");
  const range = start === end ? String(start) : `${start}-${end}`;
  return [
    `accept ipprotocol ${protocol} and dport ${range};`,
    `accept ipprotocol ${protocol} and sport ${range};`,
  ];
}
export function canonicalRules(rules: unknown): string {
  return JSON.stringify(rules, (_key, value) => {
    if (!value || typeof value !== "object" || Array.isArray(value))
      return value;
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .filter((k) => !(k === "not" || k === "or") || value[k] !== false)
        .map((k) => [k, value[k]]),
    );
  });
}
