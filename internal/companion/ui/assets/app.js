// agentport panel. The page talks only to this Companion (the view token in the
// URL is the capability). Strings are written in English and looked up in the
// catalogs embedded in the page; nothing here builds markup from strings: every
// value reaches the DOM as text (see TestPanelScriptNeverBuildsMarkupFromStrings).
'use strict';

const BASE = document.querySelector('meta[name="agentport-base"]').content;
const LANGS = ['en', 'pt-BR', 'es', 'zh-Hant'];
const LANG_SHORT = {en: 'EN', 'pt-BR': 'PT', es: 'ES', 'zh-Hant': '中文'};
const CATALOGS = (() => {
  try { return JSON.parse(document.getElementById('agentport-i18n').textContent) || {}; } catch (_) { return {}; }
})();

function store(key, value) {
  try {
    if (value === undefined) return localStorage.getItem(key);
    localStorage.setItem(key, value);
  } catch (_) { /* private windows and blocked storage: the page works without it */ }
  return null;
}

// pickLanguage applies the rule: saved choice, then the browser's languages
// (zh-TW, zh-HK, zh-MO and zh-Hant are Traditional; zh-CN is English), then English.
function pickLanguage() {
  const saved = store('agentport.lang');
  if (LANGS.includes(saved)) return saved;
  for (const tag of (navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language || 'en'])) {
    if (/^zh-(TW|HK|MO|Hant)/i.test(tag)) return 'zh-Hant';
    if (/^zh/i.test(tag)) return 'en';
    if (/^pt/i.test(tag)) return 'pt-BR';
    if (/^es/i.test(tag)) return 'es';
    if (/^en/i.test(tag)) return 'en';
  }
  return 'en';
}

let LANG = pickLanguage();

// t returns the text of an English key in the current language, with {name}
// placeholders filled in. An untranslated key shows its English text.
function t(key, vars) {
  let text = (LANG !== 'en' && CATALOGS[LANG] && CATALOGS[LANG][key]) || key;
  if (vars) for (const name of Object.keys(vars)) text = text.split('{' + name + '}').join(String(vars[name]));
  return text;
}

function applyLanguage(language) {
  LANG = LANGS.includes(language) ? language : 'en';
  document.documentElement.lang = LANG;
  store('agentport.lang', LANG);
  document.getElementById('lang-lbl').textContent = LANG_SHORT[LANG];
  document.querySelectorAll('#lang-pop [data-lang]').forEach(b => b.setAttribute('aria-checked', String(b.dataset.lang === LANG)));
  applyStatic(document);
}

// applyStatic translates the markup written with data-i18n attributes.
function applyStatic(root) {
  root.querySelectorAll('[data-i18n]').forEach(el => { el.textContent = t(el.dataset.i18n); });
  root.querySelectorAll('[data-i18n-title]').forEach(el => { el.dataset.tip = t(el.dataset.i18nTitle); el.removeAttribute('title'); });
  root.querySelectorAll('[data-i18n-aria]').forEach(el => el.setAttribute('aria-label', t(el.dataset.i18nAria)));
  root.querySelectorAll('[data-i18n-placeholder]').forEach(el => el.setAttribute('placeholder', t(el.dataset.i18nPlaceholder)));
}

// --- DOM helpers --------------------------------------------------------------

// el builds an element. Every string child becomes a text node.
function el(tag, props, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value === undefined || value === null || value === false) continue;
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = String(value);
    else if (key.startsWith('on') && typeof value === 'function') node.addEventListener(key.slice(2), value);
    else if (key === 'dataset') Object.assign(node.dataset, value);
    else node.setAttribute(key, value === true ? '' : String(value));
  }
  for (const child of children.flat(Infinity)) {
    if (child === undefined || child === null || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}
const $ = id => document.getElementById(id);
const arrow = () => el('span', {class: 'arrow'}, '→');
function route(remote, local, extraClass) {
  return el('div', {class: 'r' + (extraClass ? ' ' + extraClass : '')}, ':' + remote, arrow(), el('span', {class: 'to'}, 'localhost:' + local));
}
const ICON_X = el('svg', {width: 12, height: 12, viewBox: '0 0 16 16', 'aria-hidden': 'true'}, el('path', {d: 'm4 4 8 8M12 4l-8 8', stroke: 'currentColor', 'stroke-width': 1.6, 'stroke-linecap': 'round', fill: 'none'}));

function fmtDuration(seconds) {
  const s = Math.max(0, Math.round(Number(seconds) || 0));
  if (s < 60) return t('{n} s', {n: s});
  if (s < 3600) return t('{n} min', {n: Math.floor(s / 60)});
  return t('{h} h {m} min', {h: Math.floor(s / 3600), m: Math.floor((s % 3600) / 60)});
}
function fmtTime(value) {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '—';
  return new Intl.DateTimeFormat(LANG, {hour: '2-digit', minute: '2-digit', second: '2-digit'}).format(d);
}
function fmtDateTime(value) {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '—';
  return new Intl.DateTimeFormat(LANG, {dateStyle: 'medium', timeStyle: 'short'}).format(d);
}

// --- API ----------------------------------------------------------------------

class ApiError extends Error {
  constructor(message, status, code) { super(message); this.status = status; this.code = code; }
}

async function api(method, path, body) {
  const options = {method, cache: 'no-store', headers: {}};
  if (body !== undefined) {
    options.headers['Content-Type'] = 'application/json';
    options.body = JSON.stringify(body);
  }
  const response = await fetch(BASE + path, options);
  let data = null;
  try { data = await response.json(); } catch (_) { /* plain-text errors */ }
  if (!response.ok) {
    throw new ApiError((data && data.message) || t('Request failed ({status})', {status: response.status}), response.status, data && data.code);
  }
  return data;
}
const ports = (method, path, body) => api(method, '/api/ports' + path, body);

// --- state --------------------------------------------------------------------

const S = {
  tab: 'open',
  groupBy: 'group',
  sess: 'all',
  status: null,
  profiles: [],
  settings: {auto_open: true, notify: true},
  forwards: [],
  pending: [],
  closed: [],
  activity: [],
  selected: new Set(),
  collapsed: new Set(),
  expanded: new Set(),
  renaming: null,
  offline: false,
  live: false,
  checks: new Map(),
  // Listening ports of the server, keyed by port, with the forwarded ones marked.
  portInfo: new Map(),
  detectedBusy: false,
  detectedError: '',
  detExpanded: new Set(),
  health: new Map(),
  menuOpen: false,
  stopArmed: null,
  policy: store('agentport.policy') === 'session' ? 'session' : 'ask',
};

async function loadState() {
  try {
    const st = await ports('GET', '/state');
    S.forwards = st.forwards || [];
    S.pending = st.pending || [];
    S.closed = st.closed || [];
    S.activity = st.activity || [];
    S.offline = false;
  } catch (_) {
    S.offline = true;
  }
  renderAll();
}

async function loadStatus() {
  try {
    S.status = await api('GET', '/api/status');
  } catch (_) {
    S.status = null;
  }
  renderHeader();
}

async function loadSettings() {
  try {
    S.settings = await api('GET', '/api/settings');
  } catch (_) { /* keep the defaults shown */ }
  renderSettings();
}

async function loadProfiles() {
  try {
    const data = await api('GET', '/api/profiles');
    S.profiles = data.profiles || [];
  } catch (_) {
    S.profiles = [];
  }
}

// checkLocal asks the Companion about a local port, caching the answer briefly.
async function checkLocal(port) {
  const cached = S.checks.get(port);
  if (cached && Date.now() - cached.at < 3000) return cached.result;
  const result = await ports('GET', '/local/check?port=' + encodeURIComponent(port));
  S.checks.set(port, {at: Date.now(), result});
  return result;
}

// --- rendering ----------------------------------------------------------------

function renderAll() {
  renderHeader();
  renderPending();
  renderTabs();
  renderOpen();
  renderDetected();
  renderClosed();
  renderLog();
  renderStrip();
  renderSelection();
  renderSessionFilter();
}

function renderHeader() {
  const s = S.status;
  $('prof-host').textContent = s ? s.destination || s.profile : '—';
  const connected = !!(s && s.tunnel && s.tunnel.connected);
  const dot = $('host-dot');
  dot.classList.toggle('off', !connected);
  $('host-pill').dataset.tip = connected ? t('Connected') : t('Disconnected');
  const started = s ? Date.parse(s.started_at) : NaN;
  const uptime = Number.isNaN(started) ? 0 : (Date.now() - started) / 1000;
  $('prof-label').textContent = s ? t('· profile {name} · {uptime}', {name: s.profile, uptime: fmtDuration(uptime)}) : '';
  const version = $('about-version');
  if (version && s && s.version) version.textContent = 'agentport ' + s.version;
}

function renderSettings() {
  $('bell').setAttribute('aria-pressed', String(!!S.settings.notify));
  $('set-auto').checked = !!S.settings.auto_open;
}

function renderPolicy() {
  $('pol-lbl').textContent = S.policy === 'session' ? t('Remember for this session') : t('Ask every time');
  document.querySelectorAll('[data-pol]').forEach(b => b.setAttribute('aria-checked', String(b.dataset.pol === S.policy)));
}

// --- pending requests ---------------------------------------------------------

const DURATIONS = () => [
  ['session', t('This session')],
  ['1h', t('1 hour')],
  ['pin', t('Until I close it')],
];

function durationSelect() {
  const select = el('select', {class: 'dur', 'aria-label': t('How long to keep the port open')});
  for (const [value, label] of DURATIONS()) select.append(el('option', {value}, label));
  return select;
}

function renderPending() {
  const box = $('pending');
  box.replaceChildren();
  $('req-section').hidden = S.pending.length === 0;
  $('pending-count').textContent = String(S.pending.length);
  for (const req of S.pending) {
    box.append(req.ports.length > 1 ? batchCard(req) : singleCard(req));
  }
  if (!S.pending.length) return;
  for (const req of S.pending) fillConflicts(req);
}

// requestWho names the agent and the folder that asked, showing only what is
// known: the generic "agent" name and a bare "/" folder are placeholders the
// Companion sends when the client did not say more.
function requestWho(req) {
  const agent = req.agent && req.agent !== 'agent' ? req.agent : '';
  const cwd = req.cwd && req.cwd !== '/' ? req.cwd : '';
  return [agent || t('an agent'), cwd].filter(Boolean).join(' · ');
}

function singleCard(req) {
  const port = req.ports[0];
  const duration = durationSelect();
  const decisions = {};
  const card = el('article', {class: 'req new', dataset: {req: req.id}},
    el('div', {class: 'req-top'}, route(port.remote_port, port.remote_port), el('span', {class: 'req-agent'}, requestWho(req))),
    req.reason ? el('p', {class: 'why'}, req.reason) : null,
    port.label || port.path ? el('div', {class: 'cmd'}, [port.label, port.path].filter(Boolean).join(' · ')) : null,
    el('div', {class: 'conflict', hidden: true, dataset: {conflict: String(port.remote_port)}}),
    el('div', {class: 'req-actions'},
      el('button', {class: 'btn primary', type: 'button', onclick: () => approveSingle(req, port, duration, decisions)}, t('Approve')),
      el('button', {class: 'btn', type: 'button', onclick: () => denyRequest(req)}, t('Deny')),
      duration,
      el('button', {class: 'link', type: 'button', onclick: () => openConfig(port.remote_port, {}, req, decisions)}, t('Port settings'))));
  return card;
}

function batchCard(req) {
  const selected = new Set(req.ports.map(p => p.remote_port));
  const duration = durationSelect();
  const rows = el('div', {class: 'brows'});
  const body = el('div', {class: 'blist'});
  const all = el('input', {type: 'checkbox', checked: true, 'aria-label': t('Select all')});
  const count = el('span', {class: 'bn-count'});
  const approveBtn = el('button', {class: 'btn primary', type: 'button'});
  const sync = () => {
    count.textContent = t('{n} selected', {n: selected.size});
    approveBtn.textContent = t('Approve {n}', {n: selected.size});
    approveBtn.disabled = selected.size === 0;
    all.checked = selected.size === req.ports.length;
  };
  req.ports.forEach((p, index) => {
    const box = el('input', {type: 'checkbox', checked: true, 'aria-label': t('Select :{port}', {port: p.remote_port})});
    const row = el('label', {class: 'brow' + (index >= 10 ? ' extra' : ''), dataset: {port: String(p.remote_port)}},
      box,
      el('span', {class: 'bp'}, ':' + p.remote_port),
      el('span', {class: 'arrow'}, '→'),
      el('span', {class: 'bl'}, 'localhost:' + p.remote_port),
      el('span', {class: 'bn'}, el('b', null, p.label || ''), el('small', null, p.path || '')));
    box.addEventListener('change', () => {
      if (box.checked) selected.add(p.remote_port); else selected.delete(p.remote_port);
      row.classList.toggle('off', !box.checked);
      sync();
    });
    rows.append(row);
  });
  all.addEventListener('change', () => {
    req.ports.forEach(p => selected[all.checked ? 'add' : 'delete'](p.remote_port));
    rows.querySelectorAll('input').forEach(i => { i.checked = all.checked; i.dispatchEvent(new Event('change')); });
  });
  const more = el('button', {class: 'bmore' + (req.ports.length > 10 ? ' d' : ''), type: 'button'});
  more.textContent = t('Show {n} more', {n: Math.max(0, req.ports.length - 10)});
  more.addEventListener('click', () => {
    const open = card.classList.toggle('all');
    more.textContent = open ? t('Show less') : t('Show {n} more', {n: Math.max(0, req.ports.length - 10)});
  });
  approveBtn.addEventListener('click', () => approveBatch(req, selected, duration));
  body.append(
    el('div', {class: 'bhead'}, el('label', null, all, t('Select all')), count),
    rows, more);
  sync();
  const card = el('article', {class: 'req batch new', dataset: {req: req.id}},
    el('div', {class: 'req-top'}, el('span', {class: 'route'}, t('{n} ports', {n: req.ports.length})), el('span', {class: 'req-agent'}, requestWho(req))),
    req.reason ? el('p', {class: 'why'}, req.reason) : null,
    body,
    el('div', {class: 'req-actions'}, approveBtn,
      el('button', {class: 'btn', type: 'button', onclick: () => denyRequest(req)}, t('Deny all')), duration));
  return card;
}

// fillConflicts shows, under each single port, what holds its local port.
async function fillConflicts(req) {
  for (const p of req.ports) {
    let check;
    try { check = await checkLocal(p.remote_port); } catch (_) { continue; }
    if (check.status === 'free' || check.status === 'open_port') continue;
    const box = document.querySelector('[data-conflict="' + p.remote_port + '"]');
    if (!box) continue;
    box.hidden = false;
    box.replaceChildren();
    const who = check.process ? t('{process} (pid {pid})', {process: check.process, pid: check.pid}) : t('another program');
    const message = check.status === 'other_profile'
      ? t('Port {port} is held by another agentport profile.', {port: p.remote_port})
      : t('Port {port} is busy: {who}.', {port: p.remote_port, who});
    box.append(el('p', null, el('b', null, t('Busy port')), ' ', message));
    const remap = el('input', {type: 'radio', name: 'c' + p.remote_port, value: 'remap', checked: true});
    box.append(el('label', null, remap, t('Use another free port')));
    const stop = el('input', {type: 'radio', name: 'c' + p.remote_port, value: 'stop', disabled: !check.stoppable});
    const label = el('label', {class: check.stoppable ? '' : 'dis'}, stop, t('Stop {process} and use :{port}', {process: check.process || t('it'), port: p.remote_port}));
    box.append(label);
    if (!check.stoppable) box.append(el('small', {class: 'why-no'}, t('This program is not one agentport can stop.')));
    box.append(el('button', {class: 'link', type: 'button', onclick: () => openConfig(p.remote_port, {}, req, null)}, t('Port settings')));
    box.dataset.stop = check.stoppable ? 'yes' : 'no';
    box.dataset.check = JSON.stringify(check);
  }
}

// chosenDecisions reads the conflict boxes of a request: stop-then-use or remap.
function chosenDecisions(req) {
  const decisions = {};
  for (const p of req.ports) {
    const box = document.querySelector('[data-conflict="' + p.remote_port + '"]');
    if (!box || box.hidden) continue;
    const picked = box.querySelector('input[type=radio]:checked');
    if (picked && picked.value === 'stop') decisions[p.remote_port] = {stop: true, local: p.remote_port};
  }
  return decisions;
}

async function approveSingle(req, port, duration) {
  await approveRequest(req, new Set([port.remote_port]), duration.value);
}

async function approveBatch(req, selected, duration) {
  await approveRequest(req, selected, duration.value);
}

// CONFIG keeps the local ports chosen in the port settings of a request, by "request:remote".
const CONFIG = new Map();

async function approveRequest(req, selected, duration) {
  const decisions = chosenDecisions(req);
  try {
    for (const [remote, d] of Object.entries(decisions)) {
      if (!selected.has(Number(remote))) continue;
      await ports('POST', '/forwards/stop-owner', {remote_port: Number(remote), local_port: d.local});
    }
    const body = {
      duration,
      ports: req.ports.map(p => {
        if (!selected.has(p.remote_port)) return {remote_port: p.remote_port, skip: true};
        const local = CONFIG.get(req.id + ':' + p.remote_port);
        return local ? {remote_port: p.remote_port, local_port: local} : {remote_port: p.remote_port};
      }),
    };
    const result = await ports('POST', '/requests/' + encodeURIComponent(req.id) + '/approve', body);
    for (const p of req.ports) CONFIG.delete(req.id + ':' + p.remote_port);
    const results = result.results || [];
    const opened = results.filter(r => r.state === 'active').length;
    const failed = results.filter(r => r.state === 'error');
    if (failed.length) toast(t('{n} ports could not be opened.', {n: failed.length}) + ' ' + failed[0].message, true);
    else toast(t('{n} ports approved', {n: opened}));
    S.checks.clear();
  } catch (error) {
    toast(error.message, true);
  }
  await loadState();
}

async function denyRequest(req) {
  try {
    await ports('POST', '/requests/' + encodeURIComponent(req.id) + '/deny');
    toast(t('Denied'));
  } catch (error) {
    toast(error.message, true);
  }
  await loadState();
}

// --- tabs and lists -----------------------------------------------------------

function renderTabs() {
  for (const tab of ['open', 'det', 'closed', 'log', 'term']) {
    const button = $('t-' + tab);
    button.setAttribute('aria-selected', String(S.tab === tab));
  }
  $('p-open').hidden = S.tab !== 'open';
  $('o-tools').hidden = S.tab !== 'open' || S.forwards.length === 0;
  $('p-det').hidden = S.tab !== 'det';
  $('p-closed').hidden = S.tab !== 'closed';
  $('p-log').hidden = S.tab !== 'log';
  $('p-term').hidden = S.tab !== 'term';
  $('c-open').textContent = String(S.forwards.length);
  $('c-closed').textContent = String(S.closed.length);
}

function setTab(tab) {
  S.tab = tab;
  renderTabs();
  if (tab === 'det') loadDetected(true);
  if (tab === 'term') termShown();
}

function visibleForwards() {
  if (S.sess === 'all') return S.forwards;
  return S.forwards.filter(f => (f.owners || []).includes(S.sess));
}

function renderSessionFilter() {
  const select = $('sess-f');
  const names = new Set();
  for (const f of S.forwards) for (const o of f.owners || []) if (o !== 'user') names.add(o);
  for (const p of S.pending) if (p.agent) names.add(p.agent);
  const current = S.sess;
  select.replaceChildren(el('option', {value: 'all'}, t('All sessions')));
  for (const name of [...names].sort()) select.append(el('option', {value: name}, name));
  if (![...select.options].some(o => o.value === current)) S.sess = 'all';
  select.value = S.sess;
}

function groupKey(f) {
  if (S.groupBy === 'group') return f.group || t('No group');
  if (S.groupBy === 'session') return (f.owners || []).filter(o => o !== 'user').join(', ') || t('You');
  return '';
}

function renderOpen() {
  const box = $('p-open');
  box.replaceChildren();
  renderOpenTools();
  const rows = visibleForwards();
  if (!rows.length) {
    box.append(el('div', {class: 'list-empty'}, S.forwards.length ? t('No port matches this session filter.') : (S.offline ? t('The Companion is not answering.') : t('No ports open. The agent asks for a port, or you open one with /.'))));
    return;
  }
  if (S.groupBy === 'list') {
    for (const f of rows) box.append(forwardRow(f));
    return;
  }
  const groups = new Map();
  for (const f of rows) {
    const key = groupKey(f);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(f);
  }
  for (const [key, items] of groups) {
    const collapsed = S.collapsed.has(key);
    const allSelected = items.every(f => S.selected.has(f.remote_port));
    const check = el('input', {type: 'checkbox', class: 'rck', checked: allSelected, 'aria-label': t('Select group {name}', {name: key})});
    check.addEventListener('change', () => {
      items.forEach(f => check.checked ? S.selected.add(f.remote_port) : S.selected.delete(f.remote_port));
      renderOpen();
      renderSelection();
    });
    const toggle = el('button', {class: 'gtog', type: 'button', 'aria-expanded': String(!collapsed), onclick: () => {
      collapsed ? S.collapsed.delete(key) : S.collapsed.add(key);
      renderOpen();
    }}, el('span', {class: 'chev', 'aria-hidden': 'true'}, '▾'), el('b', null, key));
    box.append(el('div', {class: 'ghead' + (collapsed ? ' col' : '')},
      check, toggle,
      el('span', {class: 'gmeta'}, el('span', null, t('{n} ports', {n: items.length})), el('span', null, items.some(f => f.pinned) ? t('pinned') : null)),
      el('button', {class: 'btn ghost gclose', type: 'button', onclick: () => confirmClose(items.map(f => f.remote_port), t('Close group {name}?', {name: key}))}, t('Close group'))));
    if (!collapsed) for (const f of items) box.append(forwardRow(f));
  }
}

function renderOpenTools() {
  const tools = $('o-tools');
  tools.replaceChildren();
  if (!S.forwards.length) return;
  const seg = el('div', {class: 'seg', role: 'group', 'aria-label': t('Group by')});
  for (const [value, label] of [['group', t('Group')], ['session', t('Session')], ['list', t('List')]]) {
    seg.append(el('button', {type: 'button', 'aria-pressed': String(S.groupBy === value), onclick: () => { S.groupBy = value; renderOpen(); renderOpenTools(); }}, label));
  }
  tools.append(el('div', {class: 'ot-l'}, el('span', {class: 'ot-lbl'}, t('Group by')), seg),
    el('button', {class: 'btn danger', type: 'button', onclick: () => confirmCloseAll()},
      t('Close all on {server}', {server: S.status ? S.status.destination || S.status.profile : t('this server')})));
}

function forwardRow(f) {
  const checked = S.selected.has(f.remote_port);
  const box = el('input', {type: 'checkbox', class: 'rck', checked, 'aria-label': t('Select :{port}', {port: f.remote_port})});
  box.addEventListener('change', () => {
    box.checked ? S.selected.add(f.remote_port) : S.selected.delete(f.remote_port);
    renderSelection();
    row.classList.toggle('sel', box.checked);
  });
  const name = el('span', {class: 'name', title: t('Double-click to rename'), dataset: {tip: t('Double-click to rename')}, ondblclick: () => startRename(f)}, f.label || remoteName(f.remote_port) || t('No name'));
  if (S.renaming === f.remote_port) {
    const input = el('input', {class: 'rename', value: f.label || '', 'aria-label': t('Rename :{port}', {port: f.remote_port})});
    input.addEventListener('keydown', e => { if (e.key === 'Enter') commitRename(f, input.value); if (e.key === 'Escape') { S.renaming = null; renderOpen(); } });
    input.addEventListener('blur', () => commitRename(f, input.value));
    setTimeout(() => input.focus(), 0);
    name.replaceChildren(input);
  }
  const chips = el('div', {class: 'chips'});
  if (f.state === 'active') chips.append(el('span', {class: 'chip live'}, t('Live')));
  if (f.state === 'reserved') chips.append(el('span', {class: 'chip'}, t('Waiting for the tunnel')));
  if (f.state === 'session_ended') chips.append(el('span', {class: 'chip'}, t('Agent ended')));
  chips.append(f.origin === 'manual' ? el('span', {class: 'chip'}, t('You')) : el('span', {class: 'chip agent'}, t('Agent')));
  for (const o of f.owners || []) if (o !== 'user') chips.append(el('span', {class: 'chip'}, o));
  if (f.pinned) chips.append(el('span', {class: 'flag'}, t('Pinned')));
  const what = el('div', {class: 'what'}, el('b', null, name), el('small', null, f.path ? f.url + f.path : f.url), chips);
  const row = el('div', {class: 'item' + (checked ? ' sel' : ''), dataset: {port: String(f.remote_port)}},
    box,
    route(f.remote_port, f.local_port),
    what,
    el('div', {class: 'acts'},
      el('a', {class: 'ibtn', href: f.url + (f.path || ''), target: '_blank', rel: 'noopener noreferrer', 'data-tip': t('Open in a new tab'), 'aria-label': t('Open in a new tab')}, linkIcon()),
      el('button', {class: 'ibtn' + (f.pinned ? ' on' : ''), type: 'button', 'aria-pressed': String(!!f.pinned), 'data-tip': f.pinned ? t('Unpin') : t('Pin'), 'aria-label': f.pinned ? t('Unpin') : t('Pin'), onclick: () => togglePin(f)}, pinIcon()),
      el('button', {class: 'ibtn', type: 'button', 'data-tip': t('Edit'), 'aria-label': t('Edit'), onclick: () => openEdit(f)}, editIcon()),
      el('button', {class: 'ibtn', type: 'button', 'aria-expanded': String(S.expanded.has(f.remote_port)), 'data-tip': t('Details'), 'aria-label': t('Details'), onclick: () => toggleDetails(f)}, el('span', {class: 'chev'}, '▾')),
      el('button', {class: 'ibtn danger', type: 'button', 'data-tip': t('Close'), 'aria-label': t('Close'), onclick: () => confirmClose([f.remote_port], t('Close :{port}?', {port: f.remote_port}), f)}, ICON_X)));
  row.addEventListener('dblclick', e => { if (e.target.closest('button,a,input')) return; startRename(f); });
  const parts = [row];
  if (S.expanded.has(f.remote_port)) parts.push(detailsBox(f));
  return el('div', {class: 'row-wrap'}, parts);
}

function linkIcon() {
  return el('svg', {width: 14, height: 14, viewBox: '0 0 16 16', 'aria-hidden': 'true'}, el('path', {d: 'M9 3h4v4M13 3 7.5 8.5M6 4H3.5v8.5H12V10', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.4, 'stroke-linecap': 'round', 'stroke-linejoin': 'round'}));
}
function pinIcon() {
  return el('svg', {width: 14, height: 14, viewBox: '0 0 16 16', 'aria-hidden': 'true'}, el('path', {d: 'M9.5 2.5 13.5 6.5 10.5 7.5 8.5 11.5 4.5 7.5 8.5 3.5ZM7 9 2.5 13.5', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.4, 'stroke-linejoin': 'round', 'stroke-linecap': 'round'}));
}
function editIcon() {
  return el('svg', {width: 14, height: 14, viewBox: '0 0 16 16', 'aria-hidden': 'true'}, el('path', {d: 'M3 13l.6-2.6L10.8 3.2l2 2L5.6 12.4ZM9.8 4.2l2 2', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.4, 'stroke-linejoin': 'round'}));
}

// remoteName is what runs on the server behind a forwarded port, for a forward
// with no label: the process name, else the first word of its command line.
// Empty when the server has not reported it.
function remoteName(port) {
  const info = S.portInfo.get(port);
  if (!info) return '';
  if (info.process) return info.process;
  const first = (info.cmdline || '').trim().split(/\s+/)[0] || '';
  return first.split('/').pop().slice(0, 32);
}

function detailsBox(f) {
  const owners = (f.owners || []).length ? f.owners.join(', ') : t('nobody (the port is kept by you)');
  const items = [
    [t('Remote port'), ':' + f.remote_port],
    [t('Local address'), 'localhost:' + f.local_port],
    [t('Started'), fmtDateTime(f.created_at)],
    [t('Uptime'), fmtDuration(f.uptime_seconds)],
    [t('Owners'), owners],
    [t('Origin'), f.origin === 'manual' ? t('You opened it') : t('An agent asked for it')],
    [t('State'), f.state],
  ];
  const info = S.portInfo.get(f.remote_port);
  const process = info ? processBox(info) : el('p', {class: 'dp-empty'}, t('Process details appear once the port is detected on the server.'));
  return el('div', {class: 'dp-wrap'}, el('dl', {class: 'dp'}, items.map(([k, v]) => el('div', null, el('dt', null, k), el('dd', null, v)))),
    process, healthBox(f.local_port));
}

function toggleDetails(f) {
  S.expanded.has(f.remote_port) ? S.expanded.delete(f.remote_port) : S.expanded.add(f.remote_port);
  renderOpen();
  if (S.expanded.has(f.remote_port)) loadDetected(false);
}

function startRename(f) {
  S.renaming = f.remote_port;
  renderOpen();
}

async function commitRename(f, value) {
  if (S.renaming !== f.remote_port) return;
  S.renaming = null;
  const label = value.trim();
  if (label === (f.label || '')) { renderOpen(); return; }
  try {
    await ports('POST', '/forwards/' + f.remote_port + '/rename', {label});
    toast(t('Renamed'));
  } catch (error) {
    toast(error.message, true);
  }
  await loadState();
}

async function togglePin(f) {
  try {
    await ports('POST', '/forwards/' + f.remote_port + '/pin', {pinned: !f.pinned});
    toast(f.pinned ? t('Unpinned :{port}', {port: f.remote_port}) : t('Pinned :{port}', {port: f.remote_port}));
  } catch (error) {
    toast(error.message, true);
  }
  await loadState();
}

function renderSelection() {
  const bar = $('selbar');
  const n = S.selected.size;
  // Forget selections of ports that closed meanwhile.
  for (const port of [...S.selected]) if (!S.forwards.some(f => f.remote_port === port)) S.selected.delete(port);
  document.querySelectorAll('#p-open .item').forEach(row => row.classList.toggle('sel', S.selected.has(Number(row.dataset.port))));
  bar.hidden = n === 0;
  bar.replaceChildren();
  if (!n) return;
  bar.append(
    el('span', {class: 'sb-n'}, t('{n} selected', {n})),
    el('button', {class: 'btn danger', type: 'button', onclick: () => confirmClose([...S.selected], t('Close {n} ports?', {n}))}, t('Close selected')),
    el('button', {class: 'btn ghost', type: 'button', onclick: () => { S.selected.clear(); renderOpen(); renderSelection(); }}, t('Clear')));
}

// --- detected ports ----------------------------------------------------------

// loadDetected scans the server through the Companion. refresh asks for a scan
// now; without it the Companion answers from its short cache. One request also
// carries the forwarded ports, so the open forwards get their process details.
async function loadDetected(refresh) {
  S.detectedBusy = true;
  renderDetected();
  try {
    const data = await ports('GET', '/detected?all=1' + (refresh ? '&refresh=1' : ''));
    S.portInfo = new Map((data.ports || []).map(p => [p.port, p]));
    S.detectedError = data.error || '';
  } catch (error) {
    S.detectedError = error.message;
  }
  S.detectedBusy = false;
  renderDetected();
  renderOpen();
  renderMenu();
}

// detectedList is the server's listening ports that no forward uses yet.
function detectedList() {
  return [...S.portInfo.values()].filter(p => !p.forwarded).sort((a, b) => a.port - b.port);
}

function renderDetected() {
  const box = $('p-det');
  const list = detectedList();
  $('c-det').textContent = String(list.length);
  box.replaceChildren();
  if (S.detectedBusy && !list.length) {
    box.append(el('div', {class: 'list-empty'}, t('Scanning the server…')));
    return;
  }
  if (S.detectedError) box.append(el('div', {class: 'list-empty'}, t('The server could not be scanned: {error}', {error: S.detectedError})));
  else if (!list.length) box.append(el('div', {class: 'list-empty'}, t('No other listening ports were found on the server.')));
  for (const p of list) box.append(detectedRow(p));
}

function detectedRow(p) {
  const what = el('div', {class: 'what'}, el('b', null, p.process || t('Unknown process')), el('small', null, [p.bind, p.user].filter(Boolean).join(' · ')));
  const row = el('div', {class: 'item', dataset: {port: String(p.port)}},
    el('div', {class: 'r'}, ':' + p.port),
    what,
    el('div', {class: 'acts'},
      el('button', {class: 'btn', type: 'button', onclick: () => openDetected(p)}, t('Open')),
      el('button', {class: 'ibtn', type: 'button', 'aria-expanded': String(S.detExpanded.has(p.port)), 'data-tip': t('Details'), 'aria-label': t('Details'), onclick: () => toggleDetected(p)}, el('span', {class: 'chev'}, '▾'))));
  const parts = [row];
  if (S.detExpanded.has(p.port)) parts.push(processBox(p));
  return el('div', {class: 'row-wrap'}, parts);
}

function toggleDetected(p) {
  S.detExpanded.has(p.port) ? S.detExpanded.delete(p.port) : S.detExpanded.add(p.port);
  renderDetected();
}

async function openDetected(p) {
  try {
    await ports('POST', '/forwards/open', {remote_port: p.port});
    toast(t('Opened :{port}', {port: p.port}));
  } catch (error) {
    toast(error.message, true);
  }
  await loadState();
  await loadDetected(true);
}

// processBox lists what the server knows about the process that listens on a port.
function processBox(p) {
  const items = [
    [t('Process'), p.process || '-'],
    [t('PID'), p.pid ? String(p.pid) : '-'],
    [t('User'), p.user || '-'],
    [t('Bind'), p.bind || '-'],
    [t('Command line'), p.cmdline || '-'],
    [t('Working directory'), p.cwd || '-'],
  ];
  return el('dl', {class: 'dp'}, items.map(([k, v]) => el('div', null, el('dt', null, k), el('dd', null, v))));
}

// healthBox shows whether the local end of a forward answers. The check is
// cached for a few seconds on the Companion side and here.
function healthBox(port) {
  const box = el('div', {class: 'dp-health'}, t('Checking…'));
  healthOf(port).then(h => {
    const lines = [h.tcp ? t('TCP ok') : t('TCP not reachable: {error}', {error: h.tcp_error || '-'})];
    if (h.http_status) lines.push(t('HTTP status {status}, answered in {ms} ms', {status: h.http_status, ms: h.http_latency_ms}));
    box.replaceChildren(...lines.map(line => el('div', null, line)));
  }).catch(error => box.replaceChildren(el('div', null, error.message)));
  return box;
}

async function healthOf(port) {
  const cached = S.health.get(port);
  if (cached && Date.now() - cached.at < 3000) return cached.result;
  const result = await ports('GET', '/local/health?port=' + encodeURIComponent(port));
  S.health.set(port, {at: Date.now(), result});
  return result;
}

// --- port box -----------------------------------------------------------------

// renderMenu fills the list under the port box with the detected ports that
// match what is typed. Choosing one puts its number in the box; Enter opens it.
function renderMenu() {
  const menu = $('menu');
  const input = $('add-port');
  const typed = input.value.trim();
  const matches = detectedList().filter(p => !typed || String(p.port).startsWith(typed)).slice(0, 12);
  menu.replaceChildren();
  const show = S.menuOpen && matches.length > 0;
  menu.hidden = !show;
  input.setAttribute('aria-expanded', String(show));
  for (const p of matches) {
    menu.append(el('button', {class: 'mi', type: 'button', role: 'option', 'aria-selected': String(String(p.port) === typed),
      onmousedown: e => e.preventDefault(), onclick: () => pickDetected(p)},
      el('span', {class: 'mono'}, ':' + p.port),
      el('span', null, p.process || t('Unknown process'))));
  }
}

function pickDetected(p) {
  S.menuOpen = false;
  $('add-port').value = String(p.port);
  renderMenu();
  $('add-port').focus();
}

function reasonLabel(reason) {
  switch (reason) {
    case 'user': return t('You closed it');
    case 'agent': return t('The agent released it');
    case 'session_ended': return t('Agent ended');
    case 'expired': return t('Time ran out');
    default: return reason;
  }
}

function renderClosed() {
  const box = $('p-closed');
  box.replaceChildren();
  if (!S.closed.length) {
    box.append(el('div', {class: 'list-empty'}, t('Nothing closed yet.')));
    return;
  }
  for (const c of [...S.closed].reverse()) {
    const row = el('div', {class: 'item'},
      route(c.remote_port, c.local_port),
      el('div', {class: 'what'}, el('b', null, c.label || t('No name')), el('small', null, reasonLabel(c.reason) + ' · ' + fmtDateTime(c.closed_at))),
      el('div', {class: 'acts'},
        el('button', {class: 'btn', type: 'button', onclick: () => reopen(c)}, t('Reopen')),
        el('button', {class: 'btn ghost', type: 'button', onclick: () => openEditClosed(c)}, t('Reopen with changes')),
        el('button', {class: 'ibtn', type: 'button', 'data-tip': t('Remove from history'), 'aria-label': t('Remove from history'), onclick: () => forgetClosed(c)}, ICON_X)));
    box.append(row);
  }
}

async function reopen(c, changes) {
  try {
    await ports('POST', '/closed/reopen', Object.assign({remote_port: c.remote_port, local_port: c.local_port, label: c.label || '', path: c.path || '', closed_at: c.closed_at}, changes || {}));
    toast(t('Reopened :{port}', {port: c.remote_port}));
  } catch (error) {
    toast(error.message, true);
  }
  await loadState();
}

async function forgetClosed(c) {
  try {
    await ports('POST', '/closed/forget', {remote_port: c.remote_port, closed_at: c.closed_at});
  } catch (error) {
    toast(error.message, true);
  }
  await loadState();
}

function renderLog() {
  const list = $('p-log');
  list.replaceChildren();
  if (!S.activity.length) {
    list.append(el('li', {class: 'list-empty'}, t('No activity yet.')));
    return;
  }
  for (const a of [...S.activity].reverse()) {
    list.append(el('li', null, el('time', {datetime: a.at}, fmtTime(a.at)), el('i', {class: a.kind || ''}), el('span', null, a.text)));
  }
}

function renderStrip() {
  const s = S.status;
  const tunnel = $('st-tunnel');
  const tunnelUp = !!(s && s.tunnel && s.tunnel.connected);
  const tunnelError = s && s.tunnel && !tunnelUp ? s.tunnel.last_error : '';
  tunnel.textContent = s ? (tunnelUp ? t('active') : t('down') + (tunnelError ? ' — ' + tunnelError : '')) : '—';
  tunnel.title = tunnelError || '';
  tunnel.style.color = s ? (tunnelUp ? 'var(--live)' : 'var(--stop)') : '';
  const sessions = new Set();
  for (const f of S.forwards) for (const o of f.owners || []) if (o !== 'user') sessions.add(o);
  for (const p of S.pending) if (p.session) sessions.add(p.session);
  $('sess-count').textContent = t('{n} active', {n: sessions.size});
  $('st-open').textContent = String(S.forwards.length);
  $('st-panel').textContent = location.host;
  $('foot-version').textContent = s && s.version ? 'agentport ' + s.version : 'agentport';
}

function renderHostState() {
  $('host-dot').classList.toggle('off', S.offline || !(S.status && S.status.tunnel && S.status.tunnel.connected));
}

// --- modals -------------------------------------------------------------------

function openOverlay(id, content) {
  const overlay = $(id);
  overlay.replaceChildren(content);
  overlay.hidden = false;
  const first = content.querySelector('input,select,button');
  if (first) setTimeout(() => first.focus(), 0);
  return overlay;
}
function closeOverlay(id) {
  const overlay = $(id);
  if (overlay) { overlay.hidden = true; overlay.replaceChildren(); }
}
function closeAllOverlays() {
  for (const id of ['info-ov', 'help-ov', 'cfg-ov', 'confirm-ov', 'edit-ov', 'add-ov', 'switch-ov']) closeOverlay(id);
}

function modal(kind, title, ...children) {
  return el('div', {class: 'modal ' + kind, role: 'dialog', 'aria-modal': 'true', 'aria-label': title},
    el('h2', null, title), ...children);
}

function confirmDialog(title, text, action, label) {
  const dialog = modal('confirm', title,
    el('p', {class: 'cf-p'}, text),
    el('div', {class: 'cfg-foot'},
      el('button', {class: 'btn', type: 'button', onclick: () => closeOverlay('confirm-ov')}, t('Cancel')),
      el('button', {class: 'btn danger-solid', type: 'button', onclick: async () => { closeOverlay('confirm-ov'); await action(); }}, label)));
  openOverlay('confirm-ov', dialog);
}

function confirmClose(remotes, title, single) {
  const owners = single ? (single.owners || []).filter(o => o !== 'user') : [];
  const text = owners.length
    ? t('The agent sessions {owners} will be told that the port is closed.', {owners: owners.join(', ')})
    : t('No agent session is using this port.');
  confirmDialog(title, text, () => closePorts(remotes), t('Close'));
}

function confirmCloseAll() {
  const n = S.forwards.length;
  confirmDialog(t('Close all {n} ports?', {n}), t('Every open port on this server closes. Agents are told that the ports are gone.'),
    () => closeAllPorts(), t('Close all'));
}

async function closePorts(remotes) {
  try {
    const result = await ports('POST', '/forwards/close', {remote_ports: remotes});
    S.selected.clear();
    toast(t('{n} ports closed', {n: (result.closed || []).length}));
  } catch (error) {
    toast(error.message, true);
  }
  await loadState();
}

async function closeAllPorts() {
  try {
    const result = await ports('POST', '/forwards/close-all', {});
    S.selected.clear();
    toast(t('{n} ports closed', {n: (result.closed || []).length}));
  } catch (error) {
    toast(error.message, true);
  }
  await loadState();
}

function openEdit(f) {
  editPort({remote: f.remote_port, local: f.local_port, label: f.label || '', path: f.path || '', mode: 'edit'});
}

function openEditClosed(c) {
  editPort({remote: c.remote_port, local: c.local_port, label: c.label || '', path: c.path || '', mode: 'reopen', closed: c});
}

// openConfig is the per-port config of a pending request (local port and duration).
function openConfig(remote, _unused, req) {
  const local = (req && CONFIG.get(req.id + ':' + remote)) || remote;
  editPort({remote, local, label: '', path: '', mode: 'config', req});
}

// editPort is the port dialog: edit an open forward, reopen a closed one with
// changes, or configure a requested port.
function editPort(o) {
  const title = o.mode === 'reopen' ? t('Reopen :{port} with changes', {port: o.remote}) : (o.mode === 'config' ? t('Port :{port}', {port: o.remote}) : t('Edit :{port}', {port: o.remote}));
  const label = el('input', {class: 'txt', value: o.label, maxlength: 64, 'aria-label': t('Name')});
  const path = el('input', {class: 'txt', value: o.path, maxlength: 256, placeholder: '/', 'aria-label': t('Path')});
  const local = el('input', {class: 'txt', value: String(o.local), inputmode: 'numeric', maxlength: 5, 'aria-label': t('Local port')});
  const status = el('p', {class: 'st', role: 'status'});
  const duration = el('select', {class: 'dur dur-fld', 'aria-label': t('How long to keep the port open')},
    o.mode === 'edit' ? el('option', {value: ''}, t('Keep as it is')) : null,
    el('option', {value: 'session'}, t('This session')),
    el('option', {value: '1h'}, t('1 hour')),
    el('option', {value: 'pin'}, t('Until I close it')));
  let checked = {status: 'free'};
  const save = el('button', {class: 'btn primary', type: 'button'}, o.mode === 'edit' ? t('Save') : t('Open'));
  const validate = async () => {
    const port = Number(local.value);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      status.className = 'st bad';
      status.textContent = t('Use a number between 1 and 65535.');
      save.disabled = true;
      return;
    }
    if (o.mode === 'edit' && port === o.local) {
      status.className = 'st ok';
      status.textContent = t('Same port.');
      save.disabled = false;
      return;
    }
    try {
      checked = await checkLocal(port);
    } catch (_) { return; }
    const messages = {
      free: [t('Free.'), 'ok'],
      open_port: [t('Already used by an open port of this server.'), 'bad'],
      other_profile: [t('Used by another agentport profile.'), 'bad'],
      process: [t('Used by {process} (pid {pid}).', {process: checked.process || t('another program'), pid: checked.pid || '—'}), 'bad'],
    };
    const [text, cls] = messages[checked.status] || [t('Unknown.'), 'bad'];
    status.className = 'st ' + cls;
    status.textContent = text;
    save.disabled = checked.status !== 'free' && !(o.mode === 'config');
  };
  let timer = null;
  local.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(validate, 200); });
  validate();
  save.addEventListener('click', async () => {
    const body = {};
    if (label.value !== o.label) body.label = label.value.trim();
    if (path.value !== o.path) body.path = path.value.trim();
    if (Number(local.value) !== o.local) body.local_port = Number(local.value);
    if (duration.value) body.duration = duration.value;
    closeOverlay('cfg-ov'); closeOverlay('edit-ov');
    try {
      if (o.mode === 'edit') {
        await ports('POST', '/forwards/' + o.remote + '/edit', body);
        toast(t('Saved'));
      } else if (o.mode === 'reopen') {
        await reopen(o.closed, {label: body.label !== undefined ? body.label : o.label, path: body.path !== undefined ? body.path : o.path, local_port: Number(local.value), duration: duration.value});
        return;
      } else {
        if (o.req) {
          // Configuring a requested port: the local port is used when the request is approved.
          CONFIG.set(o.req.id + ':' + o.remote, Number(local.value));
          toast(t('Saved for the approval'));
        }
      }
    } catch (error) {
      toast(error.message, true);
    }
    await loadState();
  });
  const cancel = el('button', {class: 'btn', type: 'button', onclick: () => closeOverlay('edit-ov')}, t('Cancel'));
  const fields = [
    el('label', {class: 'fld'}, el('span', null, t('Name')), label),
    el('label', {class: 'fld'}, el('span', null, t('Local port')), el('div', {class: 'cfg-port'}, 'localhost:', local)),
    status,
    el('label', {class: 'fld'}, el('span', null, t('Path')), path),
    el('label', {class: 'fld'}, el('span', null, t('Keep the port for')), duration),
  ];
  openOverlay('edit-ov', modal('cfg', title, el('div', {class: 'form'}, fields), el('div', {class: 'cfg-foot'}, cancel, save)));
}

// --- switch menu --------------------------------------------------------------

function renderProfiles() {
  const menu = $('pmenu');
  menu.replaceChildren();
  if (!S.profiles.length) {
    menu.append(el('div', {class: 'mh'}, t('Servers on this computer')), el('div', {class: 'mfoot'}, t('No servers are set up yet.')));
    return;
  }
  menu.append(el('div', {class: 'mh'}, t('Servers on this computer')));
  for (const p of S.profiles) {
    const count = p.forwards || 0;
    const counter = p.running ? (count === 1 ? t('1 port') : t('{n} ports', {n: count})) : t('stopped');
    const close = p.running && count > 0 && !p.current
      ? el('span', {class: 'pcl', role: 'button', tabindex: '0', 'data-tip': t('Close the ports of {server}', {server: p.destination || p.name}), 'aria-label': t('Close the ports of {server}', {server: p.destination || p.name}), onclick: e => { e.stopPropagation(); closeProfilePorts(p); }}, ICON_X)
      : el('span', {class: 'pcl ph'});
    const row = el('button', {class: 'pi', type: 'button', role: 'menuitem', 'aria-current': String(p.current), dataset: {prof: p.name}, disabled: !p.running && !p.current, onclick: () => switchTo(p)},
      el('span', {class: 'd' + (p.running ? ' on' : '')}),
      el('span', null, p.name, el('small', null, p.destination || '')),
      el('span', {class: 'c'}, counter),
      close);
    menu.append(row);
  }
  menu.append(el('div', {class: 'mfoot'}, t('To connect another server, run agentport setup in a terminal.')));
}

function closePopovers() {
  for (const id of ['pmenu', 'pol-pop', 'lang-pop', 'help-pop']) {
    const pop = $(id);
    if (pop) pop.hidden = true;
  }
  for (const id of ['prof', 'pol-btn', 'lang-btn', 'help']) $(id).setAttribute('aria-expanded', 'false');
  $('menu').hidden = true;
}

async function closeProfilePorts(p) {
  const n = p.forwards || 0;
  confirmDialog(t('Close {n} ports on {server}?', {n, server: p.destination || p.name}), t('The agents of that server are told the ports are closed.'), async () => {
    try {
      await api('POST', '/api/profiles/close-all', {profile: p.name});
      toast(t('Ports closed on {server}', {server: p.destination || p.name}));
    } catch (error) {
      toast(error.message, true);
    }
    await loadProfiles();
    renderProfiles();
  }, t('Close'));
}

function switchTo(p) {
  closePopovers();
  if (p.current || !p.url) return;
  const remembered = store('agentport.switch');
  const open = S.forwards.length;
  const go = () => { location.href = p.url; };
  if (open > 0 && remembered !== 'keep' && remembered !== 'close') {
    const remember = el('input', {type: 'checkbox', id: 'sw-remember'});
    const dialog = modal('confirm', t('Switch to {server}?', {server: p.destination || p.name}),
      el('p', {class: 'cf-p'}, t('{n} ports are open on this server. What happens to them?', {n: open})),
      el('label', {class: 'tog cf-rem'}, remember, t('Remember my choice')),
      el('div', {class: 'cfg-foot'},
        el('button', {class: 'btn', type: 'button', onclick: () => closeOverlay('switch-ov')}, t('Cancel')),
        el('button', {class: 'btn', type: 'button', onclick: () => { closeOverlay('switch-ov'); if (remember.checked) store('agentport.switch', 'keep'); go(); }}, t('Keep open and switch')),
        el('button', {class: 'btn danger-solid', type: 'button', onclick: async () => {
          closeOverlay('switch-ov');
          if (remember.checked) store('agentport.switch', 'close');
          try { await ports('POST', '/forwards/close-all', {}); } catch (_) { /* switch anyway */ }
          go();
        }}, t('Close {n} and switch', {n: open}))));
    openOverlay('switch-ov', dialog);
    return;
  }
  if (open > 0 && remembered === 'close') {
    ports('POST', '/forwards/close-all', {}).finally(go);
    return;
  }
  go();
}

// --- about and help -------------------------------------------------------------

function infoTabs() {
  return [
    ['about', t('What it is'), [
      ['p', t('agentport lets an AI agent open a port of a server on your computer, so you can see the service it runs.')],
      ['ol', [t('The agent asks for a port.'), t('You approve it here, or let it open.'), t('The port appears on localhost, and you close it when you are done.')]],
    ]],
    ['start', t('First steps'), [
      ['p', t('Set up a server once in a terminal with agentport setup. Then ask your agent to open a port, for example the database.')],
      ['ol', [t('Install agentport on this computer.'), t('Connect a server with agentport setup.'), t('Approve the first request that appears here.')]],
    ]],
    ['mcp', t('MCP tools'), [
      ['p', t('The agent can ask for ports, list the ports it holds and release them. It cannot open ports you did not approve.')],
      ['ul', [t('It can ask for a port by number.'), t('It can release a port it opened.'), t('It cannot change the server or run commands on it.')]],
    ]],
    ['sec', t('Security and privacy'), [
      ['ul', [t('Ports open only on 127.0.0.1, never on the network.'), t('The destination is always 127.0.0.1 of the server, so the agent reaches nothing else.'), t('This panel is reachable only from this computer, with a private link.')]],
    ]],
    ['trouble', t('Troubleshooting'), [
      ['ul', [t('Busy port: pick another free port, or stop the program that holds it when agentport can stop it.'), t('Dropped tunnel: the Companion reconnects by itself; the status shows it.'), t('The agent does not see the tools: check the MCP setup with agentport doctor.')]],
    ]],
  ];
}

let infoTab = 'about';
function openInfo(tab) {
  infoTab = tab || infoTab;
  const tabs = infoTabs();
  const bar = $('info-tabs');
  bar.replaceChildren();
  for (const [key, label] of tabs.map(([k, l]) => [k, l])) {
    bar.append(el('button', {type: 'button', role: 'tab', 'aria-selected': String(key === infoTab), onclick: () => openInfo(key)}, label));
  }
  const current = tabs.find(([k]) => k === infoTab) || tabs[0];
  const body = $('info-body');
  body.replaceChildren();
  for (const [kind, content] of current[2]) {
    if (kind === 'p') body.append(el('p', null, content));
    if (kind === 'ol') body.append(el('ol', null, content.map(item => el('li', null, item))));
    if (kind === 'ul') body.append(el('ul', null, content.map(item => el('li', null, item))));
  }
  $('info-ov').hidden = false;
  $('info-ver').textContent = t('agentport panel');
}

// --- settings and actions -------------------------------------------------------

async function saveSettings(patch) {
  const next = Object.assign({}, S.settings, patch);
  try {
    S.settings = await api('PUT', '/api/settings', next);
    toast(t('Saved'));
  } catch (error) {
    toast(error.message, true);
  }
  renderSettings();
}

function setPolicy(policy) {
  S.policy = policy;
  store('agentport.policy', policy);
  renderPolicy();
  closePopovers();
  toast(policy === 'session' ? t('Ports you approve reopen without asking until the session ends.') : t('Each port asks for approval.'));
}

let themeIndex = 0;
const THEMES = [null, 'light', 'dark'];
function applyTheme(theme) {
  if (theme) document.documentElement.dataset.theme = theme;
  else delete document.documentElement.dataset.theme;
  const button = $('theme');
  button.textContent = theme === 'dark' ? '☾' : theme === 'light' ? '☀' : '◐';
  button.dataset.tip = t('Theme: {theme}', {theme: theme === 'dark' ? t('dark') : theme === 'light' ? t('light') : t('system')});
  button.removeAttribute('title');
  termRefreshTheme();
}

let stopTimer = null;
async function stopCompanion() {
  const button = $('stop-all');
  if (S.stopArmed === null) {
    S.stopArmed = Date.now();
    button.classList.add('armed');
    button.dataset.tip = t('Click again to stop the Companion');
    clearTimeout(stopTimer);
    stopTimer = setTimeout(() => { S.stopArmed = null; button.classList.remove('armed'); }, 3000);
    return;
  }
  clearTimeout(stopTimer);
  S.stopArmed = null;
  try {
    await api('POST', '/api/stop', {});
    toast(t('The Companion is stopping. Its ports close.'));
    S.offline = true;
    renderAll();
  } catch (error) {
    toast(error.message, true);
  }
}

// toast shows a short message. Errors use the same box with a different tone.
let toastTimer = null;
function toast(message, error) {
  const box = $('toast');
  box.textContent = message;
  box.dataset.kind = error ? 'error' : '';
  box.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => box.classList.remove('show'), error ? 4200 : 2600);
}

// --- tooltips and keyboard ------------------------------------------------------

let tipTimer = null;
function showTip(target) {
  const text = target.dataset.tip;
  if (!text) return;
  const tip = $('tip');
  tip.textContent = text;
  const rect = target.getBoundingClientRect();
  tip.style.left = Math.max(8, Math.min(window.innerWidth - 268, rect.left + rect.width / 2 - 130)) + 'px';
  tip.style.top = Math.max(8, rect.bottom + 8) + 'px';
  tip.classList.add('on');
}
function hideTip() {
  clearTimeout(tipTimer);
  $('tip').classList.remove('on');
}
document.addEventListener('pointerover', e => {
  const target = e.target.closest && e.target.closest('[data-tip],[title]');
  if (!target) return;
  if (target.hasAttribute('title') && !target.dataset.tip) { target.dataset.tip = target.getAttribute('title'); target.removeAttribute('title'); }
  clearTimeout(tipTimer);
  tipTimer = setTimeout(() => showTip(target), 120);
});
document.addEventListener('pointerout', hideTip);
document.addEventListener('scroll', hideTip, true);

function isTyping(target) {
  return target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT' || target.isContentEditable);
}

document.addEventListener('keydown', e => {
  if (e.key === 'Escape') {
    if (!$('edit-ov').hidden || !$('cfg-ov').hidden || !$('confirm-ov').hidden || !$('info-ov').hidden || !$('help-ov').hidden || !$('switch-ov').hidden) {
      closeAllOverlays();
      return;
    }
    closePopovers();
    return;
  }
  if (isTyping(e.target) || e.ctrlKey || e.metaKey || e.altKey) return;
  if (!$('edit-ov').hidden || !$('confirm-ov').hidden || !$('help-ov').hidden || !$('info-ov').hidden) return;
  switch (e.key) {
    case '/': e.preventDefault(); $('add-port').focus(); break;
    case '?': $('help-ov').hidden = false; break;
    case 'a': case 'A': if (S.pending[0]) { const req = S.pending[0]; approveRequest(req, new Set(req.ports.map(p => p.remote_port)), 'session'); } break;
    case 'r': case 'R': if (S.pending[0]) denyRequest(S.pending[0]); break;
    case '1': setTab('open'); renderOpen(); break;
    case '2': setTab('det'); break;
    case '3': setTab('closed'); break;
    case '4': setTab('log'); break;
    default: break;
  }
});

// --- server terminal ----------------------------------------------------------
// A WebSocket to this Companion's /api/terminal attaches a shell on the server.
// Keystrokes go up as binary frames, output comes down as binary frames, and a
// text frame carries a resize. Closing the socket ends the ssh process there.
// xterm.js and its addons are vendored in assets/vendor: nothing comes from the
// network.

const TERM = {term: null, fit: null, socket: null, state: 'idle', failure: null, available: null, resizeTimer: 0};

function termTheme() {
  const css = getComputedStyle(document.documentElement);
  const value = name => css.getPropertyValue(name).trim();
  return {background: value('--bg'), foreground: value('--fg'), cursor: value('--fg'), cursorAccent: value('--bg'), selectionBackground: value('--accent-soft')};
}

// termRefreshTheme follows the panel's light and dark theme.
function termRefreshTheme() {
  if (TERM.term) TERM.term.options.theme = termTheme();
}

// termRender shows the state, the buttons and the note in the current language.
function termRender() {
  const labels = {
    idle: t('Closed'), connecting: t('Connecting'), open: t('Connected'), ended: t('Session ended'), unavailable: t('Unavailable'),
  };
  $('term-state').textContent = labels[TERM.state] || labels.idle;
  $('term-state').dataset.state = TERM.state;
  const canOpen = TERM.state === 'idle' || TERM.state === 'ended';
  $('term-open').hidden = !canOpen;
  $('term-open').disabled = TERM.state === 'unavailable';
  $('term-open').textContent = TERM.state === 'ended' ? t('Reconnect') : t('Open terminal');
  $('term-close').hidden = !(TERM.state === 'connecting' || TERM.state === 'open');
  $('term-note').textContent = TERM.failure === 'unavailable' ? t('The server terminal is not available on this computer.')
    : TERM.failure === 'failed' ? t('The server terminal could not start. Check that the server is connected.')
      : t('Opens a shell on the connected server. Anything you type runs there. It ends when you close it or stop the Companion.');
}

function termSetState(state, failure) {
  TERM.state = state;
  TERM.failure = failure || null;
  termRender();
}

// termShown runs when the tab is selected: it asks the Companion whether a
// terminal can run here, once.
async function termShown() {
  if (TERM.available !== null) return;
  try {
    const response = await fetch(BASE + '/api/terminal', {cache: 'no-store'});
    const body = response.ok ? await response.json() : {};
    TERM.available = Boolean(body.available);
  } catch (_) {
    TERM.available = false;
  }
  if (!TERM.available) termSetState('unavailable', 'unavailable');
}

// termCreate builds the screen on first use, when the tab is visible.
function termCreate() {
  if (TERM.term) return true;
  if (typeof Terminal !== 'function' || typeof FitAddon === 'undefined') return false;
  const term = new Terminal({
    cursorBlink: true, fontSize: 13, scrollback: 5000, theme: termTheme(),
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open($('term-screen'));
  // The WebGL renderer draws without injected <style> elements, which the strict
  // panel CSP would block. Without WebGL, xterm falls back to its DOM renderer.
  try {
    if (typeof WebglAddon !== 'undefined') {
      const gl = new WebglAddon.WebglAddon();
      gl.onContextLoss(() => gl.dispose());
      term.loadAddon(gl);
    }
  } catch (_) { /* no WebGL here: the DOM renderer draws the screen */ }
  term.onData(data => termSend(new TextEncoder().encode(data)));
  TERM.term = term;
  TERM.fit = fit;
  if (window.ResizeObserver) new ResizeObserver(() => termFit()).observe($('term-screen'));
  return true;
}

function termFit() {
  const screen = $('term-screen');
  if (!TERM.fit || !TERM.term || screen.clientWidth === 0 || screen.clientHeight === 0) return;
  try { TERM.fit.fit(); } catch (_) { return; }
  clearTimeout(TERM.resizeTimer);
  TERM.resizeTimer = setTimeout(termSendResize, 60);
}

function termSendResize() {
  if (TERM.socket && TERM.socket.readyState === WebSocket.OPEN && TERM.term) {
    TERM.socket.send(JSON.stringify({type: 'resize', cols: TERM.term.cols, rows: TERM.term.rows}));
  }
}

function termSend(bytes) {
  if (TERM.socket && TERM.socket.readyState === WebSocket.OPEN) TERM.socket.send(bytes);
}

function termOpen() {
  if (TERM.socket || TERM.available !== true) return;
  if (!termCreate()) { termSetState('idle', 'failed'); return; }
  termFit();
  termSetState('connecting');
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const socket = new WebSocket(scheme + '//' + location.host + BASE + '/api/terminal?cols=' + TERM.term.cols + '&rows=' + TERM.term.rows);
  socket.binaryType = 'arraybuffer';
  TERM.socket = socket;
  let opened = false;
  socket.onopen = () => {
    if (TERM.socket !== socket) return;
    opened = true;
    termSetState('open');
    termSendResize();
    TERM.term.focus();
  };
  socket.onmessage = event => {
    if (TERM.socket === socket && event.data instanceof ArrayBuffer) TERM.term.write(new Uint8Array(event.data));
  };
  socket.onclose = () => {
    if (TERM.socket !== socket) return;
    TERM.socket = null;
    if (opened) {
      TERM.term.write('\r\n\x1b[2m' + t('Session ended') + '\x1b[0m\r\n');
      termSetState('ended');
    } else {
      termSetState('idle', 'failed');
    }
  };
}

// termClose ends the session: closing the socket ends the ssh process on the server.
function termClose() {
  const socket = TERM.socket;
  TERM.socket = null;
  if (socket) socket.close(1000, 'closed by the panel');
  if (TERM.term) TERM.term.write('\r\n\x1b[2m' + t('Session closed') + '\x1b[0m\r\n');
  termSetState('idle');
}

// --- wiring -------------------------------------------------------------------

function wire() {
  $('t-open').addEventListener('click', () => { setTab('open'); renderOpen(); });
  $('t-det').addEventListener('click', () => setTab('det'));
  $('t-closed').addEventListener('click', () => setTab('closed'));
  $('t-log').addEventListener('click', () => setTab('log'));
  $('t-term').addEventListener('click', () => setTab('term'));
  $('term-open').addEventListener('click', () => termOpen());
  $('term-close').addEventListener('click', () => termClose());
  $('sess-f').addEventListener('change', e => { S.sess = e.target.value; renderOpen(); });

  $('prof').addEventListener('click', e => {
    e.stopPropagation();
    const open = $('pmenu').hidden;
    closePopovers();
    if (open) { renderProfiles(); $('pmenu').hidden = false; $('prof').setAttribute('aria-expanded', 'true'); }
  });
  $('pmenu').addEventListener('click', e => e.stopPropagation());
  $('open-connect').addEventListener('click', () => {
    closePopovers();
    openOverlay('add-ov', modal('add', t('Connect a server'),
      el('p', {class: 'cf-p'}, t('Connect a server from this computer in a terminal with agentport setup. It appears in the Switch menu when it is running.')),
      el('div', {class: 'cfg-foot'}, el('button', {class: 'btn', type: 'button', onclick: () => closeOverlay('add-ov')}, t('Close')))));
  });

  $('pol-btn').addEventListener('click', e => { e.stopPropagation(); const open = $('pol-pop').hidden; closePopovers(); if (open) { $('pol-pop').hidden = false; $('pol-btn').setAttribute('aria-expanded', 'true'); } });
  $('pol-pop').addEventListener('click', e => e.stopPropagation());
  document.querySelectorAll('[data-pol]').forEach(b => b.addEventListener('click', () => setPolicy(b.dataset.pol)));
  $('set-auto').addEventListener('change', e => saveSettings({auto_open: e.target.checked}));

  $('bell').addEventListener('click', () => saveSettings({notify: !S.settings.notify}));

  $('lang-btn').addEventListener('click', e => { e.stopPropagation(); const open = $('lang-pop').hidden; closePopovers(); if (open) { $('lang-pop').hidden = false; $('lang-btn').setAttribute('aria-expanded', 'true'); } });
  $('lang-pop').addEventListener('click', e => e.stopPropagation());
  document.querySelectorAll('[data-lang]').forEach(b => b.addEventListener('click', () => {
    applyLanguage(b.dataset.lang);
    closePopovers();
    renderAll();
    renderTabs();
    renderPolicy();
    renderSettings();
    applyTheme(document.documentElement.dataset.theme || null);
    termRender();
  }));

  $('theme').addEventListener('click', () => {
    themeIndex = (themeIndex + 1) % THEMES.length;
    const theme = THEMES[themeIndex];
    store('agentport.theme', theme || '');
    applyTheme(theme);
  });

  $('help').addEventListener('click', e => { e.stopPropagation(); const open = $('help-pop').hidden; closePopovers(); if (open) { $('help-pop').hidden = false; $('help').setAttribute('aria-expanded', 'true'); } });
  $('help-pop').addEventListener('click', e => {
    e.stopPropagation();
    const item = e.target.closest('[data-help]');
    if (!item) return;
    closePopovers();
    if (item.dataset.help === 'keys') { $('help-ov').hidden = false; return; }
    openInfo(item.dataset.help);
  });

  $('stop-all').addEventListener('click', stopCompanion);

  document.querySelectorAll('[data-close]').forEach(b => b.addEventListener('click', () => closeAllOverlays()));
  for (const id of ['info-ov', 'help-ov', 'cfg-ov', 'confirm-ov', 'edit-ov', 'add-ov', 'switch-ov']) {
    $(id).addEventListener('click', e => { if (e.target === $(id)) closeAllOverlays(); });
  }
  document.addEventListener('click', e => {
    if (!e.target.closest || !e.target.closest('.pop-wrap, .prof-wrap, #add-form')) closePopovers();
  });

  const addForm = $('add-form');
  addForm.addEventListener('submit', async e => {
    e.preventDefault();
    const value = $('add-port').value.trim();
    const port = Number(value);
    if (!/^\d+$/.test(value) || port < 1 || port > 65535) { toast(t('Use a number between 1 and 65535.'), true); return; }
    try {
      await ports('POST', '/forwards/open', {remote_port: port});
      toast(t('Opened :{port}', {port}));
      $('add-port').value = '';
    } catch (error) {
      toast(error.message, true);
    }
    closePopovers();
    await loadState();
  });
  const openMenu = () => { S.menuOpen = true; renderMenu(); loadDetected(true); };
  $('add-port').addEventListener('focus', openMenu);
  $('add-port').addEventListener('input', openMenu);
  $('add-port').addEventListener('keydown', e => { if (e.key === 'Escape') { S.menuOpen = false; renderMenu(); } });
  $('add-port').addEventListener('blur', () => setTimeout(() => { S.menuOpen = false; renderMenu(); }, 150));
}

// --- start --------------------------------------------------------------------

function start() {
  applyLanguage(LANG);
  const savedTheme = store('agentport.theme');
  themeIndex = Math.max(0, THEMES.indexOf(savedTheme || null));
  applyTheme(THEMES[themeIndex]);
  window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', termRefreshTheme);
  termRender();
  renderPolicy();
  wire();
  loadStatus();
  loadSettings();
  loadProfiles();
  loadState();
  // The detected list feeds the Detected badge and the process names of forwards,
  // so it loads with the panel and again on every state update, not only on the tab.
  loadDetected(false);
  setInterval(loadStatus, 5000);
  setInterval(loadProfiles, 15000);
  if (window.EventSource) {
    const events = new EventSource(BASE + '/api/ports/events');
    events.addEventListener('state', () => { loadState(); loadDetected(false); });
    events.onopen = () => { S.live = true; renderHostState(); };
    events.onerror = () => { S.live = false; renderHostState(); };
  }
  setInterval(() => { if (!S.live) loadState(); }, 3000);
}

start();
