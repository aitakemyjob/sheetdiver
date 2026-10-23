package mcpbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Running reports whether a SheetDiver app answers /healthz at base.
func Running(ctx context.Context, base string) bool {
	ctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var h struct{ App string }
	return json.NewDecoder(resp.Body).Decode(&h) == nil && h.App == "sheetdiver"
}
