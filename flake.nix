{
  description = "Inference Gateway CLI - infer";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        inherit (pkgs) lib;

        version = "0.183.4";

        infer = pkgs.buildGoModule (finalAttrs: {
          __structuredAttrs = true;

          pname = "infer";
          inherit version;

          src = lib.cleanSourceWith {
            src = ./.;
            filter =
              path: type:
              let
                baseName = baseNameOf (toString path);
                relPath = lib.removePrefix (toString ./. + "/") (toString path);
              in
              !(
                baseName == ".git"
                || baseName == "dist"
                || baseName == "result"
                || baseName == ".flox"
                || baseName == ".infer"
                || baseName == ".task"
                || baseName == "node_modules"
                || (type == "regular" && relPath == "infer")
              );
          };

          vendorHash = "sha256-9+RxiDoN1d0+8KI74OJ+IfkrzYD9Ya/X2s5MUbMz1eQ=";

          goSum = ./go.sum;

          proxyVendor = true;

          env.CGO_ENABLED = "0";

          tags = [ "purego" ];

          subPackages = [ "cmd/infer" ];

          ldflags = [
            "-s"
            "-w"
            "-X=github.com/inference-gateway/cli/cmd/version.version=${version}"
          ];

          preCheck = ''
            export HOME=$TMPDIR
          '';

          checkFlags = [
            "-skip=TestIntegration"
          ];

          nativeCheckInputs = [ pkgs.git ];

          nativeBuildInputs = [
            pkgs.installShellFiles
          ];

          postInstall = ''
            if [ -f $out/bin/cli ]; then
              mv $out/bin/cli $out/bin/infer
            fi

            installShellCompletion --cmd infer \
              --bash <($out/bin/infer completion bash) \
              --fish <($out/bin/infer completion fish) \
              --zsh <($out/bin/infer completion zsh)
          '';

          meta = {
            description = "Command-line interface for the Inference Gateway - AI model interaction manager";
            longDescription = ''
              The Inference Gateway CLI is a command-line tool for managing AI model interactions.
              It provides interactive chat, autonomous agent execution, and extensive tool
              integration for LLMs, with support for both the MCP and A2A protocols, as well
              as computer use for GUI automation. It can also run as a Telegram bot for
              remote-controlling the agent from chat.
            '';
            homepage = "https://github.com/inference-gateway/cli";
            changelog = "https://github.com/inference-gateway/cli/blob/v${version}/CHANGELOG.md";
            license = lib.licenses.mit;
            maintainers = [
              {
                name = "Eden Reich";
                email = "eden.reich@gmail.com";
                github = "edenreich";
                githubId = 26537388;
              }
            ];
            mainProgram = "infer";
            platforms = lib.platforms.unix;
          };
        });
      in
      {
        packages = {
          default = infer;
          inherit infer;
        };

        apps.default = {
          type = "app";
          program = "${infer}/bin/infer";
          meta = {
            description = "Run the infer CLI";
            mainProgram = "infer";
          };
        };

        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.go-task
            pkgs.golangci-lint
            pkgs.gopls
          ];
        };
      }
    );
}
