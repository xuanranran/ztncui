import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import App from "./App.vue";
import "./style.css";

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: "/networks" },
    { path: "/networks", component: () => import("./views/Networks.vue") },
    { path: "/network/:id", component: () => import("./views/Network.vue") },
    { path: "/settings", component: () => import("./views/System.vue") },
    { path: "/:pathMatch(.*)*", redirect: "/networks" },
  ],
});
createApp(App).use(router).mount("#app");
