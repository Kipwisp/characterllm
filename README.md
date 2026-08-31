# CharacterLLM - AI Chatbot for Discord

CharacterLLM is a Discord bot that can create fun and interesting chatbot personalities based on any prompt you give it. Give it a character's name and it researches them on the web and builds a detailed persona of who they are, how they look, and how they speak, then roleplays as them in their own voice. You can also request modifications to the character (an alternate-universe twist, a personality tweak) or drop them into a scenario that shapes how the conversation starts.

Conversations persist per character and per thread: the bot remembers what has happened, who you've clashed with, and the running story, even across days of chatting. A few things you can do with it:

- `/createcharacter <description>`: summon a character from any show, game, or book with just their name (plus optional modifiers like "barista" or a scenario like "is caught out in a rainstorm").
- **Chat in any channel or thread**: mention the bot or reply to it and it answers in character.
- `/newthread` / `/setthread`: keep several separate conversations going with the same character and jump between them; each one has its own memory.
- `/editcharacter`: reshape the persona on the fly ("make him a little more patient") with a preview you can accept or reject before it's saved.
- **Let them talk back**: add a text channel with `/addambientchannel` and the character will occasionally strike up a conversation on its own inside that channel.

It can also run entirely on your own hardware: the bot uses the OpenAI-compatible chat completions API, so any local model server works out of the box. For example, start llama.cpp's server with a GGUF model (`./llama-server -m model.gguf --port 8080`) and point `LLM_URL` at `http://localhost:8080/v1/chat/completions`; vLLM, LM Studio, and Ollama work the same way.

## Prerequisites

1. **Go 1.25+**
2. **A Discord application**, you'll need the **Bot Token** (`DISCORD_TOKEN`) and the **Client ID** (`CLIENT_ID`).
3. **An LLM provider**, which provides the large language model used to power the bot.
4. **A search provider**, used for character research and image search.

## Supported Backends

### LLM APIs

Only **OpenAI-compatible chat completions** endpoints like llama.cpp (`POST /v1/chat/completions`) are supported, configured with `LLM_URL` and `LLM_MODEL`.

### Search Providers

Only **SearXNG** is supported (`SEARCH_PROVIDER=searxng`) currently, configured with `SEARXNG_URL` (plus optional `SEARXNG_ENGINES` to restrict which upstream engines it queries). It serves both character research and the avatar image search.

## Setup

### 1. Clone and configure

```sh
git clone https://github.com/Kipwisp/characterllm.git
cd characterllm
```

Create a `.env` in the project root.

```dotenv
# Minimum working configuration
DISCORD_TOKEN=<your bot token>
CLIENT_ID=<your application client ID>
LLM_URL=http://127.0.0.1:12434/v1/chat/completions
LLM_MODEL=gemma4
SEARXNG_URL=http://localhost:8080
```

### 2. Build

```sh
go build -o bin/bot ./cmd/bot
```

### 3. Run

```sh
./bin/bot
```

### Running with Docker

```sh
git clone https://github.com/Kipwisp/characterllm.git
cd characterllm

# create the .env, then:
docker compose up -d --build
```

## Config Options

### Discord

| Variable | Default | Description |
| --- | --- | --- |
| `DISCORD_TOKEN` | *(required)* | The bot token from the Discord Developer Portal. |
| `CLIENT_ID` | *(required)* | The application's client ID. |
| `COMMANDS_ADMIN_ONLY` | `false` | When true, every slash command requires the Administrator role. |
| `INVITE_COMMAND_ENABLED` | `true` | Registers the `/invite` command; false unregisters it. |

### LLM

| Variable | Default | Description |
| --- | --- | --- |
| `LLM_URL` | `http://localhost:8080/v1/chat/completions` | OpenAI-compatible chat completions endpoint. |
| `LLM_API_KEY` | *(empty)* | When set, sent as a `Bearer` Authorization header on every LLM request. |
| `LLM_MODEL` | *(empty)* | Model name to send to the LLM (it should match a model name the LLM endpoint defines). |
| `LLM_MAX_CONTEXT` | `10000` | Model context window in tokens. |
| `LLM_COMPACTION_THRESHOLD` | `0.9` | Compaction soft target in [0, 1]: compaction runs when the full prompt exceeds `LLM_MAX_CONTEXT × threshold`. |
| `LLM_RECENT_MEMORY_WINDOW` | `15` | Number of most recent turns kept verbatim; older turns are folded into the rolling summary during compaction. |
| `LLM_SUMMARY_MAX_TOKENS` | `2048` | Maximum length (tokens) of the rolling summary. |
| `LLM_TIMEOUT_SECONDS` | `120` | Per-request timeout for every LLM HTTP call. |
| `LLM_MAX_RETRIES` | `2` | Additional attempts for retryable LLM failures. |
| `LLM_VISION` | `false` | When true, image attachments on user messages are forwarded to the model. Leave false for models that lack vision support. |
| `LLM_MAX_IMAGES` | `2` | Max image attachments per message forwarded to the model. |
| `LLM_AVATAR_PICK` | `false` | When true (and `LLM_VISION` is true), the LLM picks the character's avatar during `/createcharacter` instead of the user choosing from the select menu. |

### Search

| Variable | Default | Description |
| --- | --- | --- |
| `SEARCH_PROVIDER` | `searxng` | Search backend. Currently only `searxng` is supported. |
| `SEARXNG_URL` | `http://localhost:8080` | Base URL of the SearXNG instance. |
| `SEARXNG_ENGINES` | *(empty)* | Comma-separated SearXNG engine names (e.g. `google,bing,duckduckgo`); empty uses SearXNG's own defaults. |
| `MAX_SEARCH_RESULTS` | `5` | Number of search results per query. |
| `RESEARCH_MAX_SOURCE_CHARS` | `0` (unset) | Character cap for the scraped source page included in persona synthesis. |

### Images

| Variable | Default | Description |
| --- | --- | --- |
| `MAX_IMAGE_SEARCH_RESULTS` | `10` | Number of image avatar candidates searched per character. |
| `IMAGE_MAX_EDGE` | `512` | Long-edge cap (px) for LLM processed images (avatars, vision attachments). |
| `IMAGE_CACHE_DIR` | `images/cache` | Directory for the character avatar cache. |

### Prompts

| Variable | Default | Description |
| --- | --- | --- |
| `SYSTEM_PROMPT_PATH` | `prompts/system_prompt.md` | Chat system prompt template. |
| `COMPACTION_PROMPT_PATH` | `prompts/compaction_prompt.md` | Rolling-summary compaction prompt. |
| `SYNTHESIS_PROMPT_PATH` | `prompts/synthesis_prompt.md` | Persona synthesis prompt (also the source of the persona section format rules). |
| `ANALYZER_PROMPT_PATH` | `prompts/analyzer_prompt.md` | Input analysis prompt (name/series extraction). |
| `EDIT_SECTION_PROMPT_PATH` | `prompts/edit_section_prompt.md` | `/editcharacter` rewrite prompt. |
| `SOURCE_SELECT_PROMPT_PATH` | `prompts/source_select_prompt.md` | Prompt that picks the best search result to scrape during `/createcharacter`. |

### Ambient Mode

| Variable | Default | Description |
| --- | --- | --- |
| `AMBIENT_ENABLED` | `true` | Starts the ambient scheduler and registers `/addambientchannel` and `/removeambientchannel`; when false the bot never speaks on its own. |
| `AMBIENT_MIN_SECONDS` | `14400` | Minimum of the random per-guild interval (4 h) between ambient ticks. |
| `AMBIENT_MAX_SECONDS` | `21600` | Maximum of the random per-guild interval (6 h) between ambient ticks; clamped to at least `AMBIENT_MIN_SECONDS`. |
| `AMBIENT_REPLY_COUNT` | `5` | How many recent channel messages the ambient reply-mode transcript reads. |
| `AMBIENT_TICK_PROBABILITY` | `0.5` | Chance in [0, 1] that a wake actually produces an ambient message. |
| `AMBIENT_REPLY_PROBABILITY` | `0.1` | Chance in [0, 1] that the bot joins a user message that does not address it, in any ambient channel. |

### Logging

| Variable | Default | Description |
| --- | --- | --- |
| `LOG_LEVEL` | `INFO` | One of `DEBUG`, `INFO`, `WARN`, `ERROR`. |
| `CONVERSATION_LOG` | `false` | When true, writes per-thread LLM audit files under `logs/`. |

## Testing

```sh
go build ./...
go test ./...
```

## LLM Observability

- **Audit files**: `logs/{guild_id}_{character_id}_{thread_id}.log`: A transcript of every LLM interaction with the model's reasoning included, which can be helpful when tweaking prompts. Enable with `CONVERSATION_LOG=true`.

## License

Distributed under the GNU General Public License v3.0; see [LICENSE](LICENSE) for the full text.
