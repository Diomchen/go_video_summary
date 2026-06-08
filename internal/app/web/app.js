const rootEl = document.documentElement;
const bannerStageEl = document.getElementById("banner-stage");
const tasksEl = document.getElementById("tasks");
const healthEl = document.getElementById("health");
const taskStatsEl = document.getElementById("task-stats");
const taskSearchEl = document.getElementById("task-search");
const taskResultsInfoEl = document.getElementById("task-results-info");
const themeToggleEl = document.getElementById("theme-toggle");

const tasks = new Map();
const expandedTaskIds = new Set();
const viewOrder = ["status", "submit", "tasks", "settings", "api"];

const THEME_KEY = "whisper-console-theme";

let healthState = null;
let currentFilter = "all";
let currentSearch = "";
let currentView = "status";
let bilibiliLoginPollTimer = null;
let pendingAfterLogin = null;
let settingsState = null;

let currentPage = 1;
const pageSize = 50;
let renderScheduled = false;

async function fetchJSON(url, options) {
  const resp = await fetch(url, options);
  const data = await resp.json();
  if (!resp.ok) {
    throw new Error(data.error || "Request failed");
  }
  return data;
}

function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;"
  }[char]));
}

function formatDurationMs(value) {
  const ms = Number(value || 0);
  if (!Number.isFinite(ms) || ms <= 0) {
    return "";
  }
  if (ms < 1000) {
    return `${Math.round(ms)} ms`;
  }

  const seconds = ms / 1000;
  if (seconds < 60) {
    return `${seconds.toFixed(seconds >= 10 ? 1 : 2)} s`;
  }

  const minutes = Math.floor(seconds / 60);
  const remainSeconds = seconds % 60;
  return `${minutes}m ${remainSeconds.toFixed(remainSeconds >= 10 ? 0 : 1)}s`;
}

function isCollectionURL(url) {
  return /space\.bilibili\.com\/\d+\/lists\/\d+/.test(url)
    || /bilibili\.com\/medialist\/play\/\d+/.test(url)
    || /bilibili\.com\/playlist\/pl\d+/.test(url)
    || /bilibili\.com\/watchlater(?:\/list)?/.test(url);
}

function isWatchLaterURL(url) {
  return /bilibili\.com\/watchlater(?:\/list)?/.test(url);
}

function platformLabel(name) {
  return ({ markdown: "Markdown", notion: "Notion", obsidian: "Obsidian", ima: "IMA" })[name] || name;
}

function taskStatusLabel(status) {
  return {
    queued: "排队中",
    running: "进行中",
    completed: "已完成",
    failed: "失败"
  }[status] || status;
}

function modeLabel(mode) {
  return {
    file: "文件任务",
    url: "B 站链接"
  }[mode] || mode || "任务";
}

function availablePlatformNames() {
  const platforms = (healthState && healthState.exportPlatforms) || {};
  return ["markdown", "notion", "obsidian", "ima"].filter((name) => Boolean(platforms[name]));
}

function selectedExportTargets(form) {
  return Array.from(form.querySelectorAll('input[name="exportTarget"]:checked')).map((input) => input.value);
}

function exportPathOptions(form) {
  return {
    markdownExportDir: (form.querySelector('input[name="markdownExportDir"]')?.value || "").trim(),
    obsidianExportDir: (form.querySelector('input[name="obsidianExportDir"]')?.value || "").trim()
  };
}

function renderExportOptions(containerId) {
  const container = document.getElementById(containerId);
  const platforms = (healthState && healthState.exportPlatforms) || {};
  const names = ["markdown", "notion", "obsidian", "ima"];

  container.innerHTML = names.map((name) => {
    const enabled = Boolean(platforms[name]);
    return `
      <label class="export-check ${enabled ? "" : "disabled"}">
        <input name="exportTarget" type="checkbox" value="${name}" ${enabled ? "" : "disabled"}>
        <span>
          <strong>${platformLabel(name)}</strong>
          <small>${enabled ? "已启用" : "未配置"}</small>
        </span>
      </label>
    `;
  }).join("");
}

function applyTheme(theme) {
  rootEl.setAttribute("data-theme", theme);
  window.localStorage.setItem(THEME_KEY, theme);
  themeToggleEl.textContent = theme === "dark" ? "切换浅色" : "切换深色";
}

function initializeTheme() {
  const stored = window.localStorage.getItem(THEME_KEY);
  const preferred = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
  applyTheme(stored || preferred);
}

function panelFor(view) {
  return document.querySelector(`.banner-panel[data-panel="${view}"]`);
}

function updateStageHeight() {
  const activePanel = panelFor(currentView);
  if (!activePanel) {
    return;
  }

  const scrollBody = activePanel.querySelector(".banner-panel-scroll");
  const targetHeight = scrollBody ? scrollBody.scrollHeight : activePanel.scrollHeight;
  bannerStageEl.style.height = `${Math.max(targetHeight, 0)}px`;
}

function setView(view, instant = false) {
  if (view === currentView && !instant) {
    return;
  }

  const previousView = currentView;
  const previousPanel = panelFor(previousView);
  const nextPanel = panelFor(view);
  currentView = view;

  document.querySelectorAll(".view-tab").forEach((button) => {
    button.classList.toggle("active", button.getAttribute("data-view") === view);
  });

  if (!nextPanel) {
    return;
  }

  const movingForward = viewOrder.indexOf(view) >= viewOrder.indexOf(previousView);

  if (previousPanel && previousPanel !== nextPanel && !instant) {
    previousPanel.classList.remove("active");
    previousPanel.classList.toggle("to-left", movingForward);
    nextPanel.classList.remove("to-left");
    nextPanel.classList.add("active");
    window.setTimeout(() => previousPanel.classList.remove("to-left"), 340);
  } else {
    document.querySelectorAll(".banner-panel").forEach((panel) => {
      panel.classList.toggle("active", panel === nextPanel);
      panel.classList.remove("to-left");
    });
  }

  window.requestAnimationFrame(updateStageHeight);
}

function setSource(source) {
  document.querySelectorAll(".sub-tab").forEach((button) => {
    button.classList.toggle("active", button.getAttribute("data-source") === source);
  });

  document.querySelectorAll(".source-panel").forEach((panel) => {
    panel.classList.toggle("active", panel.getAttribute("data-source-panel") === source);
  });

  if (currentView === "submit") {
    window.requestAnimationFrame(updateStageHeight);
  }
}

function shouldAutoExpand(task) {
  return task.status === "running" || task.status === "failed" || Boolean(task.error);
}

function upsertTask(task) {
  const existing = tasks.get(task.id);
  if (existing) {
    // Merge SSE summary fields onto existing full task, preserving heavy fields
    for (const key of Object.keys(task)) {
      if (task[key] !== undefined) {
        existing[key] = task[key];
      }
    }
    // Clear fields that are omitted by Go's omitempty when empty
    for (const key of ["error", "summaryError"]) {
      if (!(key in task)) {
        existing[key] = "";
      }
    }
  } else {
    tasks.set(task.id, task);
    if (shouldAutoExpand(task)) {
      expandedTaskIds.add(task.id);
    }
  }
}

function matchesSearch(task, query) {
  if (!query) {
    return true;
  }

  const text = [
    task.id,
    task.name,
    task.status,
    task.mode,
    task.originalFileName,
    task.sourceUrl,
    task.stage,
    task.authorName,
    task.collectionName,
    (task.domainTags || []).join(" ")
  ].join(" ").toLowerCase();

  return text.includes(query);
}

function filteredTasks() {
  const query = currentSearch.trim().toLowerCase();
  return Array.from(tasks.values())
    .sort((a, b) => new Date(b.updatedAt || 0) - new Date(a.updatedAt || 0))
    .filter((task) => currentFilter === "all" || task.status === currentFilter)
    .filter((task) => matchesSearch(task, query));
}

function renderHealth() {
  if (!healthState) {
    healthEl.innerHTML = '<p class="empty">正在加载系统状态...</p>';
    return;
  }

  const whisperMode = healthState.whisperBackend === "local"
    ? `本地 (${healthState.whisperLocalBin || "whisper"})`
    : `OpenAI (${healthState.whisper || ""})`;

  const items = [
    ["HTTP", healthState.http || "-"],
    ["Whisper", whisperMode],
    ["模型", healthState.whisperLocalModel || "未配置"],
    ["GPU", healthState.whisperLocalNoGPU ? "禁用" : "启用"],
    ["任务并发", String(healthState.taskWorkers || 0)],
    ["总结并发", String(healthState.summaryWorkers || 0)],
    ["自动保存", healthState.autoSaveResults ? "开启" : "关闭"],
    ["输出目录", healthState.outputDir || "未配置"],
    ["导出平台", availablePlatformNames().map(platformLabel).join(" / ") || "未配置"]
  ];

  healthEl.innerHTML = items.map(([label, value]) => `
    <article class="status-item">
      <span class="status-label">${escapeHtml(label)}</span>
      <strong>${escapeHtml(value)}</strong>
    </article>
  `).join("");
}

function renderTaskStats() {
  const list = Array.from(tasks.values());
  const total = list.length;
  const running = list.filter((task) => task.status === "running").length;
  const queued = list.filter((task) => task.status === "queued").length;
  const summaries = list.filter((task) => task.summary).length;

  taskStatsEl.innerHTML = [
    ["总任务数", total],
    ["进行中", running],
    ["排队中", queued],
    ["已有总结", summaries]
  ].map(([label, value]) => `
    <article class="metric-card">
      <span class="metric-label">${escapeHtml(label)}</span>
      <strong class="metric-value">${escapeHtml(value)}</strong>
    </article>
  `).join("");
}

function renderSavedFiles(task) {
  if (!task.savedFiles || !task.savedFiles.length) {
    return "";
  }

  const items = task.savedFiles.map((file) => `<li>${escapeHtml(file)}</li>`).join("");
  return `
    <details class="detail-card">
      <summary>已保存文件</summary>
      <div class="detail-body">
        <ul class="saved-files">${items}</ul>
      </div>
    </details>
  `;
}

function renderTimingMetrics(task) {
  const metrics = task.metrics;
  if (!metrics) {
    return "";
  }

  const items = [
    ["Pre-LLM", formatDurationMs(metrics.preLlmProcessingDurationMs)],
    ["Translate", formatDurationMs(metrics.translationDurationMs)],
    ["Summary", formatDurationMs(metrics.summaryDurationMs)],
    ["Task Total", formatDurationMs(metrics.totalTaskDurationMs)]
  ].filter(([, value]) => Boolean(value));

  if (!items.length) {
    return "";
  }

  return `
    <details class="detail-card" open>
      <summary>Timing Metrics</summary>
      <div class="detail-body">
        <ul class="exports-list">
          ${items.map(([label, value]) => `<li><strong>${escapeHtml(label)}</strong>: ${escapeHtml(value)}</li>`).join("")}
        </ul>
      </div>
    </details>
  `;
}

function renderExportActions(task, selected) {
  if (!task.summary || !selected.length) {
    return "";
  }

  return `
    <div class="task-actions">
      ${selected.map((name) => `
        <button class="ghost-btn" data-action="retry-export" data-task-id="${escapeHtml(task.id)}" data-export-target="${escapeHtml(name)}">
          重试导出到 ${escapeHtml(platformLabel(name))}
        </button>
      `).join("")}
    </div>
  `;
}

function renderExports(task) {
  const selected = (task.exportTargets && task.exportTargets.length)
    ? task.exportTargets
    : (task.summary ? availablePlatformNames() : []);

  if (!selected.length && (!task.exports || !task.exports.length)) {
    return "";
  }

  const resultMap = new Map((task.exports || []).map((item) => [item.name, item]));
  const items = selected.map((name) => {
    const item = resultMap.get(name) || { name, status: "pending" };
    const extra = item.target
      ? ` - ${escapeHtml(item.target)}`
      : item.error
        ? ` - ${escapeHtml(item.error)}`
        : "";

    return `<li><strong>${escapeHtml(platformLabel(name))}</strong>：${escapeHtml(item.status)}${extra}</li>`;
  }).join("");

  return `
    ${renderExportActions(task, selected)}
    <details class="detail-card" open>
      <summary>导出结果</summary>
      <div class="detail-body">
        <ul class="exports-list">${items}</ul>
      </div>
    </details>
  `;
}

function taskSummaryActions(task) {
  const actions = [];

  if (task.transcript && !task.summary) {
    actions.push(`
      <button class="ghost-btn" data-action="generate-summary" data-task-id="${escapeHtml(task.id)}">
        生成总结
      </button>
    `);
  }

  if (task.summaryError && !task.summary) {
    actions.push(`
      <button class="ghost-btn" data-action="retry-summary" data-task-id="${escapeHtml(task.id)}">
        重试总结
      </button>
    `);
  }

  return actions.join("");
}

function renderTaskDetails(task) {
  const details = [];

  if (task.transcript) {
    details.push(`
      <details class="detail-card">
        <summary>转写文本</summary>
        <div class="detail-body"><pre>${escapeHtml(task.transcript)}</pre></div>
      </details>
    `);
  }

  if (task.translatedText) {
    details.push(`
      <details class="detail-card">
        <summary>翻译文本</summary>
        <div class="detail-body"><pre>${escapeHtml(task.translatedText)}</pre></div>
      </details>
    `);
  }

  if (task.summary) {
    details.push(`
      <details class="detail-card" open>
        <summary>总结 Markdown</summary>
        <div class="detail-body"><pre>${escapeHtml(task.summary)}</pre></div>
      </details>
    `);
  }

  return details.join("");
}

function renderSegments(task) {
  const segs = (task.segments || []).slice(-3).reverse().map((seg) => `
    <article class="segment">
      <div class="segment-head">${escapeHtml(seg.start || "")}${seg.end ? ` - ${escapeHtml(seg.end)}` : ""}</div>
      <div class="segment-body">${escapeHtml(seg.text)}</div>
      ${seg.translated ? `<div class="translated">${escapeHtml(seg.translated)}</div>` : ""}
    </article>
  `).join("");

  return segs || '<p class="empty">还没有可展示的最新分段...</p>';
}

function renderMetaLines(task) {
  const lines = [];

  if (task.stage) {
    lines.push(`<div class="meta-line">阶段：${escapeHtml(task.stage)}</div>`);
  }

  if (task.totalChunks) {
    lines.push(`<div class="meta-line">断点进度：${escapeHtml(task.completedChunks || 0)} / ${escapeHtml(task.totalChunks)}</div>`);
  }

  if (task.exportTargets && task.exportTargets.length) {
    lines.push(`<div class="meta-line">导出目标：${escapeHtml(task.exportTargets.map(platformLabel).join(" / "))}</div>`);
  }

  if (task.markdownExportDir) {
    lines.push(`<div class="meta-line">Markdown 导出目录：${escapeHtml(task.markdownExportDir)}</div>`);
  }

  if (task.obsidianExportDir) {
    lines.push(`<div class="meta-line">Obsidian 导出目录：${escapeHtml(task.obsidianExportDir)}</div>`);
  }

  if (task.authorName) {
    lines.push(`<div class="meta-line">UP主：${escapeHtml(task.authorName)}</div>`);
  }

  if (task.sourceUrl) {
    lines.push(`
      <div class="meta-line">
        来源链接：<a href="${escapeHtml(task.sourceUrl)}" target="_blank" rel="noreferrer">${escapeHtml(task.sourceUrl)}</a>
      </div>
    `);
  }

  if (task.collectionName) {
    const collText = task.collectionIndex > 0
      ? `${escapeHtml(task.collectionName)}（第 ${escapeHtml(task.collectionIndex)} 集）`
      : escapeHtml(task.collectionName);
    const collLink = task.collectionUrl
      ? ` <a href="${escapeHtml(task.collectionUrl)}" target="_blank" rel="noreferrer">🔗</a>`
      : "";
    lines.push(`<div class="meta-line">合集：${collText}${collLink}</div>`);
  }

  if (task.domainTags && task.domainTags.length) {
    const badges = task.domainTags.map((tag) => `<span class="badge">${escapeHtml(tag)}</span>`).join(" ");
    lines.push(`<div class="meta-line">领域：${badges}</div>`);
  }

  if (task.originalFileName) {
    lines.push(`<div class="meta-line">原始文件：${escapeHtml(task.originalFileName)}</div>`);
  }

  return lines.join("");
}

function compactMetaItems(task) {
  return [
    modeLabel(task.mode),
    task.originalFileName,
    task.sourceUrl ? task.sourceUrl.replace(/^https?:\/\//, "") : ""
  ].filter(Boolean).slice(0, 2);
}

function expandedMetaItems(task) {
  return [
    modeLabel(task.mode),
    task.stage ? `阶段：${task.stage}` : "",
    task.totalChunks ? `断点：${task.completedChunks || 0}/${task.totalChunks}` : ""
  ].filter(Boolean);
}

function renderTask(task) {
  const progress = Number.isFinite(task.progressPercent) ? Math.max(0, Math.min(100, task.progressPercent)) : 0;
  const isExpanded = expandedTaskIds.has(task.id);
  const inlineMeta = expandedMetaItems(task);
  const compactMeta = compactMetaItems(task);

  const detailsMarkup = isExpanded ? `
    <div class="progress-wrap">
      <div class="progress-head">
        <strong>任务进度</strong>
        <span>${progress.toFixed(0)}%</span>
      </div>
      <div class="progress">
        <div class="progress-bar" style="width:${progress}%"></div>
      </div>
    </div>

    <div class="task-meta">${renderMetaLines(task)}</div>

    <div class="task-footer">
      <section class="task-summary">
        <div class="task-summary-head">
          <div>
            <p class="section-kicker">Artifacts</p>
            <h3>结果与产物</h3>
          </div>
        </div>
        <div class="detail-stack">
          ${renderTimingMetrics(task)}
          ${renderSavedFiles(task)}
          ${renderExports(task)}
          ${renderTaskDetails(task)}
        </div>
      </section>

      <section class="task-segments">
        <div class="task-summary-head">
          <div>
            <p class="section-kicker">Latest Segments</p>
            <h3>最新分段</h3>
          </div>
        </div>
        <div class="segment-list">${renderSegments(task)}</div>
      </section>
    </div>
  ` : "";

  return `
    <section class="task-card ${escapeHtml(task.status)} ${isExpanded ? "expanded" : "collapsed"}">
      <div class="task-topline">
        <div class="task-pills">
          <span class="task-pill">${escapeHtml(taskStatusLabel(task.status))}</span>
          ${task.translation ? '<span class="badge">翻译</span>' : ""}
          ${task.summaryRequested ? '<span class="badge">总结</span>' : ""}
        </div>
      </div>

      <div class="task-title">
        <div class="task-header-main">
          <div>
            <h4>${escapeHtml(task.name || "未命名任务")}</h4>
            <p class="task-subtitle">${escapeHtml(task.id)} · ${progress.toFixed(0)}%</p>
          </div>
          ${isExpanded ? `
            <div class="task-inline-meta">
              ${inlineMeta.map((item) => `<span>${escapeHtml(item)}</span>`).join("")}
            </div>
          ` : `
            <div class="task-compact-meta">
              ${compactMeta.map((item) => `<span>${escapeHtml(item)}</span>`).join("")}
            </div>
          `}
        </div>
        <div class="task-head-actions">
          ${isExpanded ? taskSummaryActions(task) : ""}
          <button type="button" class="task-toggle" data-action="toggle-task" data-task-id="${escapeHtml(task.id)}">
            ${isExpanded ? "收起" : "展开"}
          </button>
        </div>
      </div>

      ${task.error ? `<div class="error">${escapeHtml(task.error)}</div>` : ""}
      ${detailsMarkup}
    </section>
  `;
}

function rerenderTasks() {
  const ordered = filteredTasks();
  const total = tasks.size;
  const totalPages = Math.max(1, Math.ceil(ordered.length / pageSize));
  if (currentPage > totalPages) currentPage = totalPages;
  const start = (currentPage - 1) * pageSize;
  const pageItems = ordered.slice(start, start + pageSize);

  tasksEl.innerHTML = pageItems.length
    ? pageItems.map(renderTask).join("")
    : '<p class="empty">没有匹配的任务，试试切换筛选或清空搜索。</p>';

  // Pagination controls
  if (ordered.length > pageSize) {
    tasksEl.innerHTML += `
      <div class="pagination-controls">
        <button class="ghost-btn" data-action="page-prev" ${currentPage <= 1 ? "disabled" : ""}>上一页</button>
        <span class="pagination-info">第 ${currentPage} / ${totalPages} 页</span>
        <button class="ghost-btn" data-action="page-next" ${currentPage >= totalPages ? "disabled" : ""}>下一页</button>
      </div>
    `;
  }

  taskResultsInfoEl.textContent = total
    ? `当前显示 ${pageItems.length} / ${ordered.length} 个任务（共 ${total} 个）`
    : "暂无任务";

  document.querySelectorAll(".filter-chip").forEach((button) => {
    button.classList.toggle("active", button.getAttribute("data-filter") === currentFilter);
  });

  if (currentView === "tasks") {
    window.requestAnimationFrame(updateStageHeight);
  }
}

function scheduleRerender() {
  if (!renderScheduled) {
    renderScheduled = true;
    requestAnimationFrame(() => {
      renderScheduled = false;
      rerender();
    });
  }
}

function rerender() {
  renderTaskStats();
  rerenderTasks();
  if (currentView === "status") {
    window.requestAnimationFrame(updateStageHeight);
  }
}

async function retrySummary(taskId) {
  try {
    const task = await fetchJSON(`/api/tasks/${encodeURIComponent(taskId)}/retry-summary`, { method: "POST" });
    upsertTask(task);
    rerender();
  } catch (err) {
    alert(err.message);
  }
}

async function generateSummary(taskId) {
  try {
    const task = await fetchJSON(`/api/tasks/${encodeURIComponent(taskId)}/generate-summary`, { method: "POST" });
    upsertTask(task);
    rerender();
  } catch (err) {
    alert(err.message);
  }
}

async function retryExport(taskId, target) {
  try {
    const task = await fetchJSON(`/api/tasks/${encodeURIComponent(taskId)}/retry-exports`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ targets: [target] })
    });
    upsertTask(task);
    rerender();
  } catch (err) {
    alert(err.message);
  }
}

async function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "absolute";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand("copy");
  document.body.removeChild(textarea);
}

async function handleCopyCurl(button) {
  const targetId = button.getAttribute("data-copy-target");
  if (!targetId) {
    return;
  }

  const code = document.getElementById(targetId);
  if (!code) {
    return;
  }

  const original = button.textContent;

  try {
    await copyText(code.textContent || "");
    button.textContent = "已复制";
  } catch (err) {
    button.textContent = "复制失败";
  }

  window.setTimeout(() => {
    button.textContent = original;
  }, 1600);
}

function showSettingsMessage(id, message, isError = false) {
  const el = document.getElementById(id);
  if (!el) {
    return;
  }
  el.textContent = message;
  el.classList.toggle("error-text", Boolean(isError));
  if (message) {
    window.setTimeout(() => {
      el.textContent = "";
      el.classList.remove("error-text");
    }, 2400);
  }
}

async function loadSettings() {
  settingsState = await fetchJSON("/api/settings");
  renderSettings();
}

async function saveSettings(nextSettings) {
  settingsState = await fetchJSON("/api/settings", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(nextSettings)
  });
  renderSettings();
}

function renderSettings() {
  if (!settingsState) {
    return;
  }
  renderObsidianSettings();
  renderProviderSettings();
  renderPromptSettings();
  if (currentView === "settings") {
    window.requestAnimationFrame(updateStageHeight);
  }
}

function renderObsidianSettings() {
  const form = document.getElementById("obsidian-settings-form");
  if (!form) {
    return;
  }
  const obsidian = settingsState.obsidian || {};
  form.elements.vaultDir.value = obsidian.vaultDir || "";
  form.elements.similarityThreshold.value = obsidian.similarityThreshold || 0.82;
  form.elements.domainIndexFile.value = obsidian.domainIndexFile || "领域索引.md";
  form.elements.tagIndexFile.value = obsidian.tagIndexFile || "标签索引.md";
}

function renderProviderSettings() {
  const list = document.getElementById("provider-list");
  if (!list) {
    return;
  }
  const llm = settingsState.llm || {};
  const providers = llm.providers || [];
  list.innerHTML = providers.length ? providers.map((provider) => {
    const active = provider.id === llm.activeProviderID;
    return `
      <article class="settings-row ${active ? "active" : ""}">
        <div>
          <strong>${escapeHtml(provider.name || provider.id)}</strong>
          <span>${escapeHtml(provider.model || "-")} · ${escapeHtml(provider.baseURL || "-")}</span>
        </div>
        <div class="settings-row-actions">
          ${active ? '<span class="badge">Active</span>' : `<button type="button" class="ghost-btn compact-btn" data-settings-action="provider-activate" data-provider-id="${escapeHtml(provider.id)}">启用</button>`}
          <button type="button" class="ghost-btn compact-btn" data-settings-action="provider-edit" data-provider-id="${escapeHtml(provider.id)}">编辑</button>
          <button type="button" class="ghost-btn compact-btn" data-settings-action="provider-test" data-provider-id="${escapeHtml(provider.id)}">测试</button>
          <button type="button" class="ghost-btn compact-btn danger-btn" data-settings-action="provider-delete" data-provider-id="${escapeHtml(provider.id)}">删除</button>
        </div>
      </article>
    `;
  }).join("") : '<p class="empty">还没有 API Provider。</p>';
}

function renderPromptSettings() {
  const list = document.getElementById("prompt-list");
  if (!list) {
    return;
  }
  const prompts = settingsState.prompts || {};
  const items = prompts.items || [];
  list.innerHTML = items.length ? items.map((prompt) => {
    const active = prompt.id === prompts.activePromptID;
    return `
      <article class="settings-row ${active ? "active" : ""}">
        <div>
          <strong>${escapeHtml(prompt.name || prompt.id)}</strong>
          <span>${escapeHtml(prompt.kind || "prompt")}${prompt.sourcePath ? ` · ${escapeHtml(prompt.sourcePath)}` : ""}</span>
        </div>
        <div class="settings-row-actions">
          ${active ? '<span class="badge">Active</span>' : `<button type="button" class="ghost-btn compact-btn" data-settings-action="prompt-activate" data-prompt-id="${escapeHtml(prompt.id)}">启用</button>`}
          <button type="button" class="ghost-btn compact-btn" data-settings-action="prompt-edit" data-prompt-id="${escapeHtml(prompt.id)}">编辑</button>
          <button type="button" class="ghost-btn compact-btn danger-btn" data-settings-action="prompt-delete" data-prompt-id="${escapeHtml(prompt.id)}">删除</button>
        </div>
      </article>
    `;
  }).join("") : '<p class="empty">还没有自定义 Prompt。</p>';
}

function resetProviderForm() {
  const form = document.getElementById("provider-form");
  if (form) {
    form.reset();
    form.elements.id.value = "";
  }
}

function resetPromptForm() {
  const form = document.getElementById("prompt-form");
  if (form) {
    form.reset();
    form.elements.id.value = "";
    form.elements.kind.value = "prompt";
  }
}

function editProvider(id) {
  const provider = ((settingsState.llm || {}).providers || []).find((item) => item.id === id);
  const form = document.getElementById("provider-form");
  if (!provider || !form) {
    return;
  }
  form.elements.id.value = provider.id || "";
  form.elements.name.value = provider.name || "";
  form.elements.model.value = provider.model || "";
  form.elements.baseURL.value = provider.baseURL || "";
  form.elements.apiKey.value = provider.apiKey || "";
  setView("settings");
}

function editPrompt(id) {
  const prompt = ((settingsState.prompts || {}).items || []).find((item) => item.id === id);
  const form = document.getElementById("prompt-form");
  if (!prompt || !form) {
    return;
  }
  form.elements.id.value = prompt.id || "";
  form.elements.name.value = prompt.name || "";
  form.elements.kind.value = prompt.kind || "prompt";
  form.elements.content.value = prompt.content || "";
  setView("settings");
}

async function activateProvider(id) {
  settingsState = await fetchJSON(`/api/settings/providers/${encodeURIComponent(id)}/activate`, { method: "POST" });
  renderSettings();
}

async function testProvider(id) {
  await fetchJSON(`/api/settings/providers/${encodeURIComponent(id)}/test`, { method: "POST" });
}

async function deleteProvider(id) {
  settingsState = await fetchJSON(`/api/settings/providers/${encodeURIComponent(id)}`, { method: "DELETE" });
  renderSettings();
}

async function activatePrompt(id) {
  settingsState = await fetchJSON(`/api/settings/prompts/${encodeURIComponent(id)}/activate`, { method: "POST" });
  renderSettings();
}

async function deletePrompt(id) {
  settingsState = await fetchJSON(`/api/settings/prompts/${encodeURIComponent(id)}`, { method: "DELETE" });
  renderSettings();
}

async function loadInitial() {
  const [health, data, runtimeSettings] = await Promise.all([
    fetchJSON("/api/health"),
    fetchJSON("/api/tasks?page=1&size=200"),
    fetchJSON("/api/settings")
  ]);

  healthState = health;
  settingsState = runtimeSettings;
  renderHealth();
  renderSettings();
  renderExportOptions("file-export-options");
  renderExportOptions("url-export-options");
  const list = Array.isArray(data) ? data : (data.tasks || []);
  list.forEach((task) => upsertTask(task));
  rerender();
}

function attachEvents() {
  const sse = new EventSource("/api/events");
  sse.onmessage = (event) => {
    const data = JSON.parse(event.data);
    if (data.type === "task.deleted" && data.taskId) {
      tasks.delete(data.taskId);
      expandedTaskIds.delete(data.taskId);
      scheduleRerender();
      return;
    }
    if (data.payload && data.payload.id) {
      upsertTask(data.payload);
      scheduleRerender();
    }
  };

  themeToggleEl.addEventListener("click", () => {
    applyTheme(rootEl.getAttribute("data-theme") === "dark" ? "light" : "dark");
  });

  window.addEventListener("resize", () => {
    window.requestAnimationFrame(updateStageHeight);
  });

  taskSearchEl.addEventListener("input", (event) => {
    currentSearch = event.target.value || "";
    currentPage = 1;
    rerenderTasks();
  });

  document.getElementById("expand-active").addEventListener("click", () => {
    Array.from(tasks.values()).forEach((task) => {
      if (task.status === "running" || task.status === "queued" || task.status === "failed") {
        expandedTaskIds.add(task.id);
      }
    });
    rerenderTasks();
  });

  document.getElementById("collapse-all").addEventListener("click", () => {
    expandedTaskIds.clear();
    rerenderTasks();
  });

  document.addEventListener("click", async (event) => {
    const viewButton = event.target.closest(".view-tab");
    if (viewButton) {
      setView(viewButton.getAttribute("data-view") || "status");
      return;
    }

    const sourceButton = event.target.closest(".sub-tab");
    if (sourceButton) {
      setSource(sourceButton.getAttribute("data-source") || "file");
      return;
    }

    const copyButton = event.target.closest(".copy-btn");
    if (copyButton) {
      await handleCopyCurl(copyButton);
      return;
    }

    const filterButton = event.target.closest(".filter-chip");
    if (filterButton) {
      currentFilter = filterButton.getAttribute("data-filter") || "all";
      currentPage = 1;
      rerenderTasks();
      return;
    }

    const settingsButton = event.target.closest("[data-settings-action]");
    if (settingsButton) {
      const settingsAction = settingsButton.getAttribute("data-settings-action");
      const providerId = settingsButton.getAttribute("data-provider-id");
      const promptId = settingsButton.getAttribute("data-prompt-id");
      try {
        if (settingsAction === "provider-edit" && providerId) {
          editProvider(providerId);
        } else if (settingsAction === "provider-activate" && providerId) {
          await activateProvider(providerId);
          showSettingsMessage("provider-settings-message", "已启用");
        } else if (settingsAction === "provider-test" && providerId) {
          await testProvider(providerId);
          showSettingsMessage("provider-settings-message", "测试通过");
        } else if (settingsAction === "provider-delete" && providerId) {
          await deleteProvider(providerId);
          resetProviderForm();
          showSettingsMessage("provider-settings-message", "已删除");
        } else if (settingsAction === "prompt-edit" && promptId) {
          editPrompt(promptId);
        } else if (settingsAction === "prompt-activate" && promptId) {
          await activatePrompt(promptId);
          showSettingsMessage("prompt-settings-message", "已启用");
        } else if (settingsAction === "prompt-delete" && promptId) {
          await deletePrompt(promptId);
          resetPromptForm();
          showSettingsMessage("prompt-settings-message", "已删除");
        }
      } catch (err) {
        showSettingsMessage(
          providerId ? "provider-settings-message" : "prompt-settings-message",
          err.message,
          true
        );
      }
      return;
    }

    const actionButton = event.target.closest("[data-action]");
    if (!actionButton) {
      return;
    }

    const action = actionButton.getAttribute("data-action");
    const taskId = actionButton.getAttribute("data-task-id");

    if (action === "toggle-task" && taskId) {
      if (expandedTaskIds.has(taskId)) {
        expandedTaskIds.delete(taskId);
      } else {
        expandedTaskIds.add(taskId);
      }
      rerenderTasks();
      return;
    }

    if (action === "page-prev") {
      if (currentPage > 1) { currentPage--; rerenderTasks(); }
      return;
    }
    if (action === "page-next") {
      currentPage++;
      rerenderTasks();
      return;
    }

    if (!taskId) {
      return;
    }

    if (action === "generate-summary") {
      await generateSummary(taskId);
      return;
    }

    if (action === "retry-summary") {
      await retrySummary(taskId);
      return;
    }

    if (action === "retry-export") {
      const target = actionButton.getAttribute("data-export-target");
      if (target) {
        await retryExport(taskId, target);
      }
    }
  });
}

document.getElementById("settings-refresh").addEventListener("click", async () => {
  try {
    await loadSettings();
    showSettingsMessage("obsidian-settings-message", "已刷新");
  } catch (err) {
    showSettingsMessage("obsidian-settings-message", err.message, true);
  }
});

document.getElementById("obsidian-settings-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.target;
  const next = structuredClone(settingsState || {});
  next.obsidian = {
    ...(next.obsidian || {}),
    vaultDir: form.elements.vaultDir.value.trim(),
    domainIndexFile: form.elements.domainIndexFile.value.trim() || "领域索引.md",
    tagIndexFile: form.elements.tagIndexFile.value.trim() || "标签索引.md",
    similarityThreshold: Number(form.elements.similarityThreshold.value || 0.82)
  };
  try {
    await saveSettings(next);
    showSettingsMessage("obsidian-settings-message", "已保存");
  } catch (err) {
    showSettingsMessage("obsidian-settings-message", err.message, true);
  }
});

document.getElementById("provider-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.target;
  const id = form.elements.id.value.trim();
  const payload = {
    id,
    name: form.elements.name.value.trim(),
    baseURL: form.elements.baseURL.value.trim(),
    apiKey: form.elements.apiKey.value.trim(),
    model: form.elements.model.value.trim(),
    enabled: true
  };
  try {
    const url = id ? `/api/settings/providers/${encodeURIComponent(id)}` : "/api/settings/providers";
    settingsState = await fetchJSON(url, {
      method: id ? "PUT" : "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
    renderSettings();
    showSettingsMessage("provider-settings-message", "已保存");
  } catch (err) {
    showSettingsMessage("provider-settings-message", err.message, true);
  }
});

document.getElementById("provider-form-reset").addEventListener("click", resetProviderForm);

document.getElementById("prompt-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.target;
  const id = form.elements.id.value.trim();
  const payload = {
    id,
    name: form.elements.name.value.trim(),
    kind: form.elements.kind.value.trim() || "prompt",
    content: form.elements.content.value,
    enabled: true
  };
  try {
    const url = id ? `/api/settings/prompts/${encodeURIComponent(id)}` : "/api/settings/prompts";
    settingsState = await fetchJSON(url, {
      method: id ? "PUT" : "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
    renderSettings();
    showSettingsMessage("prompt-settings-message", "已保存");
  } catch (err) {
    showSettingsMessage("prompt-settings-message", err.message, true);
  }
});

document.getElementById("prompt-form-reset").addEventListener("click", resetPromptForm);

document.getElementById("prompt-load-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.target;
  const path = form.elements.path.value.trim();
  try {
    settingsState = await fetchJSON("/api/settings/prompts/load-file", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path })
    });
    form.reset();
    renderSettings();
    showSettingsMessage("prompt-load-message", "已加载");
  } catch (err) {
    showSettingsMessage("prompt-load-message", err.message, true);
  }
});

document.getElementById("file-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.target;
  const formData = new FormData(form);

  try {
    const created = await fetchJSON("/api/tasks", { method: "POST", body: formData });
    (Array.isArray(created) ? created : [created]).forEach((task) => upsertTask(task));
    form.reset();
    setView("tasks");
    rerender();
  } catch (err) {
    alert(err.message);
  }
});

document.getElementById("url-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.target;
  const formData = new FormData(form);
  const urlsText = formData.get("urlsText") || "";

  // Check if any line is a collection URL — if so, trigger preview flow.
  const lines = urlsText.split("\n").map((l) => l.trim()).filter(Boolean);
  const collectionURLs = lines.filter(isCollectionURL);
  if (collectionURLs.length > 0) {
    await previewCollection(collectionURLs[0], form);
    return;
  }

  const payload = {
    name: formData.get("name") || "",
    language: formData.get("language") || "",
    urlsText: urlsText,
    translate: formData.get("translate") === "on",
    summarize: formData.get("summarize") === "on",
    exportTargets: selectedExportTargets(form),
    ...exportPathOptions(form)
  };

  try {
    const created = await fetchJSON("/api/url-tasks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
    (Array.isArray(created) ? created : [created]).forEach((task) => upsertTask(task));
    form.reset();
    setView("tasks");
    rerender();
  } catch (err) {
    alert(err.message);
  }
});

// --- Collection Preview ---

let pendingCollectionForm = null;
let pendingCollectionData = null;

async function previewCollection(url, form) {
  if (isWatchLaterURL(url)) {
    const loggedIn = await ensureBilibiliLogin(() => previewCollection(url, form));
    if (!loggedIn) {
      return;
    }
  }

  pendingCollectionForm = form;
  try {
    const data = await fetchJSON("/api/collection-preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url })
    });
    pendingCollectionData = data;
    showCollectionModal(data);
  } catch (err) {
    if (isWatchLaterURL(url) && /code=-101|未登录|login/i.test(err.message || "")) {
      pendingAfterLogin = () => previewCollection(url, form);
      await openBilibiliLoginModal();
      return;
    }
    alert("合集解析失败: " + err.message);
  }
}

async function ensureBilibiliLogin(afterLogin) {
  const status = await fetchJSON("/api/bilibili/login/status");
  if (status.loggedIn) {
    return true;
  }
  pendingAfterLogin = afterLogin;
  await openBilibiliLoginModal();
  return false;
}

async function openBilibiliLoginModal() {
  stopBilibiliLoginPoll();
  const modal = document.getElementById("bilibili-login-modal");
  const qr = document.getElementById("bilibili-login-qr");
  const statusEl = document.getElementById("bilibili-login-status");
  statusEl.textContent = "正在生成二维码...";
  qr.removeAttribute("src");
  modal.classList.remove("hidden");

  try {
    const login = await fetchJSON("/api/bilibili/login/qrcode", { method: "POST" });
    qr.src = login.qrcodeDataUrl;
    statusEl.textContent = "请使用哔哩哔哩 App 扫码登录。";
    startBilibiliLoginPoll(login.qrcodeKey);
  } catch (err) {
    statusEl.textContent = `二维码生成失败：${err.message}`;
  }
}

function startBilibiliLoginPoll(qrcodeKey) {
  const statusEl = document.getElementById("bilibili-login-status");
  const poll = async () => {
    try {
      const result = await fetchJSON("/api/bilibili/login/poll", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ qrcodeKey })
      });

      if (result.status === "waiting") {
        statusEl.textContent = "等待扫码...";
        return;
      }
      if (result.status === "scanned") {
        statusEl.textContent = "已扫码，请在手机上确认登录。";
        return;
      }
      if (result.status === "expired") {
        statusEl.textContent = "二维码已过期，请关闭后重新提交。";
        stopBilibiliLoginPoll();
        return;
      }
      if (result.status === "succeeded") {
        statusEl.textContent = "登录成功，正在继续解析稍后再看列表...";
        stopBilibiliLoginPoll();
        closeBilibiliLoginModal(false);
        if (typeof pendingAfterLogin === "function") {
          const resume = pendingAfterLogin;
          pendingAfterLogin = null;
          await resume();
        }
      }
    } catch (err) {
      statusEl.textContent = `登录状态查询失败：${err.message}`;
      stopBilibiliLoginPoll();
    }
  };

  bilibiliLoginPollTimer = window.setInterval(poll, 2000);
  poll();
}

function stopBilibiliLoginPoll() {
  if (bilibiliLoginPollTimer) {
    window.clearInterval(bilibiliLoginPollTimer);
    bilibiliLoginPollTimer = null;
  }
}

function closeBilibiliLoginModal(clearPending = true) {
  stopBilibiliLoginPoll();
  document.getElementById("bilibili-login-modal").classList.add("hidden");
  if (clearPending) {
    pendingAfterLogin = null;
  }
}

function showCollectionModal(collection) {
  const modal = document.getElementById("collection-modal");
  const title = document.getElementById("collection-modal-title");
  const meta = document.getElementById("collection-modal-meta");
  const videoList = document.getElementById("collection-videos");
  const selectAll = document.getElementById("collection-select-all");
  const countEl = document.getElementById("collection-selected-count");

  title.textContent = collection.name || "合集预览";
  const authorText = collection.author ? `UP主: ${collection.author} · ` : "";
  meta.textContent = `${authorText}共 ${collection.videos.length} 个视频`;

  videoList.innerHTML = collection.videos.map((v) => `
    <label class="collection-video-item">
      <input type="checkbox" checked data-bvid="${escapeHtml(v.bvid)}" data-page-url="${escapeHtml(v.pageURL)}">
      <span class="collection-video-index">${v.index}</span>
      <span class="collection-video-title" title="${escapeHtml(v.title)}">${escapeHtml(v.title)}</span>
    </label>
  `).join("");

  selectAll.checked = true;
  updateSelectedCount();
  modal.classList.remove("hidden");

  selectAll.onchange = () => {
    videoList.querySelectorAll("input[type=checkbox]").forEach((cb) => {
      cb.checked = selectAll.checked;
    });
    updateSelectedCount();
  };

  videoList.onchange = () => {
    const checkboxes = videoList.querySelectorAll("input[type=checkbox]");
    const allChecked = Array.from(checkboxes).every((cb) => cb.checked);
    selectAll.checked = allChecked;
    updateSelectedCount();
  };
}

function updateSelectedCount() {
  const videoList = document.getElementById("collection-videos");
  const countEl = document.getElementById("collection-selected-count");
  const total = videoList.querySelectorAll("input[type=checkbox]").length;
  const selected = videoList.querySelectorAll("input[type=checkbox]:checked").length;
  countEl.textContent = `已选 ${selected} / ${total}`;
}

function closeCollectionModal() {
  document.getElementById("collection-modal").classList.add("hidden");
  pendingCollectionForm = null;
  pendingCollectionData = null;
}

async function submitSelectedVideos() {
  const videoList = document.getElementById("collection-videos");
  const checkboxes = videoList.querySelectorAll("input[type=checkbox]:checked");
  const selectedURLs = Array.from(checkboxes).map((cb) => cb.getAttribute("data-page-url"));

  if (selectedURLs.length === 0) {
    alert("请至少选择一个视频");
    return;
  }

  const form = pendingCollectionForm;
  const formData = new FormData(form);
  const payload = {
    name: formData.get("name") || "",
    language: formData.get("language") || "",
    urlsText: selectedURLs.join("\n"),
    translate: formData.get("translate") === "on",
    summarize: formData.get("summarize") === "on",
    exportTargets: selectedExportTargets(form),
    ...exportPathOptions(form)
  };

  try {
    const created = await fetchJSON("/api/url-tasks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
    (Array.isArray(created) ? created : [created]).forEach((task) => upsertTask(task));
    form.reset();
    closeCollectionModal();
    setView("tasks");
    rerender();
  } catch (err) {
    alert(err.message);
  }
}

document.getElementById("collection-modal-close").addEventListener("click", closeCollectionModal);
document.getElementById("collection-modal-cancel").addEventListener("click", closeCollectionModal);
document.getElementById("collection-modal-confirm").addEventListener("click", submitSelectedVideos);
document.getElementById("collection-modal").addEventListener("click", (event) => {
  if (event.target === event.currentTarget) {
    closeCollectionModal();
  }
});
document.getElementById("bilibili-login-close").addEventListener("click", () => closeBilibiliLoginModal());
document.getElementById("bilibili-login-cancel").addEventListener("click", () => closeBilibiliLoginModal());
document.getElementById("bilibili-login-modal").addEventListener("click", (event) => {
  if (event.target === event.currentTarget) {
    closeBilibiliLoginModal();
  }
});

initializeTheme();
setView("status", true);
setSource("file");

loadInitial()
  .then(attachEvents)
  .then(() => {
    window.requestAnimationFrame(updateStageHeight);
  })
  .catch((err) => {
    healthEl.innerHTML = `<div class="error">${escapeHtml(err.message)}</div>`;
  });
