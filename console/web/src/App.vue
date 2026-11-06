<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import {
  Network,
  Settings2,
  ChevronDown,
  ChevronRight,
  Radio,
  Menu,
  X,
  Sun,
  Moon,
  Monitor,
  LogOut,
  ArrowUpRight,
  ShieldCheck,
  CircleHelp,
} from "@lucide/vue";
import {
  api,
  auth,
  connected,
  loadOverview,
  notify,
  overview,
  toast,
} from "./api";
import type { Auth } from "./types";

const route = useRoute();
const ready = ref(false),
  sidebar = ref(false),
  password = ref(""),
  username = ref("admin"),
  error = ref(""),
  busy = ref(false);
const theme = ref(localStorage.getItem("ztncui:theme") || "system");
const pageName = computed(() =>
  route.path === "/settings"
    ? "系统设置"
    : route.params.id
      ? overview.value?.networks.find((n) => n.nwid === route.params.id)
          ?.name || "网络"
      : "全部网络",
);
let source: EventSource | undefined,
  fallback: ReturnType<typeof setInterval> | undefined;
const systemTheme = matchMedia("(prefers-color-scheme: dark)");
function applyTheme() {
  const actual =
    theme.value === "system"
      ? systemTheme.matches
        ? "dark"
        : "light"
      : theme.value;
  document.documentElement.dataset.theme = actual;
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute("content", actual === "dark" ? "#000000" : "#ffffff");
  localStorage.setItem("ztncui:theme", theme.value);
}
watch(theme, applyTheme);
systemTheme.addEventListener("change", applyTheme);
watch(
  () => route.fullPath,
  () => (sidebar.value = false),
);
function updates() {
  source?.close();
  clearInterval(fallback);
  source = new EventSource("/api/events");
  source.onopen = () => (connected.value = true);
  source.onerror = () => (connected.value = false);
  source.addEventListener("expired", () => {
    auth.authenticated = false;
    source?.close();
  });
  source.addEventListener("update", () => {
    loadOverview().catch((err) => notify(err.message));
    window.dispatchEvent(new Event("ztncui:update"));
  });
  fallback = setInterval(() => {
    if (!connected.value && !document.hidden && auth.authenticated) {
      loadOverview().catch(() => {});
      window.dispatchEvent(new Event("ztncui:update"));
    }
  }, 15000);
}
watch(
  () => auth.authenticated,
  async (value) => {
    if (value) {
      await loadOverview().catch((err) => notify(err.message));
      updates();
    } else {
      source?.close();
      connected.value = false;
      clearInterval(fallback);
      overview.value = null;
    }
  },
);
async function login() {
  busy.value = true;
  error.value = "";
  try {
    const session = await api<{ csrf: string; username: string }>(
      auth.setupRequired ? "/setup" : "/login",
      "POST",
      { username: username.value, password: password.value },
    );
    Object.assign(auth, session, { authenticated: true, setupRequired: false });
    password.value = "";
  } catch (err) {
    error.value = (err as Error).message;
  } finally {
    busy.value = false;
  }
}
async function logout() {
  try {
    await api("/logout", "POST");
    auth.authenticated = false;
  } catch (err) {
    notify((err as Error).message);
  }
}
onMounted(async () => {
  applyTheme();
  try {
    Object.assign(auth, await api<Auth>("/auth"));
  } catch (err) {
    error.value = (err as Error).message;
  } finally {
    ready.value = true;
  }
});
onBeforeUnmount(() => {
  source?.close();
  clearInterval(fallback);
  systemTheme.removeEventListener("change", applyTheme);
});
</script>

<template>
  <div v-if="!ready" class="initial-loading">
    <Radio :size="28" /><span>正在连接控制器…</span>
  </div>
  <div v-else-if="!auth.authenticated" class="login-page">
    <div class="login-brand">
      <Network :size="28" /><strong>ZeroTier <span>控制器</span></strong>
    </div>
    <form class="login-form" @submit.prevent="login">
      <span class="section-kicker">ZTNCUI CONSOLE</span>
      <h1>{{ auth.setupRequired ? "初始化管理员" : "欢迎回来" }}</h1>
      <p class="muted">
        {{
          auth.setupRequired ? "设置此控制器的管理员密码" : "登录你的网络控制台"
        }}
      </p>
      <div v-if="auth.demo" class="preview-credentials">
        <ShieldCheck :size="18" /><span
          >本地预览账号 <b>admin</b><br />密码 <code>Preview123456!</code></span
        >
      </div>
      <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
      <label class="field"
        >用户名<input
          v-model="username"
          autocomplete="username"
          :readonly="auth.setupRequired"
          required
      /></label>
      <label class="field"
        >{{ auth.setupRequired ? "设置密码" : "密码"
        }}<input
          v-model="password"
          type="password"
          :autocomplete="
            auth.setupRequired ? 'new-password' : 'current-password'
          "
          :minlength="auth.setupRequired ? 10 : undefined"
          required
      /></label>
      <button class="button primary full" :disabled="busy">
        {{ busy ? "正在登录…" : auth.setupRequired ? "创建唯一管理员" : "登录"
        }}<ArrowUpRight :size="18" />
      </button>
    </form>
    <footer class="login-footer">
      ZeroTier 网络控制器<span v-if="auth.demo">本地预览</span>
    </footer>
  </div>
  <div v-else class="app-shell">
    <div v-if="sidebar" class="sidebar-scrim" @click="sidebar = false"></div>
    <aside class="sidebar" :class="{ open: sidebar }">
      <RouterLink class="brand" to="/networks"
        ><span class="brand-mark"><Network :size="23" /></span
        ><span>ZeroTier<small>网络控制器</small></span></RouterLink
      >
      <button
        class="icon-button mobile-close"
        title="关闭导航"
        aria-label="关闭导航"
        @click="sidebar = false"
      >
        <X :size="20" />
      </button>
      <div class="workspace">
        <span class="workspace-icon"><ShieldCheck :size="18" /></span>
        <div>
          个人工作空间<small>管理员 · {{ auth.username }}</small>
        </div>
        <ChevronDown :size="15" />
      </div>
      <div class="nav-caption">工作空间</div>
      <nav aria-label="主导航">
        <RouterLink
          class="nav-link"
          :class="{ active: route.path !== '/settings' }"
          to="/networks"
          ><Network :size="18" /><span>网络</span
          ><span class="nav-count">{{
            overview?.networks.length || 0
          }}</span></RouterLink
        >
        <div class="network-tree">
          <RouterLink
            v-for="n in overview?.networks"
            :key="n.nwid"
            :to="'/network/' + n.nwid"
            :class="{ selected: route.params.id === n.nwid }"
            ><span class="tree-dot" :class="{ live: n.online > 0 }"></span
            ><span>{{ n.name }}</span
            ><span v-if="n.pending" class="pending-dot"></span
          ></RouterLink>
        </div>
        <RouterLink
          class="nav-link"
          :class="{ active: route.path === '/settings' }"
          to="/settings"
          ><Settings2 :size="18" /><span>系统设置</span></RouterLink
        >
      </nav>
      <div class="sidebar-bottom">
        <div class="controller-state">
          <span
            class="status-dot"
            :class="{ online: overview?.controller.online }"
          ></span
          ><span
            >控制器{{ overview?.controller.online ? "在线" : "未连接" }}</span
          ><span class="version"
            >v{{ overview?.controller.version || "—" }}</span
          >
        </div>
        <div class="sidebar-actions">
          <div class="theme-control" aria-label="配色模式">
            <button
              v-for="item in [
                { value: 'light', label: '亮色', icon: Sun },
                { value: 'dark', label: 'OLED 暗色', icon: Moon },
                { value: 'system', label: '跟随系统', icon: Monitor },
              ]"
              :key="item.value"
              :class="{ selected: theme === item.value }"
              :title="item.label"
              :aria-label="item.label"
              :aria-pressed="theme === item.value"
              @click="theme = item.value"
            >
              <component :is="item.icon" :size="16" />
            </button>
          </div>
          <button
            class="icon-button"
            title="退出登录"
            aria-label="退出登录"
            @click="logout"
          >
            <LogOut :size="18" />
          </button>
        </div>
      </div>
    </aside>
    <div class="main-shell">
      <header class="topbar">
        <div class="breadcrumbs">
          <button
            class="icon-button menu-button"
            aria-label="打开导航"
            title="打开导航"
            @click="sidebar = true"
          >
            <Menu :size="22" /></button
          ><RouterLink to="/networks">工作空间</RouterLink
          ><ChevronRight :size="14" /><span>{{ pageName }}</span>
        </div>
        <div class="topbar-right">
          <span v-if="auth.demo" class="preview-badge">本地预览</span
          ><span class="sync-state" :class="{ live: connected }"
            ><span class="status-dot" :class="{ online: connected }"></span
            >{{ connected ? "实时连接" : "正在重连" }}</span
          ><a
            class="icon-button help-link"
            href="https://docs.zerotier.com/controller/"
            target="_blank"
            rel="noopener"
            title="ZeroTier 文档"
            aria-label="ZeroTier 文档"
            ><CircleHelp :size="19" /></a
          ><span class="avatar" title="管理员">A</span>
        </div>
      </header>
      <main class="page-content console-page">
        <div v-if="overview?.error" class="alert danger" role="alert">
          {{ overview.error }}<span>上次数据保留</span>
        </div>
        <RouterView :key="String(route.params.id || route.path)" />
      </main>
    </div>
  </div>
  <Teleport to="body"
    ><div v-if="toast" class="toast" role="status">
      <ShieldCheck :size="17" />{{ toast }}
    </div></Teleport
  >
</template>
