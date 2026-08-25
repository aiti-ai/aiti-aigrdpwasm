/**
 * postinstall: remove main.wasm (~11MB) from the installed copy.
 *
 * The npm tarball ships the wasm (that's what jsDelivr/unpkg serve from), but at
 * runtime the page race-downloads it from CDNs via justget — no need to keep a
 * local copy, so delete it right after install to save disk.
 *
 * Skipped when:
 *   - inside a source checkout (.git exists): running `npm install` in the repo
 *     triggers this lifecycle hook too; dev build artifacts must survive;
 *   - GRDPWASM_KEEP_WASM=1 is set explicitly.
 */
import { existsSync, rmSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const wasm = join(root, "static", "main.wasm");

if (process.env.GRDPWASM_KEEP_WASM === "1") {
  console.log("[grdpwasm] GRDPWASM_KEEP_WASM=1, keep static/main.wasm");
} else if (existsSync(join(root, ".git"))) {
  console.log("[grdpwasm] source checkout detected (.git), keep static/main.wasm");
} else if (existsSync(wasm)) {
  const mb = (statSync(wasm).size / 1048576).toFixed(1);
  rmSync(wasm);
  console.log(`[grdpwasm] removed static/main.wasm (${mb} MB) - runtime fetches from CDN`);
}
