# mcp-md-index

> 🏠 **A local [Context7](https://context7.com) for your own markdown docs**

> ⚠️ **Proof of Concept** – This project is a POC and needs more testing before production use.

An open-source MCP server that indexes your markdown documentation locally, caches it, and returns token-bounded excerpts for AI agent queries.

## How It Works

1. **First query loads and indexes** the full file (e.g., `docs/nats.md`)
2. **Caches it locally** in `.mcp-cache/` for fast subsequent access
3. **Subsequent queries** retrieve small, source-linked, token-bounded excerpts (e.g., "consumer – 500 tokens") tailored to your prompt
4. **No full document reload** needed after initial indexing

## Features

- 📄 **Smart chunking** – Splits markdown by headings with configurable min/max lines per chunk
- 🔍 **BM25 scoring** – Uses TF-IDF based ranking to find the most relevant excerpts
- 🔗 **Source links** – Every excerpt includes `path#L<start>-L<end>` for easy navigation
- 📦 **Persistent cache** – Indexes survive server restarts (file hash validation)
- ⚡ **Token-bounded** – Returns excerpts that fit within your specified token limit (default: 500)
- 🌐 **Website support** – Fetch and index any URL as markdown (HTML→Markdown conversion)

## Installation

```bash
go install github.com/bad33ndj3/mcp-md-index@latest
```

Or clone and build:

```bash
git clone https://github.com/bad33ndj3/mcp-md-index.git
cd mcp-md-index
go build -o mcp-md-index .
```

## Usage

### Configure with Claude, Cursor, or other MCP clients

Add to your MCP configuration (e.g., `~/.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "mcp-md-index": {
      "command": "mcp-md-index"
    }
  }
}
```

Or with an absolute path if not in `$PATH`:

```json
{
  "mcpServers": {
    "mcp-md-index": {
      "command": "/path/to/mcp-md-index"
    }
  }
}
```

### Tools

#### `docs_load`

Load and index a markdown file. Caches it locally for fast subsequent queries.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `path` | string | ✅ | Path to a local markdown file (e.g., `docs/nats.md`) |

**Example:**
```json
{
  "path": "docs/nats.md"
}
```

**Response:**
```
Indexed and cached.

doc_id: a1b2c3d4e5f67890
path: docs/nats.md
chunks: 42
cache: .mcp-md-index-cache/a1b2c3d4e5f67890.index.json
```

#### `docs_query`

Query indexed documents. If no `doc_id` or `path` is provided, searches **all loaded documents**.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `prompt` | string | ✅ | Short query prompt (e.g., "consumer") |
| `doc_id` | string | ⚪ | DocID returned from `docs_load` |
| `path` | string | ⚪ | Path to the markdown file (derives doc_id if omitted) |
| `max_tokens` | int | ⚪ | Approx max tokens to return (default: 500) |

> If both `doc_id` and `path` are omitted, searches across **all** loaded documents.

**Example:**
```json
{
  "path": "docs/nats.md",
  "prompt": "consumer configuration",
  "max_tokens": 500
}
```

**Response:**
```markdown
### Consumer Configuration
Source: docs/nats.md#L142-L168

A consumer is a stateful view of a stream...

--------------------------------

### Durable Consumers
Source: docs/nats.md#L170-L195

Durable consumers persist their state...
```

#### `docs_load_glob`

Load multiple markdown files matching a glob pattern.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `pattern` | string | ✅ | Glob pattern (e.g., `docs/**/*.md`, `*.md`) |

**Example:**
```json
{
  "pattern": "docs/**/*.md"
}
```

#### `site_loads`

Fetch multiple website URLs, convert HTML to markdown, and cache them.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `urls` | string[] | ✅ | Array of URLs to fetch |
| `force` | bool | ⚪ | Force re-fetch even if cached (default: false) |

**Example:**
```json
{
  "urls": ["https://docs.nats.io/jetstream", "https://pkg.go.dev/example"]
}
```

**Response:**
```
Loaded 2 sites (1 from cache, 0 failed)

- https://docs.nats.io/jetstream (chunks: 28)
- https://pkg.go.dev/example (chunks: 15)
```

#### `docs_list`

List all currently cached documents.

**Parameters:** None

**Response:**
```
Loaded documents: 2

- doc_id: a1b2c3d4e5f67890
  path: /path/to/.mcp-md-index-cache/a1b2c3d4e5f67890.md
  chunks: 28
  indexed_at: 2024-01-15T10:30:00Z

- doc_id: def456789abcdef0
  path: docs/nats.md
  chunks: 42
  indexed_at: 2024-01-15T09:00:00Z
```

## How Caching Works

- **Cache location:** `.mcp-cache/` in the current working directory (configurable with `-cache-dir` flag)
- **Cache key:** SHA256 hash of the absolute file path (first 16 chars)
- **Invalidation:** Automatic when file content hash changes
- **Version control:** Cache includes a version number; incompatible caches are rejected

## Example Workflow

```
You: Load the NATS documentation
Agent: Uses docs_load with path "docs/nats.md"
       → Indexes 42 chunks, caches to disk

You: How do I configure a consumer?
Agent: Uses docs_query with prompt "consumer configuration"
       → Returns ~500 tokens of relevant excerpts with source links

You: What about push consumers?
Agent: Uses docs_query with prompt "push consumers"
       → Instant response from cached index (no re-read of file)

You: Load the JetStream docs from the website
Agent: Uses site_load with url "https://docs.nats.io/jetstream"
       → Fetches, converts to markdown, indexes and caches

You: What docs do you have loaded?
Agent: Uses docs_list
       → Shows all cached documents with doc_ids
```

## Agent Instructions

Add to your `AGENTS.md`:

```markdown
For documentation lookup: use `docs_list` first, then `docs_query` (searches all docs if no path given), or `docs_load_glob`/`site_loads` to load new docs.
```

## License

MIT
