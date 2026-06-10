function showToast(msg, ok) {
  const t = document.getElementById('store-toast');
  t.textContent = msg;
  t.className = 'store-toast ' + (ok ? 'toast-ok' : 'toast-error') + ' visible';
  setTimeout(() => t.classList.remove('visible'), 3500);
}

function installMod(moduleID) {
  const btn = document.querySelector(`#mod-${moduleID} .btn-primary`);
  if (btn) { btn.textContent = 'Instalando…'; btn.disabled = true; }

  fetch(`/api/modules/${moduleID}/install`, { method: 'POST', headers: { 'X-Requested-With': 'laladashboard' } })
    .then(r => r.json())
    .then(data => {
      if (data.status === 'installed') {
        showToast(data.warning
          ? `✓ Instalado — ${data.warning}`
          : '✓ Módulo instalado y activado', true);
        setTimeout(() => location.reload(), 1500);
      } else {
        showToast('Error al instalar', false);
        if (btn) { btn.textContent = 'Instalar'; btn.disabled = false; }
      }
    })
    .catch(e => {
      showToast('Error de conexión: ' + e.message, false);
      if (btn) { btn.textContent = 'Instalar'; btn.disabled = false; }
    });
}

function uninstallMod(moduleID) {
  if (!confirm('¿Desinstalar este módulo?')) return;
  fetch(`/api/modules/${moduleID}/uninstall`, { method: 'DELETE', headers: { 'X-Requested-With': 'laladashboard' } })
    .then(r => r.json())
    .then(data => {
      showToast('Módulo eliminado — reinicia para desactivar', true);
      setTimeout(() => location.reload(), 1500);
    })
    .catch(() => showToast('Error al desinstalar', false));
}
