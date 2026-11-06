<script setup lang="ts">
import { computed, ref, toRaw, watch } from "vue";
import {
  Check,
  ChevronLeft,
  ChevronRight,
  Pencil,
  Plus,
  RotateCcw,
  Search,
  Shield,
  Tags,
  Trash2,
  X,
} from "@lucide/vue";
import { api, notify } from "../api";
import { compileRules } from "../flow-rules";
import type {
  Capability,
  Member,
  Network,
  PolicyMetadata,
  Rule,
} from "../types";
import Modal from "./Modal.vue";
const props = defineProps<{ network: Network }>();
const emit = defineEmits<{ saved: []; dirty: [value: boolean] }>();
type Kind = "tags" | "capabilities";
type Draft = {
  tags: [number, number][];
  capabilities: number[];
  revision: string;
  networkRevision: string;
};
const busy = ref(false),
  error = ref(""),
  mode = ref<Kind>("tags"),
  search = ref(""),
  page = ref(1),
  drafts = ref<Record<string, Draft>>({});
const definition = ref<{
  kind: Kind;
  existing: number | null;
  id: number | string;
  name: string;
  defaultValue: number | string;
  defaultCap: boolean;
  values: { value: number | string; name: string }[];
  ruleMode: "source" | "json";
  body: string;
  revision: string;
} | null>(null);
const definitionOriginal = ref(""),
  deletion = ref<{ kind: Kind; id: number; revision: string } | null>(null);
const dirty = computed(
  () =>
    Object.keys(drafts.value).length > 0 ||
    (!!definition.value &&
      JSON.stringify(definition.value) !== definitionOriginal.value),
);
watch(dirty, (value) => emit("dirty", value));
const metadata = (): PolicyMetadata => ({
  tags: structuredClone(toRaw(props.network.policyMetadata?.tags ?? {})),
  capabilities: structuredClone(
    toRaw(props.network.policyMetadata?.capabilities ?? {}),
  ),
});
const label = (kind: Kind, id: number) =>
  props.network.policyMetadata?.[kind]?.[id]?.name ||
  `${kind === "tags" ? "标签" : "能力"} #${id}`;
const uint = (v: unknown) =>
  typeof v === "number" && Number.isInteger(v) && v >= 0 && v <= 4294967295;
function editDefinition(kind: Kind, id?: number) {
  error.value = "";
  const item = props.network[kind].find((d) => d.id === id);
  let nextID = 1000;
  while (props.network[kind].some((d) => d.id === nextID)) nextID++;
  const meta =
    id === undefined ? undefined : props.network.policyMetadata?.[kind]?.[id];
  definition.value = {
    kind,
    existing: id ?? null,
    id: id ?? nextID,
    name: meta?.name || "",
    defaultValue:
      kind === "tags" ? ((item?.default as number | null) ?? "") : "",
    defaultCap: kind === "capabilities" && !!item?.default,
    values:
      kind === "tags"
        ? Object.entries(
            props.network.policyMetadata?.tags[id ?? -1]?.values || {},
          ).map(([value, name]) => ({ value: Number(value), name }))
        : [],
    ruleMode: item ? "json" : "source",
    body:
      item && kind === "capabilities"
        ? JSON.stringify((item as Capability).rules, null, 2)
        : "",
    revision: props.network.revision,
  };
  definitionOriginal.value = JSON.stringify(definition.value);
}
function capabilityRules(): Rule[] {
  const d = definition.value!;
  const rules =
    d.ruleMode === "source" ? compileRules(d.body) : JSON.parse(d.body);
  if (
    !Array.isArray(rules) ||
    rules.length > 64 ||
    rules.some((r) => !r || typeof r.type !== "string")
  )
    throw new Error("能力规则必须为规则数组，最多 64 条");
  return rules;
}
async function saveDefinition() {
  const d = definition.value;
  if (!d || busy.value) return;
  error.value = "";
  try {
    if (!uint(d.id)) throw new Error("ID 必须为 0 至 4294967295 的整数");
    if (props.network[d.kind].some((v) => v.id === d.id && v.id !== d.existing))
      throw new Error("策略 ID 已存在");
    const meta: PolicyMetadata = metadata();
    let value: { id: number; default: number | null | boolean; rules?: Rule[] };
    if (d.kind === "tags") {
      if (d.defaultValue !== "" && !uint(d.defaultValue))
        throw new Error("标签默认值无效");
      const values: Record<string, string> = {};
      for (const v of d.values) {
        if (!uint(v.value) || !v.name.trim() || String(v.value) in values)
          throw new Error("枚举值必须为不重复的无符号整数，名称不能为空");
        values[String(v.value)] = v.name.trim();
      }
      meta.tags[d.id as number] = { name: d.name.trim(), values };
      value = {
        id: d.id as number,
        default: d.defaultValue === "" ? null : Number(d.defaultValue),
      };
    } else {
      meta.capabilities[d.id as number] = { name: d.name.trim() };
      value = {
        id: d.id as number,
        default: d.defaultCap,
        rules: capabilityRules(),
      };
    }
    const list = [
      ...props.network[d.kind].filter((v) => v.id !== d.existing),
      value,
    ].sort((a, b) => a.id - b.id);
    busy.value = true;
    await api(
      "/networks/" + props.network.nwid,
      "PATCH",
      { [d.kind]: list, policyMetadata: meta },
      { "If-Match": d.revision },
    );
    definition.value = null;
    emit("saved");
    notify("策略定义已保存");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function removeDefinition() {
  const d = deletion.value;
  if (!d || busy.value) return;
  busy.value = true;
  error.value = "";
  try {
    const meta = metadata();
    delete meta[d.kind][d.id];
    await api(
      "/networks/" + props.network.nwid,
      "PATCH",
      {
        [d.kind]: props.network[d.kind].filter((v) => v.id !== d.id),
        policyMetadata: meta,
      },
      { "If-Match": d.revision },
    );
    deletion.value = null;
    emit("saved");
    notify("策略定义已删除");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
function draftFor(m: Member) {
  return (
    drafts.value[m.id] ?? {
      tags: m.policyTags || [],
      capabilities: m.capabilities || [],
      revision: m.policyRevision,
      networkRevision: props.network.revision,
    }
  );
}
function mutableDraft(m: Member) {
  const current = draftFor(m);
  return (drafts.value[m.id] ??= {
    tags: current.tags.map(([id, value]) => [id, value]),
    capabilities: [...current.capabilities],
    revision: current.revision,
    networkRevision: current.networkRevision,
  });
}
function setTag(m: Member, id: number, event: Event) {
  const v = (event.target as HTMLInputElement).value,
    d = mutableDraft(m);
  d.tags = d.tags.filter((t) => t[0] !== id);
  if (v !== "") d.tags.push([id, Number(v)]);
}
function setCapability(m: Member, id: number, event: Event) {
  const d = mutableDraft(m);
  d.capabilities = d.capabilities.filter((v) => v !== id);
  if ((event.target as HTMLInputElement).checked) d.capabilities.push(id);
}
const tagValue = (m: Member, id: number) =>
  draftFor(m).tags.find((t) => t[0] === id)?.[1] ?? "";
const allTagIDs = computed(() => [
  ...new Set([
    ...props.network.tags.map((t) => t.id),
    ...props.network.members.flatMap((m) =>
      (m.policyTags || []).map((t) => t[0]),
    ),
  ]),
]);
const allCapIDs = computed(() => [
  ...new Set([
    ...props.network.capabilities.map((t) => t.id),
    ...props.network.members.flatMap((m) => m.capabilities || []),
  ]),
]);
const columns = computed(() =>
  mode.value === "tags" ? allTagIDs.value : allCapIDs.value,
);
const filtered = computed(() => {
  const q = search.value.toLowerCase().trim();
  return props.network.members.filter((m) =>
    `${m.name} ${m.id} ${m.ipAssignments.join(" ")}`.toLowerCase().includes(q),
  );
});
const pages = computed(() =>
  Math.max(1, Math.ceil(filtered.value.length / 10)),
);
const members = computed(() =>
  filtered.value.slice((page.value - 1) * 10, page.value * 10),
);
watch(search, () => (page.value = 1));
watch(pages, (n) => (page.value = Math.min(page.value, n)));
async function saveMembers(id?: string) {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  const failed: string[] = [];
  for (const [mid, d] of Object.entries(drafts.value).filter(
    ([mid]) => !id || mid === id,
  )) {
    try {
      if (d.tags.some(([id, value]) => !uint(id) || !uint(value)))
        throw new Error("标签值必须为无符号整数");
      await api(
        "/networks/" + props.network.nwid + "/members/" + mid + "/policy",
        "PATCH",
        { policyTags: d.tags, capabilities: d.capabilities },
        { "If-Match": d.revision, "If-Network-Match": d.networkRevision },
      );
      delete drafts.value[mid];
    } catch (e) {
      failed.push(
        `${props.network.members.find((m) => m.id === mid)?.name || mid}：${(e as Error).message}`,
      );
    }
  }
  busy.value = false;
  emit("saved");
  if (failed.length) error.value = failed.join("；");
  else notify("成员策略已保存");
}
</script>
<template>
  <section class="native-policies" aria-label="原生策略管理">
    <div class="policy-definitions">
      <section
        v-for="kind in ['tags', 'capabilities'] as const"
        :key="kind"
        class="policy-definition"
      >
        <header class="settings-heading">
          <h2>
            <component :is="kind === 'tags' ? Tags : Shield" :size="20" />{{
              kind === "tags" ? "原生策略标签" : "能力定义"
            }}
          </h2>
          <button class="button" :disabled="busy" @click="editDefinition(kind)">
            <Plus :size="16" />{{ kind === "tags" ? "添加标签" : "添加能力" }}
          </button>
        </header>
        <div class="table-scroll">
          <table class="data-table">
            <thead>
              <tr>
                <th>名称 / ID</th>
                <th>{{ kind === "tags" ? "默认值" : "新成员默认分配" }}</th>
                <th class="action-column">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in network[kind]" :key="item.id">
                <td>
                  <strong>{{ label(kind, item.id) }}</strong
                  ><code>#{{ item.id }}</code
                  ><small v-if="kind === 'capabilities'" class="muted"
                    >{{ (item as Capability).rules.length }} 条规则</small
                  >
                </td>
                <td>
                  {{
                    item.default === null
                      ? "无"
                      : kind === "capabilities"
                        ? item.default
                          ? "开启"
                          : "关闭"
                        : item.default
                  }}
                </td>
                <td>
                  <div class="row-actions">
                    <button
                      class="icon-button"
                      :disabled="busy"
                      :aria-label="`编辑${label(kind, item.id)}`"
                      title="编辑定义"
                      @click="editDefinition(kind, item.id)"
                    >
                      <Pencil :size="17" /></button
                    ><button
                      class="icon-button danger-text"
                      :disabled="busy"
                      :aria-label="`删除${label(kind, item.id)}`"
                      title="删除定义"
                      @click="
                        error = '';
                        deletion = {
                          kind,
                          id: item.id,
                          revision: network.revision,
                        };
                      "
                    >
                      <Trash2 :size="17" />
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="!network[kind].length">
                <td colspan="3" class="empty-cell">
                  {{ kind === "tags" ? "暂无原生策略标签" : "暂无能力定义" }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
    <div
      v-if="error && !definition && !deletion"
      class="alert danger"
      role="alert"
    >
      {{ error }}
    </div>
    <section class="policy-assignment">
      <header class="settings-heading">
        <h2>成员策略分配</h2>
        <button
          class="button primary"
          :disabled="busy || !Object.keys(drafts).length"
          @click="saveMembers()"
        >
          <Check :size="16" />保存分配<span v-if="Object.keys(drafts).length"
            >({{ Object.keys(drafts).length }})</span
          >
        </button>
      </header>
      <div class="policy-matrix-toolbar">
        <div class="segmented-control" role="tablist" aria-label="策略矩阵">
          <button
            v-for="kind in ['tags', 'capabilities'] as const"
            :key="kind"
            role="tab"
            :aria-selected="mode === kind"
            :class="{ active: mode === kind }"
            @click="mode = kind"
          >
            {{ kind === "tags" ? "标签矩阵" : "能力矩阵" }}
          </button>
        </div>
        <label class="search"
          ><Search :size="17" /><input
            v-model="search"
            type="search"
            placeholder="搜索设备、ID 或 IP"
            aria-label="搜索策略成员"
        /></label>
      </div>
      <div v-if="columns.length" class="policy-matrix-scroll">
        <table class="data-table policy-matrix">
          <thead>
            <tr>
              <th>设备名称 / ID</th>
              <th v-for="id in columns" :key="id">
                {{ label(mode, id) }}<code>#{{ id }}</code>
              </th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in members" :key="m.id">
              <td>
                <strong>{{ m.name || m.id }}</strong
                ><code>{{ m.id }}</code>
              </td>
              <td v-for="id in columns" :key="id">
                <template v-if="mode === 'tags'"
                  ><select
                    v-if="
                      Object.keys(
                        network.policyMetadata?.tags[id]?.values || {},
                      ).length
                    "
                    :value="tagValue(m, id)"
                    :disabled="busy"
                    :aria-label="`${m.name || m.id}的${label(mode, id)}`"
                    @change="setTag(m, id, $event)"
                  >
                    <option value="">未设置</option>
                    <option
                      v-for="(name, value) in network.policyMetadata?.tags[id]
                        ?.values"
                      :key="value"
                      :value="value"
                    >
                      {{ name }} ({{ value }})
                    </option>
                    <option
                      v-if="
                        tagValue(m, id) !== '' &&
                        !network.policyMetadata?.tags[id]?.values?.[
                          String(tagValue(m, id))
                        ]
                      "
                      :value="tagValue(m, id)"
                    >
                      {{ tagValue(m, id) }}
                    </option></select
                  ><input
                    v-else
                    type="number"
                    min="0"
                    max="4294967295"
                    :value="tagValue(m, id)"
                    :disabled="busy"
                    :placeholder="
                      String(
                        network.tags.find((t) => t.id === id)?.default ??
                          '未设置',
                      )
                    "
                    :aria-label="`${m.name || m.id}的${label(mode, id)}`"
                    @input="setTag(m, id, $event)" /></template
                ><input
                  v-else
                  class="switch"
                  type="checkbox"
                  :checked="draftFor(m).capabilities.includes(id)"
                  :disabled="
                    busy ||
                    (!network.capabilities.some((c) => c.id === id) &&
                      !m.capabilities.includes(id))
                  "
                  :aria-label="`${m.name || m.id}的${label(mode, id)}`"
                  @change="setCapability(m, id, $event)"
                />
              </td>
              <td>
                <div class="row-actions">
                  <button
                    class="icon-button"
                    :disabled="busy || !drafts[m.id]"
                    title="保存此设备策略"
                    :aria-label="`保存${m.name || m.id}的策略`"
                    @click="saveMembers(m.id)"
                  >
                    <Check :size="17" /></button
                  ><button
                    class="icon-button"
                    :disabled="busy || !drafts[m.id]"
                    title="撤销此设备的更改"
                    :aria-label="`撤销${m.name || m.id}的策略更改`"
                    @click="delete drafts[m.id]"
                  >
                    <RotateCcw :size="17" />
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!members.length">
              <td :colspan="columns.length + 2" class="empty-cell">
                暂无匹配的成员
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty-state compact-empty">
        <Shield :size="26" />
        <h3>暂无可分配的策略</h3>
      </div>
      <footer v-if="columns.length" class="exempt-pagination">
        <span class="muted"
          >{{ filtered.length }} 台设备<span v-if="pages > 1">
            · 第 {{ page }} / {{ pages }} 页</span
          ></span
        >
        <div v-if="pages > 1">
          <button
            class="icon-button"
            :disabled="page === 1"
            title="上一页"
            aria-label="上一页策略成员"
            @click="page--"
          >
            <ChevronLeft :size="18" /></button
          ><button
            class="icon-button"
            :disabled="page === pages"
            title="下一页"
            aria-label="下一页策略成员"
            @click="page++"
          >
            <ChevronRight :size="18" />
          </button>
        </div>
      </footer>
    </section>
  </section>
  <Modal
    :open="!!definition"
    :title="definition?.kind === 'tags' ? '策略标签定义' : '能力定义'"
    wide
    @close="!busy && (definition = null)"
    ><form v-if="definition" @submit.prevent="saveDefinition">
      <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
      <div class="form-grid">
        <label class="field"
          >名称<input v-model="definition.name" maxlength="64" /></label
        ><label class="field"
          >ID<input
            v-model.number="definition.id"
            type="number"
            min="0"
            max="4294967295"
            :disabled="definition.existing !== null"
            required
        /></label>
      </div>
      <template v-if="definition.kind === 'tags'"
        ><label class="field"
          >默认值（可选）<input
            v-model.number="definition.defaultValue"
            type="number"
            min="0"
            max="4294967295"
            placeholder="不设置默认值"
        /></label>
        <h3>枚举值</h3>
        <div
          v-for="(value, index) in definition.values"
          :key="index"
          class="policy-enum-row"
        >
          <input
            v-model.number="value.value"
            type="number"
            min="0"
            max="4294967295"
            aria-label="枚举数值"
            placeholder="数值"
            required
          /><input
            v-model="value.name"
            aria-label="枚举名称"
            placeholder="名称"
            maxlength="64"
            required
          /><button
            type="button"
            class="icon-button danger-text"
            title="删除枚举值"
            aria-label="删除枚举值"
            @click="definition.values.splice(index, 1)"
          >
            <X :size="18" />
          </button>
        </div>
        <button
          type="button"
          class="button"
          @click="definition.values.push({ value: '', name: '' })"
        >
          <Plus :size="16" />添加枚举值
        </button>
        <p class="field-hint policy-note">
          成员未设置数值时，控制器可能按默认值补齐。修改默认值不会覆盖已有成员赋值。
        </p></template
      >
      <template v-else
        ><label class="check-label"
          ><input
            v-model="definition.defaultCap"
            type="checkbox"
          />默认分配给新成员</label
        ><label class="field"
          >规则格式<select v-model="definition.ruleMode">
            <option value="source">规则语言</option>
            <option value="json">原生 JSON</option>
          </select></label
        ><label class="field"
          >能力规则<textarea
            v-model="definition.body"
            class="code-editor"
            rows="10"
            spellcheck="false"
            placeholder="accept ipprotocol tcp and dport 22;"
            required
          ></textarea
          ><span class="field-hint"
            >最多 64 条匹配或动作；基础规则中的 drop 会阻止后续能力求值，break
            则允许继续。</span
          ></label
        ></template
      >
      <footer class="modal-actions">
        <button
          type="button"
          class="button"
          :disabled="busy"
          @click="definition = null"
        >
          取消</button
        ><button class="button primary" :disabled="busy">
          <Check :size="16" />保存定义
        </button>
      </footer>
    </form></Modal
  >
  <Modal
    :open="!!deletion"
    title="删除策略定义"
    @close="!busy && (deletion = null)"
    ><p>
      删除定义不会删除成员或修改授权。仍被成员或规则使用的标签，以及仍有成员分配的能力，需要先解除引用。
    </p>
    <div v-if="error" class="alert danger" role="alert">{{ error }}</div>
    <footer class="modal-actions">
      <button class="button" :disabled="busy" @click="deletion = null">
        取消</button
      ><button
        class="button destructive"
        :disabled="busy"
        @click="removeDefinition"
      >
        <Trash2 :size="16" />确认删除
      </button>
    </footer></Modal
  >
</template>
