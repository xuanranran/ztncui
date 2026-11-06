<script lang="ts">
const openDialogs: symbol[] = [];
let documentOverflow = "";
</script>
<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import { X } from "@lucide/vue";
const props = defineProps<{
  title: string;
  open: boolean;
  wide?: boolean;
  centered?: boolean;
}>();
const emit = defineEmits<{ close: [] }>();
const root = ref<HTMLElement>();
let previous: HTMLElement | null = null;
const dialogID = Symbol();
let locked = false;
function key(event: KeyboardEvent) {
  if (!props.open || openDialogs.at(-1) !== dialogID) return;
  if (event.key === "Escape") emit("close");
  if (event.key !== "Tab") return;
  const list = [
    ...(root.value?.querySelectorAll<HTMLElement>(
      "button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href]",
    ) || []),
  ].filter((x) => x.offsetParent !== null);
  if (!list.length) return;
  if (event.shiftKey && document.activeElement === list[0]) {
    event.preventDefault();
    list.at(-1)?.focus();
  }
  if (!event.shiftKey && document.activeElement === list.at(-1)) {
    event.preventDefault();
    list[0]?.focus();
  }
}
function unlock() {
  if (!locked) return;
  const wasTop = openDialogs.at(-1) === dialogID;
  openDialogs.splice(openDialogs.indexOf(dialogID), 1);
  if (!openDialogs.length) document.body.style.overflow = documentOverflow;
  document.removeEventListener("keydown", key);
  if (wasTop) previous?.focus();
  locked = false;
}
watch(
  () => props.open,
  async (open) => {
    if (!open) return;
    if (!locked) {
      previous = document.activeElement as HTMLElement | null;
      if (!openDialogs.length) documentOverflow = document.body.style.overflow;
      openDialogs.push(dialogID);
      document.body.style.overflow = "hidden";
      document.addEventListener("keydown", key);
      locked = true;
    }
    await nextTick();
    (
      root.value?.querySelector<HTMLElement>(
        'input:not([type="checkbox"]),textarea',
      ) || root.value?.querySelector<HTMLElement>("button")
    )?.focus();
  },
  { immediate: true },
);
onBeforeUnmount(unlock);
</script>

<template>
  <Teleport to="body">
    <Transition name="dialog" appear @after-leave="unlock">
      <div v-if="open" class="modal-backdrop" @click.self="emit('close')">
        <section
          ref="root"
          class="modal"
          :class="{ wide, centered }"
          role="dialog"
          aria-modal="true"
          :aria-label="title"
        >
          <header class="modal-header">
            <h2>{{ title }}</h2>
            <button
              class="icon-button"
              aria-label="关闭"
              title="关闭"
              @click="emit('close')"
            >
              <X :size="20" />
            </button>
          </header>
          <div class="modal-content"><slot /></div>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>
