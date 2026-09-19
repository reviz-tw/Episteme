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
await import(pathToFileURL(resolve(standalone, "server.js")).href);
