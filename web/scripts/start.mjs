import { cp, mkdir } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const standalone = resolve(".next/standalone");
await mkdir(resolve(standalone, ".next"), { recursive: true });
await cp(resolve(".next/static"), resolve(standalone, ".next/static"), {
  recursive: true,
});
process.env.HOSTNAME ||= "127.0.0.1";
process.env.PORT ||= "3000";
// Next's production shutdown can wait indefinitely for an open browser connection.
// Let its normal cleanup run first, then bound shutdown of this web process only.
for (const [signal, code] of [["SIGINT", 130], ["SIGTERM", 143]]) {
  process.once(signal, () => {
    setTimeout(() => process.exit(code), 10_000).unref();
  });
}
await import(pathToFileURL(resolve(standalone, "server.js")).href);
