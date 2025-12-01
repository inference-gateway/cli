{ lib
, buildGoModule
, fetchFromGitHub
, installShellFiles
, stdenv
, darwin
}:

buildGoModule rec {
  pname = "infer";
  version = "0.76.1";

  src = fetchFromGitHub {
    owner = "inference-gateway";
    repo = "cli";
    rev = "v${version}";
    hash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="; # This will need to be updated with actual hash
  };

  vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="; # This will need to be calculated

  # macOS requires CGO for clipboard support (golang.design/x/clipboard)
  CGO_ENABLED = if stdenv.isDarwin then 1 else 0;

  buildInputs = lib.optionals stdenv.isDarwin [
    darwin.apple_sdk.frameworks.Cocoa
    darwin.apple_sdk.frameworks.UserNotifications
  ];

  ldflags = [
    "-s"
    "-w"
    "-X github.com/inference-gateway/cli/cmd.version=${version}"
    "-X github.com/inference-gateway/cli/cmd.commit=${src.rev}"
    "-X github.com/inference-gateway/cli/cmd.date=1970-01-01T00:00:00Z"
  ];

  # Disable tests that require network or external dependencies
  preCheck = ''
    export HOME=$TMPDIR
  '';

  # Some tests may fail in the Nix sandbox due to networking requirements
  checkFlags = [
    "-skip=TestIntegration"
  ];

  nativeBuildInputs = [ installShellFiles ];

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
    maintainers = with maintainers; [ ]; # Add maintainer names here when submitting to nixpkgs
    mainProgram = "infer";
    platforms = platforms.unix;
  };
}
