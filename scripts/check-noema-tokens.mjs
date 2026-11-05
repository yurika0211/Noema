import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const tokens = JSON.parse(readFileSync(join(root, "apps/tokens/colors.json"), "utf8"));
const cssName = {
  noema_bg: "--noema-bg",
  noema_ink: "--noema-ink",
  noema_accent: "--noema-accent",
  noema_paper: "--paper",
  noema_mute: "--mute",
  noema_you: "--you",
  noema_stop: "--stop",
};

const html = readFileSync(join(root, "apps/web/index.html"), "utf8");
const rootBlock = html.match(/:root\{([\s\S]*?)\}/);
if (!rootBlock) fail("apps/web/index.html has no :root");
const xml = readFileSync(join(root, "apps/android/app/src/main/res/values/colors.xml"), "utf8");
const kt = readFileSync(join(root, "apps/android/app/src/main/java/app/noema/android/ui/NoemaTokens.kt"), "utf8");

const problems = [];
for (const [key, hex] of Object.entries(tokens)) {
  const want = String(hex).toLowerCase();
  const css = cssName[key];
  if (!css) {
    problems.push(key + " has no css name");
    continue;
  }
  const cssHit = rootBlock[1].match(new RegExp(css.replaceAll("-", "\\-") + "\\s*:\\s*(#[0-9a-fA-F]{6})"));
  if (!cssHit || cssHit[1].toLowerCase() !== want) {
    problems.push("css " + css + " is " + (cssHit ? cssHit[1] : "missing") + ", want " + want);
  }
  const xmlHit = xml.match(new RegExp('name="' + key + '">(#(?:[0-9a-fA-F]{6}))'));
  if (!xmlHit || xmlHit[1].toLowerCase() !== want) {
    problems.push("xml " + key + " is " + (xmlHit ? xmlHit[1] : "missing") + ", want " + want);
  }
  const ktHit = kt.match(new RegExp("val " + key + " = Color\\(0xFF([0-9a-fA-F]{6})\\)"));
  if (!ktHit || ("#" + ktHit[1]).toLowerCase() !== want) {
    problems.push("kt " + key + " is " + (ktHit ? ktHit[1] : "missing") + ", want " + want.slice(1));
  }
}

if (problems.length) fail(problems.join("\n"));

function fail(message) {
  console.error(message);
  process.exit(1);
}
