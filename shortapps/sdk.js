// shortapps.js: how an app's page reaches its data. The page runs walled off (no
// network, no cookies); it asks the phone around it, which checks and forwards every
// call. Load it with <script src="shortapps.js"></script>.
//
//   const app = await shortapps.ready;              // { id, name, … } from app.json
//   const items = await shortapps.list("tasks", { where: [{ field: "status", op: "=", value: "todo" }],
//                                                 orderBy: [{ field: "_updated", desc: true }] });
//   const rec = await shortapps.add("tasks", { title: "Call landlord" });
//   await shortapps.update("tasks", rec.id, { status: "done" }, rec.version);
//   await shortapps.remove("tasks", rec.id, rec.version);
//   shortapps.onChange(() => render());            // someone else (the agent, another tab) changed data
(function () {
  "use strict";
  var pending = {};
  var seq = 0;
  var listeners = [];
  var resolveReady;
  var ready = new Promise(function (r) { resolveReady = r; });

  function call(method, args) {
    return new Promise(function (resolve, reject) {
      var id = ++seq;
      pending[id] = { resolve: resolve, reject: reject };
      parent.postMessage({ shortapps: 1, id: id, method: method, args: args }, "*");
    });
  }

  function applyTheme(theme) {
    if (!theme) return;
    var root = document.documentElement;
    Object.keys(theme.vars || {}).forEach(function (k) { root.style.setProperty(k, theme.vars[k]); });
    root.style.colorScheme = theme.dark ? "dark" : "light";
    root.dataset.theme = theme.dark ? "dark" : "light";
  }

  window.addEventListener("message", function (e) {
    if (e.source !== parent) return;
    var m = e.data;
    if (!m || m.shortapps !== 1) return;
    if (m.event === "init") { applyTheme(m.theme); resolveReady(m.app); return; }
    if (m.event === "theme") { applyTheme(m.theme); return; }
    if (m.event === "change") { listeners.forEach(function (f) { try { f(); } catch (err) { console.error(err); } }); return; }
    var p = pending[m.id];
    if (!p) return;
    delete pending[m.id];
    if (m.error) p.reject(new Error(m.error)); else p.resolve(m.result);
  });

  window.shortapps = {
    ready: ready,
    list: function (coll, query) { return call("list", [coll, query || {}]); },
    get: function (coll, id) { return call("get", [coll, id]); },
    add: function (coll, data, id) { return call("add", [coll, data, id || ""]); },
    update: function (coll, id, patch, version) { return call("update", [coll, id, patch, version || 0]); },
    remove: function (coll, id, version) { return call("remove", [coll, id, version || 0]); },
    history: function (coll, id) { return call("history", [coll || "", id || ""]); },
    onChange: function (f) { listeners.push(f); },
  };
  parent.postMessage({ shortapps: 1, method: "hello" }, "*");
})();
