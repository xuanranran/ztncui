<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { onBeforeRouteLeave } from "vue-router";
import {
  ShieldCheck,
  KeyRound,
  Check,
  Download,
  Activity,
  RefreshCw,
  Server,
  Search,
  Copy,
  ScanLine,
  RotateCcw,
} from "@lucide/vue";
import {
  api,
  auth,
  copyText,
  dateText,
  loadOverview,
  notify,
  overview,
} from "../api";
import type { Audit, ProbeSettings } from "../types";
const section = ref("general"),
  logs = ref<Audit[]>([]),
  busy = ref(false),
  error = ref(""),
  current = ref(""),
  password = ref(""),
  confirmation = ref("");
const logSearch = ref(""),
  refreshing = ref(false),
  loadingLogs = ref(false);
const probeSettings = ref<ProbeSettings | null>(null),
  probeForm = ref({ intervalSeconds: 60, timeoutMs: 1500 }),
  probeLoading = ref(false),
  probeSaving = ref(false),
  probeError = ref("");
const probeDirty = computed(() => !!probeSettings.value && (
  probeForm.value.intervalSeconds !== probeSettings.value.intervalSeconds ||
  probeForm.value.timeoutMs !== probeSettings.value.timeoutMs
));
const probeValid = computed(() =>
  Number.isInteger(probeForm.value.intervalSeconds) && probeForm.value.intervalSeconds >= 10 && probeForm.value.intervalSeconds <= 3600 &&
  Number.isInteger(probeForm.value.timeoutMs) && probeForm.value.timeoutMs >= 200 && probeForm.value.timeoutMs <= 10000,
);
function setProbeSettings(value: ProbeSettings) {
  probeSettings.value = value;
  probeForm.value = { intervalSeconds: value.intervalSeconds, timeoutMs: value.timeoutMs };
}
async function loadProbeSettings() {
  if (probeDirty.value && !confirm("放弃未保存的 Ping 检测设置并重新加载？")) return;
  probeLoading.value = true;
  probeError.value = "";
  try {
    setProbeSettings(await api<ProbeSettings>("/settings/probes"));
  } catch (e) {
    probeError.value = (e as Error).message;
  } finally {
    probeLoading.value = false;
  }
}
async function saveProbeSettings() {
  if (!probeSettings.value || !probeValid.value || !probeDirty.value) return;
  probeSaving.value = true;
  probeError.value = "";
  try {
    setProbeSettings(await api<ProbeSettings>("/settings/probes", "PUT", probeForm.value, { "If-Match": probeSettings.value.revision }));
    notify("Ping 检测设置已保存，后续检测使用新参数");
  } catch (e) {
    probeError.value = (e as Error).message;
  } finally {
    probeSaving.value = false;
  }
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (probeDirty.value) {
    event.preventDefault();
    event.returnValue = "";
  }
}
onBeforeRouteLeave(() => !probeDirty.value || confirm("Ping 检测设置尚未保存，是否放弃修改？"));
const filteredLogs = computed(() =>
  logs.value.filter((log) =>
    `${log.action} ${log.target} ${dateText(log.at)}`
      .toLowerCase()
      .includes(logSearch.value.toLowerCase().trim()),
  ),
);
function refreshActiveLogs() {
  if (section.value === "logs") loadLogs();
}
onMounted(() => {
  void loadProbeSettings();
  window.addEventListener("beforeunload", beforeUnload);
  window.addEventListener("ztncui:update", refreshActiveLogs);
});
onBeforeUnmount(() => {
  window.removeEventListener("beforeunload", beforeUnload);
  window.removeEventListener("ztncui:update", refreshActiveLogs);
});
async function refreshController() {
  refreshing.value = true;
  try {
    await api("/refresh", "POST");
    await loadOverview();
    notify("控制器状态已刷新");
  } catch (e) {
    notify((e as Error).message);
  } finally {
    refreshing.value = false;
  }
}
async function loadLogs() {
  if (loadingLogs.value) return;
  loadingLogs.value = true;
  try {
    logs.value = await api<Audit[]>("/audit");
  } catch (e) {
    notify((e as Error).message);
  } finally {
    loadingLogs.value = false;
  }
}
async function changePassword() {
  error.value = "";
  if (password.value !== confirmation.value) {
    error.value = "两次输入的密码不一致";
    return;
  }
  busy.value = true;
  try {
    const result = await api<{ csrf: string }>("/password", "POST", {
      current: current.value,
      password: password.value,
    });
    auth.csrf = result.csrf;
    current.value = "";
    password.value = "";
    confirmation.value = "";
    notify("密码已更新，其他会话已退出");
    await loadLogs();
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
      <h1>系统设置</h1>
    </div>
    <span class="badge success"><ShieldCheck :size="14" />单管理员</span>
  </header>
  <div class="main-tabs" role="tablist" aria-label="系统设置">
    <button
      role="tab"
      :class="{ active: section === 'general' }"
      :aria-selected="section === 'general'"
      @click="section = 'general'"
    >
      控制器</button
    ><button
      role="tab"
      :class="{ active: section === 'account' }"
      :aria-selected="section === 'account'"
      @click="section = 'account'"
    >
      账号与安全</button
    ><button
      role="tab"
      :class="{ active: section === 'logs' }"
      :aria-selected="section === 'logs'"
      @click="
        section = 'logs';
        loadLogs();
      "
    >
      操作记录
    </button>
  </div>
  <div v-if="section === 'general'" class="system-sections">
    <section class="system-section">
      <header class="settings-heading">
        <h2><Server :size="20" />控制器信息</h2>
        <div class="settings-header-actions">
          <button
            class="icon-button"
            :disabled="refreshing"
            title="刷新控制器状态"
            aria-label="刷新控制器状态"
            @click="refreshController"
          >
            <RefreshCw :size="18" :class="{ spin: refreshing }" />
          </button>
          <span
            class="badge"
            :class="overview?.controller.online ? 'success' : 'neutral'"
            >{{ overview?.controller.online ? "在线" : "离线" }}</span
          >
        </div>
      </header>
      <dl class="details-list">
        <div>
          <dt>零层地址</dt>
          <dd>
            <code>{{ overview?.controller.address || "—" }}</code
            ><button
              class="icon-button tiny"
              :disabled="!overview?.controller.address"
              title="复制零层地址"
              aria-label="复制零层地址"
              @click="copyText(overview!.controller.address)"
            >
              <Copy :size="16" />
            </button>
          </dd>
        </div>
        <div>
          <dt>ZeroTier 版本</dt>
          <dd>{{ overview?.controller.version || "—" }}</dd>
        </div>
        <div>
          <dt>管理界面</dt>
          <dd>1.0.0</dd>
        </div>
        <div>
          <dt>运行环境</dt>
          <dd>{{ auth.demo ? "本地示例控制器" : "本机 ZeroTier 控制器" }}</dd>
        </div>
        <div>
          <dt>最近采集</dt>
          <dd>
            {{ overview?.sampledAt ? dateText(overview.sampledAt) : "—" }}
          </dd>
        </div>
      </dl>
    </section>
    <section class="system-section">
      <header class="settings-heading">
        <h2><ScanLine :size="20" />Ping 检测</h2>
        <div class="settings-header-actions">
          <span v-if="probeSettings?.disabled" class="badge neutral">检测已停用</span>
          <button
            class="icon-button"
            title="重新加载 Ping 检测设置"
            aria-label="重新加载 Ping 检测设置"
            :disabled="probeLoading || probeSaving"
            @click="loadProbeSettings"
          ><RefreshCw :size="18" :class="{ spin: probeLoading }" /></button>
        </div>
      </header>
      <div v-if="probeError" class="alert danger" role="alert">{{ probeError }}</div>
      <form v-if="probeSettings" aria-label="Ping 检测设置" @submit.prevent="saveProbeSettings">
        <div class="form-grid">
          <label class="field">自动检测间隔（秒）
            <input v-model.number="probeForm.intervalSeconds" type="number" min="10" max="3600" step="1" required :disabled="probeLoading || probeSaving" />
            <span class="field-hint">10–3600 秒</span>
          </label>
          <label class="field">单个 IP 超时（毫秒）
            <input v-model.number="probeForm.timeoutMs" type="number" min="200" max="10000" step="1" required :disabled="probeLoading || probeSaving" />
            <span class="field-hint">200–10000 毫秒</span>
          </label>
        </div>
        <footer class="form-footer">
          <button type="button" class="button" :disabled="probeLoading || probeSaving" @click="probeForm = { intervalSeconds: 60, timeoutMs: 1500 }"><RotateCcw :size="16" />恢复默认</button>
          <button class="button primary" :disabled="probeLoading || probeSaving || !probeDirty || !probeValid"><Check :size="16" />保存设置</button>
        </footer>
      </form>
      <p v-else-if="probeLoading" class="muted">加载中</p>
    </section>
    <section class="system-section export-section">
      <div>
        <h2>导出管理数据</h2>
        <p class="muted">
          网络、成员与策略配置，不含登录凭据或加入令牌；此文件不是完整控制器备份。
        </p>
      </div>
      <a class="button" href="/api/export" download
        ><Download :size="17" />导出 JSON</a
      >
    </section>
  </div>
  <section v-if="section === 'account'" class="system-section account-section">
    <header class="settings-heading">
      <h2><KeyRound :size="20" />管理员密码</h2>
      <span class="badge neutral">{{ auth.username }}</span>
    </header>
    <form @submit.prevent="changePassword">
      <div v-if="error" class="alert danger">{{ error }}</div>
      <label class="field"
        >当前密码<input
          v-model="current"
          type="password"
          autocomplete="current-password"
          required
      /></label>
      <div class="form-grid">
        <label class="field"
          >新密码<input
            v-model="password"
            type="password"
            minlength="10"
            autocomplete="new-password"
            required
          /><span class="field-hint">至少 10 个字符</span></label
        ><label class="field"
          >确认新密码<input
            v-model="confirmation"
            type="password"
            minlength="10"
            autocomplete="new-password"
            required
        /></label>
      </div>
      <footer class="form-footer">
        <span class="muted">修改后其他登录会话将失效</span
        ><button class="button primary" :disabled="busy">
          <Check :size="16" />更新密码
        </button>
      </footer>
    </form>
  </section>
  <section v-if="section === 'logs'" class="list-section">
    <div class="section-toolbar">
      <label class="search audit-search"
        ><Search :size="18" /><input
          v-model="logSearch"
          type="search"
          placeholder="搜索操作、对象或时间"
          aria-label="搜索操作记录"
      /></label>
      <button
        class="icon-button bordered"
        title="刷新操作记录"
        aria-label="刷新操作记录"
        @click="loadLogs"
        :disabled="loadingLogs"
      >
        <RefreshCw :size="17" />
      </button>
    </div>
    <div class="audit-table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>时间</th>
            <th>操作</th>
            <th>对象</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="log in filteredLogs" :key="log.id">
            <td class="muted">{{ dateText(log.at) }}</td>
            <td>{{ log.action }}</td>
            <td>
              <code>{{ log.target }}</code>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="!filteredLogs.length" class="empty-state">
      <Activity :size="27" />
      <h3>
        {{
          loadingLogs
            ? "加载中"
            : logSearch
              ? "没有匹配的操作记录"
              : "暂无操作记录"
        }}
      </h3>
    </div>
    <footer class="table-footer">{{ filteredLogs.length }} 条记录</footer>
  </section>
</template>
