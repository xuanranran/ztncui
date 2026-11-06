import { reactive, ref } from "vue";
import type { Auth, Overview } from "./types";

export const auth = reactive<Auth>({
  authenticated: false,
  setupRequired: false,
  username: "admin",
  csrf: "",
  demo: false,
});
export const overview = ref<Overview | null>(null);
export const toast = ref("");
export const connected = ref(false);
let toastTimer: ReturnType<typeof setTimeout>;
export function notify(message: string) {
  toast.value = message;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (toast.value = ""), 4500);
}
export async function api<T>(
  url: string,
  method = "GET",
  body?: unknown,
  headers: Record<string, string> = {},
): Promise<T> {
  const response = await fetch("/api" + url, {
    method,
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      "X-CSRF-Token": auth.csrf,
      ...headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const result = await response.json();
  if (!response.ok) {
    if (response.status === 401 && url !== "/login") auth.authenticated = false;
    throw new Error(result.error || "请求失败");
  }
  return result as T;
}
export async function loadOverview() {
  overview.value = await api<Overview>("/overview");
}
export async function copyText(value: string) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value);
      notify("已复制");
      return;
    } catch {
      // HTTP LAN addresses and restricted clipboard permissions need a fallback.
    }
  }
  const focused = document.activeElement;
  const field = document.createElement("textarea");
  field.value = value;
  field.readOnly = true;
  field.style.position = "fixed";
  field.style.left = "-9999px";
  document.body.appendChild(field);
  try {
    field.select();
    field.setSelectionRange(0, value.length);
    if (!document.execCommand("copy")) throw new Error("Copy rejected");
    notify("已复制");
  } catch {
    notify("复制失败，请检查浏览器权限");
  } finally {
    field.remove();
    if (focused instanceof HTMLElement) focused.focus({ preventScroll: true });
  }
}
export function ago(ms: number) {
  if (!ms) return "尚未连接";
  const minutes = Math.max(0, Math.floor((Date.now() - ms) / 60000));
  if (minutes < 1) return "刚刚";
  if (minutes < 60) return `${minutes} 分钟前`;
  if (minutes < 1440) return `${Math.floor(minutes / 60)} 小时前`;
  return `${Math.floor(minutes / 1440)} 天前`;
}
export function dateText(value: string) {
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
}
