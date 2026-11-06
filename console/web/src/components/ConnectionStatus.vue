<script setup lang="ts">
import { computed } from "vue";
import type { Member } from "../types";
import { connectionStatus } from "../presence";

const props = defineProps<{ member: Member }>();
const status = computed(() => connectionStatus(props.member));
</script>

<template>
  <div class="connection-stack" :class="'connection-' + status.kind">
    <span :title="status.title">
      {{ status.label }}<span v-if="status.latency !== null" class="latency">{{ status.kind === "ping" ? "约 " : "" }}{{ status.latency }} ms</span>
    </span>
    <small :title="status.title"><span v-if="status.cached">上次记录 · </span>{{ status.address }}</small>
  </div>
</template>
