# Shannon Desktop

Native desktop application for Shannon multi-agent AI platform built with Tauri 2.0 + Next.js 15.

## Architecture

See [Architecture Documentation](../docs/shannon-desktop-ui.md) for detailed design and implementation plan.

## Tech Stack

- **Desktop**: Tauri 2.0 (Rust)
- **Frontend**: Next.js 15 + React 19
- **UI**: shadcn/ui + Tailwind v4
- **State**: Redux Toolkit + Redux Persist
- **Streaming**: Native EventSource (SSE)
- **Visualization**: React Flow (for future workflow graphs)
- **Local Storage**: Dexie.js (IndexedDB)

## Setup

### Prerequisites

```bash
# Rust (for Tauri)
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

# Node.js 20+
# Download from https://nodejs.org/
```

### Development

```bash
# Install dependencies
npm install

# Run Next.js dev server only
npm run dev

# Run Tauri desktop app in dev mode
npm run tauri:dev
```

### Build

```bash
# Build Next.js static export
npm run build

# Build Tauri desktop app
npm run tauri:build
```

Outputs:
- **Windows**: `src-tauri/target/release/bundle/msi/`
- **macOS**: `src-tauri/target/release/bundle/dmg/`
- **Linux**: `src-tauri/target/release/bundle/appimage/`

## Configuration

Set Shannon backend URL in `.env.local`:

```bash
NEXT_PUBLIC_SHANNON_URL=http://localhost:8080
```

Default: `http://localhost:8080` (local development)

## Features (MVP Phase 1)

- ✅ Thread-style chat interface
- ✅ Real-time SSE streaming
- ✅ Task submission with strategies (quick/standard/deep/academic)
- ✅ Model tier selection (small/medium/large)
- ✅ Redux state management with persistence
- ✅ Dark mode support
- ⏳ Session management sidebar (Phase 2)
- ⏳ Workflow visualization with React Flow (Phase 2)
- ⏳ Timeline replay controls (Phase 3)

## Project Structure

```
desktop/
├── app/                    # Next.js app directory
│   ├── layout.tsx          # Root layout with providers
│   ├── page.tsx            # Main chat interface
│   └── globals.css         # Global styles
├── components/
│   ├── ui/                 # shadcn/ui components
│   ├── chat/               # Chat interface components
│   │   ├── MessageList.tsx
│   │   └── TaskSubmissionForm.tsx
│   └── providers.tsx       # Redux + Theme providers
├── lib/
│   ├── shannon/            # Shannon API client
│   │   ├── types.ts        # TypeScript definitions
│   │   ├── api.ts          # REST API client
│   │   └── stream.ts       # SSE streaming hooks
│   ├── store/              # Redux store
│   │   ├── index.ts
│   │   ├── workflowSlice.ts
│   │   ├── sessionSlice.ts
│   │   └── hooks.ts
│   ├── db/                 # Dexie database
│   │   └── index.ts
│   └── utils.ts            # Utilities
├── src-tauri/              # Tauri backend (Rust)
│   ├── src/
│   │   └── main.rs
│   ├── Cargo.toml
│   └── tauri.conf.json
└── package.json
```

## Development Notes

- **SSE Streaming**: Uses native EventSource, no AI SDK needed
- **Event Model**: Based on `docs/event-types.md`
  - `LLM_PARTIAL` for incremental streaming
  - `LLM_OUTPUT` for final result
  - Feature-gated events require specific flags (TEAM_RECRUITED, MESSAGE_SENT, etc.)
- **Static Export**: Next.js configured with `output: 'export'` for Tauri
- **Persistence**: Redux state persisted to localStorage via redux-persist

## Next Steps

See todo list in [docs/shannon-desktop-ui.md](../docs/shannon-desktop-ui.md#implementation-phases):

**Phase 2 (Weeks 7-10)**:
- Session list sidebar
- React Flow workflow visualization
- Timeline API integration

**Phase 3 (Weeks 11-14)**:
- Timeline replay controls
- Cost analytics dashboard
- Multi-window support

## License

Copyright (c) 2025 Shannon AI Platform. See main LICENSE file.
