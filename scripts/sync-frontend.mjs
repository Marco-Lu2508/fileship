import { readdir, rm, cp } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = fileURLToPath(new URL("../", import.meta.url));
const source = path.join(root, "frontend", "dist");
const target = path.join(root, "internal", "static", "dist");
const currentAssets = new Set(await readdir(path.join(source, "assets")));
for (const name of await readdir(path.join(target, "assets"))) {
  if (!currentAssets.has(name)) await rm(path.join(target, "assets", name));
}
await cp(source, target, { recursive: true });
console.log("Embedded frontend synchronized.");
