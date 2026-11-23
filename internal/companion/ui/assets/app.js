const base = document.querySelector('meta[name="agentport-base"]').content;
const lang = document.querySelector('meta[name="agentport-lang"]').content;
// The server sends the translations for the page's language (none for English,
// which is the text written here). t() falls back to the English text it is given.
const messages = JSON.parse(document.querySelector('meta[name="agentport-i18n"]').content);
function t(message, ...args) {
  let text = messages[message] ?? message;
  for (const arg of args) text = text.replace(/%[sd]/, String(arg));
  return text;
}
const dateTime = new Intl.DateTimeFormat(lang, {dateStyle: 'medium', timeStyle: 'short'});

// h builds an element. Every string child becomes a text node, so nothing that
// came from the server (profile names, error messages) can turn into markup.
// The page never assigns HTML: see TestDashboardScriptNeverBuildsMarkupFromStrings.
function h(tag, props, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value === undefined || value === null || value === false) continue;
    if (key === 'class') node.className = value;
    else node.setAttribute(key, value === true ? '' : String(value));
  }
  node.append(...children.flat(Infinity).filter(child => child !== undefined && child !== null && child !== false).map(child => child instanceof Node ? child : document.createTextNode(String(child))));
  return node;
}
const text = value => String(value ?? '—');

function formatDateTime(value) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : dateTime.format(date);
}
function statusRow(label, value) {
  return h('div', {class: 'status-row'}, h('dt', null, label), h('dd', null, value));
}
function setConnection(connected, label) {
  const connection = document.querySelector('#connection');
  connection.classList.toggle('offline', !connected);
  connection.replaceChildren(h('span', {class: 'status-dot' + (connected ? '' : ' offline')}), ' ' + label);
  const live = document.querySelector('#live-indicator');
  live.classList.toggle('offline', !connected);
  live.setAttribute('aria-label', label);
}
function statusRows(s, connected) {
  const rows = [
    statusRow(t('Profile'), text(s.profile)),
    statusRow(t('Server'), h('code', null, text(s.destination))),
    statusRow(t('Tunnel'), connected ? h('span', {class: 'positive'}, t('Connected')) : h('span', {class: 'bad'}, t('Disconnected'))),
  ];
  if (s.tunnel?.last_error) rows.push(statusRow(t('Last error'), h('span', {class: 'bad'}, text(s.tunnel.last_error))));
  if (s.release?.update_available) rows.push(statusRow(t('Update available'), h('span', {class: 'bad'}, h('code', null, text(s.release.latest_version)), t(' · run '), h('code', null, 'agentport upgrade'))));
  else if (s.release?.error) rows.push(statusRow(t('Update'), h('span', {class: 'muted'}, text(s.release.error))));
  return rows;
}
async function refresh() {
  try {
    const r = await fetch(base + '/api/status', {cache: 'no-store'});
    if (!r.ok) throw new Error('status unavailable');
    const s = await r.json();
    const connected = !!s.tunnel?.connected;
    setConnection(connected, connected ? t('Tunnel connected') : t('Tunnel disconnected'));
    document.querySelector('#status').replaceChildren(...statusRows(s, connected));
    document.querySelector('#updated').textContent = t('Updated at ') + formatDateTime(new Date());
  } catch (_) {
    setConnection(false, t('Status unavailable'));
    document.querySelector('#updated').textContent = t('Update failed · data unavailable');
    document.querySelector('#status').replaceChildren(statusRow('Status', h('span', {class: 'bad'}, t('Could not reach the Companion.'))));
  }
}
document.querySelector('#stop').onclick = async () => {
  if (confirm(t('Stop the Companion?'))) {
    await fetch(base + '/api/stop', {method: 'POST'});
    setTimeout(refresh, 500);
  }
};
refresh();
setInterval(refresh, 2000);
