/*!
 * virtual-keyboard.js — programmer-oriented on-screen keyboard (based on simple-keyboard v3.8.177)
 *
 * Usage (any host page):
 *   <link rel="stylesheet" href="vendor/simple-keyboard.css" />
 *   <link rel="stylesheet" href="virtual-keyboard.css" />
 *   <script src="vendor/simple-keyboard.js"></script>
 *   <script src="virtual-keyboard.js"></script>
 *   VirtualKeyboard.mount({ root: "#vkb", onKey: fn, onLatch: fn });
 *
 * Event contract (host adapter):
 *   onKey(code, down)   — normal/special key press(true)/release(false); code is the DOM `code` (KeyA/ArrowUp/Escape…)
 *   onLatch(code, on)   — modifier latch state change (ShiftLeft/ControlLeft/AltLeft/MetaLeft/CapsLock)
 *
 * Design:
 *  - key value = code or sentinel, mapped uniformly by resolveCode() (default layer
 *    letters use KeyQ and display lowercase; uppercase layer uses UQ-style sentinels
 *    displaying uppercase; shifted symbols use Tilde/Exclam… sentinels -> base code).
 *  - {shift} press -> manually switch default/shift layout via setOptions + latch ShiftLeft (sticky).
 *  - {ctrl}/{alt}/{meta}/{capslock} are lock keys: tap to toggle (touch friendly).
 *  - after a layout switch, re-apply latch highlight classes keyed by `data-skbtn`.
 */
(function (global, factory) {
  const mod = factory();
  if (typeof module === "object" && module.exports) module.exports = mod;
  else if (typeof define === "function" && define.amd) define([], () => mod);
  else global.VirtualKeyboard = mod;
})(typeof self !== "undefined" ? self : this, function () {
  "use strict";

  /* ── uppercase letter sentinels (UQ->KeyQ, displays Q) ─────────────────── */
  const LETTERS = "QWERTYUIOPASDFGHJKLZXCVBNM".split("");
  const UPPER_TO_CODE = {}, UPPER_DISPLAY = {};
  for (const ch of LETTERS) {
    UPPER_TO_CODE["U" + ch] = "Key" + ch;
    UPPER_DISPLAY["U" + ch] = ch;
  }

  /* ── shifted-symbol sentinels -> base code (shift is latched on the uppercase layer, symbol goes out directly) ── */
  const SHIFT_SENTINEL = {
    Tilde: "Backquote", Exclam: "Digit1", At: "Digit2", Hash: "Digit3", Dollar: "Digit4",
    Percent: "Digit5", Caret: "Digit6", Ampersand: "Digit7", Asterisk: "Digit8",
    ParenLeft: "Digit9", ParenRight: "Digit0", Underscore: "Minus", Plus: "Equal",
    BraceLeft: "BracketLeft", BraceRight: "BracketRight", Pipe: "Backslash",
    Colon: "Semicolon", DoubleQuote: "Quote", LessThan: "Comma", GreaterThan: "Period", Question: "Slash"
  };

  /* ── special keys -> code ─────────────────────────────────────────── */
  const SPECIAL_CODE = {
    "{esc}": "Escape", "{tab}": "Tab", "{ctrl}": "ControlLeft", "{alt}": "AltLeft",
    "{shift}": "ShiftLeft", "{bksp}": "Backspace", "{delete}": "Delete", "{capslock}": "CapsLock",
    "{meta}": "MetaLeft", "{space}": "Space", "{enter}": "Enter",
    "{arrowup}": "ArrowUp", "{arrowdown}": "ArrowDown", "{arrowleft}": "ArrowLeft", "{arrowright}": "ArrowRight",
    "{home}": "Home", "{end}": "End", "{pageup}": "PageUp", "{pagedown}": "PageDown"
  };

  const DIRECT_CODE_RE = /^(Key[A-Z]|Digit[0-9]|F[0-9]{1,2})$|^(Backquote|Minus|Equal|BracketLeft|BracketRight|Backslash|Semicolon|Quote|Comma|Period|Slash)$/;

  /* ── layouts (value = code or sentinel; {xxx} = special key) ──────────── */
  const LOWER_ROWS = [
    "{esc} {tab} {ctrl} {alt} {shift} {bksp} {delete} {capslock}",
    "Backquote Digit1 Digit2 Digit3 Digit4 Digit5 Digit6 Digit7 Digit8 Digit9 Digit0 Minus Equal",
    "KeyQ KeyW KeyE KeyR KeyT KeyY KeyU KeyI KeyO KeyP BracketLeft BracketRight Backslash",
    "KeyA KeyS KeyD KeyF KeyG KeyH KeyJ KeyK KeyL Semicolon Quote {enter}",
    "{shift} KeyZ KeyX KeyC KeyV KeyB KeyN KeyM Comma Period Slash {shift}",
    "{meta} {space} {arrowleft} {arrowup} {arrowdown} {arrowright} {home} {end} {pageup} {pagedown}"
  ];
  const UPPER_ROWS = [
    "{esc} {tab} {ctrl} {alt} {shift} {bksp} {delete} {capslock}",
    "Tilde Exclam At Hash Dollar Percent Caret Ampersand Asterisk ParenLeft ParenRight Underscore Plus",
    "UQ UW UE UR UT UY UU UI UO UP BraceLeft BraceRight Pipe",
    "UA US UD UF UG UH UJ UK UL Colon DoubleQuote {enter}",
    "{shift} UZ UX UC UV UB UN UM LessThan GreaterThan Question {shift}",
    "{meta} {space} {arrowleft} {arrowup} {arrowdown} {arrowright} {home} {end} {pageup} {pagedown}"
  ];

  /* ── display map (single global map) ─────────────────────────────── */
  const COMMON_DISPLAY = {
    "{esc}": "esc", "{tab}": "tab", "{ctrl}": "Ctrl", "{alt}": "Alt", "{shift}": "⇧",
    "{bksp}": "⌫", "{delete}": "Del", "{capslock}": "caps", "{meta}": "⊞", "{space}": " ",
    "{enter}": "⏎", "{arrowup}": "↑", "{arrowdown}": "↓", "{arrowleft}": "←", "{arrowright}": "→",
    "{home}": "Home", "{end}": "End", "{pageup}": "PgUp", "{pagedown}": "PgDn"
  };
  const DISPLAY = Object.assign({}, COMMON_DISPLAY, UPPER_DISPLAY, {
    Backquote: "`", Digit1: "1", Digit2: "2", Digit3: "3", Digit4: "4", Digit5: "5",
    Digit6: "6", Digit7: "7", Digit8: "8", Digit9: "9", Digit0: "0", Minus: "-", Equal: "=",
    KeyQ: "q", KeyW: "w", KeyE: "e", KeyR: "r", KeyT: "t", KeyY: "y", KeyU: "u",
    KeyI: "i", KeyO: "o", KeyP: "p", BracketLeft: "[", BracketRight: "]", Backslash: "\\",
    KeyA: "a", KeyS: "s", KeyD: "d", KeyF: "f", KeyG: "g", KeyH: "h", KeyJ: "j",
    KeyK: "k", KeyL: "l", Semicolon: ";", Quote: "'",
    KeyZ: "z", KeyX: "x", KeyC: "c", KeyV: "v", KeyB: "b", KeyN: "n", KeyM: "m",
    Comma: ",", Period: ".", Slash: "/",
    Tilde: "~", Exclam: "!", At: "@", Hash: "#", Dollar: "$", Percent: "%", Caret: "^",
    Ampersand: "&", Asterisk: "*", ParenLeft: "(", ParenRight: ")", Underscore: "_", Plus: "+",
    BraceLeft: "{", BraceRight: "}", Pipe: "|", Colon: ":", DoubleQuote: "\"",
    LessThan: "<", GreaterThan: ">", Question: "?"
  });

  /* ── value -> code ────────────────────────────────── */
  function resolveCode(value) {
    if (SPECIAL_CODE[value]) return SPECIAL_CODE[value];
    if (DIRECT_CODE_RE.test(value)) return value;
    if (SHIFT_SENTINEL[value]) return SHIFT_SENTINEL[value];
    if (UPPER_TO_CODE[value]) return UPPER_TO_CODE[value];
    return null;
  }

  /* ── mount ────────────────────────────────────────── */
  function mount(opts) {
    const root = typeof opts.root === "string" ? document.querySelector(opts.root) : opts.root;
    if (!root) throw new Error("VirtualKeyboard: root not found: " + opts.root);
    const onKey = opts.onKey || function () {};
    const onLatch = opts.onLatch || function () {};

    const state = { shift: false, mods: {} };
    const LOCK_BTNS = ["{shift}", "{ctrl}", "{alt}", "{meta}", "{capslock}"];

    function setLock(value, on) {
      root.querySelectorAll('.hg-button[data-skbtn="' + value + '"]').forEach((el) => {
        el.classList.toggle("hg-vkb-lock", on);
      });
    }

    // simple-keyboard UMD export may be the class itself or a { SimpleKeyboard, default } namespace
    const SK = (typeof globalThis.SimpleKeyboard === "function")
      ? globalThis.SimpleKeyboard
      : (globalThis.SimpleKeyboard && globalThis.SimpleKeyboard.SimpleKeyboard);
    if (typeof SK !== "function") throw new Error("VirtualKeyboard: SimpleKeyboard not loaded");
    const sk = new SK(root, {
      theme: "hg-theme-vkb",
      layout: { default: LOWER_ROWS, shift: UPPER_ROWS },
      layoutName: "default",
      display: DISPLAY,
      enableLayoutCandidates: false,
      disableCaretPositioning: true,
      preventMouseDownDefault: true,
      disableButtonHold: true,
      onKeyPress(button) {
        if (button === "{shift}") {
          state.shift = !state.shift;
          onLatch("ShiftLeft", state.shift);
          // Swap layouts by manipulating the DOM directly instead of sk.setOptions
          // (it triggers a re-render that wipes or re-adds vkb-hidden-root unexpectedly).
          // Replace each row button's data-skbtn and label manually to switch layouts.
          var rows = root.querySelectorAll('.hg-row');
          var newLayout = state.shift ? UPPER_ROWS : LOWER_ROWS;
          for (var ri = 0; ri < rows.length && ri < newLayout.length; ri++) {
            var btns = rows[ri].querySelectorAll('.hg-button');
            var keys = newLayout[ri].split(' ');
            for (var ki = 0; ki < btns.length && ki < keys.length; ki++) {
              var val = keys[ki];
              btns[ki].setAttribute('data-skbtn', val);
              var display = DISPLAY[val] || COMMON_DISPLAY[val] || val.replace(/[{}]/g, '');
              btns[ki].innerHTML = '<span>' + display + '</span>';
              // update classes
              btns[ki].className = 'hg-button';
              if (state.shift && LOCK_BTNS.indexOf(val) >= 0) {
                btns[ki].classList.add('hg-vkb-lock');
              }
            }
          }
          return;
        }
        if (button === "{ctrl}") { state.mods.ControlLeft = !state.mods.ControlLeft; setLock("{ctrl}", state.mods.ControlLeft); onLatch("ControlLeft", state.mods.ControlLeft); return; }
        if (button === "{alt}") { state.mods.AltLeft = !state.mods.AltLeft; setLock("{alt}", state.mods.AltLeft); onLatch("AltLeft", state.mods.AltLeft); return; }
        if (button === "{meta}") { state.mods.MetaLeft = !state.mods.MetaLeft; setLock("{meta}", state.mods.MetaLeft); onLatch("MetaLeft", state.mods.MetaLeft); return; }
        if (button === "{capslock}") { state.mods.CapsLock = !state.mods.CapsLock; setLock("{capslock}", state.mods.CapsLock); onLatch("CapsLock", state.mods.CapsLock); return; }
        const code = resolveCode(button);
        if (code) onKey(code, true);
      },
      onKeyReleased(button) {
        if (LOCK_BTNS.indexOf(button) >= 0) return; // lock key: release is a no-op
        const code = resolveCode(button);
        if (code) onKey(code, false);
      }
    });

    return {
      open() { root.classList.remove("vkb-hidden-root"); },
      close() { root.classList.add("vkb-hidden-root"); },
      toggle() { root.classList.toggle("vkb-hidden-root"); },
      isOpen() { return !root.classList.contains("vkb-hidden-root"); },
      reset() { // release all keys (called on disconnect / session switch)
        if (state.shift) { state.shift = false; onLatch("ShiftLeft", false); }
        Object.keys(state.mods).forEach((c) => { if (state.mods[c]) { state.mods[c] = false; onLatch(c, false); } });
        LOCK_BTNS.forEach((b) => setLock(b, false));
        sk.setOptions({ layoutName: "default" });
      },
      destroy() {
        try { sk.destroy(); } catch (e) {}
      }
    };
  }

  return { mount, resolveCode };
});
