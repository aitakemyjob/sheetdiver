package testutil

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // database/sql driver
)

// JLCRows are stand-in jlcparts rows: lcsc, present, category,
// subcategory, mfr, package, manufacturer, library_type, preferred,
// description, datasheet, stock, price, attributes.
var JLCRows = [][]any{
	{97253, 1, "Power Management (PMIC)", "DC-DC Converters", "TPS563200DDCR", "TSOT-23-6", "Texas Instruments", "expand", 0, "3A 4.5V~17V 650kHz Buck", "https://www.ti.com/lit/gpn/tps563200", 10034, "1-9:1.0261,10-29:0.9097", `{"Output Current":"3A","Quiescent Current":"190uA"}`},
	{20917, 1, "Transistors/Thyristors", "MOSFETs", "AO3400A", "SOT-23", "Alpha Omega Semicon", "base", 0, "30V 5.7A 48mΩ@2.5V N-Channel", "https://jlcpcb.com/api/file/x", 757862, "1-49:0.0895", `{"Drain to Source Voltage":"30V"}`},
	{344010, 1, "Transistors/Thyristors", "MOSFETs", "AO3400A", "SOT-23", "BORN", "expand", 0, "30V 5.8A N-Channel", "", 78332, "1-99:0.0627", `{}`},
	{25744, 1, "Resistors", "Chip Resistor - Surface Mount", "0402WGF1002TCE", "0402", "UNI-ROYAL", "base", 0, "10kΩ ±1% 62.5mW", "javascript:alert(1)", 21489602, "1-:0.0033", `{"Resistance":"10kΩ"}`},
	{111236, 1, "", "", "TPS563200DDCT", "SOT-23-6", "", "expand", 0, "3A Buck", "https://www.ti.com/lit/gpn/tps563200", 0, "", `not json`},
	{999, 0, "", "", "GONE100", "", "", "expand", 0, "100% gone_part", "", 5, "", ""},
}

// JLCDatabase writes a database in the jlcparts format (with the given
// format tag) in dir and returns its bytes.
func JLCDatabase(dir, format string, rows [][]any) ([]byte, error) {
	path := filepath.Join(dir, "cache.sqlite3")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY NOT NULL, value TEXT NOT NULL)`,
		`CREATE TABLE jlc_components (lcsc INTEGER PRIMARY KEY NOT NULL, present INTEGER NOT NULL, category TEXT NOT NULL, subcategory TEXT NOT NULL,
			mfr TEXT NOT NULL, package TEXT NOT NULL, manufacturer TEXT NOT NULL, library_type TEXT NOT NULL, preferred INTEGER NOT NULL,
			description TEXT NOT NULL, datasheet TEXT NOT NULL, stock INTEGER NOT NULL, price TEXT NOT NULL, attributes TEXT NOT NULL)`,
		fmt.Sprintf(`INSERT INTO meta VALUES ('format', '%s')`, format),
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, err
		}
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO jlc_components VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r...); err != nil {
			return nil, err
		}
	}
	if err := db.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// SplitZip zips data as cache.sqlite3 and splits it like `zip -s`: a split
// marker, then cache.z01, cache.z02… of n bytes, and cache.zip last.
func SplitZip(data []byte, n int) (map[string][]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("PK\x07\x08")
	zw := zip.NewWriter(&buf)
	zw.SetOffset(4)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "cache.sqlite3", Method: zip.Deflate})
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	all := buf.Bytes()
	files := map[string][]byte{}
	for i := 1; len(all) > n; i++ {
		files[fmt.Sprintf("cache.z%02d", i)] = all[:n]
		all = all[n:]
	}
	files["cache.zip"] = all
	return files, nil
}
