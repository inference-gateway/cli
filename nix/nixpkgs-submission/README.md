# nixpkgs Submission

Quick guide for submitting `infer` to nixpkgs.

## Steps

```bash
# 1. Fork https://github.com/NixOS/nixpkgs
git clone https://github.com/YOUR_USERNAME/nixpkgs.git
cd nixpkgs
git checkout -b infer-init

# 2. Add maintainer info to maintainers/maintainer-list.nix
edenreich = {
  email = "eden.reich@gmail.com";
  github = "edenreich";
  githubId = 16985712;
  name = "Eden Reich";
};

# 3. Copy package definition
mkdir -p pkgs/by-name/in/infer
cp path/to/cli/nix/package.nix pkgs/by-name/in/infer/package.nix

# 4. Test build
nix-build -A infer

# 5. Commit and create PR
git add pkgs/by-name/in/infer/package.nix maintainers/maintainer-list.nix
git commit -m "infer: init at 0.76.1"
git push origin infer-init
```

## Package Info

- **Package**: `../package.nix` (use this file for submission)
- **Name**: `infer`
- **Command**: `infer`
- **Version**: 0.76.1
- **Status**: ✅ Builds on all platforms

## Install After Merge

```bash
nix profile install nixpkgs#infer
```
