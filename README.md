# mcp-md-index

> 🏠 **A local [Context7](https://context7.com) for your own markdown docs**

> ⚠️ **Proof of Concept** – This project is a POC and needs more testing before production use.

An open-source MCP server that indexes your markdown documentation locally, caches it, and returns token-bounded excerpts for AI agent queries.

## How It Works

1. **First query loads and indexes** the full file (e.g., `docs/nats.md`)
2. **Caches it locally** in `.mcp-md-index-cache/` for fast subsequent access
3. **Subsequent queries** retrieve small, source-linked, token-bounded excerpts (e.g., "consumer – 500 tokens") tailored to your prompt
4. **No full document reload** needed after initial indexing

## Features

- 📄 **Smart chunking** – Splits markdown by headings with configurable min/max lines per chunk
- 🔍 **BM25 scoring** – Uses TF-IDF based ranking to find the most relevant excerpts
- 🔗 **Source links** – Every excerpt includes `path#L<start>-L<end>` for easy navigation
- 📦 **Persistent cache** – Indexes survive server restarts (file hash validation)
- ⚡ **Token-bounded** – Returns excerpts that fit within your specified token limit (default: 500)

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

Query an indexed markdown document and return token-bounded, source-linked excerpts.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `prompt` | string | ✅ | Short query prompt (e.g., "consumer") |
| `doc_id` | string | ⚪ | DocID returned from `docs_load` |
| `path` | string | ⚪ | Path to the markdown file (derives doc_id if omitted) |
| `max_tokens` | int | ⚪ | Approx max tokens to return (default: 500) |

> Either `doc_id` or `path` must be provided.

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

## How Caching Works

- **Cache location:** `.mcp-md-index-cache/` in the current working directory
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
```

## License

MIT
