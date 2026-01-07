# Desktop App Troubleshooting Guide

## Common Issues and Solutions

### "boolean is not defined" Error with react-markdown

**Error Message:**
```
Runtime ReferenceError: boolean is not defined
at module evaluation (components/run-conversation.tsx:10:1)
```

This is a known issue with `react-markdown` and Next.js 16 with Turbopack.

#### Solution 1: Use Webpack Instead of Turbopack (Recommended)

Run the dev server without Turbopack:

```bash
npm run dev:webpack
```

This uses the traditional webpack bundler which doesn't have this issue.

#### Solution 2: Update Dependencies

Update to the latest versions:

```bash
npm install react-markdown@latest remark-gfm@latest rehype-highlight@latest
npm install
```

#### Solution 3: Downgrade Next.js

If the issue persists, temporarily downgrade to Next.js 15:

```bash
npm install next@15 react@18 react-dom@18
```

#### Solution 4: Use Alternative Markdown Renderer

Replace `react-markdown` with `@uiw/react-markdown-preview`:

```bash
npm uninstall react-markdown remark-gfm rehype-highlight
npm install @uiw/react-markdown-preview
```

Then update the component imports.

### Backend Connection Issues

**Error:** "Failed to fetch" or "Network error"

**Solutions:**

1. **Check backend is running:**
   ```bash
   cd ..
   make ps
   ```

2. **Verify Gateway is accessible:**
   ```bash
   curl http://localhost:8080/health
   ```

3. **Check `.env.local` configuration:**
   ```bash
   cat .env.local
   ```
   
   Ensure: `NEXT_PUBLIC_API_URL=http://localhost:8080`

4. **Restart both backend and frontend:**
   ```bash
   # Terminal 1: Backend
   cd ..
   make down
   make dev
   
   # Terminal 2: Frontend
   cd desktop
   npm run dev:webpack
   ```

### Port Already in Use

**Error:** "Port 3000 is already in use"

**Solution:**

```bash
# Kill process on port 3000
lsof -ti:3000 | xargs kill -9

# Or use a different port
PORT=3001 npm run dev
```

### Module Not Found Errors

**Error:** "Module not found: Can't resolve..."

**Solutions:**

1. **Reinstall dependencies:**
   ```bash
   rm -rf node_modules package-lock.json
   npm install
   ```

2. **Clear Next.js cache:**
   ```bash
   rm -rf .next
   npm run dev
   ```

### TypeScript Errors

**Error:** Type errors during development

**Solutions:**

1. **Update TypeScript:**
   ```bash
   npm install -D typescript@latest @types/react@latest @types/node@latest
   ```

2. **Restart TypeScript server in VS Code:**
   - Press `Cmd+Shift+P` (Mac) or `Ctrl+Shift+P` (Windows/Linux)
   - Type "TypeScript: Restart TS Server"
   - Press Enter

### Build Errors

**Error:** Build fails with various errors

**Solutions:**

1. **Clean build:**
   ```bash
   rm -rf .next out
   npm run build
   ```

2. **Check for missing environment variables:**
   ```bash
   # Ensure .env.local exists
   ls -la .env.local
   ```

3. **Verify all dependencies are installed:**
   ```bash
   npm install
   ```

### Tauri Build Issues

**Error:** Tauri build fails

**Solutions:**

1. **Update Rust:**
   ```bash
   rustup update
   ```

2. **Clean Rust build cache:**
   ```bash
   cd src-tauri
   cargo clean
   cd ..
   ```

3. **Rebuild:**
   ```bash
   npm run tauri:build
   ```

### Authentication Issues

**Error:** "Unauthorized" or "Authentication failed"

**Solutions:**

1. **For development mode**, ensure `.env.local` has:
   ```bash
   NEXT_PUBLIC_USER_ID=user_01h0000000000000000000000
   ```

2. **Check backend auth is disabled:**
   ```bash
   # In project root .env
   grep GATEWAY_SKIP_AUTH ../.env
   # Should show: GATEWAY_SKIP_AUTH=1
   ```

3. **Seed test API key:**
   ```bash
   cd ..
   make seed-api-key
   ```
   
   Then use: `sk_test_123456`

### SSE (Server-Sent Events) Not Working

**Error:** Real-time updates not appearing

**Solutions:**

1. **Check browser console for SSE errors**

2. **Verify Gateway supports SSE:**
   ```bash
   curl -N http://localhost:8080/api/v1/stream/sse?workflow_id=test
   ```

3. **Check CORS settings in backend**

4. **Try disabling browser extensions** (ad blockers can interfere with SSE)

## Getting Help

If you're still experiencing issues:

1. **Check logs:**
   ```bash
   # Backend logs
   cd ..
   make logs
   
   # Frontend console
   # Open browser DevTools (F12) and check Console tab
   ```

2. **Enable debug mode:**
   ```bash
   # In .env.local
   NEXT_PUBLIC_DEBUG=true
   ```

3. **Check versions:**
   ```bash
   node --version    # Should be 20+
   npm --version
   next --version
   ```

4. **Review documentation:**
   - [Configuration Guide](CONFIGURATION.md)
   - [Desktop README](README.md)
   - [Backend API Docs](../docs/api-reference.md)

## Quick Reset

If all else fails, do a complete reset:

```bash
# 1. Clean everything
rm -rf node_modules package-lock.json .next

# 2. Reinstall
npm install

# 3. Restart backend
cd ..
make down
make dev

# 4. Start frontend (without Turbopack)
cd desktop
npm run dev:webpack
```
