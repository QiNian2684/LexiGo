(() => {
  "use strict";

  const root = document.documentElement;
  const systemMedia = window.matchMedia("(prefers-color-scheme: dark)");
  const bridge = window.WordReviewFiles;
  const windowsTheme = Boolean(root.dataset.systemTheme);
  const colors = {light: "#f5f7fa", dark: "#16181b"};
  let preferred = root.dataset.themePreference || "system";
  let saved = preferred;
  let system = root.dataset.systemTheme;
  let pending = Promise.resolve();
  let revision = 0;
  let checking = false;

  function effectiveTheme() {
    if (preferred !== "system") return preferred;
    if (bridge?.isSystemDark) return bridge.isSystemDark() ? "dark" : "light";
    return system || (systemMedia.matches ? "dark" : "light");
  }

  function apply() {
    const theme = effectiveTheme();
    root.dataset.theme = theme;
    root.dataset.themePreference = preferred;
    document.querySelector('meta[name="theme-color"]')?.setAttribute("content", colors[theme]);
    bridge?.applyAppearance?.(theme === "dark");
    window.wordReviewWindowAppearance?.(theme === "dark").catch(() => {});
  }

  async function updateSystem() {
    if (preferred !== "system") return;
    if (!windowsTheme) {
      apply();
      return;
    }
    if (checking || document.hidden) return;
    checking = true;
    try {
      const reply = await fetch("/api/appearance", {cache: "no-store"});
      if (reply.ok) {
        const value = (await reply.json()).system;
        system = value === "dark" || value === "light" ? value : "";
        if (preferred === "system") apply();
      }
    } catch { /* A process that is closing has no system theme response. */ }
    finally { checking = false; }
  }

  apply();
  window.wordReviewSystemThemeChanged = () => { if (preferred === "system") apply(); };
  if (systemMedia.addEventListener) systemMedia.addEventListener("change", updateSystem);
  else systemMedia.addListener(updateSystem);
  window.addEventListener("focus", updateSystem);
  document.addEventListener("visibilitychange", updateSystem);
  if (windowsTheme) setInterval(updateSystem, 1800);

  document.addEventListener("DOMContentLoaded", () => {
    const choices = [...document.querySelectorAll('input[name="appearance-theme"]')];
    const status = document.getElementById("theme-status");
    if (!choices.length || !status) return;
    const reflect = () => choices.forEach(input => { input.checked = input.value === preferred; });
    const report = text => { status.textContent = text; status.hidden = !text; };
    reflect();
    for (const input of choices) input.addEventListener("change", () => {
      if (!input.checked || input.value === preferred) return;
      const mode = input.value;
      const requested = ++revision;
      preferred = mode;
      report("");
      apply();
      if (mode === "system") updateSystem();
      pending = pending.then(async () => {
        try {
          const response = await fetch("/api/appearance", {
            method: "POST",
            headers: {"Content-Type": "application/json"},
            body: JSON.stringify({theme: mode}),
            keepalive: true,
          });
          if (!response.ok) throw new Error((await response.json()).error);
          saved = mode;
        } catch (error) {
          if (requested !== revision) return;
          preferred = saved;
          reflect();
          apply();
          report(error.message?.includes("文件夹") ? error.message : "外观主题保存失败，请重试。");
        }
      });
    });
  });
})();
