#!/usr/bin/env node
"use strict";

// Downloads the native `infer` binary that matches the host platform from the
// GitHub release matching this package's version, and places it next to this
// script so `bin/run.js` can exec it. Runs as an npm `postinstall` hook.
//
// The CLI publishes raw executables (e.g. `infer-linux-amd64`) plus a
// `checksums.txt`, so this downloads the binary directly - no archive to extract.

const fs = require("fs");
const path = require("path");
const https = require("https");
const crypto = require("crypto");

const REPO = "inference-gateway/cli";
const { version } = require("../package.json");
const BASE_URL =
  process.env.INFER_CLI_BASE_URL ||
  `https://github.com/${REPO}/releases/download/v${version}`;

// Maps Node's process.platform / process.arch onto the release asset names.
const PLATFORMS = { linux: "linux", darwin: "darwin", win32: "windows" };
const ARCHES = { x64: "amd64", arm64: "arm64" };

const binName = process.platform === "win32" ? "infer.exe" : "infer";
const binPath = path.join(__dirname, binName);

function fail(message) {
  console.error(`\n@inference-gateway/cli: ${message}\n`);
  process.exit(1);
}

function resolveAsset() {
  const goos = PLATFORMS[process.platform];
  const goarch = ARCHES[process.arch];
  if (!goos || !goarch) {
    fail(
      `unsupported platform ${process.platform}/${process.arch}. ` +
        `Prebuilt binaries are published for linux, darwin, and win32 on amd64/arm64 only. ` +
        `Install from source instead: https://github.com/${REPO}#installation`
    );
  }
  return `infer-${goos}-${goarch}`;
}

function download(url) {
  return new Promise((resolve, reject) => {
    https
      .get(url, { headers: { "User-Agent": "inference-gateway-cli-npm" } }, (res) => {
        const { statusCode, headers } = res;
        if (statusCode >= 300 && statusCode < 400 && headers.location) {
          res.resume();
          resolve(download(headers.location));
          return;
        }
        if (statusCode !== 200) {
          res.resume();
          reject(new Error(`request to ${url} failed with status ${statusCode}`));
          return;
        }
        const chunks = [];
        res.on("data", (chunk) => chunks.push(chunk));
        res.on("end", () => resolve(Buffer.concat(chunks)));
      })
      .on("error", reject);
  });
}

async function verifyChecksum(binary, assetName) {
  let checksums;
  try {
    checksums = (await download(`${BASE_URL}/checksums.txt`)).toString("utf8");
  } catch (err) {
    console.warn(
      `@inference-gateway/cli: could not fetch checksums.txt (${err.message}); skipping integrity check`
    );
    return;
  }
  const line = checksums.split("\n").find((l) => l.trim().endsWith(assetName));
  if (!line) {
    console.warn(
      `@inference-gateway/cli: no checksum entry for ${assetName}; skipping integrity check`
    );
    return;
  }
  const expected = line.trim().split(/\s+/)[0];
  const actual = crypto.createHash("sha256").update(binary).digest("hex");
  if (expected !== actual) {
    fail(`checksum mismatch for ${assetName} (expected ${expected}, got ${actual})`);
  }
}

async function main() {
  if (process.env.INFER_CLI_SKIP_DOWNLOAD) {
    console.log("@inference-gateway/cli: INFER_CLI_SKIP_DOWNLOAD set, skipping binary download");
    return;
  }
  if (fs.existsSync(binPath)) {
    return;
  }

  const assetName = resolveAsset();
  const url = `${BASE_URL}/${assetName}`;
  console.log(`@inference-gateway/cli: downloading ${assetName} (v${version})`);

  let binary;
  try {
    binary = await download(url);
  } catch (err) {
    fail(
      `failed to download ${url}: ${err.message}. ` +
        `If you are offline, install from source: https://github.com/${REPO}#installation`
    );
  }

  await verifyChecksum(binary, assetName);

  fs.mkdirSync(path.dirname(binPath), { recursive: true });
  fs.writeFileSync(binPath, binary, { mode: 0o755 });
  fs.chmodSync(binPath, 0o755);
  console.log(`@inference-gateway/cli: installed infer ${version} to ${binPath}`);
}

main().catch((err) => fail(err.message));
