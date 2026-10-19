# Security policy

SheetDiver parses untrusted PDFs, downloads datasheets from the web and
exposes its library to AI agents, so we take security reports seriously.

## Reporting a vulnerability

**Please don't open a public issue.** Report privately through
[GitHub's private vulnerability reporting](https://github.com/aitakemyjob/sheetdiver/security/advisories/new).
Include what you found, how to reproduce it, and the impact you expect.

You can expect an acknowledgement within **3 business days** and a plan or
fix timeline within **10 business days**. We'll credit you in the advisory
unless you'd rather stay anonymous.

## Supported versions

Security fixes go into the latest release. Please upgrade before reporting.

## Scope

Especially interesting:

- A crafted PDF that escapes the PDF engine (PDFium runs as WebAssembly in
  wazero with no filesystem access) or makes import hang or exhaust memory
- Reading or writing files outside the library folder through the UI API or
  MCP tools (imports only read the PDF they are given)
- An agent changing the library when Settings → Agent access forbids it
- Reaching the local HTTP/MCP server from a web page (CSRF, DNS rebinding,
  CORS) or from another machine
- Script injection through agent-supplied notes, figure descriptions or
  datasheet text rendered in the UI

Out of scope: issues that require an already-compromised machine, or an
agent the user deliberately connected doing what the user asked.
