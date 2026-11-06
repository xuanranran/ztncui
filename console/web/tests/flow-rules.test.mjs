import { test } from "node:test";
import assert from "node:assert/strict";
import {
  compileRules,
  compilePolicy,
  mergeDefinitions,
  templateSource,
  defaultTemplate,
  canonicalRules,
  services,
} from "../src/flow-rules.ts";

test("default template allows existing layer two traffic", () => {
  assert.equal(
    canonicalRules(compileRules(templateSource(defaultTemplate()))),
    canonicalRules([{ type: "ACTION_ACCEPT", not: false, or: false }]),
  );
});
test("layer two filter compiles through upstream engine", () => {
  const rules = compileRules(
    templateSource({ ...defaultTemplate(), layer2: true }),
  );
  assert.deepEqual(
    rules.filter((r) => r.type === "MATCH_ETHERTYPE").map((r) => r.etherType),
    [2048, 2054, 34525],
  );
  assert.equal(rules.at(-1).type, "ACTION_ACCEPT");
});
test("every predefined service compiles, preserves address resolution and drops remaining traffic", () => {
  for (const service of services) {
    const rules = compileRules(
      templateSource({
        ...defaultTemplate(),
        restrict: true,
        services: [service.id],
      }),
    );
    assert.equal(rules[0].type, "MATCH_ETHERTYPE");
    assert.equal(rules[0].etherType, 2054);
    assert.ok(rules.some((r) => r.type === "MATCH_ICMP" && r.icmpType === 135));
    assert.ok(rules.some((r) => r.type === "MATCH_ICMP" && r.icmpType === 136));
    assert.equal(rules.at(-1).type, "ACTION_DROP");
  }
});
test("custom ranges compile both directions", () => {
  const rules = compileRules(
    templateSource({
      ...defaultTemplate(),
      restrict: true,
      ports: [{ protocol: "udp", start: 4000, end: 4010 }],
    }),
  );
  for (const type of ["MATCH_IP_SOURCE_PORT_RANGE", "MATCH_IP_DEST_PORT_RANGE"])
    assert.ok(
      rules.some((r) => r.type === type && r.start === 4000 && r.end === 4010),
    );
});
test("unsafe empty restriction and invalid ports cannot be submitted", () => {
  assert.throws(() => templateSource({ ...defaultTemplate(), restrict: true }));
  for (const [start, end] of [
    [0, 1],
    [2, 1],
    [1, 65536],
    [1.5, 2],
  ])
    assert.throws(() =>
      templateSource({
        ...defaultTemplate(),
        restrict: true,
        ports: [{ protocol: "tcp", start, end }],
      }),
    );
});
test("compiler errors retain location and unsupported policy definitions are explicit", () => {
  assert.throws(() => compileRules("accept no_such_match;"), /第 .*行/);
  assert.throws(
    () => compileRules("tag department id 1; accept;"),
    /capability\/tag/,
  );
});
test("canonical comparison ignores object key order, but retains rule order and true flags", () => {
  assert.equal(
    canonicalRules([{ type: "ACTION_ACCEPT", not: false }]),
    canonicalRules([{ type: "ACTION_ACCEPT" }]),
  );
  assert.notEqual(
    canonicalRules([{ type: "ACTION_ACCEPT", not: true }]),
    canonicalRules([{ type: "ACTION_ACCEPT" }]),
  );
  assert.notEqual(
    canonicalRules([{ type: "ACTION_ACCEPT" }, { type: "ACTION_DROP" }]),
    canonicalRules([{ type: "ACTION_DROP" }, { type: "ACTION_ACCEPT" }]),
  );
});

test("device exemptions allow source OR destination before any protocol filtering", () => {
  const rules = compileRules(
    templateSource({
      ...defaultTemplate(),
      restrict: true,
      layer2: true,
      services: ["ssh"],
      excluded: ["0c1cb97eaa", "1996777016"],
    }),
  );
  for (const [index, id] of ["0c1cb97eaa", "1996777016"].entries()) {
    assert.deepEqual(rules.slice(index * 3, index * 3 + 3), [
      { type: "MATCH_SOURCE_ZEROTIER_ADDRESS", not: false, or: false, zt: id },
      { type: "MATCH_DEST_ZEROTIER_ADDRESS", not: false, or: true, zt: id },
      { type: "ACTION_ACCEPT" },
    ]);
  }
  assert.equal(rules[6].type, "MATCH_ETHERTYPE");
  assert.equal(rules.at(-1).type, "ACTION_DROP");
});
test("exemptions do not affect unrestricted traffic and restore clears all restrictions", () => {
  const template = defaultTemplate();
  template.excluded = ["0c1cb97eaa"];
  assert.equal(
    canonicalRules(compileRules(templateSource(template))),
    canonicalRules([{ type: "ACTION_ACCEPT" }]),
  );
  assert.deepEqual(defaultTemplate().excluded, []);
  assert.deepEqual(defaultTemplate().ports, []);
  assert.deepEqual(defaultTemplate().services, []);
  assert.equal(defaultTemplate().restrict, false);
  assert.equal(defaultTemplate().layer2, false);
});
test("invalid exemption IDs cannot inject rules and excessive exemptions are rejected", () => {
  for (const id of ["", "00000000000", "x000000000", "0c1cb97eaa;accept;"])
    assert.throws(() =>
      templateSource({
        ...defaultTemplate(),
        restrict: true,
        services: ["ssh"],
        excluded: [id],
      }),
    );
  assert.throws(() =>
    templateSource({
      ...defaultTemplate(),
      restrict: true,
      services: ["ssh"],
      excluded: Array(129).fill("0c1cb97eaa"),
    }),
  );
});
test("legacy saved templates without exclusions remain valid", () => {
  const value = { layer2: false, restrict: true, services: ["ssh"], ports: [] };
  assert.equal(compileRules(templateSource(value)).at(-1).type, "ACTION_DROP");
});

test("SMB label includes the configured legacy and direct-hosted ports", () => {
  const service = services.find((s) => s.id === "smb");
  assert.deepEqual(service.ports, [
    ["udp", 137],
    ["udp", 138],
    ["tcp", 139],
    ["tcp", 445],
  ]);
  for (const port of [137, 138, 139, 445])
    assert.ok(service.detail.includes(String(port)));
});

test("full source compiles tags, enum defaults and capability definitions", () => {
  const policy = compilePolicy(
    "tag department id 7 enum 100 engineering enum 200 sales default engineering; cap ssh id 9 accept ipprotocol tcp and dport 22; ; break;",
  );
  assert.deepEqual(policy.tags, [{ id: 7, default: 100 }]);
  assert.equal(policy.metadata.tags[7].values["200"], "sales");
  assert.equal(policy.metadata.capabilities[9].name, "ssh");
  assert.equal(policy.capabilities[0].rules.at(-1).type, "ACTION_ACCEPT");
  assert.equal(policy.rules[0].type, "ACTION_BREAK");
});
test("compiled policy merges by ID without deleting unrelated definitions", () => {
  assert.deepEqual(
    mergeDefinitions(
      [
        { id: 2, default: 5 },
        { id: 7, default: null },
      ],
      [{ id: 7, default: 0 }],
    ),
    [
      { id: 2, default: 5 },
      { id: 7, default: 0 },
    ],
  );
});
test("duplicate definition IDs and oversized capabilities fail before save", () => {
  assert.throws(
    () => compilePolicy("tag a id 7; tag b id 7; accept;"),
    /ID 重复/,
  );
  assert.throws(
    () =>
      compilePolicy(
        "cap x id 1 " +
          Array(33).fill("accept ipprotocol tcp;").join(" ") +
          " ; break;",
      ),
    /64/,
  );
});
