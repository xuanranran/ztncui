import test from "node:test";
import assert from "node:assert/strict";
import { presenceLabel, presenceClass, presenceTitle, probeDescription, connectionStatus } from "../src/presence.ts";

test("authorization never replaces observed online status", () => {
  const member = { authorized: false, online: true, presenceSource: "peer" };
  assert.equal(presenceLabel(member), "在线");
  assert.equal(presenceClass(member), "is-online");
});

test("missing native activity and unanswered probes remain inconclusive", () => {
  const member = { online: false, presenceState: "unknown", probe: { status: "unprobed" } };
  assert.equal(presenceLabel(member), "未观测");
  member.probe.status = "pending";
  assert.equal(presenceLabel(member), "检测中");
  member.probe.status = "no_response";
  assert.equal(presenceLabel(member), "未响应");
  assert.match(presenceTitle(member), /不代表确定离线/);
  assert.equal(presenceClass(member), "is-offline");
  member.probe.status = "unavailable";
  assert.equal(presenceLabel(member), "未观测");
});

test("IP reachability never claims a direct ZeroTier connection", () => {
  const member = { online: true, presenceSource: "probe", probe: { status: "reachable", address: "10.0.24.34", latency: 36 } };
  assert.equal(presenceLabel(member), "在线");
  assert.match(presenceTitle(member), /不代表与控制器直连/);
  assert.match(probeDescription(member), /36 ms/);
});

test("explicit demo offline states and disabled probes are labelled", () => {
  assert.equal(presenceLabel({ online: false, presenceState: "offline" }), "离线");
  assert.match(probeDescription({ probe: { status: "disabled" } }), /停用/);
  assert.match(probeDescription({ probe: { status: "no_response", address: "10.0.24.34" } }), /防火墙/);
});

test("Ping latency remains separate from native direct and relay latency", () => {
  const member = { online: true, connection: "unknown", presenceSource: "probe", paths: [], latency: -1, probe: { status: "reachable", address: "10.0.24.34", lastSuccess: Date.now(), latency: 36 } };
  assert.equal(connectionStatus(member).label, "Ping 检测");
  assert.equal(connectionStatus(member).latency, 36);
  assert.match(connectionStatus(member).title, /不是原生链路延迟/);
  member.connection = "direct";
  member.latency = 8;
  assert.equal(connectionStatus(member).label, "直连");
  assert.equal(connectionStatus(member).latency, 8);
  member.connection = "relay";
  assert.equal(connectionStatus(member).label, "中继");
});

test("in-flight and failed rechecks retain only labelled previous Ping latency", () => {
  const member = { online: true, connection: "unknown", presenceSource: "probe", paths: [], latency: -1, probe: { status: "pending", lastSuccess: Date.now(), lastSuccessAddress: "10.0.24.34", lastSuccessLatency: 37, latency: -1 } };
  assert.equal(connectionStatus(member).cached, true);
  assert.equal(connectionStatus(member).latency, 37);
  assert.equal(connectionStatus(member).address, "10.0.24.34");
  member.probe.status = "no_response";
  assert.equal(connectionStatus(member).latency, 37);
  member.online = false;
  assert.equal(connectionStatus(member).kind, "unknown");
  assert.equal(connectionStatus(member).latency, null);
});
