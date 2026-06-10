/* LalaDashboard config page — GridStack integration */

let grid;
let saveTimer = null;

document.addEventListener('DOMContentLoaded', function () {
  grid = GridStack.init({
    column: 12,
    cellHeight: 80,
    animate: true,
    resizable: { handles: 'se' },
    draggable: { handle: '.widget-header' },
  }, '#dashboard-grid');

  if (window.DASHBOARD_ITEMS && window.DASHBOARD_ITEMS.length > 0) {
    grid.load(window.DASHBOARD_ITEMS);
  }

  grid.on('change', function () {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(saveLayout, 600);
  });
});

function saveLayout() {
  const items = grid.save(false);
  const payload = items.map(item => ({
    id: item.id,
    x: item.x ?? 0,
    y: item.y ?? 0,
    w: item.w ?? 3,
    h: item.h ?? 2,
  }));

  fetch('/api/layout', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  }).then(r => {
    if (r.ok) showSaveStatus('Guardado ✓');
    else showSaveStatus('Error al guardar');
  }).catch(() => showSaveStatus('Error de conexión'));
}

function showSaveStatus(msg) {
  const el = document.getElementById('save-status');
  if (!el) return;
  el.textContent = msg;
  el.classList.add('visible');
  setTimeout(() => el.classList.remove('visible'), 2000);
}

function addWidget(typeID) {
  const title = prompt('Nombre del widget (dejar vacío para usar el predeterminado):') ?? '';
  fetch('/api/widgets', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ type_id: typeID, title: title }),
  }).then(r => {
    if (r.ok) window.location.reload();
    else r.text().then(t => alert('Error: ' + t));
  });
}

function removeWidget(widgetID) {
  if (!confirm('¿Eliminar este widget?')) return;
  fetch('/api/widgets/' + widgetID, { method: 'DELETE' })
    .then(r => {
      if (r.ok) window.location.reload();
      else alert('Error al eliminar widget');
    });
}

function openSettings(widgetID) {
  // Fetch current settings from the page data
  const cfg = (window.DASHBOARD_ITEMS || []).find(i => i.id === widgetID);
  // For now reload — full settings UI comes with modal implementation
  alert('Configuración de widget: ' + widgetID + '\nPróximamente con formulario completo.');
}

function toggleTheme() {
  const html = document.documentElement;
  const current = html.getAttribute('data-theme') || 'dark';
  const next = current === 'dark' ? 'light' : 'dark';
  fetch('/api/theme', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ theme: next }),
  }).then(r => {
    if (r.ok) html.setAttribute('data-theme', next);
  });
}
