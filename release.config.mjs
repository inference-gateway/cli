/**
 * semantic-release configuration.
 *
 * This is a JS config (not YAML) because the installation section is appended
 * to the release notes via `writerOpts.footerPartial`, which
 * conventional-changelog-writer v9 only accepts as a function. Generating the
 * section in the notes (rather than the GitHub plugin's releaseBodyTemplate)
 * is what lets `addReleases: bottom` work: that option rewrites the release
 * body from the raw notes, so anything not in the notes gets discarded.
 */

const installationSection = (context) => {
  const version = context.version;
  const gitTag = `v${version}`;

  return `
## 📦 Installation

### npm / npx (Recommended)

Most developers already have Node.js - run \`infer\` without installing anything. npx downloads the matching native binary on first use:

\`\`\`bash
npx @inference-gateway/cli@${version} --help
npx @inference-gateway/cli@${version} chat
\`\`\`

Or install it globally:

\`\`\`bash
npm install -g @inference-gateway/cli@${version}
infer --help
\`\`\`

> Not recommended for production - prefer the install script, container image, or Nix flake below.

### Quick Install (Install Script)

Install the latest version using our install script:

\`\`\`bash
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash
\`\`\`

Or install a specific version:

\`\`\`bash
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --version ${gitTag}
\`\`\`

Custom installation directory:

\`\`\`bash
curl -fsSL https://raw.githubusercontent.com/inference-gateway/cli/main/install.sh | bash -s -- --install-dir $HOME/.local/bin
\`\`\`

### Nix Flake

Run directly without installing:

\`\`\`bash
nix run github:inference-gateway/cli/${gitTag}
\`\`\`

Or pin it in a [Flox](https://flox.dev) manifest (\`.flox/env/manifest.toml\`):

\`\`\`toml
[install]
infer.flake = "github:inference-gateway/cli/${gitTag}"
\`\`\`

### Container Image

\`\`\`bash
docker run --rm -it ghcr.io/inference-gateway/cli:${version}
\`\`\`

### Binary Download

Download the appropriate binary for your platform from the release assets, or see the [verification guide](https://github.com/inference-gateway/cli/blob/main/docs/binary-verification.md).
`;
};

export default {
  branches: [{ name: 'main' }, { name: 'rc/*', prerelease: 'rc' }],

  tagFormat: 'v${version}',

  plugins: [
    [
      '@semantic-release/commit-analyzer',
      {
        preset: 'conventionalcommits',
        releaseRules: [
          { type: 'feat', release: 'minor' },
          { type: 'fix', release: 'patch' },
          { type: 'perf', release: 'patch' },
          { type: 'docs', release: 'patch' },
          { type: 'style', release: 'patch' },
          { type: 'refactor', release: 'patch' },
          { type: 'test', release: 'patch' },
          { type: 'build', release: 'patch' },
          { type: 'ci', release: 'patch' },
          { type: 'chore', release: 'patch' },
          { type: 'revert', release: 'patch' },
          { breaking: true, release: 'minor' },
        ],
      },
    ],

    [
      '@semantic-release/release-notes-generator',
      {
        preset: 'conventionalcommits',
        presetConfig: {
          types: [
            { type: 'feat', section: '🚀 Features', hidden: false },
            { type: 'fix', section: '🐛 Bug Fixes', hidden: false },
            { type: 'perf', section: '⚡ Performance Improvements', hidden: false },
            { type: 'refactor', section: '♻️ Code Refactoring', hidden: false },
            { type: 'docs', section: '📚 Documentation', hidden: false },
            { type: 'style', section: '💄 Styles', hidden: false },
            { type: 'test', section: '✅ Tests', hidden: false },
            { type: 'build', section: '🔧 Build System', hidden: false },
            { type: 'ci', section: '👷 CI/CD', hidden: false },
            { type: 'chore', section: '🧹 Maintenance', hidden: false },
            { type: 'revert', section: '⏪ Reverts', hidden: false },
          ],
        },
        writerOpts: {
          commitsSort: ['subject', 'scope'],
          footerPartial: installationSection,
        },
      },
    ],

    [
      '@semantic-release/changelog',
      {
        changelogFile: 'CHANGELOG.md',
        changelogTitle: [
          '# Changelog',
          '',
          'All notable changes to this project will be documented in this file.',
          '',
          'The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),',
          'and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).',
        ].join('\n'),
      },
    ],

    [
      '@semantic-release/exec',
      {
        verifyReleaseCmd: [
          'if [ -n "$GITHUB_OUTPUT" ]; then',
          '  {',
          '    echo "version=${nextRelease.version}"',
          '    echo "published=true"',
          '  } >> "$GITHUB_OUTPUT"',
          'fi',
        ].join('\n'),
        prepareCmd: [
          `sed -i.bak 's|version = "[^"]*";|version = "\${nextRelease.version}";|' flake.nix && rm flake.nix.bak`,
          `sed -i.bak 's|vendorHash = "[^"]*";|vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";|' flake.nix && rm flake.nix.bak`,
          `NEW_HASH=$(nix build .#infer 2>&1 | awk '/got:/ {print $2; exit}')`,
          `if [ -z "$NEW_HASH" ]; then echo "ERROR: could not extract vendorHash from nix build output"; exit 1; fi`,
          `sed -i.bak "s|vendorHash = \\"sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\\";|vendorHash = \\"$NEW_HASH\\";|" flake.nix && rm flake.nix.bak`,
          `echo "Updated vendorHash to $NEW_HASH"`,
          `cp LICENSE npm/LICENSE`,
          `cp README.md npm/README.md`,
        ].join('\n'),
      },
    ],

    ['@semantic-release/npm', { pkgRoot: 'npm' }],

    [
      '@semantic-release/git',
      {
        assets: ['CHANGELOG.md', 'flake.nix', 'npm/package.json'],
        message: 'chore(release): ${nextRelease.version} [skip ci]',
      },
    ],

    [
      '@semantic-release/github',
      {
        releaseNameTemplate: '🚀 Version <%= nextRelease.version %>',
        assets: [
          { path: 'dist/infer-linux-amd64' },
          { path: 'dist/infer-linux-arm64' },
          { path: 'dist/infer-darwin-amd64' },
          { path: 'dist/infer-darwin-arm64' },
          { path: 'dist/infer-windows-amd64' },
          { path: 'dist/infer-windows-arm64' },
          { path: 'dist/checksums.txt' },
          { path: 'dist/checksums.txt.sigstore.json' },
        ],
        discussionCategoryName: false,
        addReleases: 'bottom',
        releasedLabels: ["<%= nextRelease.channel ? 'prerelease' : 'released' %>"],
      },
    ],
  ],
};
