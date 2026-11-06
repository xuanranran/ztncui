<script setup lang="ts">
import { computed } from "vue";
import { Monitor, Router, Server, Smartphone } from "@lucide/vue";
import { faWindows, faApple, faAndroid, faLinux, faDocker, faFreebsd } from "@fortawesome/free-brands-svg-icons";
import { deviceIconOptions, resolveDeviceIcon } from "../device-icons";

const props = defineProps<{ icon?: string; name?: string; tags?: string[] }>();
const kind = computed(() => resolveDeviceIcon(props.icon, props.name || "", props.tags));
const brands = { windows: faWindows, apple: faApple, android: faAndroid, linux: faLinux, docker: faDocker, freebsd: faFreebsd };
const brand = computed(() => brands[kind.value as keyof typeof brands]);
const label = computed(() => deviceIconOptions.find(option => option.value === kind.value)?.label || "电脑");
</script>

<template>
  <span class="device-symbol" :class="{ gateway: kind === 'openwrt' }" :title="label">
    <svg v-if="brand" :viewBox="`0 0 ${brand.icon[0]} ${brand.icon[1]}`" width="21" height="21" fill="currentColor" aria-hidden="true"><path :d="brand.icon[4] as string" /></svg>
    <component v-else :is="kind === 'openwrt' ? Router : kind === 'nas' ? Server : kind === 'phone' ? Smartphone : Monitor" :size="21" aria-hidden="true" />
  </span>
</template>
