<script setup lang="ts">
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import {
  Plus,
  Search,
  ArrowUpRight,
  Network as NetworkIcon,
  Monitor,
  ShieldAlert,
  Copy,
  ArrowDownUp,
  RefreshCw,
  ChevronRight,
  LockKeyhole,
} from "@lucide/vue";
import Modal from "../components/Modal.vue";
import { api, copyText, loadOverview, notify, overview } from "../api";
import type { Network } from "../types";

const router = useRouter();
const search = ref(""),
  sort = ref("name"),
  showCreate = ref(false),
  name = ref(""),
  busy = ref(false),
  error = ref("");
const networks = computed(() =>
  (overview.value?.networks || [])
    .filter((n) =>
      `${n.name} ${n.nwid} ${n.description}`
        .toLowerCase()
        .includes(search.value.toLowerCase()),
    )
    .sort((a, b) =>
      sort.value === "online"
        ? b.online - a.online
        : a.name.localeCompare(b.name, "zh-CN"),
    ),
);
const totals = computed(() =>
  (overview.value?.networks || []).reduce(
    (v, n) => ({
      devices: v.devices + n.total,
      online: v.online + n.online,
      pending: v.pending + n.pending,
    }),
    { devices: 0, online: 0, pending: 0 },
  ),
);
async function refresh() {
  busy.value = true;
  try {
    await api("/refresh", "POST");
    await loadOverview();
    notify("网络状态已更新");
  } catch (e) {
    notify((e as Error).message);
  } finally {
    busy.value = false;
  }
}
async function create() {
  busy.value = true;
  error.value = "";
  try {
    const n = await api<Network>("/networks", "POST", { name: name.value });
    await loadOverview();
    showCreate.value = false;
    await router.push({
      path: "/network/" + (n.nwid || n.id),
      query: { tab: "settings", section: "ipv4" },
    });
    notify("网络已创建");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <header class="page-heading">
    <div>
      <h1>网络</h1>
    </div>
    <button
      class="button primary"
      @click="
        showCreate = true;
        name = '';
        error = '';
      "
    >
      <Plus :size="18" />创建网络
    </button>
  </header>
  <section class="metrics" aria-label="网络概况">
    <div class="metric">
      <span class="metric-icon teal"><NetworkIcon :size="21" /></span>
      <div>
        <span>全部网络</span
        ><strong>{{ overview?.networks.length || 0 }}</strong>
      </div>
      <span class="metric-note"
        >私有网络
        {{ overview?.networks.filter((n) => n.private).length || 0 }}</span
      >
    </div>
    <div class="metric">
      <span class="metric-icon blue"><Monitor :size="21" /></span>
      <div>
        <span>在线设备</span
        ><strong
          >{{ totals.online }}<small>/ {{ totals.devices }}</small></strong
        >
      </div>
      <span class="metric-note"
        ><span class="status-dot online"></span>控制器已连接</span
      >
    </div>
    <div class="metric">
      <span class="metric-icon amber"><ShieldAlert :size="21" /></span>
      <div>
        <span>待授权设备</span><strong>{{ totals.pending }}</strong>
      </div>
      <span class="metric-note">{{
        totals.pending ? "等待管理员审核" : "没有待处理请求"
      }}</span>
    </div>
  </section>
  <section class="list-section" aria-label="网络列表">
    <div class="section-toolbar">
      <div class="toolbar-controls">
        <div class="search">
          <Search :size="17" /><input
            v-model="search"
            aria-label="搜索网络"
            placeholder="搜索名称或网络 ID"
          />
        </div>
        <div class="select-with-icon">
          <ArrowDownUp :size="16" /><select
            v-model="sort"
            aria-label="网络排序"
          >
            <option value="name">名称排序</option>
            <option value="online">在线设备数</option>
          </select>
        </div>
        <button
          class="icon-button bordered"
          title="刷新网络"
          aria-label="刷新网络"
          :disabled="busy"
          @click="refresh"
        >
          <RefreshCw :size="17" :class="{ spinning: busy }" />
        </button>
      </div>
    </div>
    <div class="network-table-wrap">
      <table class="data-table network-table">
        <thead>
          <tr>
            <th>网络名称</th>
            <th>网络 ID</th>
            <th>设备</th>
            <th>访问控制</th>
            <th>状态</th>
            <th class="right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="n in networks" :key="n.nwid">
            <td>
              <RouterLink :to="'/network/' + n.nwid" class="network-name"
                ><span class="network-tile"><NetworkIcon :size="20" /></span
                ><span
                  ><strong>{{ n.name }}</strong
                  ><small>{{ n.description || "—" }}</small></span
                ></RouterLink
              >
            </td>
            <td>
              <span class="inline-copy"
                ><code>{{ n.nwid }}</code
                ><button
                  class="icon-button tiny"
                  :aria-label="'复制 ' + n.name + ' 网络 ID'"
                  title="复制网络 ID"
                  @click="copyText(n.nwid)"
                >
                  <Copy :size="14" /></button
              ></span>
            </td>
            <td>
              <span class="device-count"
                ><span
                  class="status-dot"
                  :class="{ online: n.online > 0 }"
                ></span
                ><strong>{{ n.online }}</strong
                ><span class="muted">/ {{ n.total }} 在线</span></span
              ><small v-if="n.pending" class="pending-note"
                >{{ n.pending }} 台待授权</small
              >
            </td>
            <td>
              <span class="access-label"
                ><LockKeyhole :size="14" />{{
                  n.private ? "私有网络" : "公开网络"
                }}</span
              >
            </td>
            <td>
              <span class="badge success"
                ><span class="status-dot online"></span>运行中</span
              >
            </td>
            <td class="right">
              <RouterLink
                class="icon-button"
                :to="'/network/' + n.nwid"
                :aria-label="'打开 ' + n.name"
                title="打开网络"
                ><ArrowUpRight :size="19"
              /></RouterLink>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="network-mobile-list">
      <RouterLink
        v-for="n in networks"
        :key="n.nwid"
        class="mobile-network"
        :to="'/network/' + n.nwid"
        ><span class="network-tile"><NetworkIcon :size="20" /></span>
        <div>
          <strong>{{ n.name }}</strong
          ><code>{{ n.nwid }}</code
          ><span
            ><span class="status-dot online"></span>{{ n.online }} /
            {{ n.total }} 在线<span v-if="n.pending" class="pending-note"
              >{{ n.pending }} 待授权</span
            ></span
          >
        </div>
        <ChevronRight :size="18"
      /></RouterLink>
    </div>
    <div v-if="!networks.length" class="empty-state">
      <NetworkIcon :size="30" />
      <h3>{{ search ? "没有匹配的网络" : "还没有网络" }}</h3>
      <button v-if="search" class="button" @click="search = ''">清除搜索</button
      ><button v-else class="button primary" @click="showCreate = true">
        创建网络
      </button>
    </div>
    <div class="table-footer">
      <span>{{ networks.length }} 个网络</span><span>全部显示</span>
    </div>
  </section>
  <div class="controller-footer">
    <span class="status-dot online"></span
    ><span
      >此网络控制器的零层地址为
      <code>{{ overview?.controller.address || "—" }}</code></span
    ><span
      >ZeroTier 版本
      <code>{{ overview?.controller.version || "—" }}</code></span
    >
  </div>
  <Modal :open="showCreate" title="创建网络" @close="showCreate = false"
    ><form @submit.prevent="create">
      <div v-if="error" class="alert danger">{{ error }}</div>
      <label class="field"
        >网络名称<input
          v-model="name"
          placeholder="例如：家庭网络"
          maxlength="128"
          required
      /></label>
      <div class="modal-actions">
        <button type="button" class="button" @click="showCreate = false">
          取消</button
        ><button class="button primary" :disabled="busy || !name.trim()">
          <Plus :size="16" />{{ busy ? "正在创建…" : "创建网络" }}
        </button>
      </div>
    </form></Modal
  >
</template>
