import test from "node:test";
import assert from "node:assert/strict";
import { resolveDeviceIcon, deviceIconOptions } from "../src/device-icons.ts";

test("OpenWrt variants use routers without changing user tags", () => {
  for (const name of ["My-N5105-OpenWrt", "ImmortalWrt", "iStoreOS"]) assert.equal(resolveDeviceIcon("auto", name), "openwrt");
  assert.equal(resolveDeviceIcon(undefined, "router", ["网关"]), "openwrt");
  assert.equal(resolveDeviceIcon("windows", "My-OpenWrt"), "windows");
});
test("platform choices and safe fallback remain deterministic", () => {
  assert.equal(deviceIconOptions.some(option => option.value === "github"), false);
  assert.equal(resolveDeviceIcon("github", ""), "computer");
  for (const option of deviceIconOptions.filter(x => x.value !== "auto")) assert.equal(resolveDeviceIcon(option.value, ""), option.value);
  assert.equal(resolveDeviceIcon(undefined, "MacBook Pro"), "apple");
  assert.equal(resolveDeviceIcon("invalid", ""), "computer");
  assert.equal(resolveDeviceIcon(undefined, "家庭服务器 Debian"), "linux");
});
