# nixpkgs Submission

Quick guide for submitting `inference-gateway-cli` to nixpkgs.

## Steps

```bash
# 1. Fork https://github.com/NixOS/nixpkgs
git clone https://github.com/YOUR_USERNAME/nixpkgs.git
cd nixpkgs
git checkout -b inference-gateway-cli-init

# 2. Add maintainer info to maintainers/maintainer-list.nix
edenreich = {
  email = "eden.reich@gmail.com";
  github = "edenreich";
  githubId = 16985712;
  name = "Eden Reich";
};

# 3. Copy package definition
mkdir -p pkgs/by-name/in/inference-gateway-cli
cp path/to/cli/nix/package.nix pkgs/by-name/in/inference-gateway-cli/package.nix

# 4. Test build
nix-build -A inference-gateway-cli

# 5. Commit and create PR
git add pkgs/by-name/in/inference-gateway-cli/package.nix maintainers/maintainer-list.nix
git commit -m "inference-gateway-cli: init at 0.76.1"
git push origin inference-gateway-cli-init
```

## Package Info

- **Package**: `../package.nix` (use this file for submission)
- **Name**: `inference-gateway-cli`
- **Command**: `infer`
- **Version**: 0.76.1
- **Status**: ✅ Builds on all platforms

## Install After Merge

```bash
nix profile install nixpkgs#inference-gateway-cli
```
