# Cove

Cove is a native macOS chat client. It draws its interface with [MyGo](https://mygo.egoist.dev/docs)'s `ui` package, not a web view, and it talks to OpenAI-compatible Chat Completions endpoints (`POST {baseURL}/chat/completions`, streaming). Replies render as Markdown as they arrive. Chats stay on this Mac.

The current release is 0.7.0.

## Features

- Presets for OpenAI, Anthropic, DeepSeek, OpenRouter, xAI, and Ollama, plus a custom endpoint. A new chat inherits the default model, base URL, and reasoning settings.
- Streaming Markdown, with a copy button on code blocks. Reasoning text, when the model sends it, is shown above the answer and is not sent back on later requests.
- Chats are stored as JSONL under Application Support. API keys are stored in the macOS login keychain, not in the chat files.
- Find in the open chat, quotes taken from a selection, and per-chat settings for the model, temperature, max tokens, reasoning, system prompt, and extra request JSON.
- Six themes (Warm, Tide, Graphite, Mist, Sage, Lavender), each in light and dark. Appearance can follow the system or be pinned to light or dark.

## Requirements

The app is built for macOS on Apple Silicon (`darwin/arm64`). It is not a universal binary and does not run on an Intel Mac.

Building from source needs Go 1.27 or newer. MyGo does not use cgo, so a Linux machine can cross-compile the Mac app.

## Install

Homebrew is the recommended install. The cask is `cove` in [foyoodo/tap](https://github.com/foyoodo/homebrew-tap). It is Apple Silicon (`arm64`) only and needs macOS 12 or newer.

```sh
brew tap foyoodo/tap && brew install --cask cove
```

Or install the cask in one command:

```sh
brew install --cask foyoodo/tap/cove
```

Upgrade with `brew upgrade --cask cove`. Uninstall with `brew uninstall --cask cove`. Add `--zap` to also remove app data (chats, caches, logs, and preferences). API keys live in the login keychain under the service `dev.cove.chat` and are not removed by `--zap`.

The app is unsigned and not notarized. The cask clears the quarantine attribute, so a Homebrew install opens directly. Only a zip install needs the Gatekeeper step below.

The tap's cask auto-updates hourly from Cove's GitHub Releases and tracks stable releases. Until the first stable release exists, it points at the newest prerelease.

### Download a zip

Release builds are an unsigned `Cove.app` inside a zip.

1. Unzip the archive.
2. Drag `Cove.app` into the Applications folder, or leave it in any directory.
3. Apps downloaded from a browser or a chat carry a quarantine flag, and Gatekeeper will refuse to open them. Clear it in Terminal, then open the app:

```sh
xattr -dr com.apple.quarantine /Applications/Cove.app
```

Use the path where you actually put the `.app`. You can also Control-click the icon in Finder, choose Open, and confirm. macOS will still say the developer cannot be verified. That is expected for an unsigned app.

A disk image can only be produced on a Mac. A cross-compile produces the `.app`.

## Configure a model

The first launch has no chats. Open Settings from the sidebar gear or from **File → Settings…** (⌘,) and choose a provider. The preset fills the usual base URL and model id. Custom keeps the URL you type.

| Provider | Base URL | Default model |
| --- | --- | --- |
| OpenAI | `https://api.openai.com/v1` | `gpt-4o-mini` |
| Anthropic | `https://api.anthropic.com/v1` | `claude-sonnet-4.5` |
| DeepSeek | `https://api.deepseek.com/v1` | `deepseek-flash` |
| OpenRouter | `https://openrouter.ai/api/v1` | `openai/gpt-4o-mini` |
| xAI | `https://api.x.ai/v1` | `grok-4` |
| Ollama | `http://localhost:11434/v1` | `llama3.2` |

The API key is sent as `Authorization: Bearer …`. Hosts on `openai.azure.com` use the `api-key` header instead. Ollama on localhost can be used without a key. Temperature and max tokens are omitted until you set them. A system prompt is sent as one `system` message and is not shown in the transcript.

Settings for the whole app are a separate window. **More → Chat Settings…** (⌥⌘,) changes only the open chat.

## Build and test

```sh
CGO_ENABLED=0 go tool mygo build -platform darwin/arm64
go test ./...
go vet ./...
```

The `.app` is written to `build/darwin-arm64/`. The build copies `icon/compiled/Assets.car` and `resources/Cove.icns` into the bundle. Those icon files are already compiled; do not regenerate them. `mygo.json` sets the bundle name, the version, and the Info.plist icon keys.

`go tool mygo dev` runs the app from source on a Mac.

## Releases and CI

Pushes to `main` and pull requests run on Ubuntu: `go vet ./...`, `go test ./...`, and `CGO_ENABLED=0 go tool mygo build -platform darwin/arm64`. MyGo needs no cgo, so the Apple Silicon app cross-compiles on Linux. The workflow uploads `Cove-darwin-arm64.zip` and keeps it for 5 days. Unzipping that archive produces `Cove.app`, with the bundle layout and the executable bit intact.

A release tag has to match `mygo.json`. The release workflow fails when the tag, without the leading `v`, is not the `version` field. `Cove.app` keeps the version from `mygo.json`.

1. Set `version` in `mygo.json` and commit.
2. Tag that commit and push the tag:

```sh
git tag vX.Y.Z && git push origin vX.Y.Z
```

The tag push runs the tests, builds `Cove.app` for `darwin/arm64` on Ubuntu, and publishes `Cove-X.Y.Z-darwin-arm64.zip` and `Cove-X.Y.Z-darwin-arm64.zip.sha256` on a GitHub Release. The notes are the generated changelog plus the install steps: unzip, move `Cove.app` to `/Applications`, and run `xattr -dr com.apple.quarantine /Applications/Cove.app` (or Control-click and choose Open). The build is unsigned, not notarized, and Apple Silicon only.

A hyphenated version such as `0.8.0-rc.1`, tagged `v0.8.0-rc.1`, is published as a prerelease. To build an existing tag by hand, open Actions, choose Release, run the workflow, and enter the tag, including the leading `v`.

## Project layout

```text
.github/workflows/   CI on pull requests, and the tag release
*.go                 the app: chat, storage, providers, and the window
themes/              palettes; tokens.json is the color and layout source
icon/                compiled Assets.car and the 1024px source PNG
resources/Cove.icns  the icon used on macOS 12–15
tools/inkgap/        glyph-ink measurement used by the layout
testdata/            images the layout tests open
notices/             generator for third_party_notices.txt
DESIGN.md            short visual overview
mygo.json            bundle name, version, and icon
```

`themes/tokens.json` is embedded in the binary. Components take colors from it rather than from literals in the UI code.

## third_party/mygo

MyGo is vendored at `third_party/mygo`. `go.mod` replaces `github.com/egoist/mygo` with that directory (v0.5.5). Cove's windows, text, and controls are MyGo widgets. The vendored tree includes MyGo's own license, locale catalogs, and command-line tool. Change Cove in the packages at the repository root. Change `third_party/mygo` only when you mean to move the pin.
