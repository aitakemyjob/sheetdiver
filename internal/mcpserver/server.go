// Package mcpserver exposes SheetDiver to coding agents over the Model
// Context Protocol. Agents search the datasheet library, read sections,
// parameters, pins and registers, look at figures and pages as images,
// record what they learn (parameters, figure descriptions, notes), work
// with projects, and run the design calculators.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/core"
)

const instructions = `SheetDiver is the user's component datasheet library (a desktop app they may be watching).
Each datasheet is extracted into parts (manufacturer part numbers, with orderable variants and family patterns such as
STM32U545xx), sections, figures (graphs, schematics, waveforms…), spec parameters (min/typ/max with page), pins and
registers. Projects group the parts of a board with their supply rails.

- Missing a part? import_datasheet downloads it: by part_number (manufacturer URLs), url (PDF or product page),
  path, or data_base64 if you fetched the PDF yourself because the site blocks scripted downloads.
- Find things with search (full text across everything) and list_parts. Any part reference may be a base MPN, an
  orderable code (TPS563200DDCR) or a family member; SheetDiver resolves it.
- Read with get_part (overview and key parameters), get_parameters, get_section (pin description, register map,
  layout guidelines, design procedure… by kind or title), get_pins, get_registers, read_pages and get_tables.
- LOOK at graphs and schematics with get_figure / get_page_image: they return the image. Read values off graphs
  yourself; the extractor only knows captions and axes. get_tables with a table returns its image too: tables
  marked IRREGULAR (selection matrices, grouped rows) have scrambled cells, so read the image and save them
  with record_table, one row per combination.
- Extraction is mechanical and can be wrong or incomplete. When you verify or find something, record it:
  record_parameters (with page), describe_figure (what a graph shows: axes, conditions, curves), record_pins,
  record_registers, record_recommendations (inductors, capacitors, suggested part numbers) and add_note. Recorded
  data is kept when a datasheet is re-extracted and is what the calculators and project checks use.
- Projects: create_project, set_rail, add_to_project (with supply/output rails and load currents), then
  get_project for the BOM, power budget per rail (regulator input currents included) and voltage/current checks.
- Calculations: list_calculators, then calculate (buck, boost, ldo, gate_drive, gate_divider, divider). Give a part
  and SheetDiver fills inputs from its datasheet (and from its rails in a project) and says where each came from.
  Prefer these over mental arithmetic, and check the "missing" inputs it reports.
- Pass show=true (or call show) to put a part, page, figure, project or result on the user's screen.
Values are SI unless a unit is given; pages are 1-based.`

// Server is the MCP server.
type Server struct {
	core *core.Service
	srv  *mcp.Server
}

// New creates the MCP server with every tool registered and logs each
// newly connected client to the activity feed.
func New(c *core.Service) *Server {
	s := &Server{core: c}
	s.srv = mcp.NewServer(&mcp.Implementation{
		Name:    "sheetdiver",
		Title:   "SheetDiver",
		Version: c.Version,
	}, &mcp.ServerOptions{
		Instructions: instructions,
		InitializedHandler: func(ctx context.Context, req *mcp.InitializedRequest) {
			name := "agent"
			if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil && p.ClientInfo.Name != "" {
				name = p.ClientInfo.Name
			}
			c.RecordActivity(core.Activity{Tool: "connect", Summary: name + " connected"})
		},
	})
	s.registerLibrary()
	s.registerWrite()
	s.registerProjects()
	s.registerCalc()
	return s
}

// MCP returns the underlying SDK server (used by tests and the stdio mode).
func (s *Server) MCP() *mcp.Server { return s.srv }

// Handler serves the streamable HTTP transport.
func (s *Server) Handler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.srv }, &mcp.StreamableHTTPOptions{
		SessionTimeout: 30 * time.Minute,
	})
}

// Run serves MCP over stdio (used by `sheetdiver mcp --standalone`).
func (s *Server) Run(ctx context.Context) error {
	return s.srv.Run(ctx, &mcp.StdioTransport{})
}

// access is what a tool needs from the user's settings.
type access int

const (
	read access = iota
	write
	importing
)

// result is a tool's output: text, plus optional images.
type result struct {
	text   string
	images [][]byte
}

// addTool registers a tool, enforcing the access switches and recording
// every call in the activity feed so the user can see what the agent did.
func addTool[In any](s *Server, t *mcp.Tool, need access, summary func(In) string, h func(context.Context, In) (result, error)) {
	mcp.AddTool(s.srv, t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		var err error
		switch need {
		case read:
			err = s.core.AgentRead()
		case write:
			err = s.core.AgentWrite()
		case importing:
			err = s.core.AgentImport()
		}
		var out result
		if err == nil {
			out, err = h(ctx, in)
		}
		a := core.Activity{Tool: t.Name, Summary: summary(in), DurationMS: time.Since(start).Milliseconds()}
		if err != nil {
			a.Error = err.Error()
		}
		s.core.RecordActivity(a)
		if err != nil {
			if errors.Is(err, core.ErrAgentDisabled) {
				return nil, nil, fmt.Errorf("%w (the user can enable it under Settings → Agent access)", err)
			}
			return nil, nil, err
		}
		res := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out.text}}}
		for _, img := range out.images {
			res.Content = append(res.Content, &mcp.ImageContent{Data: img, MIMEType: "image/png"})
		}
		return res, nil, nil
	})
}

// text wraps plain text as a result.
func text(s string) result { return result{text: s} }

// boolPtr returns a pointer to b.
func boolPtr(b bool) *bool { return &b }

// readOnly annotates a tool that only reads.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
}

// writes annotates a tool that changes the library (never files outside
// it), idempotent when re-run with the same arguments.
func writes(title string, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: boolPtr(false), IdempotentHint: idempotent, OpenWorldHint: boolPtr(false)}
}
