<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import {
  Info,
  Waypoints,
  Route as RouteIcon,
  Globe,
  Radio,
  ShieldAlert,
  Check,
  RotateCcw,
  Shuffle,
  Plus,
  Trash2,
  Copy,
  ChevronRight,
} from "@lucide/vue";
import Modal from "./Modal.vue";
import { api, copyText, loadOverview, notify } from "../api";
import type { Network, Pool, Route } from "../types";

const props = defineProps<{ network: Network }>(),
  emit = defineEmits<{ saved: []; dirty: [value: boolean] }>();
const route = useRoute(),
  router = useRouter();
const sections = [
  { id: "details", name: "网络详情", icon: Info },
  { id: "ipv4", name: "IPv4 分配", icon: Waypoints },
  { id: "ipv6", name: "IPv6 分配", icon: Globe },
  { id: "routes", name: "托管路由", icon: RouteIcon },
  { id: "dns", name: "DNS 设置", icon: Globe },
  { id: "multicast", name: "组播与访问控制", icon: Radio },
  { id: "danger", name: "危险操作", icon: ShieldAlert },
];
const section = computed(() =>
  sections.some((s) => s.id === route.query.section)
    ? String(route.query.section)
    : "details",
);
type Form = {
  name: string;
  description: string;
  ipv4Subnet: string;
  ipv6Subnet: string;
  v4AssignMode: Network["v4AssignMode"];
  v6AssignMode: Network["v6AssignMode"];
  routes: Route[];
  ipAssignmentPools: Pool[];
  dns: Network["dns"];
  private: boolean;
  enableBroadcast: boolean;
  multicastLimit: number;
  mtu: number;
};
function cloneJSON<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T;
}
function initial(n: Network): Form {
  return cloneJSON({
    name: n.name,
    description: n.description,
    ipv4Subnet:
      n.routes.find((r) => !r.via && !r.target.includes(":"))?.target || "",
    ipv6Subnet:
      n.routes.find((r) => !r.via && r.target.includes(":"))?.target || "",
    v4AssignMode: n.v4AssignMode || { zt: false },
    v6AssignMode: n.v6AssignMode || {
      zt: false,
      rfc4193: false,
      "6plane": false,
    },
    routes: n.routes,
    ipAssignmentPools: n.ipAssignmentPools,
    dns: { domain: n.dns?.domain || "", servers: n.dns?.servers || [] },
    private: n.private,
    enableBroadcast: n.enableBroadcast,
    multicastLimit: n.multicastLimit,
    mtu: n.mtu,
  });
}
const form = ref<Form>(initial(props.network)),
  baseline = ref(JSON.stringify(form.value)),
  revision = ref(props.network.revision),
  busy = ref(false),
  error = ref("");
const dirty = computed(() => JSON.stringify(form.value) !== baseline.value);
watch(dirty, (value) => emit("dirty", value));
onBeforeUnmount(() => emit("dirty", false));
const conflict = computed(
  () => dirty.value && props.network.revision !== revision.value,
);
const prefix = ref("10"),
  second = ref(0),
  third = ref(24),
  preset = ref(""),
  rangeStart = ref(""),
  rangeEnd = ref(""),
  v6Start = ref(""),
  v6End = ref(""),
  dnsServer = ref(""),
  newTarget = ref(""),
  newVia = ref(""),
  confirmDelete = ref(false),
  deleteName = ref("");
const v4Pools = computed(() =>
    form.value.ipAssignmentPools.filter((p) => !p.ipRangeStart.includes(":")),
  ),
  v6Pools = computed(() =>
    form.value.ipAssignmentPools.filter((p) => p.ipRangeStart.includes(":")),
  );
function syncBuilder() {
  const parts = form.value.ipv4Subnet.split("/")[0]?.split(".") || [];
  if (parts.length === 4) {
    prefix.value = parts[0] || "10";
    second.value = Number(parts[1]);
    third.value = Number(parts[2]);
  }
}
syncBuilder();
function reset() {
  form.value = initial(props.network);
  baseline.value = JSON.stringify(form.value);
  revision.value = props.network.revision;
  error.value = "";
  rangeStart.value = "";
  rangeEnd.value = "";
  v6Start.value = "";
  v6End.value = "";
  syncBuilder();
}
watch(
  () => props.network,
  (n) => {
    if (!dirty.value) {
      form.value = initial(n);
      baseline.value = JSON.stringify(form.value);
      revision.value = n.revision;
      syncBuilder();
    }
  },
);
function changeSection(id: string) {
  if (id === section.value) return;
  if (dirty.value && !confirm("当前设置尚未保存，是否放弃修改？")) return;
  reset();
  void router.replace({ query: { ...route.query, section: id } });
}
onBeforeRouteLeave(
  () => !dirty.value || confirm("当前设置尚未保存，是否离开？"),
);
function localRoutes(protocol: number, subnet: string) {
  const routes = cloneJSON(form.value.routes);
  const at = routes.findIndex(
    (r) => !r.via && r.target.includes(":") === (protocol === 6),
  );
  if (!subnet) {
    if (at >= 0) routes.splice(at, 1);
  } else if (at >= 0) routes[at] = { target: subnet, via: null };
  else routes.unshift({ target: subnet, via: null });
  return routes;
}
function random(max: number, min = 0) {
  const values = new Uint32Array(1);
  crypto.getRandomValues(values);
  return min + (values[0]! % (max - min + 1));
}
function buildIPv4() {
  if (prefix.value === "172")
    second.value = Math.max(16, Math.min(31, Number(second.value)));
  if (prefix.value === "192") second.value = 168;
  second.value = Math.max(0, Math.min(255, Number(second.value)));
  third.value = Math.max(0, Math.min(255, Number(third.value)));
  const base = `${prefix.value}.${second.value}.${third.value}`;
  form.value.ipv4Subnet = base + ".0/24";
  form.value.ipAssignmentPools = [
    ...v6Pools.value,
    { ipRangeStart: base + ".1", ipRangeEnd: base + ".254" },
  ];
}
function generate() {
  second.value =
    prefix.value === "172"
      ? random(31, 16)
      : prefix.value === "192"
        ? 168
        : random(255);
  third.value = random(255);
  buildIPv4();
}
function generateIPv6() {
  const b = crypto.getRandomValues(new Uint8Array(5));
  const hex = [...b].map((n) => n.toString(16).padStart(2, "0"));
  const base = `fd${hex[0]}:${hex[1]}${hex[2]}:${hex[3]}${hex[4]}`;
  form.value.ipv6Subnet = base + "::/48";
  form.value.v6AssignMode.zt = true;
  form.value.ipAssignmentPools = [
    ...v4Pools.value,
    {
      ipRangeStart: base + "::1",
      ipRangeEnd: base + ":ffff:ffff:ffff:ffff:ffff",
    },
  ];
}
function usePreset() {
  if (!preset.value) return;
  const values = preset.value.split(".");
  prefix.value = values[0]!;
  second.value = Number(values[1]);
  third.value = Number(values[2]);
  buildIPv4();
  const isWide =
    ["10.144", "10.241", "10.242", "10.243", "10.244"].includes(
      values.slice(0, 2).join("."),
    ) || values[0] === "172";
  if (isWide) {
    const base = values.slice(0, 2).join(".");
    form.value.ipv4Subnet = base + ".0.0/16";
    form.value.ipAssignmentPools = [
      ...v6Pools.value,
      {
        ipRangeStart: base + ".0.1",
        ipRangeEnd: base + ".255.254",
      },
    ];
  }
}
function addRange(v6 = false) {
  const start = (v6 ? v6Start.value : rangeStart.value).trim(),
    end = (v6 ? v6End.value : rangeEnd.value).trim();
  if (!start || !end) return;
  if (
    form.value.ipAssignmentPools.some(
      (p) => p.ipRangeStart === start && p.ipRangeEnd === end,
    )
  ) {
    error.value = "该地址范围已存在";
    return;
  }
  form.value.ipAssignmentPools.push({ ipRangeStart: start, ipRangeEnd: end });
  if (v6) {
    v6Start.value = "";
    v6End.value = "";
  } else {
    rangeStart.value = "";
    rangeEnd.value = "";
  }
  error.value = "";
}
function removePool(pool: Pool) {
  form.value.ipAssignmentPools = form.value.ipAssignmentPools.filter(
    (p) => p !== pool,
  );
}
function addRoute() {
  if (!newTarget.value.trim()) return;
  if (form.value.routes.some((r) => r.target === newTarget.value.trim())) {
    error.value = "路由目标已存在";
    return;
  }
  form.value.routes.push({
    target: newTarget.value.trim(),
    via: newVia.value.trim() || null,
  });
  newTarget.value = "";
  newVia.value = "";
  error.value = "";
}
function addDNS() {
  if (
    dnsServer.value.trim() &&
    !form.value.dns.servers.includes(dnsServer.value.trim())
  )
    form.value.dns.servers.push(dnsServer.value.trim());
  dnsServer.value = "";
}
async function save() {
  busy.value = true;
  error.value = "";
  try {
    let patch: Record<string, unknown> = {};
    const f = form.value;
    if (section.value === "details")
      patch = { name: f.name, description: f.description };
    if (section.value === "ipv4")
      patch = {
        v4AssignMode: f.v4AssignMode,
        routes: localRoutes(4, f.ipv4Subnet.trim()),
        ipAssignmentPools: f.ipAssignmentPools,
      };
    if (section.value === "ipv6")
      patch = {
        v6AssignMode: f.v6AssignMode,
        routes: localRoutes(6, f.ipv6Subnet.trim()),
        ipAssignmentPools: f.ipAssignmentPools,
      };
    if (section.value === "routes") patch = { routes: f.routes };
    if (section.value === "dns") patch = { dns: f.dns };
    if (section.value === "multicast")
      patch = {
        private: f.private,
        enableBroadcast: f.enableBroadcast,
        multicastLimit: Number(f.multicastLimit),
        mtu: Number(f.mtu),
      };
    await api("/networks/" + props.network.nwid, "PATCH", patch, {
      "If-Match": revision.value,
    });
    baseline.value = JSON.stringify(form.value);
    emit("saved");
    notify("网络设置已保存");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function removeNetwork() {
  busy.value = true;
  try {
    await api("/networks/" + props.network.nwid, "DELETE");
    confirmDelete.value = false;
    baseline.value = JSON.stringify(form.value);
    await loadOverview();
    await router.push("/networks");
    notify("网络已删除");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="settings-layout">
    <nav class="settings-nav" aria-label="网络设置分区">
      <button
        v-for="s in sections"
        :key="s.id"
        :class="{ active: section === s.id, danger: s.id === 'danger' }"
        @click="changeSection(s.id)"
      >
        <component :is="s.icon" :size="17" /><span>{{ s.name }}</span
        ><ChevronRight :size="14" />
      </button>
    </nav>
    <section class="settings-content">
      <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
      <div v-if="conflict" class="alert warning">
        网络设置已在其他位置更新。<button class="text-button" @click="reset">
          重新加载
        </button>
      </div>
      <form @submit.prevent="save">
        <template v-if="section === 'details'"
          ><header class="settings-heading">
            <h2>网络详情</h2>
            <span class="muted">基本信息</span>
          </header>
          <label class="field"
            >网络 ID
            <div class="input-with-action">
              <input class="mono-input" :value="network.nwid" readonly /><button
                type="button"
                class="icon-button bordered"
                title="复制网络 ID"
                aria-label="复制网络 ID"
                @click="copyText(network.nwid)"
              >
                <Copy :size="17" />
              </button></div></label
          ><label class="field"
            >网络名称<input
              v-model="form.name"
              maxlength="128"
              required /></label
          ><label class="field"
            >网络描述<span class="optional-label">可选</span
            ><textarea
              v-model="form.description"
              rows="4"
              maxlength="4096"
              placeholder="为这个网络添加备注"
            ></textarea></label
        ></template>
        <template v-if="section === 'ipv4'"
          ><header class="settings-heading">
            <h2>IPv4 分配</h2>
            <span class="muted">子网与自动分配地址</span>
          </header>
          <label class="field"
            >IPv4 子网<input
              v-model="form.ipv4Subnet"
              class="mono-input"
              placeholder="10.0.24.0/24"
            /><span class="field-hint"
              >同时作为此网络的本地托管路由</span
            ></label
          >
          <div class="subnet-builder">
            <label class="field"
              >固定前缀<select v-model="prefix" @change="buildIPv4">
                <option value="10">10</option>
                <option value="172">172</option>
                <option value="192">192.168</option>
              </select></label
            ><label class="field"
              >第二段<input
                v-model.number="second"
                type="number"
                :min="prefix === '172' ? 16 : 0"
                :max="prefix === '172' ? 31 : 255"
                :disabled="prefix === '192'"
                @change="buildIPv4" /></label
            ><label class="field"
              >第三段<input
                v-model.number="third"
                type="number"
                min="0"
                max="255"
                @change="buildIPv4" /></label
            ><button type="button" class="button" @click="generate">
              <Shuffle :size="16" />生成数字
            </button>
          </div>
          <label class="field"
            >常用网段<select v-model="preset" @change="usePreset">
              <option value="">选择常用私有网段</option>
              <option
                v-for="p in [
                  '10.147.17',
                  '10.147.18',
                  '10.147.19',
                  '10.147.20',
                  '10.0.24',
                  '10.144.0',
                  '10.241.0',
                  '10.242.0',
                  '10.243.0',
                  '10.244.0',
                  '172.22.0',
                  '172.23.0',
                  '172.24.0',
                  '172.25.0',
                  '172.26.0',
                  '172.27.0',
                  '172.28.0',
                  '172.29.0',
                  '172.30.0',
                  '192.168.191',
                  '192.168.192',
                  '192.168.193',
                  '192.168.194',
                  '192.168.195',
                  '192.168.196',
                ]"
                :key="p"
                :value="p"
              >
                {{
                  p.startsWith("172.") ||
                  ["10.144", "10.241", "10.242", "10.243", "10.244"].some((v) =>
                    p.startsWith(v + "."),
                  )
                    ? p.split(".").slice(0, 2).join(".") + ".0.0/16"
                    : p + ".0/24"
                }}
              </option>
            </select></label
          >
          <div class="toggle-row section-divider">
            <div>
              <strong>自动分配 IPv4 地址</strong
              ><small>新成员从已配置的范围获取地址</small>
            </div>
            <input
              v-model="form.v4AssignMode.zt"
              class="switch"
              type="checkbox"
              aria-label="自动分配 IPv4"
            />
          </div>
          <fieldset :disabled="!form.v4AssignMode.zt">
            <div class="range-form">
              <label class="field"
                >起始 IPv4<input
                  v-model="rangeStart"
                  class="mono-input"
                  placeholder="10.0.24.1" /></label
              ><span class="range-arrow">→</span
              ><label class="field"
                >结束 IPv4<input
                  v-model="rangeEnd"
                  class="mono-input"
                  placeholder="10.0.24.254" /></label
              ><button
                type="button"
                class="icon-button bordered range-add"
                title="添加 IPv4 范围"
                aria-label="添加 IPv4 范围"
                :disabled="!rangeStart || !rangeEnd"
                @click="addRange()"
              >
                <Plus :size="18" />
              </button>
            </div>
          </fieldset>
          <h3 class="subheading">当前 IPv4 自动分配范围</h3>
          <div class="range-list">
            <div v-for="p in v4Pools" :key="p.ipRangeStart">
              <code>{{ p.ipRangeStart }}</code
              ><span class="muted">→</span><code>{{ p.ipRangeEnd }}</code
              ><button
                type="button"
                class="icon-button danger-text"
                title="删除此地址范围"
                aria-label="删除此 IPv4 范围"
                @click="removePool(p)"
              >
                <Trash2 :size="16" />
              </button>
            </div>
            <div v-if="!v4Pools.length" class="muted">尚未配置范围</div>
          </div></template
        >
        <template v-if="section === 'ipv6'"
          ><header class="settings-heading">
            <h2>IPv6 分配</h2>
            <span class="muted">自定义地址池和自动地址模式</span>
          </header>
          <label class="field"
            >IPv6 子网<input
              v-model="form.ipv6Subnet"
              class="mono-input"
              placeholder="fd00:24::/64"
          /></label>
          <button type="button" class="button" @click="generateIPv6">
            <Shuffle :size="16" />生成 IPv6 子网和范围
          </button>
          <div class="toggle-row">
            <div>
              <strong>从自定义 IPv6 范围分配</strong
              ><small>地址范围须位于本地 IPv6 子网内</small>
            </div>
            <input
              v-model="form.v6AssignMode.zt"
              class="switch"
              type="checkbox"
              aria-label="自定义 IPv6 范围"
            />
          </div>
          <fieldset :disabled="!form.v6AssignMode.zt">
            <div class="range-form">
              <label class="field"
                >起始 IPv6<input
                  v-model="v6Start"
                  class="mono-input"
                  placeholder="fd00:24::1" /></label
              ><span class="range-arrow">→</span
              ><label class="field"
                >结束 IPv6<input
                  v-model="v6End"
                  class="mono-input"
                  placeholder="fd00:24::ffff" /></label
              ><button
                type="button"
                class="icon-button bordered range-add"
                title="添加 IPv6 范围"
                aria-label="添加 IPv6 范围"
                :disabled="!v6Start || !v6End"
                @click="addRange(true)"
              >
                <Plus :size="18" />
              </button>
            </div>
          </fieldset>
          <div v-if="v6Pools.length" class="range-list">
            <div v-for="p in v6Pools" :key="p.ipRangeStart">
              <code>{{ p.ipRangeStart }}</code
              ><span>→</span><code>{{ p.ipRangeEnd }}</code
              ><button
                type="button"
                class="icon-button danger-text"
                title="删除此 IPv6 范围"
                aria-label="删除此 IPv6 范围"
                @click="removePool(p)"
              >
                <Trash2 :size="16" />
              </button>
            </div>
          </div>
          <div class="toggle-row">
            <div>
              <strong>RFC4193 本地唯一地址</strong
              ><small>每台设备自动获得稳定的 /128 地址</small>
            </div>
            <input
              v-model="form.v6AssignMode.rfc4193"
              class="switch"
              type="checkbox"
              aria-label="RFC4193 地址分配"
            />
          </div>
          <div class="toggle-row">
            <div>
              <strong>6PLANE 路由地址</strong
              ><small>按设备节点标识分配 /80 地址空间</small>
            </div>
            <input
              v-model="form.v6AssignMode['6plane']"
              class="switch"
              type="checkbox"
              aria-label="6PLANE 地址分配"
            /></div
        ></template>
        <template v-if="section === 'routes'"
          ><header class="settings-heading">
            <h2>托管路由</h2>
            <span class="muted">{{ form.routes.length }} / 128 条</span>
          </header>
          <h3 class="subheading">添加路由</h3>
          <div class="route-form">
            <label class="field"
              >目标网段（CIDR）<input
                v-model="newTarget"
                class="mono-input"
                placeholder="192.168.2.0/24" /></label
            ><label class="field"
              >网关 IP<span class="optional-label">本地子网留空</span
              ><input
                v-model="newVia"
                class="mono-input"
                placeholder="10.0.24.187" /></label
            ><button
              type="button"
              class="button"
              :disabled="!newTarget"
              @click="addRoute"
            >
              <Plus :size="17" />添加
            </button>
          </div>
          <h3 class="subheading section-divider">活动托管路由</h3>
          <div class="routes-table-wrap">
            <table class="data-table routes-table">
              <thead>
                <tr>
                  <th>目标网段</th>
                  <th>网关</th>
                  <th>范围</th>
                  <th class="right">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(r, index) in form.routes" :key="index">
                  <td>
                    <code>{{ r.target }}</code>
                  </td>
                  <td>
                    <code v-if="r.via">{{ r.via }}</code
                    ><span v-else class="muted">网络本身</span>
                  </td>
                  <td>
                    <span class="badge" :class="r.via ? 'info' : 'success'">{{
                      r.via ? "经成员转发" : "本地子网"
                    }}</span>
                  </td>
                  <td class="right">
                    <button
                      type="button"
                      class="icon-button danger-text"
                      title="删除路由"
                      :aria-label="'删除路由 ' + r.target"
                      @click="form.routes.splice(index, 1)"
                    >
                      <Trash2 :size="16" />
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
            <div v-if="!form.routes.length" class="empty-state compact-empty">
              暂无托管路由
            </div>
          </div></template
        >
        <template v-if="section === 'dns'"
          ><header class="settings-heading">
            <h2>DNS 设置</h2>
            <span class="muted">网络搜索域与 DNS 服务器</span>
          </header>
          <label class="field"
            >搜索域<input v-model="form.dns.domain" placeholder="home.arpa"
          /></label>
          <div class="inline-form">
            <label class="field"
              >DNS 服务器地址<input
                v-model="dnsServer"
                class="mono-input"
                placeholder="10.0.24.187 或 IPv6 地址"
                @keydown.enter.prevent="addDNS" /></label
            ><button
              type="button"
              class="icon-button bordered"
              title="添加 DNS 服务器"
              aria-label="添加 DNS 服务器"
              :disabled="!dnsServer.trim()"
              @click="addDNS"
            >
              <Plus :size="19" />
            </button>
          </div>
          <div class="dns-list">
            <div v-for="(server, index) in form.dns.servers" :key="server">
              <span class="metric-icon blue small-icon"
                ><Globe :size="17" /></span
              ><code>{{ server }}</code
              ><button
                type="button"
                class="icon-button danger-text"
                :title="'移除 ' + server"
                :aria-label="'移除 DNS ' + server"
                @click="form.dns.servers.splice(index, 1)"
              >
                <Trash2 :size="16" />
              </button>
            </div>
          </div>
          <div class="inline-note">
            <Info :size="17" /><span
              >客户端需要启用“允许 DNS 配置”，才能采用这些设置。</span
            >
          </div></template
        >
        <template v-if="section === 'multicast'"
          ><header class="settings-heading">
            <h2>组播与访问控制</h2>
            <span class="muted">广播、组播和网络访问权限</span>
          </header>
          <div class="toggle-row">
            <div>
              <strong>私有网络</strong
              ><small>加入的设备需要管理员授权，建议保持开启</small>
            </div>
            <input
              v-model="form.private"
              class="switch"
              type="checkbox"
              aria-label="私有网络"
            />
          </div>
          <div class="toggle-row">
            <div>
              <strong>启用广播</strong
              ><small>允许向网络内的所有设备发送广播</small>
            </div>
            <input
              v-model="form.enableBroadcast"
              class="switch"
              type="checkbox"
              aria-label="启用广播"
            />
          </div>
          <div class="form-grid section-divider">
            <label class="field"
              >组播接收设备上限<input
                v-model.number="form.multicastLimit"
                type="number"
                min="0"
                max="4096"
                required /></label
            ><label class="field"
              >MTU<input
                v-model.number="form.mtu"
                type="number"
                min="1280"
                max="10000"
                required
            /></label></div
        ></template>
        <template v-if="section === 'danger'"
          ><header class="settings-heading"><h2>危险操作</h2></header>
          <div class="danger-zone">
            <ShieldAlert :size="24" />
            <div>
              <h3>删除此网络</h3>
              <p>网络及其成员配置将被删除，设备会失去连接。</p>
            </div>
            <button
              type="button"
              class="button destructive"
              @click="
                confirmDelete = true;
                deleteName = '';
              "
            >
              删除网络
            </button>
          </div></template
        >
        <footer v-if="section !== 'danger'" class="form-footer">
          <span class="save-state">{{ dirty ? "有未保存的更改" : "" }}</span>
          <div>
            <button
              type="button"
              class="button"
              :disabled="!dirty || busy"
              @click="reset"
            >
              <RotateCcw :size="15" />重置</button
            ><button class="button primary" :disabled="!dirty || busy">
              <Check :size="16" />{{ busy ? "正在保存…" : "保存设置" }}
            </button>
          </div>
        </footer>
      </form>
    </section>
  </div>
  <Modal
    :open="confirmDelete"
    title="确认删除网络"
    @close="confirmDelete = false"
    ><p>
      输入网络名称 <strong>{{ network.name }}</strong> 以确认删除。
    </p>
    <label class="field"
      >网络名称<input v-model="deleteName" autocomplete="off"
    /></label>
    <div class="modal-actions">
      <button class="button" @click="confirmDelete = false">取消</button
      ><button
        class="button destructive"
        :disabled="deleteName !== network.name || busy"
        @click="removeNetwork"
      >
        永久删除网络
      </button>
    </div></Modal
  >
</template>
