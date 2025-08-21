# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [0.33.0](https://github.com/inference-gateway/cli/compare/v0.32.2...v0.33.0) (2025-08-21)

### 🚀 Features

* **security:** Block LLM git push to main/master branches ([#95](https://github.com/inference-gateway/cli/issues/95)) ([2910217](https://github.com/inference-gateway/cli/commit/2910217fd0916c32b68c054d4cd0dc865ee5bdc7))

## [0.32.2](https://github.com/inference-gateway/cli/compare/v0.32.1...v0.32.2) (2025-08-21)

### ♻️ Code Refactoring

* Remove HistoryCommand and its registration from the service container ([4bf5a16](https://github.com/inference-gateway/cli/commit/4bf5a167b792d16d75459495c2902297915ce541))

## [0.32.1](https://github.com/inference-gateway/cli/compare/v0.32.0...v0.32.1) (2025-08-20)

### ♻️ Code Refactoring

* Remove ModelsCommand and its registration from the service container ([3a71240](https://github.com/inference-gateway/cli/commit/3a712404d6c88c4278d7330bb5c72254276cbd23))

## [0.32.0](https://github.com/inference-gateway/cli/compare/v0.31.0...v0.32.0) (2025-08-20)

### 🚀 Features

* Add Tools mode with !! prefix for direct tool execution ([#90](https://github.com/inference-gateway/cli/issues/90)) ([1d56cf2](https://github.com/inference-gateway/cli/commit/1d56cf2e3255c84a1f2ffe2e67a44f6cc3d0ebce)), closes [#88](https://github.com/inference-gateway/cli/issues/88) [#88](https://github.com/inference-gateway/cli/issues/88)

## [0.31.0](https://github.com/inference-gateway/cli/compare/v0.30.0...v0.31.0) (2025-08-20)

### 🚀 Features

* Implement Github Tool ([#86](https://github.com/inference-gateway/cli/issues/86)) ([b8dc12b](https://github.com/inference-gateway/cli/commit/b8dc12b23613f99cd416673ebd8d55100493381f)), closes [#85](https://github.com/inference-gateway/cli/issues/85)

## [0.30.0](https://github.com/inference-gateway/cli/compare/v0.29.6...v0.30.0) (2025-08-20)

### 🚀 Features

* Add optimization and compact command configurations ([977755f](https://github.com/inference-gateway/cli/commit/977755f0cef3f9e73eedc8d78df65c3f40032f44))

## [0.29.6](https://github.com/inference-gateway/cli/compare/v0.29.5...v0.29.6) (2025-08-20)

### 💄 Styles

* Add harmonica dependency and improve todo tool result formatting ([943a05b](https://github.com/inference-gateway/cli/commit/943a05b62ba90ff396f12225e6fdfc745b6a418c))

## [0.29.5](https://github.com/inference-gateway/cli/compare/v0.29.4...v0.29.5) (2025-08-20)

### 🧹 Maintenance

* Update configuration and improve chat session handling ([b598d14](https://github.com/inference-gateway/cli/commit/b598d147b972b1ef406d654669a38f71a75f4305))

## [0.29.4](https://github.com/inference-gateway/cli/compare/v0.29.3...v0.29.4) (2025-08-20)

### ♻️ Code Refactoring

* Improve / reconcile logging ([5596a1f](https://github.com/inference-gateway/cli/commit/5596a1fd326e55fbb8d0b364c890313dc612f3d5))

## [0.29.3](https://github.com/inference-gateway/cli/compare/v0.29.2...v0.29.3) (2025-08-20)

### 📚 Documentation

* Add new tools and update README documentation for improved functionality ([5f2f080](https://github.com/inference-gateway/cli/commit/5f2f0803293ea75fbfe2c2e5fa9909b2f2b95f20))

## [0.29.2](https://github.com/inference-gateway/cli/compare/v0.29.1...v0.29.2) (2025-08-20)

### ♻️ Code Refactoring

* Refactor tool parameter types from `map[string]interface{}` to `map[string]any` ([28647de](https://github.com/inference-gateway/cli/commit/28647de3e9a772fa14126245a228722ad26b5f5e))

## [0.29.1](https://github.com/inference-gateway/cli/compare/v0.29.0...v0.29.1) (2025-08-20)

### 🧹 Maintenance

* Update input placeholder to include command hint ([11a40d2](https://github.com/inference-gateway/cli/commit/11a40d2bcdd40c741af344ca3112e722235f02b0))

## [0.29.0](https://github.com/inference-gateway/cli/compare/v0.28.1...v0.29.0) (2025-08-20)

### 🚀 Features

* Implement centralized sandbox configuration ([#83](https://github.com/inference-gateway/cli/issues/83)) ([1324205](https://github.com/inference-gateway/cli/commit/1324205d072c7160a98f863ee117326929a92305)), closes [#82](https://github.com/inference-gateway/cli/issues/82)

## [0.28.1](https://github.com/inference-gateway/cli/compare/v0.28.0...v0.28.1) (2025-08-19)

### ♻️ Code Refactoring

* Split config init into separate init command ([#77](https://github.com/inference-gateway/cli/issues/77)) ([fa99c76](https://github.com/inference-gateway/cli/commit/fa99c76d4163ed4b9343389cb0070fd7015910b3)), closes [#75](https://github.com/inference-gateway/cli/issues/75) [#75](https://github.com/inference-gateway/cli/issues/75)

## [0.28.0](https://github.com/inference-gateway/cli/compare/v0.27.0...v0.28.0) (2025-08-19)

### 🚀 Features

* Add total input tokens to Usage Status ([#78](https://github.com/inference-gateway/cli/issues/78)) ([2df6874](https://github.com/inference-gateway/cli/commit/2df6874d8d35a80b6ab30c665d7aa52be0c92d08)), closes [#76](https://github.com/inference-gateway/cli/issues/76) [#76](https://github.com/inference-gateway/cli/issues/76)

## [0.27.0](https://github.com/inference-gateway/cli/compare/v0.26.0...v0.27.0) (2025-08-19)

### 🚀 Features

* Implement dynamic status indicators with improved visual feedback ([#68](https://github.com/inference-gateway/cli/issues/68)) ([265c781](https://github.com/inference-gateway/cli/commit/265c7815d90beecc66bd3904bae9d75549f3f851)), closes [#67](https://github.com/inference-gateway/cli/issues/67) [#67](https://github.com/inference-gateway/cli/issues/67)

## [0.26.0](https://github.com/inference-gateway/cli/compare/v0.25.1...v0.26.0) (2025-08-17)

### 🚀 Features

* **chat:** Add Ctrl+R support for expanding/collapsing all tool calls ([#74](https://github.com/inference-gateway/cli/issues/74)) ([7e21b92](https://github.com/inference-gateway/cli/commit/7e21b9251a9da91cbabd1497f237d22709fa7a3c)), closes [#71](https://github.com/inference-gateway/cli/issues/71) [#71](https://github.com/inference-gateway/cli/issues/71)

## [0.25.1](https://github.com/inference-gateway/cli/compare/v0.25.0...v0.25.1) (2025-08-17)

### 🧹 Maintenance

* **history:** Clean up comments ([99810e6](https://github.com/inference-gateway/cli/commit/99810e6a5591e83f229c04783190e1dd2aec7edf))

## [0.25.0](https://github.com/inference-gateway/cli/compare/v0.24.3...v0.25.0) (2025-08-17)

### 🚀 Features

* **chat:** Add shell history integration for chat commands ([#70](https://github.com/inference-gateway/cli/issues/70)) ([4e83b7d](https://github.com/inference-gateway/cli/commit/4e83b7d5dad70289fadf5ead0a98bc81c0de9fc7)), closes [#69](https://github.com/inference-gateway/cli/issues/69)

## [0.24.3](https://github.com/inference-gateway/cli/compare/v0.24.2...v0.24.3) (2025-08-17)

### 🧹 Maintenance

* Improve Claude Code quality check ([#72](https://github.com/inference-gateway/cli/issues/72)) ([e82a057](https://github.com/inference-gateway/cli/commit/e82a057d1542afa1a732acab45a97432bf871f0a))

## [0.24.2](https://github.com/inference-gateway/cli/compare/v0.24.1...v0.24.2) (2025-08-17)

### 🧹 Maintenance

* Rename fetch tool to web_fetch in config files ([c434e92](https://github.com/inference-gateway/cli/commit/c434e9202bd10f5481f3898111ee471bb7b15853))

## [0.24.1](https://github.com/inference-gateway/cli/compare/v0.24.0...v0.24.1) (2025-08-17)

### ♻️ Code Refactoring

* Rename Fetch Tool to WebFetch ([#65](https://github.com/inference-gateway/cli/issues/65)) ([394c5d2](https://github.com/inference-gateway/cli/commit/394c5d2a1128356fbb71467fe7bb2d07dabac0e4))

## [0.24.0](https://github.com/inference-gateway/cli/compare/v0.23.1...v0.24.0) (2025-08-17)

### 🚀 Features

* Enhance Edit tool with diff preview and update UI for colored diffs ([4ee6ba9](https://github.com/inference-gateway/cli/commit/4ee6ba9f02357cb46ad72c51a6784073fda9561f))

## [0.23.1](https://github.com/inference-gateway/cli/compare/v0.23.0...v0.23.1) (2025-08-17)

### 🧹 Maintenance

* Update system prompt and tool preferences in configuration files ([ce3d98f](https://github.com/inference-gateway/cli/commit/ce3d98fa1108f7326675d7e71283305a5cb16003))

## [0.23.0](https://github.com/inference-gateway/cli/compare/v0.22.2...v0.23.0) (2025-08-17)

### 🚀 Features

* Update system prompt for interactive CLI assistant with detailed guidelines ([5c05915](https://github.com/inference-gateway/cli/commit/5c05915d0baedfe58959448577ec61325c78c4c5))

## [0.22.2](https://github.com/inference-gateway/cli/compare/v0.22.1...v0.22.2) (2025-08-17)

### ♻️ Code Refactoring

* Refactor Read Tool for deterministic behavior ([#63](https://github.com/inference-gateway/cli/issues/63)) ([76017a1](https://github.com/inference-gateway/cli/commit/76017a1ba0763bed03095df2604b4efd2d66ce2a)), closes [#59](https://github.com/inference-gateway/cli/issues/59) [#59](https://github.com/inference-gateway/cli/issues/59)

## [0.22.1](https://github.com/inference-gateway/cli/compare/v0.22.0...v0.22.1) (2025-08-17)

### 🧹 Maintenance

* Add 'task' command to tool whitelist ([faf497d](https://github.com/inference-gateway/cli/commit/faf497d54a3b25e3bea0479a63d80ca6c6f894fc))

## [0.22.0](https://github.com/inference-gateway/cli/compare/v0.21.0...v0.22.0) (2025-08-17)

### 🚀 Features

* Add MultiEdit tool for batch file editing ([#62](https://github.com/inference-gateway/cli/issues/62)) ([6024652](https://github.com/inference-gateway/cli/commit/6024652a7646d9e2820c272fbe997b0e31229d80)), closes [#60](https://github.com/inference-gateway/cli/issues/60) [#60](https://github.com/inference-gateway/cli/issues/60)

## [0.21.0](https://github.com/inference-gateway/cli/compare/v0.20.0...v0.21.0) (2025-08-17)

### 🚀 Features

* Implement Edit tool with exact string replacement ([#61](https://github.com/inference-gateway/cli/issues/61)) ([bfe19e9](https://github.com/inference-gateway/cli/commit/bfe19e97e1db010f83fdc0adeb34ad59de5d6ec3)), closes [#54](https://github.com/inference-gateway/cli/issues/54) [#54](https://github.com/inference-gateway/cli/issues/54)

## [0.20.0](https://github.com/inference-gateway/cli/compare/v0.19.0...v0.20.0) (2025-08-17)

### 🚀 Features

* Implement TodoWrite LLM tool for structured task lists ([#58](https://github.com/inference-gateway/cli/issues/58)) ([2770589](https://github.com/inference-gateway/cli/commit/27705897299891dcc411b3d04a528f772a07ceac)), closes [#42](https://github.com/inference-gateway/cli/issues/42)

## [0.19.0](https://github.com/inference-gateway/cli/compare/v0.18.5...v0.19.0) (2025-08-16)

### 🚀 Features

* Add Grep tool with ripgrep-powered search functionality ([#57](https://github.com/inference-gateway/cli/issues/57)) ([114dfb0](https://github.com/inference-gateway/cli/commit/114dfb0abe38b84d8641d59366f41efaa53b2466)), closes [#56](https://github.com/inference-gateway/cli/issues/56) [#56](https://github.com/inference-gateway/cli/issues/56)

## [0.18.5](https://github.com/inference-gateway/cli/compare/v0.18.4...v0.18.5) (2025-08-16)

### ♻️ Code Refactoring

* Add appending mode to Write Tool for chunked file writing ([#55](https://github.com/inference-gateway/cli/issues/55)) ([64eb38e](https://github.com/inference-gateway/cli/commit/64eb38e15dfff5bfc67041725cbcd985b616494b)), closes [#53](https://github.com/inference-gateway/cli/issues/53)

## [0.18.4](https://github.com/inference-gateway/cli/compare/v0.18.3...v0.18.4) (2025-08-16)

### ♻️ Code Refactoring

* Update keyboard shortcuts for sending messages to use 'Enter' key ([0c8220d](https://github.com/inference-gateway/cli/commit/0c8220df0d460c53abf74ebd3008ee607e4bbcf8))

### ✅ Tests

* Update default gateway timeout in TestDefaultConfig to 200 ([7b77f8f](https://github.com/inference-gateway/cli/commit/7b77f8f8cf5d04c857fc6ed0ac7c85255bb191c7))

## [0.18.3](https://github.com/inference-gateway/cli/compare/v0.18.2...v0.18.3) (2025-08-16)

### 🧹 Maintenance

* Increase timeout value in gateway configuration to 200 ([7765fb0](https://github.com/inference-gateway/cli/commit/7765fb001c67f29c300ab8276559ad51efb1ae44))

## [0.18.2](https://github.com/inference-gateway/cli/compare/v0.18.1...v0.18.2) (2025-08-16)

### ♻️ Code Refactoring

* FetchTool with HTTP client and warning for content size ([296701a](https://github.com/inference-gateway/cli/commit/296701a0bdc0ac98b2b37a5010a38c9b2f7b7cc8))

### 🧹 Maintenance

* Add 'gh' command to tool whitelist in config ([2849e97](https://github.com/inference-gateway/cli/commit/2849e97a09edc1e00aff35a0879f8dcbe3d34cf9))

## [0.18.1](https://github.com/inference-gateway/cli/compare/v0.18.0...v0.18.1) (2025-08-16)

### ♻️ Code Refactoring

* Extract components.go file to multiple components files ([#51](https://github.com/inference-gateway/cli/issues/51)) ([67f9167](https://github.com/inference-gateway/cli/commit/67f91678ceff4482b885b1b4d71b317bd8bc8f8e)), closes [#49](https://github.com/inference-gateway/cli/issues/49)

## [0.18.0](https://github.com/inference-gateway/cli/compare/v0.17.1...v0.18.0) (2025-08-15)

### 🚀 Features

* Implement Delete Tool with improved security features ([#48](https://github.com/inference-gateway/cli/issues/48)) ([8b5fade](https://github.com/inference-gateway/cli/commit/8b5fadee84012f95d278dd1cbda45c585ff8a2ea)), closes [#47](https://github.com/inference-gateway/cli/issues/47) [#47](https://github.com/inference-gateway/cli/issues/47)

## [0.17.1](https://github.com/inference-gateway/cli/compare/v0.17.0...v0.17.1) (2025-08-15)

### 💄 Styles

* Clean up whitespace and formatting across multiple files ([74ab3c5](https://github.com/inference-gateway/cli/commit/74ab3c5202593ed3c0e5c0ed54feff0192a0d201))

## [0.17.0](https://github.com/inference-gateway/cli/compare/v0.16.2...v0.17.0) (2025-08-15)

### 🚀 Features

* Implement Write Tool for filesystem operations ([#46](https://github.com/inference-gateway/cli/issues/46)) ([ccf3091](https://github.com/inference-gateway/cli/commit/ccf3091b64edcaaa2ac019e2692e9b4d58a67950)), closes [#45](https://github.com/inference-gateway/cli/issues/45)

## [0.16.2](https://github.com/inference-gateway/cli/compare/v0.16.1...v0.16.2) (2025-08-15)

### 📚 Documentation

* Update default model configuration and enhance tool management in CLI examples ([8558a98](https://github.com/inference-gateway/cli/commit/8558a9864c48a9f5b9dff2063d0d1859dc670f91))

## [0.16.1](https://github.com/inference-gateway/cli/compare/v0.16.0...v0.16.1) (2025-08-15)

### 📚 Documentation

* Correct alignment of the Inference Gateway CLI header in README ([dd4f05c](https://github.com/inference-gateway/cli/commit/dd4f05c8762324c56707eb2af3ba0c3608bc4106))

## [0.16.0](https://github.com/inference-gateway/cli/compare/v0.15.0...v0.16.0) (2025-08-15)

### 🚀 Features

* Implement Tree tool for directory structure visualization ([#43](https://github.com/inference-gateway/cli/issues/43)) ([69fa9c5](https://github.com/inference-gateway/cli/commit/69fa9c58f7cbb832f20fcb5ab5ee7e05613c48f2)), closes [#39](https://github.com/inference-gateway/cli/issues/39) [#39](https://github.com/inference-gateway/cli/issues/39)

## [0.15.0](https://github.com/inference-gateway/cli/compare/v0.14.3...v0.15.0) (2025-08-15)

### 🚀 Features

* Add scrollable chat history with mouse wheel and keyboard support ([#41](https://github.com/inference-gateway/cli/issues/41)) ([0b09dcb](https://github.com/inference-gateway/cli/commit/0b09dcb8f645ccb790abe4c719b316f2ae9d6282)), closes [#34](https://github.com/inference-gateway/cli/issues/34) [#34](https://github.com/inference-gateway/cli/issues/34)

## [0.14.3](https://github.com/inference-gateway/cli/compare/v0.14.2...v0.14.3) (2025-08-14)

### ♻️ Code Refactoring

* Add sequential processing for tool calls in chat handler ([24850f4](https://github.com/inference-gateway/cli/commit/24850f4c68df085a362c969c7434ccbe0217eaa9))

## [0.14.2](https://github.com/inference-gateway/cli/compare/v0.14.1...v0.14.2) (2025-08-14)

### 📚 Documentation

* Correct alignment of the Inference Gateway CLI header in README.md ([ca3665a](https://github.com/inference-gateway/cli/commit/ca3665ae234830b5a1279c277faa5d3f0051d797))

## [0.14.1](https://github.com/inference-gateway/cli/compare/v0.14.0...v0.14.1) (2025-08-14)

### ♻️ Code Refactoring

* Refactor tool.go into modular tools package ([#40](https://github.com/inference-gateway/cli/issues/40)) ([4f3aaaa](https://github.com/inference-gateway/cli/commit/4f3aaaa02789536c85d85f44747c8b51fe67a2a3)), closes [#38](https://github.com/inference-gateway/cli/issues/38)

## [0.14.0](https://github.com/inference-gateway/cli/compare/v0.13.1...v0.14.0) (2025-08-14)

### 🚀 Features

* Implement FileSearch tool for filesystem file searching ([#36](https://github.com/inference-gateway/cli/issues/36)) ([8990815](https://github.com/inference-gateway/cli/commit/8990815cea8c66a758c0fa614d0dc4ef1f7e24f6)), closes [#13](https://github.com/inference-gateway/cli/issues/13)

## [0.13.1](https://github.com/inference-gateway/cli/compare/v0.13.0...v0.13.1) (2025-08-14)

### ♻️ Code Refactoring

* Make use of Tools using the OpenAI spec properly ([#35](https://github.com/inference-gateway/cli/issues/35)) ([0278421](https://github.com/inference-gateway/cli/commit/02784214e45f690fd532b92931559b55db9b7b6d))

## [0.13.0](https://github.com/inference-gateway/cli/compare/v0.12.2...v0.13.0) (2025-08-14)

### 🚀 Features

* Implement Web Search Tool ([#32](https://github.com/inference-gateway/cli/issues/32)) ([e80769b](https://github.com/inference-gateway/cli/commit/e80769bd2794b2e6979e885a3fc567bf8421061a)), closes [#31](https://github.com/inference-gateway/cli/issues/31)

## [0.12.2](https://github.com/inference-gateway/cli/compare/v0.12.1...v0.12.2) (2025-08-13)

### 📚 Documentation

* **fix:** Simplify installation command formatting in README ([5a76c73](https://github.com/inference-gateway/cli/commit/5a76c731937c61d7da939bc2cdb278faa186bbac))
* Update configuration formatting and add fetch options ([0eebc3a](https://github.com/inference-gateway/cli/commit/0eebc3a69cd435cdb4db27cedebf6daedc395e3d))

## [0.12.1](https://github.com/inference-gateway/cli/compare/v0.12.0...v0.12.1) (2025-08-13)

### 📚 Documentation

* Add section for verifying release binaries with Cosign ([4822268](https://github.com/inference-gateway/cli/commit/48222687a0f05713ab917fbe376f7c263c2eb62a))

## [0.12.0](https://github.com/inference-gateway/cli/compare/v0.11.1...v0.12.0) (2025-08-13)

### 🚀 Features

* Add Cosign integration for signing checksums in release process ([7230948](https://github.com/inference-gateway/cli/commit/7230948ce4e7f5a6f43a07311d4536e3ec28e58f))

## [0.11.1](https://github.com/inference-gateway/cli/compare/v0.11.0...v0.11.1) (2025-08-13)

### 🐛 Bug Fixes

* Include checksums.txt in GitHub releases ([cc7ffc0](https://github.com/inference-gateway/cli/commit/cc7ffc0607903d62087459b37a35e315bffd4aa2))

## [0.11.0](https://github.com/inference-gateway/cli/compare/v0.10.3...v0.11.0) (2025-08-13)

### 🚀 Features

* Implement Fetch tool with GitHub integration ([#28](https://github.com/inference-gateway/cli/issues/28)) ([28b07dc](https://github.com/inference-gateway/cli/commit/28b07dce9242ab97e6f24063cf95066c927e6948)), closes [#23](https://github.com/inference-gateway/cli/issues/23) [owner/repo#123](https://github.com/owner/repo/issues/123)

## [0.10.3](https://github.com/inference-gateway/cli/compare/v0.10.2...v0.10.3) (2025-08-13)

### 🧹 Maintenance

* Create dependabot.yml ([d64f569](https://github.com/inference-gateway/cli/commit/d64f569e40474957056bc1177db8df56cf733753))

## [0.10.2](https://github.com/inference-gateway/cli/compare/v0.10.1...v0.10.2) (2025-08-13)

### 📚 Documentation

* Improve docs and add markdownlint support to ensure consistent docs ([1066cc8](https://github.com/inference-gateway/cli/commit/1066cc8c6af521ba4bb8f29f41a01e5a73329ab1))

## [0.10.1](https://github.com/inference-gateway/cli/compare/v0.10.0...v0.10.1) (2025-08-13)

### 🐛 Bug Fixes

* Add copy/paste functionality to chat input field ([#30](https://github.com/inference-gateway/cli/issues/30)) ([4c02389](https://github.com/inference-gateway/cli/commit/4c02389fa46a6730f8a660f151950f45ce51b0de)), closes [#29](https://github.com/inference-gateway/cli/issues/29)

## [0.10.0](https://github.com/inference-gateway/cli/compare/v0.9.0...v0.10.0) (2025-08-12)

### 🚀 Features

* Add system prompt configuration support ([#26](https://github.com/inference-gateway/cli/issues/26)) ([bf4a5ff](https://github.com/inference-gateway/cli/commit/bf4a5ffdd472637feada17a7b8ea835d7c90178b)), closes [#25](https://github.com/inference-gateway/cli/issues/25) [#25](https://github.com/inference-gateway/cli/issues/25)

## [0.9.0](https://github.com/inference-gateway/cli/compare/v0.8.1...v0.9.0) (2025-08-12)

### 🚀 Features

* **security:** Add excluded paths configuration for improved security ([#24](https://github.com/inference-gateway/cli/issues/24)) ([22c4d72](https://github.com/inference-gateway/cli/commit/22c4d7251cc2d35b84c0cca3482ef491326a6ec3)), closes [#21](https://github.com/inference-gateway/cli/issues/21)

## [0.8.1](https://github.com/inference-gateway/cli/compare/v0.8.0...v0.8.1) (2025-08-12)

### ♻️ Code Refactoring

* Restructure CLI command organization and enable tools by default ([#20](https://github.com/inference-gateway/cli/issues/20)) ([5c56424](https://github.com/inference-gateway/cli/commit/5c564249eac90a32cf1bdcc8e4c452b8c2fbe194)), closes [#19](https://github.com/inference-gateway/cli/issues/19)

## [0.8.0](https://github.com/inference-gateway/cli/compare/v0.7.0...v0.8.0) (2025-08-12)

### 🚀 Features

* Add default model configuration for chat sessions ([#18](https://github.com/inference-gateway/cli/issues/18)) ([4c361d9](https://github.com/inference-gateway/cli/commit/4c361d901cd68bd4423978339dc9dce2b493bfd9)), closes [#17](https://github.com/inference-gateway/cli/issues/17)

## [0.7.0](https://github.com/inference-gateway/cli/compare/v0.6.0...v0.7.0) (2025-08-12)

### 🚀 Features

* Implement Read tool for file system access ([#16](https://github.com/inference-gateway/cli/issues/16)) ([45e8d0a](https://github.com/inference-gateway/cli/commit/45e8d0a1ad0fcc0e44ddc2e623218d3c1825ce4b)), closes [#14](https://github.com/inference-gateway/cli/issues/14) [#14](https://github.com/inference-gateway/cli/issues/14)

## [0.6.0](https://github.com/inference-gateway/cli/compare/v0.5.4...v0.6.0) (2025-08-12)

### 🚀 Features

* Implement structured logging and update config paths in CLI commands ([125e4aa](https://github.com/inference-gateway/cli/commit/125e4aaeb73554042be6c46cf3ae5013da989c0d))

## [0.5.4](https://github.com/inference-gateway/cli/compare/v0.5.3...v0.5.4) (2025-08-12)

### ♻️ Code Refactoring

* Replace b.WriteString with fmt.Fprintf for improved string formatting in file selection ([7315289](https://github.com/inference-gateway/cli/commit/7315289d89ef59f61110412fd6a8b83593cd8011))

## [0.5.3](https://github.com/inference-gateway/cli/compare/v0.5.2...v0.5.3) (2025-08-12)

### 📚 Documentation

* Update Go Report Card badge for consistency in style ([e615d2c](https://github.com/inference-gateway/cli/commit/e615d2c8f0edc070200a49eda196fe822ef06d1e))

## [0.5.2](https://github.com/inference-gateway/cli/compare/v0.5.1...v0.5.2) (2025-08-12)

### ♻️ Code Refactoring

* Simplify file selection rendering and improve search functionality ([9cd3d6b](https://github.com/inference-gateway/cli/commit/9cd3d6b1e01f539ac1944847a8427fda6e2f2e04))

## [0.5.1](https://github.com/inference-gateway/cli/compare/v0.5.0...v0.5.1) (2025-08-12)

### ♻️ Code Refactoring

* **lint:** Add GolangCI configuration and improve code structure ([0db0283](https://github.com/inference-gateway/cli/commit/0db0283e45d1fbc901d9fc2538af59b9f6e8d639))

## [0.5.0](https://github.com/inference-gateway/cli/compare/v0.4.0...v0.5.0) (2025-08-12)

### 🚀 Features

* Add model visibility in chat UI and enhanced export format ([#15](https://github.com/inference-gateway/cli/issues/15)) ([cc98503](https://github.com/inference-gateway/cli/commit/cc98503b16a3448dacf4b2098dde580920999630)), closes [#12](https://github.com/inference-gateway/cli/issues/12) [#12](https://github.com/inference-gateway/cli/issues/12)

## [0.4.0](https://github.com/inference-gateway/cli/compare/v0.3.3...v0.4.0) (2025-08-11)

### 🚀 Features

* Add interactive file selection dropdown with @ symbol ([#10](https://github.com/inference-gateway/cli/issues/10)) ([8cabd7d](https://github.com/inference-gateway/cli/commit/8cabd7dbf6bba8bc6dc4c2c5101ddd718851fe96)), closes [#3](https://github.com/inference-gateway/cli/issues/3)

## [0.3.3](https://github.com/inference-gateway/cli/compare/v0.3.2...v0.3.3) (2025-08-11)

### ♻️ Code Refactoring

* Improve The TUI Using Bubble Tea ([#11](https://github.com/inference-gateway/cli/issues/11)) ([b318d1c](https://github.com/inference-gateway/cli/commit/b318d1c9d69abcef6cc838adadfa93bd58dbdfc6))

## [0.3.2](https://github.com/inference-gateway/cli/compare/v0.3.1...v0.3.2) (2025-08-10)

### 👷 CI/CD

* Add CI workflow for automated testing and linting ([#9](https://github.com/inference-gateway/cli/issues/9)) ([00d519a](https://github.com/inference-gateway/cli/commit/00d519a1639212dbdf904f75fed7819e488a6f18))

## [0.3.1](https://github.com/inference-gateway/cli/compare/v0.3.0...v0.3.1) (2025-08-10)

### ♻️ Code Refactoring

* Move configuration to .infer/config.yaml directory structure ([#8](https://github.com/inference-gateway/cli/issues/8)) ([430670a](https://github.com/inference-gateway/cli/commit/430670a1900d27eab89fe81f22ee175ff39b8e0e)), closes [#7](https://github.com/inference-gateway/cli/issues/7)

## [0.3.0](https://github.com/inference-gateway/cli/compare/v0.2.0...v0.3.0) (2025-08-10)

### 🚀 Features

* Add safety approval before executing any command ([#6](https://github.com/inference-gateway/cli/issues/6)) ([ba5fd7e](https://github.com/inference-gateway/cli/commit/ba5fd7e7a1e4e2217525b445bc8d36b570414c70)), closes [#5](https://github.com/inference-gateway/cli/issues/5) [#5](https://github.com/inference-gateway/cli/issues/5)

## [0.2.0](https://github.com/inference-gateway/cli/compare/v0.1.15...v0.2.0) (2025-08-10)

### 🚀 Features

* Add /compact command for exporting chat conversations ([#4](https://github.com/inference-gateway/cli/issues/4)) ([f5380e8](https://github.com/inference-gateway/cli/commit/f5380e8cfc70c4661f44354ec1f1c8a4983a4cce)), closes [#2](https://github.com/inference-gateway/cli/issues/2)

## [0.1.15](https://github.com/inference-gateway/cli/compare/v0.1.14...v0.1.15) (2025-08-10)

### ♻️ Code Refactoring

* Improve chat session handling with streaming support and tool call management ([76dce0e](https://github.com/inference-gateway/cli/commit/76dce0e086207cc5449ea1da492e4b5bb33abb5a))

## [0.1.14](https://github.com/inference-gateway/cli/compare/v0.1.13...v0.1.14) (2025-08-10)

### 📚 Documentation

* Disable discussion category name in GitHub release configuration ([d2b98c6](https://github.com/inference-gateway/cli/commit/d2b98c664c18d032b022623837e16ee4a5540963))

## [0.1.13](https://github.com/inference-gateway/cli/compare/v0.1.12...v0.1.13) (2025-08-10)

### 🐛 Bug Fixes

* Update status command to fetch live model count from API ([ff3c2a2](https://github.com/inference-gateway/cli/commit/ff3c2a2b518a4f6de67974dd774c924f27c7aa30))

## [0.1.12](https://github.com/inference-gateway/cli/compare/v0.1.11...v0.1.12) (2025-08-10)

### 🐛 Bug Fixes

* **docs:** Update CLI command references for model listing ([1e57285](https://github.com/inference-gateway/cli/commit/1e57285e3e26aa9be6deed44287dd408d5480837))

### 📚 Documentation

* Update example commands to specify model for prompt input ([d38d17b](https://github.com/inference-gateway/cli/commit/d38d17b598cdafc84db49e50ae4bc122edd1d0d2))

## [0.1.11](https://github.com/inference-gateway/cli/compare/v0.1.10...v0.1.11) (2025-08-10)

### ♻️ Code Refactoring

* Remove model deployment commands from documentation and update CLI commands ([1ef0d7e](https://github.com/inference-gateway/cli/commit/1ef0d7eebfdcf9128bedcad87d8d733bfed44341))

## [0.1.10](https://github.com/inference-gateway/cli/compare/v0.1.9...v0.1.10) (2025-08-10)

### 🐛 Bug Fixes

* Update installation paths to include local bin directory for CLI tools ([870ac22](https://github.com/inference-gateway/cli/commit/870ac22869d664bd61e7515d6011391c45863c41))

## [0.1.9](https://github.com/inference-gateway/cli/compare/v0.1.8...v0.1.9) (2025-08-10)

### 🐛 Bug Fixes

* Ensure latest version is fetched before installation in install_cli function ([f0239e2](https://github.com/inference-gateway/cli/commit/f0239e28f1aae24de3a3664a6ac35c5882fe3b82))

## [0.1.8](https://github.com/inference-gateway/cli/compare/v0.1.7...v0.1.8) (2025-08-10)

### 🐛 Bug Fixes

* Ensure end-of-file-fixer applies to all files except manifest.lock ([c5f77fe](https://github.com/inference-gateway/cli/commit/c5f77fe00089b5cb33812452443eb1d657c4052b))

## [0.1.7](https://github.com/inference-gateway/cli/compare/v0.1.6...v0.1.7) (2025-08-10)

### ♻️ Code Refactoring

* Update installation scripts and README for improved CLI usage and environment setup ([0cbca8b](https://github.com/inference-gateway/cli/commit/0cbca8b291f7540e1604c6970aa799baef2e333b))

## [0.1.6](https://github.com/inference-gateway/cli/compare/v0.1.5...v0.1.6) (2025-08-10)

### ♻️ Code Refactoring

* Remove default models when the inference gateway is not up ([7a7677b](https://github.com/inference-gateway/cli/commit/7a7677bf2cb0bb7c18d390b6b3e4f49e968ee105))

## [0.1.5](https://github.com/inference-gateway/cli/compare/v0.1.4...v0.1.5) (2025-08-10)

### 🔧 Build System

* Add checksum generation for built binaries ([77eddf6](https://github.com/inference-gateway/cli/commit/77eddf6d09d108c13e78621e1a353c92dd8ddd9c))

## [0.1.4](https://github.com/inference-gateway/cli/compare/v0.1.3...v0.1.4) (2025-08-10)

### 🐛 Bug Fixes

* Pass correct version to build during semantic-release ([4a24542](https://github.com/inference-gateway/cli/commit/4a24542a9750617dbc23a1fabf26e88c60c6d551))

## [0.1.3](https://github.com/inference-gateway/cli/compare/v0.1.2...v0.1.3) (2025-08-10)

### 🐛 Bug Fixes

* Add dynamic version information to CLI build ([431a842](https://github.com/inference-gateway/cli/commit/431a84243e0b5cc1105040f0bdee9c01ba90be5b))

### 📚 Documentation

* Update installation command to specify version 0.1.1 ([efa3629](https://github.com/inference-gateway/cli/commit/efa3629d92f300cdfe5108017380f4a72dcd900c))

## [0.1.2](https://github.com/inference-gateway/cli/compare/v0.1.1...v0.1.2) (2025-08-10)

### 📚 Documentation

* Format warning section in README for better readability ([99baf6c](https://github.com/inference-gateway/cli/commit/99baf6c84af58d81e0334952e9a0ecc8525c916a))

## [0.1.1](https://github.com/inference-gateway/cli/compare/v0.1.0...v0.1.1) (2025-08-10)

### 👷 CI/CD

* Add Claude Code GitHub Workflow ([#1](https://github.com/inference-gateway/cli/issues/1)) ([cde6c53](https://github.com/inference-gateway/cli/commit/cde6c53372de892f41ae5495a8122ee8a105100b))
