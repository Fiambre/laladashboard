(function () {
  var lastVersion = (typeof window.DASHBOARD_VERSION !== 'undefined') ? window.DASHBOARD_VERSION : null;

  function refreshAllWidgets() {
    document.querySelectorAll('[id^="widget-"]').forEach(function (el) {
      var url = el.getAttribute('hx-get');
      if (url) htmx.ajax('GET', url, { target: el, swap: 'innerHTML' });
    });
  }

  function checkVersion() {
    fetch('/api/config-version')
      .then(function (r) { return r.json(); })
      .then(function (data) {
        if (lastVersion === null) { lastVersion = data.version; return; }
        if (data.version !== lastVersion) {
          lastVersion = data.version;
          refreshAllWidgets();
        }
      })
      .catch(function () {});
  }

  setInterval(checkVersion, 3000);
})();
