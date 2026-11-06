import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

export default defineConfig({
  plugins: [vue()],
  build: { outDir: "../ui/dist", emptyOutDir: true },
  server: {
    port: 5174,
    strictPort: true,
    proxy: { "/api": "http://127.0.0.1:3101" },
  },
});
