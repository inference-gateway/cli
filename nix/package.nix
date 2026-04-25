{ lib
, buildGoModule
, fetchFromGitHub
, installShellFiles
, stdenv
, swift
, apple-sdk
}:

buildGoModule rec {
  pname = "infer";
  version = "0.103.5";

  src = fetchFromGitHub {
    owner = "inference-gateway";
    repo = "cli";
    rev = "v${version}";
    hash = "sha256-r4fozLRdZMZdvyJCnzOFOaN9SbkH5G22zdazFvdnzO4=";
  };

  vendorHash = "sha256-3kHd6AetSaOGSMeYsmeGPifE8oMrcUp/UQ4L6yK/CIg=";

  # Use the Go module proxy layout instead of `go mod vendor`. The robotgo
  # dependency includes CGO `#include` directives that reference C headers
  # in subpackages (e.g. screen/goScreen.h) that `go mod vendor` strips
  # because no Go code imports those subpackages directly. proxyVendor
  # preserves the full module layout, including the headers CGO needs.
  proxyVendor = true;

  # macOS requires CGO for clipboard support (golang.design/x/clipboard)
  env.CGO_ENABLED = if stdenv.isDarwin then "1" else "0";

  ldflags = [
    "-s"
    "-w"
    "-X github.com/inference-gateway/cli/cmd.version=${version}"
    "-X github.com/inference-gateway/cli/cmd.commit=${src.rev}"
  ];

  # Disable tests that require network or external dependencies
  preCheck = ''
    export HOME=$TMPDIR
  '';

  # Some tests may fail in the Nix sandbox due to networking requirements
  checkFlags = [
    "-skip=TestIntegration"
  ];

  nativeBuildInputs = [ installShellFiles ]
    ++ lib.optionals stdenv.isDarwin [ swift ];

  buildInputs = lib.optionals stdenv.isDarwin [ apple-sdk ];

  # On macOS, the Go binary embeds a SwiftUI floating-window helper app via
  # //go:embed. The build/ folder is gitignored, so we compile the Swift
  # sources before `go build` runs. The same build.sh is used by the
  # standard release workflow, keeping a single source of truth for the
  # Swift app build. build.sh reads SDKROOT and skips codesign when it is
  # not available in the sandbox.
  preBuild = lib.optionalString stdenv.isDarwin ''
    export SDKROOT="${apple-sdk}/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk"
    pushd internal/display/macos/ComputerUse > /dev/null
    bash ./build.sh
    popd > /dev/null
  '';

  postInstall = ''
    # Rename binary from 'cli' to 'infer' if needed
    if [ -f $out/bin/cli ]; then
      mv $out/bin/cli $out/bin/infer
    fi

    # Generate shell completions
    installShellCompletion --cmd infer \
      --bash <($out/bin/infer completion bash) \
      --fish <($out/bin/infer completion fish) \
      --zsh <($out/bin/infer completion zsh)
  '';

  meta = with lib; {
    description = "Command-line interface for the Inference Gateway - AI model interaction manager";
    longDescription = ''
      The Inference Gateway CLI is a powerful command-line tool for managing AI model interactions.
      It provides interactive chat, autonomous agent execution, and extensive tool integration for LLMs.

      Features:
      - Interactive chat with various AI models
      - Autonomous agent execution with tool support
      - Clean Architecture with domain-driven design
      - Multiple storage backends (SQLite, PostgreSQL, Redis)
      - Terminal UI with BubbleTea framework
      - Extensive tool system (Bash, Read, Write, Grep, A2A, etc.)
    '';
    homepage = "https://github.com/inference-gateway/cli";
    changelog = "https://github.com/inference-gateway/cli/blob/main/CHANGELOG.md";
    license = licenses.mit;
    maintainers = with maintainers; [ edenreich ];
    mainProgram = "infer";
    platforms = platforms.unix;
  };
}
