<script setup lang="ts">
import {
  computed,
  defineAsyncComponent,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { onBeforeRouteUpdate, useRoute, useRouter } from "vue-router";
import {
  ArrowLeft,
  Copy,
  QrCode,
  Plus,
  Search,
  RefreshCw,
  Pin,
  PinOff,
  ChevronRight,
  ArrowUp,
  ArrowDown,
  ArrowDownUp,
  ShieldCheck,
  ShieldX,
  X,
  MoreHorizontal,
  Monitor,
  Check,
  Download,
  AlertCircle,
  ScanLine,
} from "@lucide/vue";
import Modal from "../components/Modal.vue";
import NetworkSettings from "../components/NetworkSettings.vue";
import AuthorizationBadge from "../components/AuthorizationBadge.vue";
import DeviceIcon from "../components/DeviceIcon.vue";
import { deviceIconOptions } from "../device-icons";
import PresenceStatus from "../components/PresenceStatus.vue";
import ConnectionStatus from "../components/ConnectionStatus.vue";
import { probeDescription } from "../presence";
const FlowRules = defineAsyncComponent(
  () => import("../components/FlowRules.vue"),
);
import { api, ago, copyText, dateText, loadOverview, notify } from "../api";
import type { Member, Network } from "../types";

const route = useRoute(),
  router = useRouter(),
  id = String(route.params.id);
const network = ref<Network | null>(null),
  error = ref(""),
  search = ref(""),
  filter = ref("all"),
  sort = ref("default"),
  busy = ref(false);
const probing = ref(false);
const selected = ref<string[]>([]),
  editor = ref<Member | null>(null),
  editorForm = ref({
    icon: "auto",
    name: "",
    ips: "",
    tags: "",
    authorized: false,
    activeBridge: false,
    noAutoAssignIps: false,
  }),
  editorError = ref("");
const qr = ref(""),
  qrOpen = ref(false),
  addOpen = ref(false),
  addForm = ref({ id: "", name: "", authorized: true }),
  addError = ref(""),
  deleteID = ref(""),
  bulkErrors = ref<string[]>([]);
const activeTab = computed(() => String(route.query.tab || "members"));
const editorStatus = computed(() =>
  network.value?.members.find((member) => member.id === editor.value?.id) || editor.value,
);
const unsaved = ref(false);
onBeforeRouteUpdate(
  (to) =>
    String(to.query.tab || "members") === activeTab.value ||
    !unsaved.value ||
    confirm("当前设置尚未保存，是否放弃修改？"),
);
function beforeUnload(event: BeforeUnloadEvent) {
  if (unsaved.value) {
    event.preventDefault();
    event.returnValue = "";
  }
}
window.addEventListener("beforeunload", beforeUnload);
const joinURL = computed(() => `https://joinzt.com/addnetwork?nwid=${id}&v=1`);
let version = 0;
async function load() {
  const current = ++version;
  try {
    const next = await api<Network>("/networks/" + id);
    if (current === version) {
      network.value = next;
      error.value = "";
      selected.value = selected.value.filter((id) =>
        next.members.some((m) => m.id === id),
      );
    }
  } catch (e) {
    if (current === version) error.value = (e as Error).message;
  }
}
async function probeMembers(member?: Member) {
  probing.value = true;
  try {
    const result = await api<{ scheduled: number; eligible: number; demo: boolean }>(
      `/networks/${id}${member ? `/members/${member.id}` : ""}/probe`,
      "POST",
    );
    notify(result.demo ? "预览模式不发送真实探测" : !result.eligible
      ? "仅检测已授权设备的托管私有 IP"
      : result.scheduled ? `已安排 ${result.scheduled} 台设备检测`
        : "检测已在进行或处于冷却时间内");
    await api("/refresh", "POST");
    await load();
  } catch (e) {
    notify((e as Error).message);
  } finally {
    probing.value = false;
  }
}
function liveUpdate() {
  if (!document.hidden) void load();
}
onMounted(() => {
  void load();
  window.addEventListener("ztncui:update", liveUpdate);
});
onBeforeUnmount(() => {
  window.removeEventListener("ztncui:update", liveUpdate);
  window.removeEventListener("beforeunload", beforeUnload);
});
function tab(value: string) {
  void router.replace({ query: { ...route.query, tab: value } });
}
function ipNumber(m: Member) {
  const ip = m.ipAssignments.find((ip) => !ip.includes(":"));
  return ip
    ? ip.split(".").reduce((n, x) => n * 256 + Number(x), 0)
    : Number.MAX_SAFE_INTEGER;
}
const members = computed(() => {
  const q = search.value.toLowerCase();
  return (network.value?.members || [])
    .filter((m) =>
      `${m.name} ${m.id} ${m.ipAssignments.join(" ")} ${m.tags.join(" ")}`
        .toLowerCase()
        .includes(q),
    )
    .filter(
      (m) =>
        filter.value === "all" ||
        (filter.value === "online" && m.online) ||
        (filter.value === "offline" && !m.online) ||
        (filter.value === "pending" && !m.authorized) ||
        (filter.value === "pinned" && m.pinned),
    )
    .sort((a, b) => {
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
      if (sort.value !== "default") {
        const av = ipNumber(a),
          bv = ipNumber(b);
        if (av === Number.MAX_SAFE_INTEGER || bv === Number.MAX_SAFE_INTEGER)
          return av === bv ? 0 : av === Number.MAX_SAFE_INTEGER ? 1 : -1;
        return (av - bv) * (sort.value === "asc" ? 1 : -1);
      }
      return a.originalIndex - b.originalIndex;
    });
});
const allSelected = computed(
  () =>
    members.value.length > 0 &&
    members.value.every((m) => selected.value.includes(m.id)),
);
function selectAll() {
  if (allSelected.value)
    selected.value = selected.value.filter(
      (id) => !members.value.some((m) => m.id === id),
    );
  else
    selected.value = [
      ...new Set([...selected.value, ...members.value.map((m) => m.id)]),
    ];
}
async function patchMember(mid: string, p: Record<string, unknown>) {
  busy.value = true;
  try {
    await api("/networks/" + id + "/members/" + mid, "PATCH", p);
    await Promise.all([load(), loadOverview()]);
    return true;
  } catch (e) {
    notify((e as Error).message);
    return false;
  } finally {
    busy.value = false;
  }
}
function openEditor(m: Member) {
  editor.value = m;
  editorError.value = "";
  editorForm.value = {
    icon: deviceIconOptions.some(option => option.value === m.icon) ? m.icon! : "auto",
    name: m.name,
    ips: m.ipAssignments.join("\n"),
    tags: m.tags.join(", "),
    authorized: m.authorized,
    activeBridge: m.activeBridge,
    noAutoAssignIps: m.noAutoAssignIps,
  };
}
async function saveEditor() {
  if (!editor.value) return;
  busy.value = true;
  editorError.value = "";
  try {
    const f = editorForm.value;
    await api("/networks/" + id + "/members/" + editor.value.id, "PATCH", {
      name: f.name,
      icon: f.icon,
      ipAssignments: f.ips
        .split(/[\n,]+/)
        .map((s) => s.trim())
        .filter(Boolean),
      tags: f.tags
        .split(/[,，]+/)
        .map((s) => s.trim())
        .filter(Boolean),
      authorized: f.authorized,
      activeBridge: f.activeBridge,
      noAutoAssignIps: f.noAutoAssignIps,
    });
    editor.value = null;
    await Promise.all([load(), loadOverview()]);
    notify("设备设置已保存");
  } catch (e) {
    editorError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function bulk(p: Record<string, unknown>) {
  busy.value = true;
  bulkErrors.value = [];
  try {
    const result = await api<{
      results: { id: string; ok: boolean; error?: string }[];
    }>("/networks/" + id + "/batch", "POST", { ids: selected.value, patch: p });
    bulkErrors.value = result.results
      .filter((r) => !r.ok)
      .map((r) => `${r.id}: ${r.error}`);
    selected.value = result.results.filter((r) => !r.ok).map((r) => r.id);
    await Promise.all([load(), loadOverview()]);
    notify(`已更新 ${result.results.filter((r) => r.ok).length} 台设备`);
  } catch (e) {
    notify((e as Error).message);
  } finally {
    busy.value = false;
  }
}
async function showQR() {
  qrOpen.value = true;
  try {
    const QRCode = (await import("qrcode")).default;
    qr.value = await QRCode.toDataURL(joinURL.value, {
      width: 280,
      margin: 3,
      errorCorrectionLevel: "M",
    });
  } catch {
    notify("二维码生成失败");
  }
}
async function add() {
  busy.value = true;
  addError.value = "";
  try {
    await api("/networks/" + id + "/members", "POST", addForm.value);
    addOpen.value = false;
    await Promise.all([load(), loadOverview()]);
    notify("设备已添加");
  } catch (e) {
    addError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function remove() {
  busy.value = true;
  try {
    await api("/networks/" + id + "/members/" + deleteID.value, "DELETE");
    deleteID.value = "";
    await Promise.all([load(), loadOverview()]);
    notify("设备已取消授权并删除");
  } catch (e) {
    notify((e as Error).message);
  } finally {
    busy.value = false;
  }
}
async function refresh() {
  busy.value = true;
  try {
    await api("/refresh", "POST");
    await load();
    notify("设备状态已更新");
  } catch (e) {
    notify((e as Error).message);
  } finally {
    busy.value = false;
  }
}
function exportCSV() {
  const rows = [
    ["名称", "设备 ID", "托管 IP", "授权", "在线"],
    ...members.value.map((m) => [
      m.name,
      m.id,
      m.ipAssignments.join(" "),
      m.authorized ? "是" : "否",
      m.online ? "是" : "否",
    ]),
  ];
  const csv =
    "\ufeff" +
    rows
      .map((r) =>
        r
          .map(
            (s) =>
              '"' +
              (/^[=+@\-\t\r]/.test(s) ? "'" : "") +
              s.replaceAll('"', '""') +
              '"',
          )
          .join(","),
      )
      .join("\r\n");
  const a = document.createElement("a");
  a.href = URL.createObjectURL(
    new Blob([csv], { type: "text/csv;charset=utf-8" }),
  );
  a.download = `${id}-members.csv`;
  a.click();
  URL.revokeObjectURL(a.href);
}
</script>

<template>
  <div v-if="error" class="alert danger" role="alert">
    {{ error }}<button class="button small" @click="load">重试</button>
  </div>
  <div v-if="!network && !error" class="skeleton-page">
    <div></div>
    <div></div>
    <div></div>
  </div>
  <template v-if="network">
    <header class="page-heading network-heading">
      <div>
        <RouterLink to="/networks" class="back-link"
          ><ArrowLeft :size="15" />全部网络</RouterLink
        >
        <h1>
          {{ network.name
          }}<span class="badge neutral network-private">{{
            network.private ? "私有网络" : "公开网络"
          }}</span>
        </h1>
        <div class="network-id-line">
          <span>NETWORK ID</span><code>{{ id }}</code
          ><button
            class="icon-button tiny"
            title="复制网络 ID"
            aria-label="复制网络 ID"
            @click="copyText(id)"
          >
            <Copy :size="15" /></button
          ><button
            class="icon-button tiny"
            title="加入网络二维码"
            aria-label="加入网络二维码"
            @click="showQR"
          >
            <QrCode :size="17" />
          </button>
        </div>
      </div>
      <div class="heading-actions">
        <button class="button" @click="showQR">
          <QrCode :size="17" /><span>分享网络</span></button
        ><button
          class="button primary"
          @click="
            addOpen = true;
            addError = '';
            addForm = { id: '', name: '', authorized: true };
          "
        >
          <Plus :size="17" />添加设备
        </button>
      </div>
    </header>
    <div class="main-tabs" role="tablist" aria-label="网络页面">
      <button
        :class="{ active: activeTab === 'members' }"
        role="tab"
        :aria-selected="activeTab === 'members'"
        @click="tab('members')"
      >
        成员设备</button
      ><button
        :class="{ active: activeTab === 'settings' }"
        role="tab"
        :aria-selected="activeTab === 'settings'"
        @click="tab('settings')"
      >
        网络设置</button
      ><button
        :class="{ active: activeTab === 'rules' }"
        role="tab"
        :aria-selected="activeTab === 'rules'"
        @click="tab('rules')"
      >
        流规则
      </button>
    </div>
    <template v-if="activeTab === 'members'">
      <section class="metrics compact" aria-label="设备概况">
        <div class="metric">
          <span class="metric-icon blue"><Monitor :size="20" /></span>
          <div>
            <span>全部设备</span><strong>{{ network.total }}</strong>
          </div>
        </div>
        <div class="metric">
          <span class="metric-icon teal"><ShieldCheck :size="20" /></span>
          <div>
            <span>当前在线</span
            ><strong
              >{{ network.online
              }}<small>已授权 {{ network.authorized }}</small></strong
            >
          </div>
        </div>
        <div class="metric">
          <span class="metric-icon amber"><AlertCircle :size="20" /></span>
          <div>
            <span>待授权</span><strong>{{ network.pending }}</strong>
          </div>
          <button
            v-if="network.pending"
            class="text-button"
            @click="filter = 'pending'"
          >
            去处理<ChevronRight :size="15" />
          </button>
        </div>
      </section>
      <section class="list-section member-section" aria-label="成员列表">
        <div class="section-toolbar">
          <div class="toolbar-controls">
            <div class="search">
              <Search :size="17" /><input
                v-model="search"
                aria-label="搜索设备"
                placeholder="搜索设备、IP 或标签"
              />
            </div>
            <select v-model="filter" aria-label="设备筛选">
              <option value="all">全部设备</option>
              <option value="online">在线设备</option>
              <option value="offline">未在线设备</option>
              <option value="pending">待授权</option>
              <option value="pinned">已置顶</option></select
            ><button
              class="icon-button bordered"
              title="导出当前设备 CSV"
              aria-label="导出设备"
              @click="exportCSV"
            >
              <Download :size="17" /></button
            ><button
              class="icon-button bordered"
              title="检测设备可达性"
              aria-label="检测设备可达性"
              :disabled="probing"
              @click="probeMembers()"
            >
              <ScanLine :size="17" :class="{ spinning: probing }" /></button
            ><button
              class="icon-button bordered"
              title="刷新设备"
              aria-label="刷新设备"
              :disabled="busy"
              @click="refresh"
            >
              <RefreshCw :size="17" :class="{ spinning: busy }" />
            </button>
          </div>
        </div>
        <div v-if="selected.length" class="bulk-toolbar">
          <span
            >已选择 <strong>{{ selected.length }}</strong> 台</span
          ><button
            class="button small"
            :disabled="busy"
            @click="bulk({ authorized: true })"
          >
            <ShieldCheck :size="15" />授权</button
          ><button
            class="button small"
            :disabled="busy"
            @click="bulk({ authorized: false })"
          >
            <ShieldX :size="15" />取消授权</button
          ><button
            class="button small"
            :disabled="busy"
            @click="bulk({ pinned: true })"
          >
            <Pin :size="15" />置顶</button
          ><button
            class="icon-button"
            title="取消选择"
            aria-label="取消选择"
            @click="selected = []"
          >
            <X :size="16" />
          </button>
        </div>
        <div v-if="bulkErrors.length" class="alert danger" role="alert">
          <div v-for="e in bulkErrors" :key="e">{{ e }}</div>
        </div>
        <div class="member-table-wrap">
          <table class="data-table member-table">
            <thead>
              <tr>
                <th class="checkbox-col">
                  <input
                    type="checkbox"
                    :checked="allSelected"
                    aria-label="选择当前筛选的全部设备"
                    @change="selectAll"
                  />
                </th>
                <th>设备名称 / ID</th>
                <th>在线状态</th>
                <th>授权状态</th>
                <th class="member-ip-cell">
                  <div class="ip-sort">
                    托管 IP
                    <div class="sort-buttons">
                      <button
                        v-for="s in [
                          { value: 'asc', label: 'IP 升序', icon: ArrowUp },
                          { value: 'desc', label: 'IP 降序', icon: ArrowDown },
                          {
                            value: 'default',
                            label: '恢复默认排序',
                            icon: ArrowDownUp,
                          },
                        ]"
                        :key="s.value"
                        :class="{ active: sort === s.value }"
                        :title="s.label"
                        :aria-label="s.label"
                        @click="sort = s.value"
                      >
                        <component :is="s.icon" :size="13" />
                      </button>
                    </div>
                  </div>
                </th>
                <th class="optional-col">连接</th>
                <th class="optional-col">最近活动</th>
                <th class="right">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="m in members"
                :key="m.id"
                :class="{
                  'pinned-row': m.pinned,
                  'selected-row': selected.includes(m.id),
                }"
              >
                <td>
                  <input
                    v-model="selected"
                    type="checkbox"
                    :value="m.id"
                    :aria-label="'选择 ' + (m.name || m.id)"
                  />
                </td>
                <td>
                  <div class="member-identity">
                    <button type="button" class="device-icon-button" :aria-label="'更改 ' + (m.name || m.id) + ' 的图标'" title="更改设备图标" @click="openEditor(m)"><DeviceIcon :icon="m.icon" :name="m.name" :tags="m.tags" /></button>
                    <div class="member-label">
                      <button class="name-link" @click="openEditor(m)">
                        {{ m.name || "未命名设备"
                        }}<Pin
                          v-if="m.pinned"
                          :size="12"
                          class="pin-indicator"
                        />
                      </button>
                      <div class="member-subline">
                        <button
                          class="id-copy"
                          :title="'复制 ' + m.id"
                          @click="copyText(m.id)"
                        >
                          <code>{{ m.id }}</code></button
                        ><span
                          v-for="tag in m.tags"
                          :key="tag"
                          class="mini-tag"
                          >{{ tag }}</span
                        >
                      </div>
                    </div>
                  </div>
                </td>
                <td class="member-online-cell">
                  <div class="status-stack">
                    <PresenceStatus :member="m" />
                    <small>{{ m.version ? "v" + m.version : "—" }}<span v-if="m.presenceSource === 'probe'"> · IP 可达</span></small>
                  </div>
                </td>
                <td class="member-authorization-cell">
                  <AuthorizationBadge :authorized="m.authorized" />
                </td>
                <td class="member-ip-cell">
                  <div class="ip-list">
                    <button
                      v-for="ip in m.ipAssignments"
                      :key="ip"
                      class="ip-copy"
                      :title="'复制 ' + ip"
                      @click="copyText(ip)"
                    >
                      <code>{{ ip }}</code></button
                    ><span v-if="!m.ipAssignments.length" class="muted"
                      >未分配</span
                    >
                  </div>
                  <div
                    v-if="m.connection !== 'unknown' || (m.online && m.presenceSource === 'probe')"
                    class="compact-connection"
                  >
                    <ConnectionStatus :member="m" />
                  </div>
                </td>
                <td class="optional-col">
                  <ConnectionStatus :member="m" />
                </td>
                <td class="optional-col">
                  <span class="muted small-text">{{
                    ago(m.lastSeen)
                  }}</span>
                </td>
                <td class="right">
                  <div class="row-actions">
                    <button
                      class="icon-button pin-action"
                      :class="{ pinned: m.pinned }"
                      :title="m.pinned ? '取消置顶' : '置顶设备'"
                      :aria-label="
                        m.pinned ? '取消置顶 ' + m.name : '置顶 ' + m.name
                      "
                      :disabled="busy"
                      @click="patchMember(m.id, { pinned: !m.pinned })"
                    >
                      <component
                        :is="m.pinned ? PinOff : Pin"
                        :size="16"
                      /></button
                    ><button
                      class="icon-button"
                      :title="'编辑 ' + m.name"
                      :aria-label="'编辑 ' + m.name"
                      @click="openEditor(m)"
                    >
                      <MoreHorizontal :size="19" />
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="!members.length" class="empty-state">
          <Monitor :size="29" />
          <h3>
            {{
              search || filter !== "all"
                ? "没有匹配的设备"
                : "暂无设备加入此网络"
            }}
          </h3>
          <button
            v-if="search || filter !== 'all'"
            class="button"
            @click="
              search = '';
              filter = 'all';
            "
          >
            重置筛选</button
          ><button v-else class="button" @click="showQR">
            <QrCode :size="16" />分享网络
          </button>
        </div>
        <div class="table-footer">
          <span>显示 {{ members.length }} / {{ network.total }} 台设备</span
          ><span
            >{{ network.members.filter((m) => m.pinned).length }} 台置顶</span
          >
        </div>
      </section>
    </template>
    <NetworkSettings
      v-else-if="activeTab === 'settings'"
      :network="network"
      @saved="
        load();
        loadOverview();
      "
      @dirty="unsaved = $event"
    />
    <FlowRules
      v-else
      :network="network"
      @saved="load"
      @dirty="unsaved = $event"
    />
    <Modal :open="qrOpen" centered title="加入此网络" @close="qrOpen = false"
      ><div class="qr-content">
        <p class="muted">使用手机相机扫描二维码即可连接</p>
        <img
          v-if="qr"
          :src="qr"
          width="280"
          height="280"
          alt="加入此 ZeroTier 网络的二维码"
        />
        <div class="network-pill">
          <span>{{ network.name }}</span
          ><code>{{ id }}</code>
        </div>
        <button
          class="button qr-copy"
          title="复制加入链接"
          aria-label="复制加入链接"
          @click="copyText(joinURL)"
        >
          复制加入链接 <Copy :size="16" />
        </button>
      </div>
    </Modal>
    <Modal
      :open="!!editor"
      :title="editor?.name || '设备详情'"
      wide
      @close="editor = null"
      ><form v-if="editor" @submit.prevent="saveEditor">
        <div class="device-detail-head">
          <code>{{ editor.id }}</code
          ><PresenceStatus v-if="editorStatus" :member="editorStatus" />
          <AuthorizationBadge :authorized="editorStatus?.authorized || false" />
          <span class="muted">{{
            editor.version ? "ZeroTier " + editor.version : ""
          }}</span>
        </div>
        <div v-if="editorStatus" class="probe-details">
          <div>
            <strong>可达性检测</strong>
            <p>{{ probeDescription(editorStatus) }}</p>
            <small v-if="editorStatus.probe?.checkedAt">服务器探测 · {{ dateText(new Date(editorStatus.probe.checkedAt).toISOString()) }}</small>
          </div>
          <button
            type="button"
            class="button small"
            :disabled="probing || !editorStatus.authorized || !editorStatus.ipAssignments.length"
            @click="probeMembers(editorStatus)"
          ><ScanLine :size="16" />检测</button>
        </div>
        <div v-if="editorError" class="alert danger">{{ editorError }}</div>
        <label class="field">设备图标
          <div class="device-icon-picker">
            <DeviceIcon :icon="editorForm.icon" :name="editorForm.name" :tags="editor.tags" />
            <select v-model="editorForm.icon" aria-label="设备图标"><option v-for="option in deviceIconOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select>
          </div>
        </label>
        <label class="field"
          >设备名称<input v-model="editorForm.name" maxlength="256"
        /></label>
        <div class="form-grid">
          <label class="field"
            >托管 IP<textarea
              v-model="editorForm.ips"
              rows="3"
              placeholder="每行一个 IPv4 或 IPv6 地址"
              class="mono-input"
            ></textarea></label
          ><label class="field"
            >文字标签<input
              v-model="editorForm.tags"
              placeholder="家庭, 网关"
            /><span class="field-hint">逗号分隔</span></label
          >
        </div>
        <div class="toggle-row">
          <div><strong>设备授权</strong><small>允许此设备加入网络</small></div>
          <input
            v-model="editorForm.authorized"
            class="switch"
            type="checkbox"
            aria-label="设备授权"
          />
        </div>
        <div class="toggle-row">
          <div>
            <strong>主动桥接</strong
            ><small>允许二层桥接；访问其他子网仍需配置托管路由</small>
          </div>
          <input
            v-model="editorForm.activeBridge"
            class="switch"
            type="checkbox"
            aria-label="主动桥接"
          />
        </div>
        <div class="toggle-row">
          <div>
            <strong>关闭自动 IP 分配</strong
            ><small>保留手动配置的托管地址</small>
          </div>
          <input
            v-model="editorForm.noAutoAssignIps"
            class="switch"
            type="checkbox"
            aria-label="关闭自动 IP 分配"
          />
        </div>
        <div class="modal-actions split">
          <button
            type="button"
            class="button danger-text"
            @click="
              deleteID = editor.id;
              editor = null;
            "
          >
            删除设备
          </button>
          <div>
            <button type="button" class="button" @click="editor = null">
              取消</button
            ><button class="button primary" :disabled="busy">
              <Check :size="16" />保存
            </button>
          </div>
        </div>
      </form></Modal
    >
    <Modal :open="addOpen" title="添加成员设备" @close="addOpen = false"
      ><form @submit.prevent="add">
        <div v-if="addError" class="alert danger">{{ addError }}</div>
        <label class="field"
          >设备 ID<input
            v-model="addForm.id"
            class="mono-input"
            placeholder="10 位 ZeroTier 节点 ID"
            pattern="[a-f0-9]{10}"
            required /></label
        ><label class="field"
          >设备名称<input v-model="addForm.name" maxlength="256" /></label
        ><label class="check-label"
          ><input
            v-model="addForm.authorized"
            type="checkbox"
          />授权此设备</label
        >
        <div class="modal-actions">
          <button type="button" class="button" @click="addOpen = false">
            取消</button
          ><button class="button primary" :disabled="busy">
            <Plus :size="16" />添加设备
          </button>
        </div>
      </form></Modal
    >
    <Modal :open="!!deleteID" title="删除设备" @close="deleteID = ''"
      ><p>
        设备 <code>{{ deleteID }}</code> 将被取消授权并从成员列表删除。
      </p>
      <div class="modal-actions">
        <button class="button" @click="deleteID = ''">取消</button
        ><button class="button destructive" :disabled="busy" @click="remove">
          确认删除
        </button>
      </div></Modal
    >
  </template>
</template>
