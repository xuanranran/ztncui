const saved = localStorage.getItem("ztncui:theme") || "system";
document.documentElement.dataset.theme =
  saved === "system"
    ? matchMedia("(prefers-color-scheme: dark)").matches
      ? "dark"
      : "light"
    : saved;
