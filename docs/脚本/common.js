/* Shared helpers used by the community pages and retained audit records. */
function escapeHtml(value) {
    return String(value == null ? '' : value).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;');
}
function loadJSON(key, fallback) {
    try { const value = localStorage.getItem(key); return value ? JSON.parse(value) : fallback; }
    catch (_) { return fallback; }
}
function saveJSON(key, value) {
    try { localStorage.setItem(key, JSON.stringify(value)); return true; }
    catch (_) { showToast('保存失败，请检查浏览器存储空间'); return false; }
}
function showToast(message) {
    const node = document.createElement('div');
    node.className = 'error-toast'; node.setAttribute('role', 'status');
    node.textContent = message; document.body.appendChild(node);
    setTimeout(() => node.remove(), 4000);
}
function maskName(name) {
    const value = String(name || '');
    if (value.length < 2) return value;
    if (value.length === 2) return value[0] + '*';
    return value[0] + '*'.repeat(value.length - 2) + value.at(-1);
}
function displayName(name, masked = false) { return masked ? maskName(name) : name; }
function iconSVG(name, size = 22, color = '#365B49') {
    const paths = {
        home: '<path d="m3 10 9-7 9 7v11H3z"/><path d="M9 21v-8h6v8"/>',
        heart: '<path d="M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z"/>',
        person: '<circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/>',
        moon: '<path d="M21 13a9 9 0 1 1-10-10 7 7 0 0 0 10 10Z"/>'
    };
    const width = Number.isFinite(Number(size)) ? Math.max(12, Math.min(64, Number(size))) : 22;
    const stroke = /^#[0-9a-f]{3,8}$/i.test(color) ? color : '#365B49';
    return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="' + width + '" height="' + width + '" viewBox="0 0 24 24" fill="none" stroke="' + stroke + '" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">' + (paths[name] || paths.heart) + '</svg>');
}
