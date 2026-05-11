/**
 * Interactive leaderboard + expandable last-N games from data/leaderboard.json.
 */

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

/** @type {{ as_of?: string; players?: object[] }} */
let leaderboardState = {};
function closeAllDetails(tbody) {
  tbody.querySelectorAll(".detail-row").forEach((el) => el.classList.remove("open"));
  tbody.querySelectorAll(".name-toggle").forEach((el) => el.setAttribute("aria-expanded", "false"));
}

function gamesTable(games) {
  if (!Array.isArray(games) || games.length === 0) {
    return '<p class="muted nested-hint">No recent games listed (missing from export or CI used empty matches).</p>';
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
  return `<table class="nested">
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

function render(_metaEl, tbody, filterEl, q) {
  const players = leaderboardState.players ?? [];
  const needle = q.trim().toLowerCase();

  tbody.replaceChildren();

  players.forEach((p, idx) => {
    const name = p.name ?? "";
    if (needle && !String(name).toLowerCase().includes(needle)) {
      return;
    }
    const id = slugId(p.key, idx);
    const dispRank = needle.length > 0 ? "—" : p.rank != null ? String(p.rank) : String(idx + 1);

    const tr = document.createElement("tr");
    tr.className = "player-row";
    const nameBtn = document.createElement("button");
    nameBtn.type = "button";
    nameBtn.className = "name-toggle";
    nameBtn.dataset.playerId = id;
    nameBtn.setAttribute("aria-expanded", "false");
    nameBtn.innerHTML = escapeHtml(name) + '<span class="caret" aria-hidden="true"></span>';

    const tdRank = document.createElement("td");
    tdRank.className = "num";
    tdRank.textContent = dispRank;
    const tdName = document.createElement("td");
    tdName.appendChild(nameBtn);
    const tdElo = document.createElement("td");
    tdElo.className = "num";
    tdElo.textContent = p.elo != null ? Number(p.elo).toFixed(1) : "—";
    const tdGames = document.createElement("td");
    tdGames.className = "num";
    tdGames.textContent = p.games != null ? String(p.games) : "—";
    tr.append(tdRank, tdName, tdElo, tdGames);
    tbody.appendChild(tr);

    const detail = document.createElement("tr");
    detail.className = "detail-row";
    detail.id = `detail-${id}`;
    const td = document.createElement("td");
    td.colSpan = 4;
    td.className = "detail-cell";
    td.innerHTML = gamesTable(p.recent_games);
    detail.appendChild(td);
    tbody.appendChild(detail);

    bindRowToggle(tbody, id, nameBtn);
  });

  if (tbody.querySelector(".player-row") == null) {
    const empty = document.createElement("tr");
    empty.innerHTML = `<td colspan="4" class="muted">No players match.</td>`;
    tbody.appendChild(empty);
  }
}

async function mountHome(config) {
  const { metaEl, tbodyEl, filterEl } = config;
  try {
    const res = await fetch("data/leaderboard.json");
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }
    leaderboardState = await res.json();
    if (metaEl && leaderboardState.as_of) {
      metaEl.textContent = `As of ${leaderboardState.as_of} · click a name for the last exported games`;
    }
  } catch (e) {
    tbodyEl.innerHTML = `<tr><td colspan="4" class="error">Could not load leaderboard (${escapeHtml(String(e.message))}).</td></tr>`;
    return;
  }

  render(metaEl, tbodyEl, filterEl, filterEl.value);
  filterEl.addEventListener("input", () => render(metaEl, tbodyEl, filterEl, filterEl.value));
}

window.mountHome = mountHome;
