# Prompt Pipeline Architecture

A local-first context compilation pipeline that transforms vague user intent into rich, actionable prompts for Claude. Uses Ollama for cheap local inference and embedding, Go for deterministic data gathering, and Claude for the expensive reasoning work.

## Design Principle

> **Your pipeline provides FACTS. Claude provides REASONING.**
>
> The local pipeline gathers code context, matches rules, searches prior decisions, and refines intent. Claude receives a precise, unambiguous prompt with everything it needs to produce correct, convention-following code on the first pass.

---

## System Components

### 1. Context Database (`context.db` — SQLite)

Single-file database backing all search and caching. Three index types coexist.

**Schema:**

```sql
-- Turn pairs from past conversations
CREATE TABLE turns (
    turn_id       INTEGER PRIMARY KEY,
    user_msg      TEXT NOT NULL,
    assistant_msg TEXT NOT NULL,
    content_hash  TEXT UNIQUE NOT NULL,  -- sha256(user + \x00 + assistant)[:24]
    created_at    INTEGER
);

-- FTS5 index over turns
CREATE VIRTUAL TABLE turns_fts USING fts5(
    turn_id,
    user_msg,
    assistant_msg,
    content='turns',
    content_rowid='turn_id'
);

-- Embeddings for turns (semantic search)
CREATE TABLE turn_embeddings (
    turn_id  INTEGER PRIMARY KEY REFERENCES turns(turn_id),
    vec      BLOB NOT NULL  -- float32 × 768 dims = 3072 bytes per row
);

-- Enriched rule manifests (LLM-generated at index time)
CREATE TABLE rules (
    rule_id       TEXT PRIMARY KEY,         -- e.g. "editor/drag-and-drop"
    path          TEXT NOT NULL,            -- .rules/drag-and-drop.md
    raw_content   TEXT NOT NULL,            -- original markdown
    summary       TEXT NOT NULL,            -- LLM-generated 1-2 sentence summary
    keywords      TEXT NOT NULL DEFAULT '[]', -- JSON array
    triggers      TEXT NOT NULL DEFAULT '[]', -- JSON array of natural language intents
    applies_to    TEXT NOT NULL DEFAULT '[]', -- JSON array of glob patterns
    dependencies  TEXT NOT NULL DEFAULT '[]', -- JSON array of other rule_ids
    conflicts     TEXT NOT NULL DEFAULT '[]', -- JSON array of prohibited patterns
    context_hash  TEXT NOT NULL,            -- hash(rule content + surrounding code context)
    updated_at    INTEGER
);

-- FTS5 index over rules
CREATE VIRTUAL TABLE rules_fts USING fts5(
    rule_id,
    summary,
    keywords,
    triggers,
    content='rules',
    content_rowid='rowid'
);

-- Embeddings for rules (triggers + summary concatenated)
CREATE TABLE rule_embeddings (
    rule_id  TEXT PRIMARY KEY REFERENCES rules(rule_id),
    vec      BLOB NOT NULL,
    text     TEXT NOT NULL  -- what was embedded (for debugging)
);

-- File metadata for incremental indexing
CREATE TABLE file_meta (
    path      TEXT PRIMARY KEY,
    mod_time  INTEGER NOT NULL,
    file_hash TEXT NOT NULL
);

-- Indexer state
CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);
```

**Memory budget at query time:**

| Item | Size |
|---|---|
| 10k turn embeddings in Go heap | ~30 MB (float32 × 768 × 10k) |
| 50 rule embeddings in Go heap | ~150 KB |
| SQLite FTS5 index | on disk, queried |
| Total in-process memory | ~30 MB |
| Ollama VRAM (qwen3:14b) | ~12 GB |
| Ollama VRAM (nomic-embed-text, when loaded) | ~300 MB |

---

### 2. Indexer (`cli index`)

Runs before chat sessions. Incrementally indexes transcripts, rule files, and their surrounding code context. Two sub-pipelines.

#### 2a. Transcript Indexer

Parses chat transcript files, deduplicates by content hash, embeds new turns.

```
transcript files (jsonl, md, or custom format)
       │
       ▼
┌──────────────────────────┐
│ Parse into turn pairs     │
│ (user_msg, assistant_msg) │
└──────────┬───────────────┘
           │
           ▼
┌──────────────────────────┐
│ For each turn:            │
│   hash = sha256(user+ast) │
│   EXISTS in DB? → skip    │
│   NEW? → queue for embed  │
└──────────┬───────────────┘
           │ (new turns only)
           ▼
┌──────────────────────────┐
│ Batch embed via Ollama    │
│ model: nomic-embed-text   │
│ prefix: "search_document" │
│ batch size: 50-100        │
│ ~2-3 sec per 100 turns    │
└──────────┬───────────────┘
           │
           ▼
┌──────────────────────────┐
│ INSERT into:              │
│   turns                   │
│   turns_fts               │
│   turn_embeddings         │
│ Single transaction        │
│ UPDATE meta.last_offset   │
└──────────────────────────┘
```

#### 2b. Rule Enrichment Indexer

Walks the monorepo for `.rules/` directories, gathers surrounding code context, calls an LLM to extract structured metadata, then embeds the result.

**Step 1: Gather context (pure Go, no LLM)**

For each `.rules/*.md` file found in the repo:

```
┌─────────────────────────────────────────────────────────┐
│ Gather (all deterministic, no LLM):                      │
│                                                          │
│ 1. Rule file contents (the .md)                          │
│                                                          │
│ 2. Directory tree (2 levels up + 2 down from .rules/)    │
│    → gives structural context                            │
│                                                          │
│ 3. Import graph snapshot of governed directories          │
│    For each .ts/.tsx/.go file in sibling src/:            │
│    - extract import/require statements (regex)            │
│    - extract exported function/type/const names           │
│    - extract interface/struct definitions (names only)    │
│    This gives the LLM the dependency web without          │
│    reading full file bodies. ~50-200 lines per package.   │
│                                                          │
│ 4. Package dependencies                                  │
│    Relevant entries from package.json or go.mod           │
│                                                          │
│ 5. Sibling rule filenames                                │
│    Other .rules/*.md at the same level                   │
│    (so LLM can reference them in dependencies field)     │
│                                                          │
│ Total context per rule: ~1-2k tokens                     │
└─────────────────────────────────────────────────────────┘
```

**Step 2: Check cache**

```
context_hash = sha256(rule_content + tree + imports + deps)
if context_hash matches DB → skip (nothing changed)
```

**Step 3: LLM enrichment call**

One call per changed rule. Uses qwen3:14b (same chat model, already available) or a smaller model — this is structured extraction, not hard reasoning.

**Enrichment Agent Prompt:**

```
You are a rule indexer for a software project. Given a rule file and its
surrounding code context, produce a JSON object that will be used to match
this rule to future developer tasks.

INPUT:
- Rule file contents
- Directory tree showing where this rule sits in the project
- Import/export graph of nearby source files
- Package dependencies in scope
- Names of sibling rule files

OUTPUT (JSON only, no preamble):
{
  "summary": "1-2 sentences: what this rule governs, why it exists, and
              what goes wrong if you violate it",

  "keywords": ["specific terms a developer would use when working in this
                area — library names, API names, component names, hook names,
                function names, file names, patterns, concepts"],

  "applies_to": ["glob patterns for files this rule governs, inferred from
                  the directory structure — e.g. packages/editor/src/components/**"],

  "triggers": ["natural language descriptions of tasks or intents that should
                activate this rule — write these as things a developer would say
                when starting a task, e.g. 'add drag and drop to a list',
                'make items reorderable', 'implement sortable tracks'"],

  "dependencies": ["rule_ids of other rules that should also be included
                    whenever this rule is active — use the sibling rule
                    filenames to infer IDs"],

  "conflicts_with": ["patterns, libraries, or approaches that this rule
                      explicitly prohibits — e.g. 'react-beautiful-dnd',
                      'mouse-only drag handlers without keyboard support'"]
}

Be specific. Keywords should include actual names from the import graph.
Triggers should be diverse — cover different phrasings of the same intent.
applies_to globs should be precise, not overly broad.
```

**Step 4: Embed triggers + summary**

```
embed_text = "search_document: " + summary + " " + join(triggers, ". ")
```

One nomic-embed-text call per rule. Store vector in `rule_embeddings`.

**Step 5: Store everything**

Insert/update `rules`, `rules_fts`, `rule_embeddings` in a single transaction.

**Typical timings for a repo with 30 rules:**

| Step | Time |
|---|---|
| Walk + gather context | <1 sec |
| LLM enrichment (only changed rules) | ~1 sec per rule |
| Embedding | ~2 sec total batch |
| SQLite writes | negligible |
| Full cold index | ~35 sec |
| Incremental (1 rule changed) | ~3 sec |

---

### 3. Rule Scorer (Go, no LLM)

At query time, before calling any LLM, the Go code scores all rules against the user's input using three signals and returns a ranked list.

```
User input: "I want to make tracks reorderable,
             look at TrackEditor.tsx"
       │
       ▼
┌─────────────────────────────────────────────────────────┐
│ SIGNAL 1: Path matching (instant, zero cost)             │
│                                                          │
│ Extract file references from user input                  │
│ → "TrackEditor.tsx"                                      │
│ → resolve to full path: packages/editor/src/components/  │
│ → glob match against all rules' applies_to               │
│ → hit: editor/component-structure  (score: 1.0)          │
│ → hit: editor/drag-and-drop        (score: 1.0)          │
├─────────────────────────────────────────────────────────┤
│ SIGNAL 2: Keyword / FTS5 match (fast, zero cost)         │
│                                                          │
│ FTS5 query against rules_fts                             │
│ → "tracks reorderable TrackEditor"                       │
│ → hit: editor/drag-and-drop        (BM25: 0.82)          │
│ → hit: editor/state-management     (BM25: 0.34)          │
├─────────────────────────────────────────────────────────┤
│ SIGNAL 3: Semantic match (~50ms, one nomic embed call)   │
│                                                          │
│ Embed user input with "search_query:" prefix             │
│ Cosine similarity against all rule embeddings (in-memory)│
│ → hit: editor/drag-and-drop        (cosine: 0.91)        │
│ → hit: editor/accessibility        (cosine: 0.67)        │
├─────────────────────────────────────────────────────────┤
│ SCORE FUSION:                                            │
│                                                          │
│ final_score = (0.40 × path_score)                        │
│             + (0.25 × keyword_score)    [normalized 0-1] │
│             + (0.35 × semantic_score)                    │
│                                                          │
│ Path match weighted highest: if the user pointed at a    │
│ file, rules governing that file are almost certainly     │
│ relevant.                                                │
├─────────────────────────────────────────────────────────┤
│ DEPENDENCY EXPANSION:                                    │
│                                                          │
│ For top-scoring rules, pull their dependencies:          │
│ drag-and-drop.dependencies → [accessibility,             │
│                                state-management]         │
│ Include dependency rules at a reduced score.             │
├─────────────────────────────────────────────────────────┤
│ OUTPUT: ranked rule list                                 │
│                                                          │
│ 1. editor/drag-and-drop         (0.94) ← direct match   │
│ 2. editor/component-structure   (0.72) ← path match     │
│ 3. editor/accessibility         (0.67) ← dependency     │
│ 4. editor/state-management      (0.54) ← dependency     │
└─────────────────────────────────────────────────────────┘
```

---

### 4. Transcript Search (Go + nomic-embed-text)

Hybrid search over past conversation turns. Called by the Go pipeline when assembling context, not by the LLM directly.

```
query: "drag and drop" (extracted from user input)
       │
       ├──► Embed query via nomic ("search_query:" prefix)
       │    Cosine similarity against in-memory turn vectors
       │    Top 3 semantic matches
       │
       ├──► FTS5 keyword search on turns_fts
       │    Top 3 keyword matches
       │
       ▼
Merge + deduplicate + score fusion
Return top-k turn pairs with full text
```

---

### 5. Prompt Refinement Agent (qwen3:14b, local)

The only LLM call in the pipeline before Claude. Receives all pre-gathered context and performs intent refinement — the one task that requires language understanding.

**This is a single-shot call. No tools. No loop.**

#### System Prompt

```
You are a prompt refinement agent. Your job is to take a developer's raw
intent and produce a structured, actionable prompt for a coding agent.

You will receive:
- The developer's original request
- Pre-selected project rules (already matched and ranked)
- Relevant source code context (already gathered)
- Prior conversation context (if any matches were found)

You do NOT need to search for files, select rules, or gather context.
That work is already done. Focus entirely on understanding intent and
producing a clear prompt.

Your output MUST follow this exact structure:

## Restated Intent
What the developer is actually asking for, stated precisely and
unambiguously. Reference specific components, files, and patterns from
the provided context. Transform vague intent into concrete technical
description.

## Open Questions
Ambiguities that could lead to wrong implementation. For each:
- State the question clearly
- State the default answer if one can be inferred from the rules or code context
- Mark as [USER DECISION] if no default exists and the developer must choose
- Mark as [DEFAULTED] if a reasonable default can be inferred

Keep this list short. Only include questions where the answer materially
affects implementation. Do not ask about things the rules already decide.

## Task Breakdown
Concrete implementation steps. Each step should be:
- A single clear action
- Referencing specific files and functions from the context
- Ordered by dependency (what must come first)
- Small enough that each step could be a single commit

Do NOT explain how to implement each step in detail. The coding agent
is highly capable. Describe WHAT to do, WHERE to do it, and WHAT
CONSTRAINTS apply. Do not describe HOW.

## Constraints
Non-obvious requirements extracted from the rules that apply to THIS
specific task. Do not repeat full rules — extract only the specific
constraints the coding agent must follow. Frame as actionable
do/don't statements.
```

#### User Message (assembled by Go)

```
## Developer Request
{user's original input, verbatim}

## Source Code Context
{contents of referenced files, or relevant excerpts}
{import graph / type signatures of related files}

## Applicable Rules (ranked by relevance)
### {rule_id} (relevance: {score})
{rule raw_content}

### {rule_id} (relevance: {score})
{rule raw_content}

## Prior Decisions
{matched transcript turns, if any}
Turn {id}: User asked: "{user_msg}"
           Result: "{assistant_msg summary}"
```

#### Optional Second Pass

If the refinement agent's output includes references to files or context it hasn't seen, the Go code can detect this (look for "I would need to see..." or file paths not in the provided context), fetch the missing pieces, and make one more call:

```
Pass 1: LLM sees initial context → outputs refined prompt
        + optionally: "Additional context needed: [list]"

If list is empty → done

If list has items → Go fetches, appends to context → Pass 2
Pass 2: LLM sees updated context → outputs final prompt

Maximum two LLM calls. Deterministic control flow.
```

---

### 6. Prompt Assembler (Go, no LLM)

Final step. Takes the refinement agent's structured output and combines it with static injections to produce the prompt sent to Claude.

```
┌─────────────────────────────────────────────────────────┐
│ FINAL PROMPT TO CLAUDE                                   │
│                                                          │
│ {static system prompt / persona boilerplate}             │
│                                                          │
│ {project-level static context — always injected}         │
│   - repo structure overview                              │
│   - tech stack summary                                   │
│   - global conventions                                   │
│                                                          │
│ ## Task                                                  │
│ {restated intent from refinement agent}                  │
│                                                          │
│ ## Implementation Steps                                  │
│ {task breakdown from refinement agent}                   │
│                                                          │
│ ## Constraints                                           │
│ {constraints from refinement agent}                      │
│                                                          │
│ ## Open Questions                                        │
│ {only [DEFAULTED] items — [USER DECISION] items          │
│  were resolved interactively before this point}          │
│                                                          │
│ ## Reference Code                                        │
│ {source files and excerpts, same as what the             │
│  refinement agent saw}                                   │
│                                                          │
│ ## Applicable Rules                                      │
│ {full rule text for top-scored rules}                    │
│                                                          │
│ ## Prior Decisions                                       │
│ {transcript matches, if any}                             │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

---

## End-to-End Flow

```
User types: "I want to make tracks reorderable, look at TrackEditor.tsx"
       │
       ▼
┌─────────────────────────────────────────────────────────┐
│ GO CLI: DETERMINISTIC PIPELINE                           │
│                                                          │
│ 1. Parse user input                                      │
│    - extract file references → ["TrackEditor.tsx"]       │
│    - extract keywords → ["tracks", "reorderable"]        │
│                                                          │
│ 2. Resolve file paths                                    │
│    - find TrackEditor.tsx in monorepo                    │
│    - read its contents                                   │
│    - read its import graph (what it imports/exports)     │
│                                                          │
│ 3. Score rules (Rule Scorer)                             │
│    - path match against applies_to globs                │
│    - FTS5 keyword match                                  │
│    - semantic embed + cosine similarity                  │
│    - score fusion → ranked rule list                    │
│    - expand dependencies                                │
│                                                          │
│ 4. Load top rule contents from disk                      │
│                                                          │
│ 5. Search transcripts (Transcript Search)                │
│    - hybrid keyword + semantic search                   │
│    - return top matching turn pairs                      │
│                                                          │
│ 6. Assemble context bundle for refinement agent          │
│                                                          │
│ Time: <1 sec (one nomic embed call ~50ms, rest is I/O)  │
└──────────────────┬──────────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────────┐
│ QWEN3:14B — REFINEMENT (local, single shot)              │
│                                                          │
│ Input: system prompt + assembled context bundle           │
│ Output: structured refined prompt                        │
│                                                          │
│ Time: ~3-5 sec                                           │
│ VRAM: ~12 GB (already loaded)                            │
└──────────────────┬──────────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────────┐
│ GO CLI: POST-PROCESSING                                  │
│                                                          │
│ 1. Parse structured output from refinement agent         │
│                                                          │
│ 2. Check for [USER DECISION] flags                       │
│    → present choices to user in terminal                 │
│    → collect answers                                     │
│    → optionally re-run refinement with answers           │
│                                                          │
│ 3. Check for missing context requests (optional pass 2)  │
│    → fetch additional files if needed                    │
│    → re-run refinement once more                        │
│                                                          │
│ 4. Assemble final prompt (Prompt Assembler)              │
│    → inject static boilerplate                          │
│    → inject refinement output                           │
│    → inject rule text + code context + transcripts      │
│                                                          │
│ Time: <1 sec (unless user interaction needed)            │
└──────────────────┬──────────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────────┐
│ CLAUDE API                                               │
│                                                          │
│ Receives a prompt that:                                  │
│ ✓ Is unambiguous (intent restated precisely)             │
│ ✓ Has all code context it needs to see                   │
│ ✓ Has constraints it must follow (from rules)            │
│ ✓ Has a clear implementation plan it can follow          │
│   or improve upon                                        │
│ ✓ Has history so it won't contradict past decisions      │
│ ✓ Has open questions already resolved                    │
│                                                          │
│ Claude's job: implement. Not guess, not discover,        │
│ not infer conventions. Just build it right.              │
└─────────────────────────────────────────────────────────┘
```

---

## Tool Definitions

Tools exposed to the refinement agent are listed below. In the current architecture, the refinement agent does NOT use tools (single-shot call with pre-gathered context). These definitions exist for the alternative tool-loop architecture, or for future use if the refinement agent needs to request additional context.

### search_context

Searches past conversation transcripts for relevant prior decisions.

```json
{
  "name": "search_context",
  "description": "Search previous conversations for relevant context. Returns matching prompt/response pairs ranked by relevance. Use this to find prior decisions, past approaches, or previously discussed constraints.",
  "parameters": {
    "type": "object",
    "required": ["query"],
    "properties": {
      "query": {
        "type": "string",
        "description": "What to search for — describe the topic, decision, or concept"
      },
      "max_results": {
        "type": "integer",
        "description": "Maximum results to return. Default 3."
      }
    }
  }
}
```

### get_rules

Retrieves full rule file contents by ID.

```json
{
  "name": "get_rules",
  "description": "Retrieve project rules by their IDs. Rules contain conventions, required libraries, prohibited patterns, and architectural decisions for specific parts of the codebase.",
  "parameters": {
    "type": "object",
    "required": ["rule_ids"],
    "properties": {
      "rule_ids": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Rule IDs to retrieve, e.g. ['editor/drag-and-drop', 'editor/accessibility']"
      }
    }
  }
}
```

### read_file

Reads a file or line range from the monorepo.

```json
{
  "name": "read_file",
  "description": "Read a file from the project. Returns file contents. Use start_line/end_line to read a specific range for large files.",
  "parameters": {
    "type": "object",
    "required": ["path"],
    "properties": {
      "path": {
        "type": "string",
        "description": "File path relative to monorepo root"
      },
      "start_line": {
        "type": "integer",
        "description": "Start line (1-indexed). Omit to read from beginning."
      },
      "end_line": {
        "type": "integer",
        "description": "End line (inclusive). Omit to read to end."
      }
    }
  }
}
```

### search_files

Keyword/regex search across the codebase (ripgrep wrapper).

```json
{
  "name": "search_files",
  "description": "Search project files by keyword or pattern. Returns matching lines with file path, line number, and surrounding context (±3 lines). Use for finding function definitions, usages, imports, or specific patterns.",
  "parameters": {
    "type": "object",
    "required": ["query"],
    "properties": {
      "query": {
        "type": "string",
        "description": "Search string or regex pattern"
      },
      "glob": {
        "type": "string",
        "description": "File glob to limit search, e.g. '*.tsx' or 'packages/editor/**'"
      },
      "max_results": {
        "type": "integer",
        "description": "Maximum results. Default 10."
      }
    }
  }
}
```

### list_files

Directory listing with optional glob filtering.

```json
{
  "name": "list_files",
  "description": "List files and directories at a given path. Returns names, sizes, and modification times. Respects .gitignore.",
  "parameters": {
    "type": "object",
    "required": ["path"],
    "properties": {
      "path": {
        "type": "string",
        "description": "Directory path relative to monorepo root"
      },
      "depth": {
        "type": "integer",
        "description": "Max directory depth. Default 2."
      },
      "glob": {
        "type": "string",
        "description": "Filter pattern, e.g. '*.tsx'"
      }
    }
  }
}
```

---

## Model Configuration

### Hardware Budget: 24 GB VRAM

| Model | Purpose | VRAM | Loaded When |
|---|---|---|---|
| qwen3:14b (Q4_K_M) | Prompt refinement + rule enrichment | ~12 GB | Chat sessions + indexing |
| nomic-embed-text | Embedding for search | ~300 MB | Indexing + query-time search |
| **Total peak** | | **~12.3 GB** | |

Ollama config:

```bash
OLLAMA_MAX_LOADED_MODELS=2 ollama serve
```

Both models stay warm. nomic loads/unloads fast enough (~500ms) that `MAX_LOADED_MODELS=1` also works if memory is tight.

### Model Recommendations

For the chat/refinement model, prioritize tool-calling reliability if using the tool-loop variant:

| Model | Size | Strength | Notes |
|---|---|---|---|
| qwen3:14b | ~9 GB | Best balance for 24 GB budget | Strong tool calling, good reasoning |
| qwen3.5:9b | ~6 GB | Best tool-calling reliability at small size | Leaves more room for context |
| qwen3:32b (Q4_K_M) | ~20 GB | Highest quality reasoning | Tight fit, short context only |

For embeddings, nomic-embed-text v1.5 is the default. Use `search_document:` prefix when indexing, `search_query:` prefix when querying (asymmetric retrieval).

---

## Incremental Indexing Strategy

### Transcripts

```
Track: meta.last_indexed_offset (byte offset or line number)

On index:
  1. Read transcript from last_offset to end
  2. Parse new turn pairs
  3. For each: hash → check DB → skip if exists
  4. Batch embed new turns (50-100 per call)
  5. INSERT + UPDATE offset
  6. Cost: ~2-3 sec per 100 new turns
```

### Rules

```
Track: rules.context_hash = sha256(rule_content + tree + imports + deps)

On index:
  1. Walk monorepo for .rules/ directories
  2. For each rule file:
     a. Gather surrounding context (Go, no LLM)
     b. Compute context_hash
     c. Compare to DB → skip if unchanged
  3. For changed rules only:
     a. Call enrichment LLM (~1 sec per rule)
     b. Embed triggers + summary (~50ms per rule)
     c. Upsert in DB
  4. Cost: ~3 sec per changed rule
```

### Files (for file_meta table)

```
Track: file_meta.mod_time + file_meta.file_hash

On index:
  1. Walk allowed directories (respecting .gitignore)
  2. For each file:
     a. mod_time unchanged? → skip entirely
     b. mod_time changed? → hash contents
     c. Hash unchanged? → update mod_time only
     d. Hash changed? → re-index
  3. Used primarily for cache invalidation,
     not for embedding file contents
```

---

## VRAM Timeline

```
INDEXING PHASE:
  nomic-embed-text  ████████░░░░░░░░░░░░░░░░░░░░░░
  qwen3:14b         ░░░░░░░░████████░░░░░░░░░░░░░░  (rule enrichment)
  Peak VRAM:        ~12.3 GB

CHAT PHASE:
  nomic-embed-text  ░░█░░░░░░░░░░░░░░░░░░░░░░░░░░░  (one query embed)
  qwen3:14b         ░░░████████░░░░░░░░░░░░░░░░░░░░  (refinement call)
  Peak VRAM:        ~12.3 GB

  nomic is loaded for <1 sec per query.
  qwen3 stays loaded for the session.
```

---

## Directory Structure

```
monorepo/
├── .ai/
│   └── (global rules and static prompt boilerplate)
│
├── packages/
│   ├── editor/
│   │   ├── .rules/
│   │   │   ├── drag-and-drop.md
│   │   │   ├── component-structure.md
│   │   │   └── ...
│   │   └── src/
│   ├── engine/
│   │   ├── .rules/
│   │   │   └── audio-pipeline.md
│   │   └── src/
│   └── ...
│
└── tools/
    └── prompt-cli/           ← your Go CLI
        ├── cmd/
        │   ├── index.go      ← indexer subcommand
        │   └── prompt.go     ← prompt builder subcommand
        ├── internal/
        │   ├── index/
        │   │   ├── transcripts.go
        │   │   ├── rules.go
        │   │   └── enrichment.go
        │   ├── search/
        │   │   ├── embeddings.go   ← cosine similarity, vector I/O
        │   │   ├── fts.go          ← FTS5 queries
        │   │   └── hybrid.go       ← score fusion
        │   ├── scorer/
        │   │   └── rules.go        ← three-signal rule scoring
        │   ├── tools/
        │   │   ├── readfile.go
        │   │   ├── searchfiles.go  ← ripgrep wrapper
        │   │   ├── listfiles.go
        │   │   └── dispatch.go     ← tool call router
        │   ├── prompt/
        │   │   ├── refine.go       ← refinement agent call
        │   │   └── assemble.go     ← final prompt assembly
        │   └── db/
        │       └── sqlite.go       ← schema, migrations, queries
        ├── context.db               ← generated, gitignored
        └── go.mod
```

---

## Key Dependencies (Go)

| Package | Purpose |
|---|---|
| `github.com/ollama/ollama/api` | Ollama SDK — chat + embed |
| `modernc.org/sqlite` | Pure Go SQLite with FTS5 (no CGo) |
| `github.com/bmatcuk/doublestar` | Glob matching for applies_to |
| ripgrep (external binary) | File search (shell out to `rg`) |

---

## What Each Layer Does

| Layer | Does | Does NOT do |
|---|---|---|
| **Go pipeline** | Parse input, resolve files, score rules, search transcripts, read code, assemble context | Reason about intent, analyze architecture, make implementation decisions |
| **qwen3:14b** | Restate intent, identify ambiguities, decompose into steps, extract relevant constraints | Search for files, select rules, gather context, write implementation code |
| **Claude** | Architect solutions, write idiomatic code, handle edge cases, integrate with existing patterns | Guess project conventions, discover file structure, infer past decisions |
