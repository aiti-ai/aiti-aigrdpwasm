/*!
 * remote-keys.js — remote key helpers (X11/RFB keysym domain; host-agnostic shared component)
 *
 * Used by VNC (noVNC sendKey wants keysyms); RDP (scancode domain) doesn't need this module.
 *   codeToKeysym(code)  — DOM `code` (KeyA/ArrowUp/F5…) -> keysym, 0 when unknown
 *   charToKeysym(ch)    — char -> keysym (direct Latin-1 mapping; otherwise the Unicode extension convention 0x01000000|cp)
 */
(function (global, factory) {
  const mod = factory();
  if (typeof module === "object" && module.exports) module.exports = mod;
  else if (typeof define === "function" && define.amd) define([], () => mod);
  else global.RemoteKeys = mod;
})(typeof self !== "undefined" ? self : this, function () {
  "use strict";

  /* modifier keys */
  const MODS = {
    ShiftLeft: 0xffe1, ShiftRight: 0xffe2,
    ControlLeft: 0xffe3, ControlRight: 0xffe4,
    AltLeft: 0xffe9, AltRight: 0xffea,
    MetaLeft: 0xffeb, MetaRight: 0xffec,
    CapsLock: 0xffe5, NumLock: 0xff7f
  };

  /* special keys */
  const SPECIAL = {
    Escape: 0xff1b, Tab: 0xff09, Backspace: 0xff08, Delete: 0xffff,
    Enter: 0xff0d, Space: 0x20,
    ArrowUp: 0xff52, ArrowDown: 0xff54, ArrowLeft: 0xff51, ArrowRight: 0xff53,
    Home: 0xff50, End: 0xff57, PageUp: 0xff55, PageDown: 0xff56,
    Insert: 0xff63, PrintScreen: 0xff61, ScrollLock: 0xff14, Pause: 0xff13, Menu: 0xff67
  };
  for (let i = 1; i <= 12; i++) SPECIAL["F" + i] = 0xffbe + i - 1; // XK_F1..F12

  /* symbol keys (unshifted chars of the main layout) */
  const SYM = {
    Backquote: 0x60, Minus: 0x2d, Equal: 0x3d,
    BracketLeft: 0x5b, BracketRight: 0x5d, Backslash: 0x5c,
    Semicolon: 0x3b, Quote: 0x27, Comma: 0x2c, Period: 0x2e, Slash: 0x2f
  };

  function codeToKeysym(code) {
    if (MODS[code] !== undefined) return MODS[code];
    if (SPECIAL[code] !== undefined) return SPECIAL[code];
    let m;
    if ((m = /^Key([A-Z])$/.exec(code))) return m[1].charCodeAt(0) + 32; // lowercase a-z
    if ((m = /^Digit([0-9])$/.exec(code))) return m[1].charCodeAt(0);
    if (SYM[code] !== undefined) return SYM[code];
    return 0;
  }

  function charToKeysym(ch) {
    const u = ch.codePointAt(0);
    if (u >= 0x20 && u <= 0xff) return u;
    return 0x01000000 | u;
  }

  return { codeToKeysym, charToKeysym };
});
