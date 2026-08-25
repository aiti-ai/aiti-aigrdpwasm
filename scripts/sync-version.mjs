/**
 * prepack: sync the package.json version into PKG_VERSION inside
 * static/index.html so the runtime CDN URLs always match the published version.
 */
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const { version } = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));

const html = join(root, "static", "index.html");
const src = readFileSync(html, "utf8");
if (!/const PKG_VERSION = "[^"]*";/.test(src)) {
  console.error("[grdpwasm] PKG_VERSION placeholder not found in index.html");
  process.exit(1);
}
const next = src.replace(
  /const PKG_VERSION = "[^"]*";/,
  `const PKG_VERSION = "${version}";`
);
if (next !== src) writeFileSync(html, next);
writeFileSync(html, next);
console.log(`[grdpwasm] index.html PKG_VERSION -> ${version}`);
