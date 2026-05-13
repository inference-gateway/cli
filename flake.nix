{
  description = "Inference Gateway CLI - infer";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        inherit (pkgs) lib stdenv;

        version = "0.109.4";

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
                || (type == "directory" && lib.hasPrefix "internal/display/macos/ComputerUse/build" relPath)
              );
          };

          vendorHash = "sha256-+ntde+NYik4gEicMlyonBAE+gkHoYYiw3G0dbQ/gX2I=";

          goSum = ./go.sum;

          proxyVendor = true;

          env.CGO_ENABLED = if stdenv.hostPlatform.isDarwin then "1" else "0";

          ldflags = [
            "-s"
            "-w"
            "-X=github.com/inference-gateway/cli/cmd.version=${version}"
            "-X=github.com/inference-gateway/cli/cmd.commit=${self.shortRev or "dirty"}"
          ];

          preCheck = ''
            export HOME=$TMPDIR
          '';

          checkFlags = [
            "-skip=TestIntegration"
          ];

          nativeBuildInputs = [
            pkgs.installShellFiles
          ]
          ++ lib.optionals stdenv.hostPlatform.isDarwin [ pkgs.swift ];

          buildInputs = lib.optionals stdenv.hostPlatform.isDarwin [ pkgs.apple-sdk ];

          preBuild = lib.optionalString stdenv.hostPlatform.isDarwin ''
            export SDKROOT="${pkgs.apple-sdk}/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk"
            pushd internal/display/macos/ComputerUse > /dev/null
            bash ./build.sh
            popd > /dev/null
          '';

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
