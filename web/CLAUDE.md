# Web — Restruct Dashboard

## Stack
- React 19.2 + TypeScript 5.9 + Vite 8 + Tailwind 4
- React Compiler enabled (auto-memoization via babel-plugin-react-compiler)
- Base-UI primitives + CVA variants (not Radix — use @base-ui/react)
- Zustand for client state, wouter for routing

## React 19 Conventions

### Data fetching: `use()` + Suspense, not useEffect
- Fetch data with `use(promise)` inside a Suspense boundary — no `useEffect` + `useState` + `isLoading` pattern
- Every `use(promise)` needs a `<Suspense>` above it and an `<ErrorBoundary>` beside it
- **Never create promises inside render** — cache them or lift to parent. New promise per render = infinite suspend loop
- Place Suspense boundaries at section level (per card/panel), not per component
- For parallel fetches, create all promises at the parent, pass down as props

### Transitions for expensive updates
- Wrap expensive state updates in `useTransition` — use `isPending` as loading indicator instead of manual loading state
- Use `useDeferredValue` when you don't control the state setter (e.g. props from parent)
- Compare current vs deferred value to show staleness (`opacity: isStale ? 0.7 : 1`)
- Don't wrap cheap updates in `startTransition` — adds overhead for no benefit

### React 19 API changes — use the new patterns
- `ref` is a regular prop — no `forwardRef` wrapper needed
- `use(Context)` replaces `useContext(Context)` — and unlike hooks, `use()` works inside conditionals
- `<Context value={x}>` replaces `<Context.Provider value={x}>`
- No `React.FC`, no `React.PropsWithChildren` — type props directly on the function

### React Compiler is active — trust it
- Don't manually `useMemo`/`useCallback` unless profiling shows the compiler missed something
- Keep components pure (no side effects during render) so the compiler can optimize
- If you must opt out, use `"use no memo"` directive — don't fight the compiler with manual memos

## Code Style
- Named exports only — no default exports (better refactoring, better tree-shake)
- No `as` type assertions — fix the types instead
- No `React.FC` — just `function Component(props: Props)`
- Props as `interface` when extensible, `type` for unions/intersections
- Use `satisfies` for type-safe object literals
- Use `React.ComponentProps<typeof X>` to derive prop types from existing components

## Components
- Use shadcn/Base-UI components before building custom ones
- Co-locate component-specific types with the component file
- No unstable nested component definitions (components inside render = remount every render)

## Do NOT
- Do not use `useEffect` for data fetching — use `use()` + Suspense
- Do not use `forwardRef` — ref is a prop in React 19
- Do not use `React.FC` or `React.PropsWithChildren`
- Do not manually `useMemo`/`useCallback` — React Compiler handles it
- Do not create promises inside render — cache or lift to parent
- Do not use `as` type assertions
- Do not use `<Context.Provider>` — use `<Context value={x}>` directly

## Design Context

### Users
Solo developer monitoring their own restruct prompt refinement pipeline. The dashboard is a personal instrumentation tool — every element should serve the operator's need to quickly assess system health and refinement quality.

### Brand Personality
Calm, precise, understated. The interface should feel like a well-calibrated instrument — quiet until something needs attention.

### Aesthetic Direction
- **References:** Linear, Vercel — clean, minimal, high-contrast with excellent typography
- **Theme:** Light mode primary (dark mode secondary)
- **Font:** Geist Variable (already in use)
- **Color:** Monochromatic grayscale with restrained accent use. OKLCH color space for perceptual consistency
- **Density:** Tighter than current — reduce whitespace between metrics, tighten card padding, increase information density without sacrificing readability
- **Typography:** Small but legible. Favor text-sm as body default, text-xs for metadata. Use font-medium over font-bold where possible for quieter hierarchy

### Design Principles
1. **Density over decoration** — Maximize useful information per viewport. Reduce padding, margins, and gaps to the minimum that maintains visual clarity. No decorative whitespace.
2. **Quiet hierarchy** — Use weight, size, and muted color to create hierarchy instead of borders, backgrounds, or visual noise. Let the data breathe through typography, not through spacing.
3. **Alignment is everything** — Consistent left edges, baseline-aligned text, uniform grid gutters. Misalignment is the first thing that makes a UI feel unpolished.
4. **Monochrome discipline** — Color is reserved for status (error, success, warning) and data visualization. UI chrome stays grayscale. No gratuitous accent colors on buttons or badges.
5. **Calm until urgent** — Default state is quiet and neutral. Visual intensity (color, weight, animation) is earned by importance — errors, active streams, anomalies.
