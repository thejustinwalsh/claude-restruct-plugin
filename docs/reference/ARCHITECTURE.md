# Restruct Architecture

## System Overview

```mermaid
graph TB
    subgraph "Developer"
        U[User types prompt<br/>in Claude Code]
    end

    subgraph "Claude Code Harness"
        HC[UserPromptSubmit Hook<br/>sends JSON to stdin]
        CC[Claude Code receives<br/>additionalContext]
        CL[Claude LLM<br/>sees original prompt +<br/>injected context]
    end

    subgraph "Restruct CLI (Go binary)"
        RF[refine.go<br/>entry point]
        TG[toggle check<br/>enabled/disabled]
        PT[passthrough check<br/>follow-ups, commands]
        PL[Pipeline.Refine]
        FR[FrameContext<br/>add preamble]
        HO[HookOutput<br/>JSON to stdout]
    end

    subgraph "Pipeline Stages"
        RL[1. Rules Load<br/>walk to git root]
        GC[2. Git Context<br/>branch + commits]
        CK[3. Cache Check<br/>SHA256 key lookup]
        SC[4. Session Context<br/>DB: recent intents]
        PB[5. Prompt Build<br/>number rules, assemble]
        OC[6. Ollama Check<br/>connectivity]
        ME[7. Model Ensure<br/>load into VRAM]
        OI[8. LLM Inference<br/>stream JSON output]
        PA[9. Parse JSON<br/>extract classification]
        CO[10. Compose Context<br/>Go: assemble XML + footer]
        CW[11. Cache Write]
    end

    subgraph "Local LLM (Ollama)"
        LLM[Qwen 2.5 Coder 14B<br/>~100-200 token JSON]
    end

    subgraph "Data Stores"
        DB[(SQLite<br/>refinements + input_prompt<br/>+ llm_output<br/>sessions, pipeline_events)]
        FC[(File Cache<br/>~/.cache/restruct/)]
    end

    subgraph "Web Dashboard"
        SV[Go Server :8377<br/>Chi router + /api/info]
        SSE[SSE Hub<br/>1s DB poll]
        WEB[React SPA<br/>wouter routing<br/>4-panel refinement detail]
    end

    U --> HC
    HC --> RF
    RF --> TG
    TG -->|disabled| HO
    TG -->|enabled| PT
    PT -->|skip| HO
    PT -->|refine| PL

    PL --> RL
    RL --> GC
    GC --> CK
    CK -->|hit| FR
    CK -->|miss| SC
    SC --> PB
    PB --> OC
    OC --> ME
    ME --> OI
    OI --> LLM
    LLM --> PA
    PA --> CO
    CO --> CW
    CW --> FR

    FR --> HO
    HO --> CC
    CC --> CL

    RL -.->|read| DB
    SC -.->|query| DB
    RF -.->|write| DB
    CK -.->|read| FC
    CW -.->|write| FC

    OI -.->|stream tokens| SV
    SV --> SSE
    SSE --> WEB
    DB -.->|poll| SSE
```

## Data Flow: What the LLM Actually Does

```mermaid
graph LR
    subgraph "Input to LLM (assembled in Go)"
        RP[Raw Prompt]
        NR[Numbered Context Rules<br/>1. no hardcoding<br/>2. use gofmt<br/>...]
        NA[Numbered Anti-Patterns<br/>1. no eval<br/>2. no CGO<br/>...]
        GT[Git: branch +<br/>commit messages]
        SS[Session Clips<br/>- 2m ago: Fixed auth...]
    end

    subgraph "LLM Output (~150 tokens)"
        JS["JSON Classification<br/>{type, intent,<br/>recent_activity,<br/>analysis[],<br/>relevant_rules[int],<br/>relevant_anti_patterns[int],<br/>clarification[]}"]
    end

    subgraph "Post-Process: compose.go (<1ms)"
        CP[composeContext<br/>resolves indices → text<br/>adds constraints if impl type<br/>adds process guardrails<br/>builds dynamic footer]
    end

    subgraph "Static Data (never touches LLM)"
        PR[Process Guardrails<br/>After Every Change rules]
        CN[Constraints<br/>Present plan first<br/>Use sub-agents<br/>Investigate before assuming]
        FT[Dynamic Footer<br/>only references<br/>sections present]
    end

    subgraph "Final Output to Claude"
        XML["&lt;context type=...&gt;<br/>&lt;intent&gt;...&lt;/intent&gt;<br/>&lt;applicable_rules&gt;selected&lt;/applicable_rules&gt;<br/>&lt;constraints&gt;guardrails&lt;/constraints&gt;<br/>&lt;analysis&gt;from LLM&lt;/analysis&gt;<br/>&lt;anti_patterns&gt;selected&lt;/anti_patterns&gt;<br/>&lt;repo_state&gt;Branch: x | activity&lt;/repo_state&gt;<br/>&lt;/context&gt;<br/>How to use this context: ..."]
    end

    RP --> JS
    NR --> JS
    NA --> JS
    GT --> JS
    SS --> JS

    JS --> CP
    PR --> CP
    CN --> CP
    FT --> CP

    CP --> XML

    style JS fill:#f9f,stroke:#333
    style CP fill:#9f9,stroke:#333
    style PR fill:#9f9,stroke:#333
    style CN fill:#9f9,stroke:#333
    style FT fill:#9f9,stroke:#333
```

## Sequence: Single Prompt Lifecycle

```mermaid
sequenceDiagram
    participant U as User
    participant CC as Claude Code
    participant R as Restruct CLI
    participant P as Pipeline
    participant O as Ollama
    participant DB as SQLite
    participant S as Server
    participant W as Web UI

    U->>CC: types "fix the auth bug"
    CC->>R: stdin: HookInput JSON

    Note over R: Toggle check + passthrough filter

    R->>DB: INSERT refinement (status=pending)
    DB-->>R: refID=42

    R->>P: Refine(ctx, prompt, sink)

    P->>P: Load rules (walk to git root)
    P->>P: Git: branch + 5 commits
    P->>P: Cache check (SHA256 key)

    alt Cache Hit
        P-->>R: cached result
    else Cache Miss
        P->>DB: GetRecentIntents(sessionID, 5)
        DB-->>P: session clips

        P->>P: Build user message<br/>(numbered rules + git + session)

        P->>O: ChatStream(system, user, temp=0.3)

        loop Token streaming
            O-->>P: token chunk
            P-->>S: POST /api/stream/token
            S-->>W: SSE: refinement:streaming
        end

        O-->>P: complete JSON response
        P->>P: parseLLMOutput → LLMClassification
        P->>P: composeContext(classification, rules, branch)

        Note over P: compose.go adds:<br/>- Selected context rules (by index)<br/>- Process guardrails (After Every Change)<br/>- Constraints (if impl type)<br/>- Anti-patterns (by index, any type)<br/>- repo_state (branch + recent_activity)<br/>- Dynamic footer

        P->>P: Cache write
    end

    P-->>R: RefineResult

    R->>DB: UPDATE refinement<br/>(refined_prompt, input_prompt,<br/>llm_output, latency)
    R->>R: FrameContext(composed XML)

    R->>CC: stdout: HookOutput JSON<br/>{additionalContext, suppressOutput: true}

    CC->>CC: Append additionalContext<br/>after user's original prompt

    Note over CC: Claude sees:<br/>1. "fix the auth bug" (original)<br/>2. [Project rules analysis...]<br/>   <context type="code_change">...<br/>   How to use this context: ...

    S->>DB: Poll for new refinements (1s)
    DB-->>S: refinement #42 complete
    S-->>W: SSE: refinement:new
```

## Request Type → Output Sections

```mermaid
graph TD
    subgraph "LLM Classification"
        T{type?}
    end

    subgraph "Always Included"
        I[intent]
        A[analysis]
        RS[repo_state<br/>branch + LLM activity summary]
        FT[dynamic footer<br/>only present sections]
    end

    subgraph "Code Change / Refactor / Debug"
        PG[process guardrails<br/>After Every Change only]
        CO[constraints<br/>Plan first, sub-agents,<br/>investigate before assuming]
    end

    subgraph "LLM-Selected (any type)"
        CR[applicable_rules<br/>by index from numbered list]
        AP[anti_patterns<br/>by index from numbered list]
        CL[clarification_needed<br/>triggers MUST-ask directive]
    end

    T -->|code_change| PG
    T -->|refactor| PG
    T -->|debug| PG
    T -->|question| I
    T -->|docs| I

    T --> I
    T --> A
    T --> RS
    T --> FT
    T --> CR
    T --> AP
    T --> CL

    PG --> CO

    style I fill:#9f9
    style A fill:#9f9
    style RS fill:#9f9
    style FT fill:#9f9
    style PG fill:#ff9
    style CO fill:#ff9
    style CR fill:#9ff
    style AP fill:#9ff
    style CL fill:#9ff
```

## Component Map

```mermaid
graph TB
    subgraph "cli/cmd/"
        refine[refine.go<br/>Hook entry point]
        serve[serve.go<br/>Server + /api/info]
        session[session.go<br/>Session tracking]
        toggle_cmd[toggle.go<br/>Enable/disable]
    end

    subgraph "cli/internal/pipeline/"
        pipeline[pipeline.go<br/>12-stage orchestration<br/>+ parseLLMOutput]
        compose[compose.go<br/>composeContext<br/>buildFooter]
        passthrough[passthrough.go<br/>Follow-up detection]
    end

    subgraph "cli/internal/prompt/"
        builder[builder.go<br/>ParseRules + Build<br/>Numbered rule lists]
        template[template.go<br/>System prompt loader]
        framing[framing.go<br/>Output preamble]
        tmpl[system_prompt.tmpl<br/>v4: JSON classify +<br/>downstream consequences]
    end

    subgraph "cli/internal/"
        ollama[ollama/<br/>Streaming client<br/>Retry + stall detection]
        git[git/<br/>Branch + commits only]
        rules[rules/<br/>Walk-to-root loader<br/>SHA256 hash]
        cache[cache/<br/>File-based store<br/>SHA256 keys]
        db[db/<br/>SQLite + 4 migrations<br/>input_prompt, llm_output<br/>WAL mode]
        sink[sink/<br/>HttpTokenSink<br/>Background batching]
        toggle[toggle/<br/>Sentinel file]
    end

    subgraph "cli/internal/server/"
        server[server.go<br/>Chi router + API]
        sse[sse/hub.go<br/>Pub-sub + DB poll]
        streambuf[streambuf/<br/>Token accumulator]
    end

    subgraph "web/src/"
        app[App.tsx<br/>wouter routing]
        dashboard[Dashboard.tsx]
        detail[RefinementDetail.tsx<br/>4-panel flow view]
        store[store/<br/>Zustand state]
        useSSE[useSSE.ts<br/>SSE + streaming]
    end

    refine --> pipeline
    refine --> passthrough
    pipeline --> compose
    pipeline --> builder
    pipeline --> ollama
    pipeline --> git
    pipeline --> rules
    pipeline --> cache
    pipeline --> db
    refine --> sink
    refine --> framing

    sink --> server
    server --> sse
    server --> streambuf
    sse --> db

    useSSE --> server
    store --> useSSE
    detail --> store
    dashboard --> store
```

## Web UI: Refinement Detail (4-Panel Flow)

```
┌─────────────────────────────────────────────────────────┐
│ 1. User Prompt                                          │
│ What the developer typed in Claude Code                 │
├─────────────────────────────────────────────────────────┤
│ 2. LLM Input                                           │
│ System prompt (v4 classification instructions)          │
│ + User message (numbered rules, git, session, prompt)   │
├─────────────────────────────────────────────────────────┤
│ 3. LLM Output                                          │
│ Raw JSON: {type, intent, recent_activity, analysis,     │
│   relevant_rules[int], relevant_anti_patterns[int],     │
│   clarification[]}                                      │
├─────────────────────────────────────────────────────────┤
│ 4. Final Context (additionalContext)                    │
│ Composed XML with resolved rules + static constraints   │
│ + dynamic footer                                        │
└─────────────────────────────────────────────────────────┘
```
