// ATLAS frontend — POST the brief, parse SSE stream, render live matrix.
// EventSource doesn't support POST bodies, so we use fetch() with a ReadableStream
// and parse SSE frames manually.

const $ = (sel) => document.querySelector(sel);

const els = {
  brief:        $("#brief"),
  numAngles:    $("#num-angles"),
  numMarkets:   $("#num-markets"),
  launch:       $("#launch"),
  status:       $("#status"),
  statusText:   $("#status-text"),
  log:          $("#log"),
  matrix:       $("#matrix"),
  empty:        $("#empty"),
  hero:         $("#hero-title"),
  heroSub:      $("#hero-sub"),
  meta:         $("#meta"),
  metaProduct:  $("#meta-product"),
  metaCount:    $("#meta-count"),
  metaElapsed:  $("#meta-elapsed"),
};

let runStartTs = 0;
let metaTimer = null;

els.launch.addEventListener("click", launch);
els.brief.addEventListener("keydown", (e) => {
  if (e.metaKey && e.key === "Enter") launch();
});

async function launch() {
  const text = els.brief.value.trim();
  if (!text) {
    els.brief.focus();
    return;
  }

  resetUI();
  setStatus("active", "running");
  runStartTs = performance.now();

  startMetaTicker();

  try {
    const res = await fetch("/api/campaign", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        text,
        num_angles: parseInt(els.numAngles.value, 10) || 4,
        num_markets: parseInt(els.numMarkets.value, 10) || 3,
      }),
    });

    if (!res.ok || !res.body) {
      const t = await res.text().catch(() => "");
      throw new Error(`HTTP ${res.status}: ${t || "no body"}`);
    }

    await consumeSSE(res.body, onEvent);
  } catch (err) {
    appendLog("system", `error: ${err.message}`, true);
    setStatus("", "error");
  } finally {
    els.launch.disabled = false;
    stopMetaTicker();
  }
}

// ── SSE parser ─────────────────────────────────────────────────────────────

async function consumeSSE(stream, handler) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buf = "";

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });

    // Frames are separated by a blank line ("\n\n").
    let i;
    while ((i = buf.indexOf("\n\n")) >= 0) {
      const frame = buf.slice(0, i);
      buf = buf.slice(i + 2);

      // Each frame can have multiple "data: " lines; join them.
      const data = frame
        .split("\n")
        .filter((l) => l.startsWith("data:"))
        .map((l) => l.slice(5).trim())
        .join("\n");

      if (!data) continue;
      try {
        handler(JSON.parse(data));
      } catch (e) {
        console.warn("bad SSE frame", data, e);
      }
    }
  }
}

// ── Event dispatch ─────────────────────────────────────────────────────────

function onEvent(ev) {
  switch (ev.type) {
    case "log":
      appendLog(ev.agent || "system", ev.message);
      break;
    case "strategy":
      renderStrategy(ev.strategy);
      break;
    case "cell":
      upsertCell(ev.cell);
      if (ev.stats) updateProgress(ev.stats);
      break;
    case "done":
      setStatus("done", `done · ${formatMs(ev.stats.elapsed_ms)}`);
      appendLog("system", `Campaign complete in ${formatMs(ev.stats.elapsed_ms)}`);
      stopMetaTicker();
      if (ev.stats) {
        els.metaElapsed.textContent = formatMs(ev.stats.elapsed_ms) + " · final";
        els.metaElapsed.classList.add("accent");
      }
      break;
    case "error":
      appendLog("system", ev.message, true);
      setStatus("", "error");
      break;
  }
}

// ── Strategy / matrix scaffolding ──────────────────────────────────────────

let strategy = null;

function renderStrategy(s) {
  strategy = s;
  els.hero.textContent = s.product_name;
  els.heroSub.textContent = s.tagline;
  els.meta.hidden = false;
  els.metaProduct.textContent = s.product_name;
  els.metaCount.textContent = `${s.angles.length} × ${s.markets.length} = ${s.angles.length * s.markets.length} cells`;
  els.empty.classList.add("hidden");

  // Build the grid scaffolding — one cell per (angle, market) in row-major order.
  els.matrix.innerHTML = "";
  for (const angle of s.angles) {
    for (const market of s.markets) {
      const id = cellId(angle.id, market.id);
      const cell = document.createElement("article");
      cell.className = "cell is-loading";
      cell.id = id;
      cell.innerHTML = `
        <header class="cell-head">
          <span class="cell-angle">${escapeHtml(angle.name)}</span>
          <span class="cell-market">${escapeHtml(market.name)}</span>
        </header>
        <div class="cell-image">
          <span class="stage-glyph" data-glyph>waiting</span>
        </div>
        <div class="cell-copy">
          <h3 class="cell-headline" data-headline>—</h3>
          <p class="cell-body" data-body>${escapeHtml(angle.hypothesis)}</p>
          <span class="cell-cta" data-cta>...</span>
        </div>
        <footer class="cell-foot">
          <span data-status>queued</span>
          <span data-timer>—</span>
        </footer>
      `;
      els.matrix.appendChild(cell);
    }
  }
}

function upsertCell(c) {
  const node = document.getElementById(cellId(c.angle_id, c.market_id));
  if (!node) return;

  const $$ = (sel) => node.querySelector(sel);

  $$("[data-status]").textContent = c.status;
  $$("[data-status]").classList.toggle("done", c.status === "done");
  $$("[data-status]").classList.toggle("err",  c.status === "error");

  if (c.elapsed_ms != null) {
    $$("[data-timer]").textContent = formatMs(c.elapsed_ms);
  }

  const glyph = $$("[data-glyph]");
  if (glyph) {
    glyph.textContent = stageLabel(c.status);
  }

  if (c.copy) {
    $$("[data-headline]").textContent = c.copy.headline;
    $$("[data-body]").textContent     = c.copy.body;
    $$("[data-cta]").textContent      = c.copy.cta;
  }

  // Prefer upscaled image when available; fall back to the gen image.
  const imgSrc = c.upscale_url || c.image_url;
  if (imgSrc) {
    let img = $$(".cell-image img");
    if (!img) {
      img = document.createElement("img");
      img.alt = c.copy?.headline || "";
      img.referrerPolicy = "no-referrer";
      $$(".cell-image").appendChild(img);
    }
    if (img.src !== imgSrc) img.src = imgSrc;
    node.classList.add("has-image");
  }

  if (c.status === "done" || c.status === "error") {
    node.classList.remove("is-loading");
  }
}

function updateProgress(stats) {
  els.metaCount.textContent = `${stats.completed} / ${stats.total_cells} cells`;
  if (stats.completed === stats.total_cells) {
    els.metaCount.classList.add("accent");
  }
}

// ── Log stream ─────────────────────────────────────────────────────────────

function appendLog(agent, msg, isError = false) {
  const line = document.createElement("div");
  line.className = `log-line agent-${agent}${isError ? " error" : ""}`;
  line.innerHTML = `
    <span class="log-agent">[${escapeHtml(agent)}]</span>
    <span class="log-msg">${escapeHtml(msg)}</span>
  `;
  els.log.appendChild(line);
  els.log.scrollTop = els.log.scrollHeight;
}

// ── UI helpers ─────────────────────────────────────────────────────────────

function resetUI() {
  els.launch.disabled = true;
  els.log.innerHTML = "";
  els.matrix.innerHTML = "";
  els.meta.hidden = true;
  els.metaElapsed.classList.remove("accent");
  els.metaCount.classList.remove("accent");
  els.empty.classList.add("hidden");
  els.hero.textContent = "Strategist running...";
  els.heroSub.textContent = "decomposing the brief";
}

function setStatus(cls, txt) {
  els.status.className = `status ${cls}`;
  els.statusText.textContent = txt;
}

function startMetaTicker() {
  metaTimer = setInterval(() => {
    const ms = performance.now() - runStartTs;
    els.metaElapsed.textContent = formatMs(ms) + " elapsed";
  }, 200);
}
function stopMetaTicker() {
  if (metaTimer) clearInterval(metaTimer);
}

function stageLabel(status) {
  return {
    pending:   "queued",
    writing:   "copywriter writing...",
    directing: "art director briefing...",
    rendering: "magnific rendering...",
    upscaling: "magnific upscaling...",
    done:      "delivered",
    error:     "failed",
  }[status] || status;
}

function cellId(angleId, marketId) {
  return `cell--${angleId}--${marketId}`;
}

function formatMs(ms) {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function escapeHtml(s) {
  if (s == null) return "";
  return String(s)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}
