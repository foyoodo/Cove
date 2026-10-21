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

## Project layout

```text
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
