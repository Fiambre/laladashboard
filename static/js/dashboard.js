(function () {
  var lastVersion = (typeof window.DASHBOARD_VERSION !== 'undefined') ? window.DASHBOARD_VERSION : null;

  function checkVersion() {
    fetch('/api/config-version')
      .then(function (r) { return r.json(); })
      .then(function (data) {
        if (lastVersion === null) { lastVersion = data.version; return; }
        if (data.version !== lastVersion) {
          window.location.reload();
        }
      })
      .catch(function () {});
  }

  setInterval(checkVersion, 3000);
})();
