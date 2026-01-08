# Quick Start: Using Developer Tools in Tauri App

## Opening Developer Tools

There are two ways to open the browser developer tools in the Planet (Shannon) desktop app:

### Method 1: Menu Bar (Recommended)
1. Launch the app
2. Click **View** in the menu bar
3. Select **Developer Tools**

### Method 2: Keyboard Shortcut
- **macOS**: `Cmd + Shift + I`
- **Windows/Linux**: `Ctrl + Shift + I`

## What You Can Do with DevTools

### 1. Debug Network Requests
- Open the **Network** tab
- Submit a task in the app
- See the actual request payload being sent to the API
- Verify that `prompt` field is being sent correctly

### 2. View Console Logs
- Open the **Console** tab
- See any JavaScript errors or warnings
- View `console.log()` output from the app

### 3. Inspect React Components
- Use the **Elements** tab to inspect the DOM
- See component structure and styling
- Debug layout issues

### 4. Debug State Management
- Open **Application** → **Local Storage** to see Redux persist data
- Check **IndexedDB** for Dexie database entries
- View session storage

## Common Debugging Scenarios

### Scenario 1: Task Submission Fails
1. Open DevTools (Cmd/Ctrl + Shift + I)
2. Go to **Network** tab
3. Submit a task
4. Look for the `/api/v1/tasks` POST request
5. Check:
   - Request payload (should have `prompt` field)
   - Response status code
   - Response body for error messages

### Scenario 2: UI Not Updating
1. Open **Console** tab
2. Look for React errors or warnings
3. Check Redux state in Application → Local Storage
4. Verify WebSocket/SSE connections in Network tab

### Scenario 3: Styling Issues
1. Open **Elements** tab
2. Inspect the element with issues
3. Check computed styles
4. Test CSS changes live

## API Endpoint Reference

The app connects to the Shannon API at `http://localhost:8080` by default.

Key endpoints:
- `POST /api/v1/tasks` - Submit a new task
- `GET /api/v1/tasks/{id}` - Get task status
- `GET /api/v1/tasks/{id}/stream` - Stream task events (SSE)
- `GET /api/v1/sessions` - List sessions
- `POST /api/v1/sessions` - Create a session

## Expected Request Format

When submitting a task, the request should look like:

```json
{
  "prompt": "Your task description here",
  "task_type": "chat",
  "session_id": "optional-session-id",
  "metadata": {
    "force_research": true,
    "research_strategy": "deep"
  }
}
```

**Note**: The field is `prompt`, not `query`!

## Troubleshooting

### DevTools Won't Open
- Make sure you're running a recent version of the app
- Try the keyboard shortcut if the menu doesn't work
- Restart the app

### Can't See Network Requests
- Make sure you opened DevTools **before** submitting the task
- Check if "Preserve log" is enabled in Network tab
- Verify the app is connecting to the correct API URL

### API Returns 400 Bad Request
- Check the request payload in Network tab
- Verify all required fields are present
- Ensure `prompt` field exists (not `query`)

## Development Mode

For development with hot reload:

```bash
cd desktop
npm run tauri:dev
```

This will:
- Start Next.js dev server on port 3000
- Launch Tauri app with hot reload
- Enable all debug features
- Open DevTools automatically (in debug mode)

## Building for Production

```bash
cd desktop
npm run build
npm run tauri:build
```

Note: DevTools are still available in production builds for debugging purposes.
