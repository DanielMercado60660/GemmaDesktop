import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// Pin the actual pre-change App and history/composer helpers. All generated
// sources stay in the ignored dependency cache; the working tree is untouched.
const root = dirname(fileURLToPath(import.meta.url));
const baseline = "d8dbce0741994373a5cc4677e62791d1ff59a806";
const cache = resolve(root, "node_modules/.cache/history-baseline");
const files = ["App.tsx", "lib/conversation.ts", "lib/composer.ts"];
for (const file of files) {
  const destination = resolve(cache, file);
  const source = execFileSync(
    "git",
    ["show", `${baseline}:frontend/src/${file}`],
    {
      cwd: root,
      encoding: "utf8",
    },
  );
  const rewritten = source.replace(
    /(from\s+|import\()(["'])(\.{1,2}\/[^"']+)\2/g,
    (_match, prefix: string, quote: string, relative: string) => {
      const absolute = resolve(root, "src", dirname(file), relative);
      const local = files.find(
        (candidate) =>
          resolve(root, "src", candidate).replace(/\.(tsx?|jsx?)$/, "") ===
          absolute,
      );
      return `${prefix}${quote}${local ? resolve(cache, local) : absolute}${quote}`;
    },
  );
  mkdirSync(dirname(destination), { recursive: true });
  writeFileSync(destination, rewritten);
}
export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@baseline-app": resolve(cache, "App.tsx") } },
  test: {
    environment: "jsdom",
    include: ["tests/bench/history.test.tsx"],
    setupFiles: ["./tests/ui/setup.ts"],
    restoreMocks: true,
    testTimeout: 30000,
  },
});
