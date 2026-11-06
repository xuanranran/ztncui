<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { onBeforeRouteLeave } from "vue-router";
import {
  Check,
  ChevronLeft,
  ChevronRight,
  Monitor,
  Plus,
  RotateCcw,
  Search,
  Shield,
  Trash2,
} from "@lucide/vue";
import { api, notify } from "../api";
import AuthorizationBadge from "./AuthorizationBadge.vue";
import {
  canonicalRules,
  compileRules,
  compilePolicy,
  mergeDefinitions,
  defaultTemplate,
  services,
  templateSource,
} from "../flow-rules";
import type { Template } from "../flow-rules";
import type { Network } from "../types";
import Modal from "./Modal.vue";
import NativePolicies from "./NativePolicies.vue";
const props = defineProps<{ network: Network }>(),
  emit = defineEmits<{ saved: []; dirty: [value: boolean] }>();
type Editor = {
  mode: "template" | "source" | "json";
  template: Template;
  source: string;
  json: string;
};
function fromNetwork(n: Network): Editor {
  const saved = n.flowEditor;
  const matches =
    saved &&
    canonicalRules(saved.rules) === canonicalRules(n.rules) &&
    (!saved.policySnapshot ||
      canonicalRules(saved.policySnapshot) ===
        canonicalRules({ tags: n.tags, capabilities: n.capabilities }));
  return {
    mode: matches
      ? saved.mode
      : canonicalRules(n.rules) === canonicalRules([{ type: "ACTION_ACCEPT" }])
        ? "template"
        : "json",
    template: matches
      ? { ...defaultTemplate(), ...structuredClone(toRaw(saved.template)) }
      : defaultTemplate(),
    source: saved?.source || "accept;",
    json: JSON.stringify(n.rules || [], null, 2),
  };
}
const form = ref(fromNetwork(props.network)),
  original = ref(JSON.stringify(form.value)),
  revision = ref(props.network.revision),
  busy = ref(false),
  error = ref(""),
  confirmSave = ref(false);
const confirmTemplate = ref(false);
const confirmRestore = ref(false),
  exemptSearch = ref(""),
  exemptPage = ref(1);
const exemptMembers = computed(() => {
  const query = exemptSearch.value.trim().toLowerCase();
  const rows = props.network.members.map((m) => ({
    id: m.id,
    name: m.name || m.id,
    authorized: m.authorized,
    known: true,
    ips: m.ipAssignments.join(" "),
  }));
  const known = new Set(rows.map((m) => m.id));
  for (const id of form.value.template.excluded) {
    if (!known.has(id))
      rows.push({
        id,
        name: "未在当前成员列表中",
        authorized: false,
        known: false,
        ips: "",
      });
  }
  return rows.filter((m) =>
    `${m.name} ${m.id} ${m.ips}`.toLowerCase().includes(query),
  );
});
const exemptPages = computed(() =>
  Math.max(1, Math.ceil(exemptMembers.value.length / 10)),
);
const visibleExemptMembers = computed(() =>
  exemptMembers.value.slice((exemptPage.value - 1) * 10, exemptPage.value * 10),
);
watch(exemptSearch, () => (exemptPage.value = 1));
watch(
  exemptPages,
  (count) => (exemptPage.value = Math.min(exemptPage.value, count)),
);
const baseDirty = computed(() => JSON.stringify(form.value) !== original.value);
const policyDirty = ref(false);
const dirty = computed(() => baseDirty.value || policyDirty.value);
const compiled = computed(() => {
  try {
    const f = form.value;
    const sourcePolicy = f.mode === "source" ? compilePolicy(f.source) : null;
    const rules =
      f.mode === "json"
        ? JSON.parse(f.json)
        : (sourcePolicy?.rules ?? compileRules(templateSource(f.template)));
    if (
      !Array.isArray(rules) ||
      !rules.length ||
      rules.length > 1024 ||
      rules.some((r) => !r || typeof r.type !== "string")
    )
      throw new Error("规则须为包含 type 字段的 JSON 数组，最多 1024 条");
    return { rules, sourcePolicy, error: "" };
  } catch (e) {
    return { rules: [], sourcePolicy: null, error: (e as Error).message };
  }
});
watch(dirty, (value) => emit("dirty", value));
onBeforeUnmount(() => emit("dirty", false));
watch(
  () => props.network,
  (n) => {
    if (!baseDirty.value) {
      form.value = fromNetwork(n);
      original.value = JSON.stringify(form.value);
      revision.value = n.revision;
    }
  },
);
onBeforeRouteLeave(() => !dirty.value || confirm("流规则尚未保存，是否离开？"));
function changeMode(event: Event) {
  const next = (event.target as HTMLSelectElement).value as Editor["mode"];
  if (next === "template" && form.value.mode !== "template") {
    confirmTemplate.value = true;
    (event.target as HTMLSelectElement).value = form.value.mode;
    return;
  }
  if (
    next === "source" &&
    form.value.mode === "template" &&
    !compiled.value.error
  )
    form.value.source = templateSource(form.value.template);
  if (next === "json" && !compiled.value.error)
    form.value.json = JSON.stringify(compiled.value.rules, null, 2);
  form.value.mode = next;
}
async function save() {
  if (compiled.value.error || busy.value) return;
  await persistEditor(
    structuredClone(toRaw(form.value)),
    compiled.value.rules,
    "流规则已保存",
  );
}
async function restoreDefaults() {
  if (busy.value) return;
  const next: Editor = {
    mode: "template",
    template: defaultTemplate(),
    source: "accept;",
    json: JSON.stringify([{ type: "ACTION_ACCEPT" }], null, 2),
  };
  await persistEditor(next, compileRules("accept;"), "已恢复默认规则");
}
async function persistEditor(
  next: Editor,
  rules: Record<string, unknown>[],
  message: string,
) {
  busy.value = true;
  error.value = "";
  try {
    const parsed = next.mode === "source" ? compilePolicy(next.source) : null;
    const hasDefinitions =
      !!parsed && (parsed.tags.length > 0 || parsed.capabilities.length > 0);
    const policySnapshot = hasDefinitions
      ? {
          tags: mergeDefinitions(props.network.tags, parsed!.tags),
          capabilities: mergeDefinitions(
            props.network.capabilities,
            parsed!.capabilities,
          ),
        }
      : undefined;
    const oldMeta = props.network.policyMetadata ?? {
      tags: {},
      capabilities: {},
    };
    await api(
      "/networks/" + props.network.nwid,
      "PATCH",
      {
        rules,
        ...(policySnapshot
          ? {
              ...policySnapshot,
              policyMetadata: {
                tags: { ...oldMeta.tags, ...parsed!.metadata.tags },
                capabilities: {
                  ...oldMeta.capabilities,
                  ...parsed!.metadata.capabilities,
                },
              },
            }
          : {}),
        flowEditor: {
          mode: next.mode,
          template: next.template,
          source: next.source,
          rules,
          ...(policySnapshot ? { policySnapshot } : {}),
        },
      },
      { "If-Match": revision.value },
    );
    next.json = JSON.stringify(rules, null, 2);
    form.value = next;
    original.value = JSON.stringify(next);
    confirmSave.value = false;
    confirmRestore.value = false;
    emit("saved");
    notify(message);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <section class="rules-section">
    <header class="settings-heading rules-heading">
      <h2>{{ form.mode === "template" ? "模板流规则" : "自定义流规则" }}</h2>
      <div class="rules-actions">
        <button
          class="button"
          :disabled="busy"
          @click="
            error = '';
            confirmRestore = true;
          "
        >
          <RotateCcw :size="16" />恢复默认规则
        </button>
        <select
          :value="form.mode"
          :disabled="busy"
          aria-label="规则编辑模式"
          @change="changeMode"
        >
          <option value="template">模板规则</option>
          <option value="source">自定义规则语言</option>
          <option value="json">原生 JSON</option>
        </select>
      </div>
    </header>
    <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
    <template v-if="form.mode === 'template'">
      <div class="toggle-row layer2-filter">
        <div>
          <strong><Shield :size="17" />二层协议过滤</strong
          ><small
            >开启后仅允许 IPv4、IPv6 和 ARP；关闭后允许其他以太网协议。</small
          >
        </div>
        <input
          v-model="form.template.layer2"
          class="switch"
          type="checkbox"
          aria-label="二层协议过滤"
          :disabled="busy"
        />
      </div>
      <div class="toggle-row restriction-toggle">
        <div>
          <strong>限制网络流量</strong
          ><small>关闭时设备可自由通信；开启后仅放行所选服务和端口。</small>
        </div>
        <input
          v-model="form.template.restrict"
          class="switch"
          type="checkbox"
          aria-label="限制网络流量"
          :disabled="busy"
        />
      </div>
      <fieldset
        :disabled="!form.template.restrict || busy"
        class="service-options"
      >
        <legend>允许的服务</legend>
        <label v-for="s in services" :key="s.id" class="check-label"
          ><input
            v-model="form.template.services"
            type="checkbox"
            :value="s.id"
          /><span
            >{{ s.name }}<small class="muted">{{ s.detail }}</small></span
          ></label
        >
        <div
          v-for="(port, index) in form.template.ports"
          :key="index"
          class="custom-port"
        >
          <select v-model="port.protocol" aria-label="端口协议">
            <option value="tcp">TCP</option>
            <option value="udp">UDP</option>
          </select>
          <input
            v-model.number="port.start"
            type="number"
            min="1"
            max="65535"
            aria-label="起始端口"
          />
          <span>至</span
          ><input
            v-model.number="port.end"
            type="number"
            min="1"
            max="65535"
            aria-label="结束端口"
          />
          <button
            class="icon-button danger-text"
            title="删除端口"
            aria-label="删除端口"
            @click="form.template.ports.splice(index, 1)"
          >
            <Trash2 :size="17" />
          </button>
        </div>
        <button
          class="button"
          @click="
            form.template.ports.push({
              protocol: 'tcp',
              start: 8080,
              end: 8080,
            })
          "
        >
          <Plus :size="16" />添加自定义端口
        </button>
      </fieldset>
      <section class="exempt-section" aria-labelledby="exempt-heading">
        <header>
          <h3 id="exempt-heading"><Monitor :size="19" />豁免设备</h3>
          <p class="muted">
            所选设备的收发流量不受模板限制；仍需具备网络授权。
          </p>
        </header>
        <label class="search exempt-search"
          ><Search :size="18" /><input
            v-model="exemptSearch"
            type="search"
            placeholder="搜索设备名称、ID 或 IP"
            aria-label="搜索豁免设备"
        /></label>
        <div class="table-scroll">
          <table class="data-table exempt-table">
            <thead>
              <tr>
                <th>设备名称 / ID</th>
                <th>授权状态</th>
                <th>豁免限制</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="m in visibleExemptMembers" :key="m.id">
                <td>
                  <strong>{{ m.name }}</strong
                  ><code>{{ m.id }}</code>
                </td>
                <td>
                  <AuthorizationBadge v-if="m.known" :authorized="m.authorized" />
                  <span v-else class="badge neutral">未注册</span>
                </td>
                <td>
                  <input
                    v-model="form.template.excluded"
                    class="switch"
                    type="checkbox"
                    :value="m.id"
                    :disabled="!form.template.restrict || busy"
                    :aria-label="`豁免 ${m.name}`"
                  />
                </td>
              </tr>
              <tr v-if="!exemptMembers.length">
                <td colspan="3" class="empty-cell">
                  {{ exemptSearch ? "没有匹配的设备" : "暂无成员设备" }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <footer v-if="exemptPages > 1" class="exempt-pagination">
          <span class="muted"
            >{{ exemptMembers.length }} 台设备 · 第 {{ exemptPage }} /
            {{ exemptPages }} 页</span
          >
          <div>
            <button
              class="icon-button"
              :disabled="exemptPage === 1"
              title="上一页"
              aria-label="上一页豁免设备"
              @click="exemptPage--"
            >
              <ChevronLeft :size="18" /></button
            ><button
              class="icon-button"
              :disabled="exemptPage === exemptPages"
              title="下一页"
              aria-label="下一页豁免设备"
              @click="exemptPage++"
            >
              <ChevronRight :size="18" />
            </button>
          </div>
        </footer>
      </section>
    </template>
    <div v-if="network.capabilities?.length" class="alert warning">
      网络包含原生能力定义。模板和 JSON 仅修改基础规则；源码中的定义会合并保存。
    </div>
    <label v-if="form.mode !== 'template'" class="field"
      >{{ form.mode === "json" ? "规则 JSON" : "规则定义" }}
      <textarea
        v-if="form.mode === 'json'"
        v-model="form.json"
        class="code-editor"
        spellcheck="false"
        rows="17"
        aria-label="流规则 JSON"
        :disabled="busy"
      ></textarea>
      <textarea
        v-else
        v-model="form.source"
        class="code-editor"
        spellcheck="false"
        rows="17"
        aria-label="流规则源代码"
        :disabled="busy"
      ></textarea>
    </label>
    <div v-if="compiled.error" class="alert warning" role="alert">
      {{ compiled.error }}
    </div>
    <details v-else class="compiled-preview">
      <summary>编译结果 · {{ compiled.rules.length }} 条</summary>
      <pre>{{ JSON.stringify(compiled.rules, null, 2) }}</pre>
    </details>
    <footer class="form-footer">
      <span class="save-state">{{ dirty ? "有未保存的更改" : "" }}</span>
      <div>
        <button
          class="button"
          :disabled="!baseDirty || busy"
          @click="
            form = JSON.parse(original);
            error = '';
          "
        >
          <RotateCcw :size="15" />重置
        </button>
        <button
          class="button primary"
          :disabled="!baseDirty || busy || !!compiled.error"
          @click="confirmSave = true"
        >
          <Check :size="16" />保存规则
        </button>
      </div>
    </footer>
  </section>
  <NativePolicies
    :network="network"
    @saved="emit('saved')"
    @dirty="policyDirty = $event"
  />
  <Modal :open="confirmSave" title="应用流规则" @close="confirmSave = false"
    ><p>
      此操作会替换基础流规则，可能中断现有连接。源码中的标签和能力定义会合并保存，其他定义保留。
    </p>
    <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
    <div class="modal-actions">
      <button class="button" :disabled="busy" @click="confirmSave = false">
        取消</button
      ><button class="button primary" :disabled="busy" @click="save">
        <Check :size="16" />确认应用
      </button>
    </div>
  </Modal>
  <Modal
    :open="confirmTemplate"
    title="切换为模板规则"
    @close="confirmTemplate = false"
    ><p>当前编辑区将切换为服务模板。自定义规则在保存前不会被写入或替换。</p>
    <div class="modal-actions">
      <button class="button" @click="confirmTemplate = false">取消</button
      ><button
        class="button primary"
        @click="
          form.mode = 'template';
          confirmTemplate = false;
        "
      >
        确认切换
      </button>
    </div>
  </Modal>
  <Modal
    :open="confirmRestore"
    title="恢复默认规则"
    @close="!busy && (confirmRestore = false)"
    ><p>
      将立即恢复为允许所有流量，清除服务限制、自定义端口和设备豁免，并替换当前未保存的草稿。原生策略标签和能力定义保持不变。
    </p>
    <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
    <div class="modal-actions">
      <button class="button" :disabled="busy" @click="confirmRestore = false">
        取消</button
      ><button class="button primary" :disabled="busy" @click="restoreDefaults">
        <RotateCcw :size="16" />确认恢复
      </button>
    </div>
  </Modal>
</template>
