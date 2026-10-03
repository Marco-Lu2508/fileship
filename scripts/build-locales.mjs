import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const languages = ["en", "de", "fr", "es", "it", "pt"];
const dictionaries = Object.fromEntries(languages.map((lang) => [lang, {}]));
for (const line of readFileSync(
  path.join(root, "scripts/translations.txt"),
  "utf8",
)
  .split(/\r?\n/)
  .filter(Boolean)) {
  const values = line.split("|");
  if (
    values.length !== languages.length ||
    values.some((value) => !value.trim())
  )
    throw new Error(`Invalid translation row: ${values[0]}`);
  if (values[0] in dictionaries.en)
    throw new Error(`Duplicate key: ${values[0]}`);
  languages.forEach((lang, i) => (dictionaries[lang][values[0]] = values[i]));
}
for (const lang of languages)
  writeFileSync(
    path.join(root, `frontend/public/locales/${lang}.json`),
    JSON.stringify(dictionaries[lang], null, 2) + "\n",
  );
console.log(
  `${Object.keys(dictionaries.en).length} phrases in ${languages.length} languages`,
);
