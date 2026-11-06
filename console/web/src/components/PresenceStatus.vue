<script setup lang="ts">
import type { Member } from "../types";
import { presenceClass, presenceLabel, presenceTitle } from "../presence";

defineProps<{ member: Member }>();
</script>

<template>
  <span class="status-label member-presence" :class="presenceClass(member)" :title="presenceTitle(member)">
    <span class="presence-dot" aria-hidden="true"></span>{{ presenceLabel(member) }}
  </span>
</template>
