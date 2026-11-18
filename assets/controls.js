(() => {
  "use strict";

  const phone = document.body.classList.contains("platform-android");
  const fields = new WeakMap();
  const svgNS = "http://www.w3.org/2000/svg";
  let menu;
  let frame = 0;
  let backdropDown = false;

  function icon(name) {
    const svg = document.createElementNS(svgNS, "svg");
    svg.classList.add("icon");
    svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("focusable", "false");
    const use = document.createElementNS(svgNS, "use");
    use.setAttribute("href", "/icons.svg#" + name);
    svg.append(use);
    return svg;
  }

  const popup = document.createElement("dialog");
  popup.className = "control-popup";
  popup.id = "control-popup";
  const header = document.createElement("header");
  header.className = "popup-header";
  const title = document.createElement("h2");
  title.id = "popup-title";
  const dismiss = document.createElement("button");
  dismiss.type = "button";
  dismiss.className = "icon-button";
  dismiss.setAttribute("aria-label", "关闭选项");
  dismiss.append(icon("close"));
  header.append(title, dismiss);
  const list = document.createElement("div");
  list.className = "popup-list";
  list.id = "popup-list";
  popup.append(header, list);
  popup.setAttribute("aria-labelledby", title.id);
  document.body.append(popup);

  function close(restoreFocus = true) {
    if (!menu) return false;
    const anchor = menu.anchor;
    menu = undefined;
    anchor.setAttribute("aria-expanded", "false");
    anchor.removeAttribute("aria-activedescendant");
    list.removeAttribute("aria-activedescendant");
    popup.close();
    if (restoreFocus && anchor.isConnected) anchor.focus({preventScroll: true});
    return true;
  }

  function findIndex(start, direction) {
    const length = menu.items.length;
    for (let i = 1; i <= length; i++) {
      const index = (start + direction * i + length * 2) % length;
      if (!menu.items[index].disabled) return index;
    }
    return -1;
  }

  function highlight(index, reveal) {
    if (index < 0) return;
    menu.active = index;
    const option = list.children[index];
    for (const child of list.children) child.toggleAttribute("data-active", child === option);
    const owner = phone || menu.action ? list : menu.anchor;
    owner.setAttribute("aria-activedescendant", option.id);
    if (reveal) option.scrollIntoView({block: "nearest"});
  }

  function choose(index) {
    const item = menu?.items[index];
    if (!item || item.disabled) return;
    close();
    item.pick();
  }

  function position() {
    if (!menu || phone) return;
    const {anchor, point} = menu;
    if (!anchor.isConnected || !anchor.getClientRects().length) {
      close(false);
      return;
    }
    const r = anchor.getBoundingClientRect();
    const viewport = window.visualViewport;
    const left = viewport?.offsetLeft || 0;
    const top = viewport?.offsetTop || 0;
    const right = left + (viewport?.width || document.documentElement.clientWidth);
    const bottom = top + (viewport?.height || window.innerHeight);
    const padding = 10;
    const gap = 7;
    const width = Math.min(Math.max(r.width, menu.action ? 210 : 175), right - left - 2 * padding);
    popup.style.width = width + "px";
    const height = list.scrollHeight + 2;
    const below = (point?.y ?? r.bottom) + gap;
    const above = (point?.y ?? r.top) - gap;
    const roomBelow = bottom - padding - below;
    const roomAbove = above - top - padding;
    const placeAbove = roomBelow < height && roomAbove > roomBelow;
    const fit = Math.min(height, Math.max(0, placeAbove ? roomAbove : roomBelow));
    list.style.maxHeight = Math.max(0, fit - 2) + "px";
    const align = point?.x ?? (menu.action ? r.right - width : r.left);
    const x = Math.min(Math.max(align, left + padding), right - padding - width);
    const y = Math.min(Math.max(placeAbove ? above - fit : below, top + padding), bottom - padding - fit);
    popup.style.left = x + "px";
    popup.style.top = y + "px";
  }

  function reposition() {
    if (!menu || phone || frame) return;
    frame = requestAnimationFrame(() => { frame = 0; position(); });
  }

  function open(anchor, label, items, action = false, point) {
    if (!items.some(item => !item.disabled) || anchor.disabled) return;
    if (menu?.anchor === anchor && !point) {
      close();
      return;
    }
    close(false);
    backdropDown = false;
    menu = {anchor, items, action, point, active: -1, search: "", typedAt: 0};
    title.textContent = label;
    list.setAttribute("aria-labelledby", title.id);
    list.setAttribute("role", action ? "menu" : "listbox");
    list.tabIndex = phone || action ? 0 : -1;
    list.style.maxHeight = "";
    list.replaceChildren(...items.map((item, index) => {
      const row = document.createElement("div");
      row.className = "popup-item" + (item.danger ? " destructive" : "");
      row.id = "popup-item-" + index;
      row.setAttribute("role", action ? "menuitem" : "option");
      if (item.disabled) row.setAttribute("aria-disabled", "true");
      if (!action) row.setAttribute("aria-selected", String(Boolean(item.selected)));
      if (item.icon) row.append(icon(item.icon));
      const text = document.createElement("span");
      text.textContent = item.label;
      row.append(text);
      if (item.selected) row.append(icon("check"));
      row.addEventListener("pointermove", event => {
        if (event.pointerType !== "touch" && !item.disabled && menu?.active !== index) highlight(index, false);
      });
      row.addEventListener("click", () => choose(index));
      return row;
    }));
    if (phone) popup.showModal();
    else popup.show();
    anchor.setAttribute("aria-expanded", "true");
    position();
    (phone || action ? list : anchor).focus({preventScroll: true});
    const selected = items.findIndex(item => item.selected && !item.disabled);
    highlight(selected < 0 ? findIndex(-1, 1) : selected, true);
  }

  function menuKeys(event) {
    if (!menu || event.isComposing) return;
    const key = event.key;
    let handled = true;
    switch (key) {
      case "Escape": close(); break;
      case "ArrowDown": highlight(findIndex(menu.active, 1), true); break;
      case "ArrowUp": highlight(findIndex(menu.active, -1), true); break;
      case "Home": highlight(findIndex(-1, 1), true); break;
      case "End": highlight(findIndex(0, -1), true); break;
      case "Enter": case " ":
        if (event.target === dismiss) close();
        else choose(menu.active);
        break;
      case "Tab":
        if (!phone) {
          const anchor = menu.anchor;
          close(false);
          anchor.focus({preventScroll: true});
        }
        return;
      default:
        handled = false;
        if (key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
          const now = Date.now();
          const keyText = key.toLocaleLowerCase();
          menu.search = now - menu.typedAt < 600 ? menu.search + keyText : keyText;
          menu.typedAt = now;
          const prefix = menu.search;
          const count = menu.items.length;
          for (let i = 0; i < count; i++) {
            const index = ((prefix.length > 1 ? menu.active : menu.active + 1) + i + count) % count;
            if (!menu.items[index].disabled && menu.items[index].label.toLocaleLowerCase().startsWith(prefix)) {
              highlight(index, true);
              break;
            }
          }
          handled = true;
        }
    }
    if (handled) {
      event.preventDefault();
      event.stopPropagation();
    }
  }

  function syncSelect(select) {
    const field = fields.get(select);
    if (!field) return;
    field.trigger.disabled = select.disabled;
    field.value.textContent = select.selectedOptions[0]?.label || field.label;
    if (select.disabled && menu?.anchor === field.trigger) close(false);
  }

  function enhanceSelect(select) {
    const labelNode = document.querySelector('label[for="' + select.id + '"]');
    const label = select.getAttribute("aria-label") || labelNode?.textContent.trim() || select.id;
    const wrap = document.createElement("div");
    wrap.className = "select-control";
    const trigger = document.createElement("button");
    trigger.type = "button";
    trigger.id = select.id + "-control";
    trigger.setAttribute("role", "combobox");
    trigger.setAttribute("aria-haspopup", "listbox");
    trigger.setAttribute("aria-expanded", "false");
    trigger.setAttribute("aria-controls", list.id);
    const name = document.createElement("span");
    name.className = "visually-hidden";
    name.id = select.id + "-control-label";
    name.textContent = label;
    const value = document.createElement("span");
    value.id = select.id + "-control-value";
    trigger.setAttribute("aria-labelledby", name.id + " " + value.id);
    trigger.append(name, value, icon("down"));
    select.before(wrap);
    wrap.append(select, trigger);
    select.hidden = true;
    select.setAttribute("aria-hidden", "true");
    if (labelNode) labelNode.htmlFor = trigger.id;
    fields.set(select, {trigger, value, label});
    const show = () => {
      const batch = select.id === "batch-action";
      const options = [...select.options].filter(option => !option.hidden && (!batch || option.value)).map(option => ({
        label: option.label,
        disabled: option.disabled,
        selected: option.selected && !batch,
        pick() {
          if (select.value === option.value) return;
          select.value = option.value;
          select.dispatchEvent(new Event("change", {bubbles: true}));
        },
      }));
      open(trigger, label, options);
    };
    trigger.addEventListener("click", show);
    trigger.addEventListener("keydown", event => {
      if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
        event.preventDefault();
        event.stopPropagation();
        show();
        if (event.key === "Home") highlight(findIndex(-1, 1), true);
        if (event.key === "End") highlight(findIndex(0, -1), true);
      }
    });
    select.addEventListener("change", () => syncSelect(select));
    syncSelect(select);
  }

  document.addEventListener("pointerdown", event => {
    if (menu && !phone && !popup.contains(event.target) && !menu.anchor.contains(event.target)) close(false);
  }, true);
  document.addEventListener("keydown", menuKeys, true);
  window.addEventListener("resize", reposition);
  window.addEventListener("scroll", reposition, {capture: true, passive: true});
  window.visualViewport?.addEventListener("resize", reposition);
  window.visualViewport?.addEventListener("scroll", reposition);
  popup.addEventListener("cancel", event => { event.preventDefault(); close(); });
  popup.addEventListener("pointerdown", event => {
    const r = popup.getBoundingClientRect();
    backdropDown = phone && event.target === popup &&
      (event.clientX < r.left || event.clientX > r.right || event.clientY < r.top || event.clientY > r.bottom);
  });
  popup.addEventListener("click", () => { if (backdropDown) close(); });
  dismiss.addEventListener("click", () => close());
  document.querySelectorAll("select[data-styled]").forEach(enhanceSelect);
  window.WordReviewControls = {
    icon, syncSelect, close,
    context(anchor, label, items, point) { open(anchor, label, items, true, point); },
    isOpen: () => Boolean(menu),
  };
})();
