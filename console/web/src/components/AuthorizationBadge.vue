<script setup lang="ts">
import { CircleCheck, CircleX } from "@lucide/vue";

defineProps<{ authorized: boolean }>();
</script>

<template>
  <span
    class="authorization-badge"
    :class="authorized ? 'authorized' : 'unauthorized'"
  >
    <component :is="authorized ? CircleCheck : CircleX" :size="15" aria-hidden="true" />
    {{ authorized ? "已授权" : "未授权" }}
  </span>
</template>
