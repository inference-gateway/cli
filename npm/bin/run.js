#!/usr/bin/env node
"use strict";

// Locates the native `infer` binary fetched by bin/install.js and execs it,
// forwarding all arguments, stdio, and the exit code.

const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const binName = process.platform === "win32" ? "infer.exe" : "infer";
const binPath = path.join(__dirname, binName);

if (!fs.existsSync(binPath)) {
  const install = spawnSync(process.execPath, [path.join(__dirname, "install.js")], {
    stdio: "inherit",
  });
  if (install.status !== 0 || !fs.existsSync(binPath)) {
    console.error(
      "@inference-gateway/cli: native binary not found and could not be installed. " +
        "Reinstall the package or install from source: " +
        "https://github.com/inference-gateway/cli#installation"
    );
    process.exit(1);
  }
}

const result = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(`@inference-gateway/cli: failed to run binary: ${result.error.message}`);
  process.exit(1);
}

process.exit(result.status === null ? 1 : result.status);
