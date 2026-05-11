/**
 * @param {string} dataPath relative to current page (e.g. ../data/leaderboard.json)
 * @param {{
 *   metaEl: HTMLElement | null,
 *   tbodyEl: HTMLElement,
 *   filterInput: HTMLInputElement | null,
 *   rankCol: boolean,
 *   nameKey: string,
 *   eloKey: string,
 *   gamesKey?: string | null,
 * }} opts
 */
async function mountEloTable(dataPath, opts) {
  const { metaEl, tbodyEl, filterInput, rankCol, nameKey, eloKey, gamesKey } = opts;

  let rows = [];
  try {
    const res = await fetch(dataPath);
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }
    const data = await res.json();

    if (Array.isArray(data.players)) {
      rows = data.players;
      if (metaEl) {
        if (data.as_of) {
          metaEl.textContent = `As of ${data.as_of}`;
        } else if (data.fetched_at_rfc3339) {
          const bits = [data.source, data.loaded_from, `snapshot ${data.fetched_at_rfc3339}`].filter(Boolean);
          metaEl.textContent = bits.join(" · ");
        }
      }
    }
  } catch (e) {
    tbodyEl.innerHTML = "";
    const tr = document.createElement("tr");
    tr.innerHTML = `<td colspan="4" class="error">Could not load data (${String(e.message)}). Run the deploy workflow or open with a local server so fetch works.</td>`;
    tbodyEl.appendChild(tr);
    return;
  }

  function render(filter) {
    const q = (filter || "").trim().toLowerCase();
    const qRaw = (filter || "").trim();
    tbodyEl.innerHTML = "";
    let rank = 0;
    for (const p of rows) {
      const name = p[nameKey] ?? "";
      if (q && !String(name).toLowerCase().includes(q)) {
        continue;
      }
      rank += 1;
      const tr = document.createElement("tr");
      const elo = p[eloKey];
      const games = gamesKey != null ? p[gamesKey] : null;
      const displayRank = qRaw.length > 0 ? rank : rankCol ? (p.rank ?? rank) : rank;
      tr.innerHTML = `
        <td class="num">${escapeHtml(String(displayRank))}</td>
        <td>${escapeHtml(String(name))}</td>
        <td class="num">${elo != null ? Number(elo).toFixed(1) : "—"}</td>
        <td class="num">${games != null ? escapeHtml(String(games)) : "—"}</td>
      `;
      tbodyEl.appendChild(tr);
    }
    if (tbodyEl.children.length === 0) {
      const tr = document.createElement("tr");
      tr.innerHTML = `<td colspan="4" class="muted">No rows match this filter.</td>`;
      tbodyEl.appendChild(tr);
    }
  }

  if (filterInput) {
    filterInput.addEventListener("input", () => render(filterInput.value));
  }
  render(filterInput ? filterInput.value : "");
}

function escapeHtml(s) {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

window.mountEloTable = mountEloTable;
