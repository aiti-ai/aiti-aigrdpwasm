/**
 * aiti-ui-env shared viewer component: text -> RFB keyboard event sequence (for VNC "type into remote")
 *
 * Why: the VNC clipboard is not reliable on every target —
 *   - a QEMU VM's VNC clipboard needs a guest agent (spice-vdagent etc.) to reach the guest; without one,
 *     ClientCutText data dangles inside QEMU (noVNC sends successfully but the guest never receives it);
 *   - x11vnc drops ClientCutText silently for roughly the first 8s of a connection, before selwin exists;
 *   - the legacy extension clipboard protocol is Latin-1 only; non-ASCII turns into '?'.
 * Fallback = split the text into keyboard events sent one by one (equivalent to a human typing on the
 * remote); every VNC server supports that.
 *
 * Mapping convention (US layout, matching the official noVNC Keyboard): each char provides
 *   - keysym: the char's X11 keysym (used by the legacy KeyEvent protocol; x11vnc/libvncserver reverse-map keysym -> key+Shift)
 *   - code  : DOM code name (used by the QEMU extended key event to look up the Xt scancode table for the AT scancode)
 *   - shift : whether the char needs Shift (the QEMU extended key event only understands scancode + modifier state, so Shift down/up must be sent along)
 * Non-ASCII (CJK/emoji...) cannot map to a single key -> skipped and counted (an IME is needed on the remote); the caller surfaces this to the user.
 */

/** XK_Shift_L: modifier keysym (DOM code = "ShiftLeft") */
export const SHIFT_KEYSYM = 0xffe1;

const LETTERS = (() => {
  const m = {};
  for (let i = 0; i < 26; i++) {
    const lower = String.fromCharCode(97 + i);
    const upper = String.fromCharCode(65 + i);
    const code = "Key" + upper;
    m[lower] = { keysym: lower.codePointAt(0), code, shift: false };
    m[upper] = { keysym: upper.codePointAt(0), code, shift: true };
  }
  return m;
})();

const DIGITS = (() => {
  const m = {};
  for (let d = 0; d <= 9; d++) m[String(d)] = { keysym: 0x30 + d, code: "Digit" + d, shift: false };
  return m;
})();

/** [char, DOM code]; shifted chars not listed here live in SHIFTED */
const PLAIN_PUNCT = [
  [" ", "Space"], ["`", "Backquote"], ["-", "Minus"], ["=", "Equal"],
  ["[", "BracketLeft"], ["]", "BracketRight"], ["\\", "Backslash"], [";", "Semicolon"],
  ["'", "Quote"], [",", "Comma"], [".", "Period"], ["/", "Slash"],
];
const SHIFTED_PUNCT = [
  ["!", "Digit1"], ["@", "Digit2"], ["#", "Digit3"], ["$", "Digit4"], ["%", "Digit5"],
  ["^", "Digit6"], ["&", "Digit7"], ["*", "Digit8"], ["(", "Digit9"], [")", "Digit0"],
  ["_", "Minus"], ["+", "Equal"], ["{", "BracketLeft"], ["}", "BracketRight"], ["|", "Backslash"],
  [":", "Semicolon"], ['"', "Quote"], ["<", "Comma"], [">", "Period"], ["?", "Slash"], ["~", "Backquote"],
];

/** Control chars: keysyms use the X11 function-key keysyms */
const CONTROL = {
  "\n": { keysym: 0xff0d, code: "Enter", shift: false },
  "\t": { keysym: 0xff09, code: "Tab", shift: false },
  "\b": { keysym: 0xff08, code: "Backspace", shift: false },
};

const TABLE = (() => {
  const m = Object.assign({}, LETTERS, DIGITS, CONTROL);
  for (const [ch, code] of PLAIN_PUNCT) m[ch] = { keysym: ch.codePointAt(0), code, shift: false };
  for (const [ch, code] of SHIFTED_PUNCT) m[ch] = { keysym: ch.codePointAt(0), code, shift: true };
  return m;
})();

/** One char -> key spec; returns null when unmappable (non-ASCII / unknown control char) */
export function charToKey(ch) {
  if (typeof ch !== "string" || ch.length === 0) return null;
  return TABLE[ch] || null;
}

/**
 * Text -> key event sequence.
 * @param {string} text
 * @returns {{keys: {keysym:number, code:string, shift:boolean}[], skipped: number}}
 *   skipped = number of unmappable chars (non-ASCII, \r, ...); the \r of \r\n is not counted (the newline is sent as \n).
 */
export function textToKeys(text) {
  const keys = [];
  let skipped = 0;
  for (const ch of String(text == null ? "" : text)) {
    if (ch === "\r") continue; // CRLF: drop \r, send Enter for \n (avoids double newline)
    const spec = charToKey(ch);
    if (spec) keys.push(spec);
    else skipped++;
  }
  return { keys, skipped };
}

export default { textToKeys, charToKey, SHIFT_KEYSYM };
