/*!
 * shortcut-bar.js — remote-desktop shortcut bar (host-agnostic shared component; canonical source lives in aiti-ui-env)
 *
 * Usage:
 *   <script src=".../shortcut-bar.js"></script>
 *   ShortcutBar.mount({ root: "#shortcuts", onPress(code), onRelease(code), combos? });
 *
 * Semantics: holding a chip presses every key in the combo in order; release /
 * pointer-out releases them in reverse. ("Hold Alt+Tab then tap Tab"-style flows
 * can be layered on top by callers; this component stays minimal.)
 * `combos` overrides the defaults: [{ label, codes: ["ControlLeft", ...] }] with DOM codes.
 */
(function (global, factory) {
  const mod = factory();
  if (typeof module === "object" && module.exports) module.exports = mod;
  else if (typeof define === "function" && define.amd) define([], () => mod);
  else global.ShortcutBar = mod;
})(typeof self !== "undefined" ? self : this, function () {
  "use strict";

  const DEFAULT_COMBOS = [
    { label: "Ctrl+Alt+T", codes: ["ControlLeft", "AltLeft", "KeyT"] },
    { label: "Alt+Tab", codes: ["AltLeft", "Tab"] },
    { label: "⊞ Win", codes: ["MetaLeft"] },
    { label: "Ctrl+C", codes: ["ControlLeft", "KeyC"] },
    { label: "Ctrl+V", codes: ["ControlLeft", "KeyV"] },
    { label: "Ctrl+Z", codes: ["ControlLeft", "KeyZ"] },
    { label: "Ctrl+S", codes: ["ControlLeft", "KeyS"] },
    { label: "F5", codes: ["F5"] },
    { label: "Esc", codes: ["Escape"] },
    { label: "PrtSc", codes: ["PrintScreen"] }
  ];

  function mount(opts) {
    const root = typeof opts.root === "string" ? document.querySelector(opts.root) : opts.root;
    if (!root) throw new Error("ShortcutBar: root not found");
    const onPress = opts.onPress || function () {};
    const onRelease = opts.onRelease || function () {};
    const combos = opts.combos || DEFAULT_COMBOS;

    root.classList.add("scb-root");
    let held = null; // { btn, codes }

    function release() {
      if (!held) return;
      for (let i = held.codes.length - 1; i >= 0; i--) onRelease(held.codes[i]);
      held.btn.classList.remove("scb-on");
      held = null;
    }

    combos.forEach((c) => {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "scb-chip";
      b.textContent = c.label;
      const down = (e) => {
        e.preventDefault();
        e.stopPropagation();
        if (held && held.btn !== b) release();
        if (held) return; // same chip already held; ignore repeated presses
        held = { btn: b, codes: c.codes };
        b.classList.add("scb-on");
        for (const code of c.codes) onPress(code);
      };
      const up = () => {
        if (held && held.btn === b) release();
      };
      b.addEventListener("pointerdown", down);
      b.addEventListener("pointerup", up);
      b.addEventListener("pointercancel", up);
      b.addEventListener("pointerleave", up);
      root.appendChild(b);
    });

    return {
      release,
      isOpen() { return true; }
    };
  }

  return { mount, DEFAULT_COMBOS };
});
