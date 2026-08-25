/*!
 * system-keyboard.js — system (device-native) soft-keyboard proxy (host-agnostic shared component)
 *
 * Purpose: a remote-desktop canvas (canvas / tabindex=-1) usually fails to summon
 * the IME on mobile; this component uses one hidden <input> to recall the system
 * keyboard and forwards typed characters/backspace/enter to the host via callbacks.
 *
 *   const kb = SystemKeyboard.mount({ onChange(ev) });  // ev = {type:"char",ch} | {type:"special",name}
 *   kb.open()/close()/toggle(); kb.isOpen;
 *
 * Notes:
 *  - the input must not be display:none (some browsers won't pop the IME); hosts style it as a near-transparent fixed bar
 *  - during IME composition the input fires repeatedly; forward each segment (the remote receives Unicode keysyms)
 */
(function (global, factory) {
  const mod = factory();
  if (typeof module === "object" && module.exports) module.exports = mod;
  else if (typeof define === "function" && define.amd) define([], () => mod);
  else global.SystemKeyboard = mod;
})(typeof self !== "undefined" ? self : this, function () {
  "use strict";

  function mount(opts) {
    const onChange = opts.onChange || function () {};

    const input = document.createElement("input");
    input.type = "text";
    input.className = "syskb-input";
    input.name = "syskb";
    input.setAttribute("autocomplete", "off");
    input.setAttribute("autocapitalize", "off");
    input.setAttribute("autocorrect", "off");
    input.spellcheck = false;
    document.body.appendChild(input);

    function flush() {
      const v = input.value;
      if (!v) return;
      input.value = "";
      for (const ch of v) onChange({ type: "char", ch: ch });
    }
    input.addEventListener("input", flush);
    input.addEventListener("keydown", (e) => {
      // special keys from physical keyboards never show up in value
      if (e.key === "Backspace") { e.preventDefault(); onChange({ type: "special", name: "Backspace" }); }
      else if (e.key === "Enter") { flush(); onChange({ type: "special", name: "Enter" }); }
    });

    return {
      el: input,
      open() { input.focus(); },
      close() { input.blur(); },
      toggle() { if (document.activeElement === input) input.blur(); else input.focus(); },
      get isOpen() { return document.activeElement === input; }
    };
  }

  return { mount };
});
