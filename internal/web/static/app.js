// Pan/zoom, view toggle and jump-to-element for the archdiag page.
(() => {
  "use strict";
  let userBox = null;   // viewBox chosen by the user; null follows the server's
  let serverBox = null; // last viewBox sent by the server
  let highlighted = null;
  let drag = null;

  const svg = () => document.getElementById("diagram");
  const readBox = (s) => (s.getAttribute("viewBox") || "0 0 1 1").split(/[\s,]+/).map(Number);
  const writeBox = (s, b) => s.setAttribute("viewBox", b.map((n) => n.toFixed(1)).join(" "));

  // SVG units per screen pixel, and the letterbox offsets of the default
  // preserveAspectRatio (xMidYMid meet).
  function metrics(s, b) {
    const r = s.getBoundingClientRect();
    const scale = Math.max(b[2] / r.width, b[3] / r.height);
    return { r, scale, ox: (r.width - b[2] / scale) / 2, oy: (r.height - b[3] / scale) / 2 };
  }

  // After the server replaces the diagram, keep the user's pan/zoom and highlight.
  function sync() {
    const s = svg();
    if (!s) return;
    serverBox = readBox(s);
    if (userBox) writeBox(s, userBox);
    if (highlighted) document.getElementById(highlighted)?.classList.add("highlight");
  }

  document.addEventListener("wheel", (e) => {
    const s = svg();
    if (!s || !s.contains(e.target)) return;
    e.preventDefault();
    const b = readBox(s);
    const { r, scale, ox, oy } = metrics(s, b);
    const ux = b[0] + (e.clientX - r.left - ox) * scale;
    const uy = b[1] + (e.clientY - r.top - oy) * scale;
    const k = Math.exp(e.deltaY * 0.0015);
    userBox = [ux - (ux - b[0]) * k, uy - (uy - b[1]) * k, b[2] * k, b[3] * k];
    writeBox(s, userBox);
  }, { passive: false });

  document.addEventListener("pointerdown", (e) => {
    const s = svg();
    if (!s || !s.contains(e.target) || e.target.closest(".node")) return;
    drag = { x: e.clientX, y: e.clientY, box: readBox(s) };
    s.setPointerCapture(e.pointerId);
    s.classList.add("dragging");
  });

  document.addEventListener("pointermove", (e) => {
    const s = svg();
    if (!drag || !s) return;
    const { scale } = metrics(s, drag.box);
    const [x, y, w, h] = drag.box;
    userBox = [x - (e.clientX - drag.x) * scale, y - (e.clientY - drag.y) * scale, w, h];
    writeBox(s, userBox);
  });

  document.addEventListener("pointerup", () => {
    drag = null;
    svg()?.classList.remove("dragging");
  });

  // Double-click the background to reset the view.
  document.addEventListener("dblclick", (e) => {
    const s = svg();
    if (!s || !s.contains(e.target) || !serverBox) return;
    userBox = null;
    writeBox(s, serverBox);
  });

  function focusOn(id) {
    const s = svg();
    const el = document.getElementById(id);
    if (!s || !el) return;
    if (highlighted) document.getElementById(highlighted)?.classList.remove("highlight");
    highlighted = id;
    el.classList.add("highlight");
    const bb = el.getBBox();
    const cur = readBox(s);
    const w = Math.max(bb.width * 3, 360);
    const h = w * (cur[3] / cur[2]);
    userBox = [bb.x + bb.width / 2 - w / 2, bb.y + bb.height / 2 - h / 2, w, h];
    writeBox(s, userBox);
  }

  document.addEventListener("click", (e) => {
    const viewBtn = e.target.closest(".views button");
    if (viewBtn) {
      document.getElementById("main").dataset.view = viewBtn.dataset.view;
      for (const b of document.querySelectorAll(".views button")) {
        b.setAttribute("aria-pressed", String(b === viewBtn));
      }
      return;
    }
    const jump = e.target.closest(".jump");
    if (jump) focusOn(jump.dataset.target);
  });

  // afterSwap (not afterSettle) so the user's view is back before the next paint.
  document.addEventListener("htmx:afterSwap", (e) => {
    if (e.target.id === "canvas") sync();
  });
  sync();
})();
