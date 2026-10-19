## What does this change?

<!-- A short summary and the motivation. Link issues with "Fixes #123". -->

## How was it tested?

<!-- Commands you ran, screenshots or a short GIF for UI changes. -->

## Checklist

- [ ] New behaviour and bug fixes have tests that fail without the change
- [ ] `make test` passes (Go with `-race` + frontend typecheck/tests)
- [ ] New Go functions have doc comments; new TS functions/components have JSDoc
- [ ] UI changes follow the flat style (no gradients/glows) and work in a light and a dark theme
- [ ] New or changed MCP tools are listed in `docs/MCP.md` and covered in `internal/mcpserver/mcpserver_test.go`
- [ ] Go ↔ TypeScript JSON types updated together (`frontend/src/lib/types.ts`), lists never serialize as `null`
- [ ] Docs updated (README / docs/) where behaviour changed
