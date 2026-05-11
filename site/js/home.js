/**
 * Chunked leaderboard: data/index.json + outline + page-XXXXXX.json.
 * Legacy fallback: single data/leaderboard.json.
 */

const DATA = "data/";

/** @typedef {{ rank: number; key: string; name: string }} OutlineRow */

let webIdx = null;
/** @type {OutlineRow[]} */
let outlinePlayers = [];
/** @type {Map<number, object>} */
const detailByRank = new Map();
const loadedChunks = new Set();
/** @type {Map<number, Promise<void>>} */
const chunkInflight = new Map();

let legacyMono = false;
/** chunk mode only: inclusive max rank browsed (starting at page_size) */
let browseRevealMaxRank = 0;
let initialized = false;

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function slugId(key, rank) {
  const raw = `${rank}:${String(key ?? "")}`;
  try {
    return (
      "p_" +
      btoa(unescape(encodeURIComponent(raw))).replace(/[+\\/=]/g, (c) => ({ "+": "-", "/": "_", "=": "" })[c])
    );
  } catch {
    return `p_${rank}`;
  }
}

function rankToChunkIndex(rank, pageSize) {
  if (!pageSize || rank < 1) {
    return 0;
  }
  return Math.floor((rank - 1) / pageSize);
}

function resultLabel(code) {
  switch (code) {
    case "W":
      return "Win";
    case "L":
      return "Loss";
    case "D":
      return "Draw";
    default:
      return code || "—";
  }
}

function formatDelta(v) {
  if (v == null || Number.isNaN(Number(v))) {
    return "—";
  }
  const n = Number(v);
  return (n > 0 ? "+" : "") + n.toFixed(1);
}

async function fetchJSON(rel) {
  const r = await fetch(DATA + rel);
  if (!r.ok) {
    throw new Error(`HTTP ${r.status} ${rel}`);
  }
  return await r.json();
}

function resetState() {
  outlinePlayers.length = 0;
  detailByRank.clear();
  loadedChunks.clear();
  chunkInflight.clear();
  webIdx = null;
  legacyMono = false;
  browseRevealMaxRank = 0;
}

async function loadLegacyMonolith() {
  resetState();
  legacyMono = true;
  /** @type {{ as_of?: string; players?: object[] }} */
  const blob = await fetchJSON("leaderboard.json");
  const players = Array.isArray(blob.players) ? blob.players : [];
  webIdx = { as_of: blob.as_of, total_players: players.length };
  browseRevealMaxRank = players.length;

  outlinePlayers.push(
    ...players.map((p) => ({
      rank: Number(p.rank),
      key: String(p.key ?? ""),
      name: String(p.name ?? ""),
    })),
  );
  for (const p of players) {
    detailByRank.set(Number(p.rank), p);
  }
}

async function loadChunkMode() {
  resetState();
  webIdx = await fetchJSON("index.json");
  if (Number(webIdx.version) !== 2 || !Number.isFinite(webIdx.page_size) || webIdx.total_players == null) {
    throw new Error("bad index.json");
  }

  /** @type {{ players?: OutlineRow[] }} */
  const out = await fetchJSON(webIdx.outline_file || "outline.json");
  if (!Array.isArray(out.players)) {
    throw new Error("bad outline");
  }
  outlinePlayers.push(...out.players);

  const ps = Number(webIdx.page_size) || 50;
  const total = Number(webIdx.total_players);
  browseRevealMaxRank = total <= 0 ? 0 : Math.min(total, ps);

  await ensurePagesThroughRank(browseRevealMaxRank);
}

async function ensureChunk(pgIdx) {
  const pagesArr = Array.isArray(webIdx.pages) ? webIdx.pages : [];
  if (pgIdx < 0 || pgIdx >= pagesArr.length) {
    return;
  }
  if (loadedChunks.has(pgIdx)) {
    return;
  }

  let p = chunkInflight.get(pgIdx);
  if (!p) {
    p = (async () => {
      /** @type {{ players?: object[] }} */
      const chunk = await fetchJSON(pagesArr[pgIdx]);
      if (!Array.isArray(chunk.players)) {
        throw new Error("bad chunk " + pgIdx);
      }
      for (const pl of chunk.players) {
        detailByRank.set(Number(pl.rank), pl);
      }
      loadedChunks.add(pgIdx);
    })().finally(() => {
      chunkInflight.delete(pgIdx);
    });
    chunkInflight.set(pgIdx, p);
  }
  await p;
}

async function ensurePagesThroughRank(maxRankInclusive) {
  const ps = Number(webIdx?.page_size) || 50;
  const pages = Array.isArray(webIdx?.pages) ? webIdx.pages : [];
  if (!maxRankInclusive || pages.length === 0) {
    return;
  }
  const clamped = Math.min(maxRankInclusive, outlinePlayers.at(-1)?.rank ?? maxRankInclusive);
  const hi = rankToChunkIndex(clamped, ps);
  const lim = Math.min(hi, pages.length - 1);
  for (let pg = 0; pg <= lim; pg++) {
    await ensureChunk(pg);
  }
}

async function ensurePagesForRanks(rankList) {
  const ps = Number(webIdx.page_size);
  const pages = Array.isArray(webIdx.pages) ? webIdx.pages : [];
  /** @type {Set<number>} */
  const pg = new Set();
  for (const r of rankList) {
    const idx = rankToChunkIndex(r, ps);
    if (idx >= 0 && idx < pages.length) {
      pg.add(idx);
    }
  }
  await Promise.all([...pg].map((idx) => ensureChunk(idx)));
}

function closeAllDetails(tbody) {
  tbody.querySelectorAll(".detail-row").forEach((el) => el.classList.remove("open"));
  tbody.querySelectorAll(".name-toggle").forEach((el) => el.setAttribute("aria-expanded", "false"));
}

function gamesTable(games) {
  if (!Array.isArray(games) || games.length === 0) {
    return '<p class="muted nested-hint">No recent games in this export.</p>';
  }
  const rowsHtml = games
    .map(
      (g) => `<tr>
        <td class="mono">${escapeHtml(g.time || "")}</td>
        <td>${escapeHtml(resultLabel(g.result || ""))}</td>
        <td>${escapeHtml(g.opponent || "")}</td>
        <td class="num">${formatDelta(g.delta_elo)}</td>
        <td>${escapeHtml(g.event_id || "—")}</td>
      </tr>`,
    )
    .join("");
  return `<p class="subhead muted">Recent games (newest first)</p>
    <table class="nested">
      <thead>
        <tr>
          <th>When</th>
          <th>Result</th>
          <th>Opponent</th>
          <th class="num">Δ Elo</th>
          <th>Event</th>
        </tr>
      </thead>
      <tbody>${rowsHtml}</tbody>
    </table>`;
}

function eventsSummary(events) {
  if (!Array.isArray(events) || events.length === 0) {
    return "";
  }
  const rowsHtml = events
    .map((e) => {
      const evId = escapeHtml(((e.event_id ?? "") + "").trim() || "—");
      const day = escapeHtml(((e.last_played_rfc3339 ?? "") + "").slice(0, 10) || "—");
      let sum = "—";
      const dg = Number(e.delta_games) || 0;
      if (dg > 0) {
        sum = formatDelta(Number(e.total_delta_elo));
      }
      const gCt = Number(e.games) ?? 0;
      return `<tr>
        <td class="mono">${day}</td>
        <td class="mono ev-id">${evId}</td>
        <td class="num">${Number(e.wins) || 0}-${Number(e.losses) || 0}-${Number(e.draws) || 0}</td>
        <td class="num">${gCt}</td>
        <td class="num">${sum}</td>
        <td class="num">${dg}/${gCt}</td>
      </tr>`;
    })
    .join("");
  return `<p class="subhead muted">Last ${events.length} distinct events</p>
    <table class="nested events-mini">
      <thead>
        <tr>
          <th>Last day</th>
          <th>Event</th>
          <th class="num">W-L-D</th>
          <th class="num">Gms</th>
          <th class="num">ΣΔ</th>
          <th class="num">Rated</th>
        </tr>
      </thead>
      <tbody>${rowsHtml}</tbody>
    </table>`;
}

function bindRowToggle(tbody, id, btn) {
  btn.addEventListener("click", () => {
    const detail = document.getElementById(`detail-${id}`);
    if (!detail) {
      return;
    }
    const willOpen = !detail.classList.contains("open");
    closeAllDetails(tbody);
    if (willOpen) {
      detail.classList.add("open");
      btn.setAttribute("aria-expanded", "true");
    }
  });
}

/** @returns {OutlineRow[]} */
function rowsVisibleForBrowse() {
  return outlinePlayers.filter((o) => o.rank <= browseRevealMaxRank);
}

/** @param {OutlineRow[]} displayRows */
function renderTable(tbody, displayRows, filterNeedleLower) {
  tbody.replaceChildren();
  displayRows.sort((a, b) => a.rank - b.rank);

  for (const o of displayRows) {
    const id = slugId(o.key, o.rank);
    const player = detailByRank.get(o.rank);
    if (!player) {
      throw new Error(`missing chunk data for rank ${o.rank}`);
    }

    const tr = document.createElement("tr");
    tr.className = "player-row";
    const nameBtn = document.createElement("button");
    nameBtn.type = "button";
    nameBtn.className = "name-toggle";
    nameBtn.setAttribute("aria-expanded", "false");
    nameBtn.innerHTML = escapeHtml(player.name ?? o.name) + '<span class="caret" aria-hidden="true"></span>';

    const tdRank = document.createElement("td");
    tdRank.className = "num";
    tdRank.textContent = String(o.rank);

    const tdName = document.createElement("td");
    tdName.appendChild(nameBtn);

    const tdElo = document.createElement("td");
    tdElo.className = "num";
    tdElo.textContent = player.elo != null ? Number(player.elo).toFixed(1) : "—";

    const tdGames = document.createElement("td");
    tdGames.className = "num";
    tdGames.textContent = player.games != null ? String(player.games) : "—";

    tr.append(tdRank, tdName, tdElo, tdGames);
    tbody.appendChild(tr);

    const detail = document.createElement("tr");
    detail.className = "detail-row";
    detail.id = `detail-${id}`;
    const td = document.createElement("td");
    td.colSpan = 4;
    td.className = "detail-cell";
    td.innerHTML = `${eventsSummary(player.recent_events)}${gamesTable(player.recent_games)}`;
    detail.appendChild(td);
    tbody.appendChild(detail);

    bindRowToggle(tbody, id, nameBtn);
  }

  if (displayRows.length === 0 && filterNeedleLower) {
    const empty = document.createElement("tr");
    empty.innerHTML = `<td colspan="4" class="muted">No players match.</td>`;
    tbody.appendChild(empty);
  }
}

function updateChrome(metaEl, loadStatusEl, loadMoreBtn, needle, /** @type {{ browseRowsShown?: number }} */ ctx) {
  const flt = needle.trim().length > 0;

  let asOf = "";
  if (legacyMono) {
    asOf = /** @type {*} */ (webIdx).as_of ?? "";
  } else {
    asOf = webIdx?.as_of ?? "";
  }

  const parts = [];
  if (asOf) parts.push(`As of ${asOf}`);
  if (!legacyMono && webIdx) {
    parts.push(`${webIdx.total_players} ranked · chunks of ${webIdx.page_size}`);
  }
  if (legacyMono) {
    parts.push("legacy single-file leaderboard");
  }
  parts.push(`Click names for recent events · games`);

  metaEl.textContent = parts.join(" · ");

  if (flt) {
    loadMoreBtn.style.display = "none";
    const n = ctx?.browseRowsShown ?? 0;
    loadStatusEl.textContent = n === 0 ? "No outline match for that filter." : `${n} name match(es)`;
    return;
  }

  if (legacyMono) {
    loadMoreBtn.style.display = "none";
    loadStatusEl.textContent =
      browseRevealMaxRank > 0 ?
        `${browseRevealMaxRank} loaded (full list).`
      : "";
    return;
  }

  if (!webIdx) {
    loadMoreBtn.style.display = "none";
    loadStatusEl.textContent = "";
    return;
  }

  const totalPlayers = Number(webIdx.total_players);
  if (!Number.isFinite(totalPlayers)) {
    loadMoreBtn.style.display = "none";
    loadStatusEl.textContent = "";
    return;
  }

  const pages = Array.isArray(webIdx.pages) ? webIdx.pages : [];

  if (totalPlayers === 0) {
    loadMoreBtn.style.display = "none";
    loadStatusEl.textContent = "No rankings in this snapshot.";
    return;
  }

  const revealed = browseRevealMaxRank;
  loadStatusEl.textContent = `Showing rank 1–${revealed} of ${totalPlayers} · ${loadedChunks.size}/${pages.length || 1} chunks fetched`;

  if (revealed >= totalPlayers || pages.length === 0 || loadedChunks.size >= pages.length) {
    loadMoreBtn.style.display = "none";
  } else {
    loadMoreBtn.style.display = "inline-block";
    loadMoreBtn.textContent = `Load next ${webIdx.page_size} rankings`;
  }
}

async function hydrateAndRender(metaEl, tbodyEl, filterEl, loadStatusEl, loadMoreBtn, filterNeedle) {
  const needle = filterNeedle.trim().toLowerCase();

  try {
    if (!legacyMono && webIdx?.pages?.length) {
      if (!needle) {
        await ensurePagesThroughRank(browseRevealMaxRank);
      } else {
        const matched = outlinePlayers.filter((o) => String(o.name).toLowerCase().includes(needle));
        await ensurePagesForRanks(matched.map((o) => o.rank));
      }
    }

    /** @type {OutlineRow[]} */
    let rows;
    if (needle) {
      rows = outlinePlayers.filter((o) => String(o.name).toLowerCase().includes(needle));
    } else if (legacyMono) {
      rows = outlinePlayers;
    } else {
      rows = rowsVisibleForBrowse();
    }

    renderTable(tbodyEl, rows, needle);
    updateChrome(metaEl, loadStatusEl, loadMoreBtn, filterNeedle, { browseRowsShown: rows.length });
  } catch (e) {
    tbodyEl.innerHTML = `<tr><td colspan="4" class="error">${escapeHtml(String(e.message || e))}</td></tr>`;
  }
}

async function bootstrap() {
  try {
    await loadChunkMode();
  } catch {
    resetState();
    try {
      await loadLegacyMonolith();
      return;
    } catch (_e2) {
      throw new Error("Need data/index.json + outline (+ pages) or legacy data/leaderboard.json.");
    }
  }
}

window.mountHome = async function mountHome(config) {
  const { metaEl, tbodyEl, filterEl, loadStatusEl, loadMoreBtn } = config;

  if (!initialized) {
    try {
      await bootstrap();
      initialized = true;
    } catch (e) {
      tbodyEl.innerHTML = `<tr><td colspan="4" class="error">${escapeHtml(String(e.message || e))}</td></tr>`;
      return;
    }


    loadMoreBtn.addEventListener("click", async () => {
      if (legacyMono || !webIdx) {
        return;
      }
      const total = Number(webIdx.total_players);
      browseRevealMaxRank = Math.min(total, browseRevealMaxRank + Number(webIdx.page_size));
      await hydrateAndRender(metaEl, tbodyEl, filterEl, loadStatusEl, loadMoreBtn, filterEl.value);
    });

    let debounceTimer = null;
    filterEl.addEventListener("input", () => {
      clearTimeout(debounceTimer);
      debounceTimer = setTimeout(() => {
        void hydrateAndRender(metaEl, tbodyEl, filterEl, loadStatusEl, loadMoreBtn, filterEl.value);
      }, 130);
    });
  }

  await hydrateAndRender(metaEl, tbodyEl, filterEl, loadStatusEl, loadMoreBtn, filterEl.value);
};
