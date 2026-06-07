const state = {
  targetIp: "",
  target: "",
  running: false,
  dockVisible: true,
  progress: {
    scanned: 0,
    total: 0,
    phase: "idle",
    discovered: 0
  },
  sites: new Map(),
  cardNodes: new Map()
};

const elements = {
  settingsButton: document.getElementById("settings-button"),
  settingsModal: document.getElementById("settings-modal"),
  settingsBackdrop: document.getElementById("settings-backdrop"),
  settingsClose: document.getElementById("settings-close"),
  targetInput: document.getElementById("target-input"),
  targetButton: document.getElementById("target-button"),
  targetHint: document.getElementById("target-hint"),
  scanPhase: document.getElementById("scan-phase"),
  progressFill: document.getElementById("progress-fill"),
  progressText: document.getElementById("progress-text"),
  siteGrid: document.getElementById("site-grid"),
  emptyState: document.getElementById("empty-state"),
  statusPill: document.getElementById("status-pill"),
  rescanButton: document.getElementById("rescan-button"),
  template: document.getElementById("site-card-template")
};

let hideDockTimer = null;

function formatDate(value) {
  if (!value) {
    return "暂无记录";
  }
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

function phaseLabel(phase) {
  const map = {
    idle: "空闲",
    preparing: "准备中",
    "history-and-common": "优先扫描历史和常见端口",
    "full-range": "扫描剩余端口",
    done: "扫描完成"
  };
  return map[phase] || phase;
}

function setHint(text, isError = false) {
  elements.targetHint.textContent = text;
  elements.targetHint.classList.toggle("error", isError);
}

function openSettings() {
  elements.settingsModal.hidden = false;
  elements.targetInput.value = state.target || "";
  elements.targetInput.focus();
}

function closeSettings() {
  elements.settingsModal.hidden = true;
}

function updateProgress(snapshot) {
  state.targetIp = snapshot.targetIp || state.targetIp;
  state.target = snapshot.target || snapshot.targetIp || state.target;
  state.running = Boolean(snapshot.running);
  state.progress = snapshot.progress || state.progress;

  const total = Math.max(state.progress.total || 0, 1);
  const percent = Math.min(100, Math.round(((state.progress.scanned || 0) / total) * 100));

  elements.scanPhase.textContent = phaseLabel(state.progress.phase);
  elements.progressFill.style.width = `${percent}%`;
  elements.progressText.textContent = `${state.progress.scanned || 0} / ${state.progress.total || 0} 端口`;
  elements.statusPill.textContent = state.running ? "扫描中" : "空闲";
  elements.statusPill.className = `status-pill ${state.running ? "running" : state.progress.phase === "done" ? "done" : "idle"}`;
  updateDockVisibility();

  if (!elements.targetHint.classList.contains("error")) {
    setHint(`当前扫描目标: ${state.targetIp || state.target || "未设置"}`);
  }
}

function updateDockVisibility() {
  if (hideDockTimer) {
    clearTimeout(hideDockTimer);
    hideDockTimer = null;
  }

  const isDone = !state.running && state.progress.phase === "done";
  if (state.running || state.progress.scanned > 0 || state.progress.phase === "preparing") {
    state.dockVisible = true;
  }

  elements.progressText.parentElement.classList.toggle("is-hidden", false);
  elements.progressText.parentElement.classList.toggle("is-visible", state.dockVisible);

  if (isDone) {
    hideDockTimer = window.setTimeout(() => {
      state.dockVisible = false;
      elements.progressText.parentElement.classList.remove("is-visible");
      elements.progressText.parentElement.classList.add("is-hidden");
    }, 1600);
  }
}

function sortSites() {
  return [...state.sites.values()].sort((a, b) => new Date(b.lastSeenAt).getTime() - new Date(a.lastSeenAt).getTime());
}

function createCard(site) {
  const fragment = elements.template.content.cloneNode(true);
  const card = fragment.querySelector(".site-card");
  const icon = fragment.querySelector(".site-icon");
  const title = fragment.querySelector(".site-title");
  const url = fragment.querySelector(".site-url");
  const port = fragment.querySelector(".site-port");
  const code = fragment.querySelector(".site-code");
  const type = fragment.querySelector(".site-type");
  const seen = fragment.querySelector(".site-seen");

  card.dataset.id = site.id;
  card.href = site.url;
  icon.src = site.iconUrl;
  icon.alt = `${site.title} 图标`;
  icon.onerror = () => {
    icon.onerror = null;
    icon.src = "data:image/svg+xml;charset=utf-8,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'%3E%3Crect width='64' height='64' rx='14' fill='%23132238'/%3E%3Cpath d='M18 22.5c0-2.5 2-4.5 4.5-4.5h19c2.5 0 4.5 2 4.5 4.5v13c0 2.5-2 4.5-4.5 4.5h-8.5l-5 6.5c-.7.8-2 .3-2-.8V40h-3.5c-2.5 0-4.5-2-4.5-4.5v-13Z' fill='%2368d5ff'/%3E%3C/svg%3E";
  };
  title.textContent = site.title;
  url.textContent = site.url;
  port.textContent = `端口 ${site.port}`;
  code.textContent = site.statusCode ? `HTTP ${site.statusCode}` : "已发现";
  type.textContent = site.scheme.toUpperCase();
  seen.textContent = `最近发现 ${formatDate(site.lastSeenAt)}`;

  return card;
}

function updateCard(card, site) {
  const icon = card.querySelector(".site-icon");
  const title = card.querySelector(".site-title");
  const url = card.querySelector(".site-url");
  const port = card.querySelector(".site-port");
  const code = card.querySelector(".site-code");
  const type = card.querySelector(".site-type");
  const seen = card.querySelector(".site-seen");

  card.href = site.url;
  title.textContent = site.title;
  url.textContent = site.url;
  port.textContent = `端口 ${site.port}`;
  code.textContent = site.statusCode ? `HTTP ${site.statusCode}` : "已发现";
  type.textContent = site.scheme.toUpperCase();
  seen.textContent = `最近发现 ${formatDate(site.lastSeenAt)}`;
  if (icon.src !== site.iconUrl) {
    icon.src = site.iconUrl;
  }
  icon.alt = `${site.title} 图标`;
}

function renderSites() {
  const sortedSites = sortSites();
  const liveIds = new Set(sortedSites.map((site) => site.id));

  for (const [id, card] of state.cardNodes.entries()) {
    if (!liveIds.has(id)) {
      card.remove();
      state.cardNodes.delete(id);
    }
  }

  sortedSites.forEach((site, index) => {
    let card = state.cardNodes.get(site.id);
    if (!card) {
      card = createCard(site);
      state.cardNodes.set(site.id, card);
    } else {
      updateCard(card, site);
    }

    const currentChild = elements.siteGrid.children[index];
    if (currentChild !== card) {
      elements.siteGrid.insertBefore(card, currentChild || null);
    }
  });

  elements.emptyState.hidden = sortedSites.length > 0;
}

function syncSnapshot(snapshot) {
  updateProgress(snapshot);

  if (Array.isArray(snapshot.sites)) {
    const incomingIds = new Set();
    for (const site of snapshot.sites) {
      incomingIds.add(site.id);
      state.sites.set(site.id, site);
    }
    for (const id of [...state.sites.keys()]) {
      if (!incomingIds.has(id)) {
        state.sites.delete(id);
      }
    }
    renderSites();
  }
}

function upsertSite(site) {
  state.sites.set(site.id, site);
  state.progress.discovered = state.sites.size;
  renderSites();
}

function removeSite(site) {
  state.sites.delete(site.id);
  state.progress.discovered = state.sites.size;
  renderSites();
}

async function loadInitialState() {
  const response = await fetch("/api/state", { cache: "no-store" });
  const snapshot = await response.json();
  syncSnapshot(snapshot);
}

function connectEvents() {
  const events = new EventSource("/api/events");
  events.addEventListener("hello", (event) => {
    syncSnapshot(JSON.parse(event.data));
  });
  events.addEventListener("status", (event) => {
    syncSnapshot(JSON.parse(event.data));
  });
  events.addEventListener("progress", (event) => {
    updateProgress(JSON.parse(event.data));
  });
  events.addEventListener("site-found", (event) => {
    upsertSite(JSON.parse(event.data));
  });
  events.addEventListener("site-removed", (event) => {
    removeSite(JSON.parse(event.data));
  });
}

async function applyTarget() {
  const target = elements.targetInput.value.trim();
  try {
    const response = await fetch("/api/target", {
      method: "POST",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify({
        target,
        rescan: true
      })
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "设置目标失败");
    }
    setHint(`当前扫描目标: ${payload.targetIp || payload.target}`);
    syncSnapshot(payload);
    closeSettings();
  } catch (error) {
    setHint(error.message || "设置目标失败", true);
  }
}

elements.settingsButton.addEventListener("click", openSettings);
elements.settingsClose.addEventListener("click", closeSettings);
elements.settingsBackdrop.addEventListener("click", closeSettings);

elements.rescanButton.addEventListener("click", async () => {
  setHint(`当前扫描目标: ${state.targetIp || state.target || "未设置"}`);
  await fetch("/api/scan", {
    method: "POST"
  });
  closeSettings();
});

elements.targetButton.addEventListener("click", applyTarget);
elements.targetInput.addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    event.preventDefault();
    applyTarget();
  }
  if (event.key === "Escape") {
    closeSettings();
  }
});

document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && !elements.settingsModal.hidden) {
    closeSettings();
  }
});

loadInitialState().catch((error) => {
  elements.progressText.textContent = `初始化失败: ${error.message}`;
});
connectEvents();
