// Applies the saved theme before first paint to avoid a flash (CSP forbids inline scripts).
(function () {
  try {
    var t = localStorage.getItem("docveta.theme") || "system";
    var dark = t === "dark" || (t === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList.toggle("dark", dark);
  } catch (e) {}
})();
