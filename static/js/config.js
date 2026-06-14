/* LalaDashboard config page — GridStack + widget settings modal */

let grid;
let saveTimer = null;
let activeWidgetID = null;

document.addEventListener('DOMContentLoaded', function () {
  grid = GridStack.init({
    column: 12,
    cellHeight: 40,
    animate: true,
    minRow: 2,
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

  // Close modal on overlay click or ESC
  document.getElementById('settings-modal').addEventListener('click', function (e) {
    if (e.target === this) closeSettings();
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeSettings();
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

// ── Settings modal ────────────────────────────────────────────────────────────

function openSettings(widgetID) {
  const data = (window.WIDGET_DATA || {})[widgetID];
  if (!data) return;

  activeWidgetID = widgetID;

  document.getElementById('settings-modal-title').textContent = 'Configurar: ' + data.title;
  document.getElementById('settings-modal-error').textContent = '';
  document.getElementById('settings-modal-body').innerHTML = buildSettingsForm(data.schema || [], data.settings || {});
  document.getElementById('settings-modal').classList.add('open');

  // Focus the first input
  const first = document.querySelector('#settings-modal-body .form-control');
  if (first) first.focus();
}

function buildSettingsForm(schema, settings) {
  if (!schema || schema.length === 0) {
    return '<p style="color:var(--text-dim)">Este widget no tiene configuración.</p>';
  }
  return schema.map(field => {
    const value = settings[field.key] !== undefined ? settings[field.key] : (field.default || '');
    const required = field.required ? 'required' : '';
    const placeholder = field.placeholder ? `placeholder="${escapeAttr(field.placeholder)}"` : '';

    let input;
    if (field.type === 'select' && field.options && field.options.length > 0) {
      const options = field.options.map(opt =>
        `<option value="${escapeAttr(opt)}"${opt === value ? ' selected' : ''}>${escapeHTML(opt)}</option>`
      ).join('');
      input = `<select class="form-control" id="field-${escapeAttr(field.key)}" name="${escapeAttr(field.key)}" ${required}>${options}</select>`;
    } else if (field.type === 'textarea') {
      input = `<textarea class="form-control" id="field-${escapeAttr(field.key)}" name="${escapeAttr(field.key)}" rows="4" ${placeholder} ${required}>${escapeHTML(value)}</textarea>`;
    } else {
      const type = field.type === 'number' ? 'number' : field.type === 'url' ? 'url' : 'text';
      input = `<input class="form-control" type="${type}" id="field-${escapeAttr(field.key)}" name="${escapeAttr(field.key)}" value="${escapeAttr(value)}" ${placeholder} ${required}>`;
    }

    const requiredMark = field.required ? ' <span style="color:var(--danger)">*</span>' : '';
    return `<div class="form-group">
      <label class="form-label" for="field-${escapeAttr(field.key)}">${escapeHTML(field.label)}${requiredMark}</label>
      ${input}
    </div>`;
  }).join('');
}

function closeSettings() {
  document.getElementById('settings-modal').classList.remove('open');
  document.getElementById('settings-modal-body').innerHTML = '';
  document.getElementById('settings-modal-error').textContent = '';
  activeWidgetID = null;
}

function submitSettings() {
  if (!activeWidgetID) return;

  const body = document.getElementById('settings-modal-body');
  const controls = body.querySelectorAll('.form-control');
  const settings = {};

  for (const el of controls) {
    if (el.required && !el.value.trim()) {
      document.getElementById('settings-modal-error').textContent = `El campo "${el.name}" es obligatorio.`;
      el.focus();
      return;
    }
    settings[el.name] = el.value;
  }

  fetch(`/api/widgets/${activeWidgetID}/settings`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  }).then(r => {
    if (r.ok) {
      showSaveStatus('Guardado ✓');
      closeSettings();
    } else {
      r.text().then(t => {
        document.getElementById('settings-modal-error').textContent = 'Error: ' + t;
      });
    }
  }).catch(e => {
    document.getElementById('settings-modal-error').textContent = 'Error de conexión: ' + e.message;
  });
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

// ── Helpers ───────────────────────────────────────────────────────────────────

function escapeHTML(str) {
  return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function escapeAttr(str) {
  return String(str).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}
