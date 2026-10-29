package spice

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Reading ngspice's raw files: one plot after another, each a text
// header (its name, whether its values are complex, its vectors and how
// many points) and its values, in binary (doubles, little-endian, point
// by point) or as text.

// Vector is one signal of a plot: its name ("v(out)", "time"), its
// kind ("voltage", "current", "time", "frequency"), and its values (Im
// only for a complex plot).
type Vector struct {
	Name string    `json:"name"`
	Kind string    `json:"kind"`
	Re   []float64 `json:"-"`
	Im   []float64 `json:"-"`
}

// Plot is one analysis's results: its name ("Transient Analysis"),
// whether its values are complex (AC), and its vectors, the first being
// the scale (time, frequency, the swept source).
type Plot struct {
	// ID is the analysis it's of, as ngspice names its plots: tran1,
	// ac1, op1, and noise1 and noise2 for a noise analysis.
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Complex bool     `json:"complex"`
	Points  int      `json:"points"`
	Vectors []Vector `json:"vectors"`
}

// ReadRaw reads every plot in a raw file.
func ReadRaw(data []byte) ([]Plot, error) {
	r := bufio.NewReader(bytes.NewReader(data))
	var plots []Plot
	for {
		p, err := readPlot(r)
		if err == io.EOF {
			return plots, nil
		}
		if err != nil {
			return plots, err
		}
		plots = append(plots, p)
	}
}

// readPlot reads one plot's header and values.
func readPlot(r *bufio.Reader) (Plot, error) {
	var p Plot
	nvars := 0
	sawHeader := false
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF && line == "" {
			if sawHeader {
				return p, io.ErrUnexpectedEOF
			}
			return p, io.EOF
		}
		if err != nil && err != io.EOF {
			return p, err
		}
		line = strings.TrimRight(line, "\r\n")
		key, val, _ := strings.Cut(line, ":")
		val = strings.TrimSpace(val)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "title", "date", "command", "option":
			sawHeader = true
		case "plotname":
			sawHeader, p.Name = true, val
		case "flags":
			p.Complex = strings.Contains(strings.ToLower(val), "complex")
		case "no. variables":
			nvars, _ = strconv.Atoi(val)
		case "no. points":
			p.Points, _ = strconv.Atoi(val)
		case "variables":
			for i := 0; i < nvars; i++ {
				vl, err := r.ReadString('\n')
				if err != nil {
					return p, fmt.Errorf("reading the vectors: %w", err)
				}
				f := strings.Fields(vl)
				if len(f) < 3 {
					return p, fmt.Errorf("vector line %q", strings.TrimSpace(vl))
				}
				p.Vectors = append(p.Vectors, Vector{Name: f[1], Kind: f[2]})
			}
		case "binary":
			return p, readBinary(r, &p)
		case "values":
			return p, readValues(r, &p)
		}
	}
}

// maxRawPoints caps what's read, against a corrupt header.
const maxRawPoints = 50_000_000

// readBinary reads a plot's values as doubles, point by point.
func readBinary(r *bufio.Reader, p *Plot) error {
	if p.Points*len(p.Vectors) > maxRawPoints {
		return fmt.Errorf("%d points of %d vectors is too many", p.Points, len(p.Vectors))
	}
	per := 1
	if p.Complex {
		per = 2
	}
	buf := make([]byte, 8*per*len(p.Vectors))
	for i := range p.Vectors {
		p.Vectors[i].Re = make([]float64, 0, p.Points)
		if p.Complex {
			p.Vectors[i].Im = make([]float64, 0, p.Points)
		}
	}
	for pt := 0; pt < p.Points; pt++ {
		if _, err := io.ReadFull(r, buf); err != nil {
			return fmt.Errorf("point %d of %d: %w", pt+1, p.Points, err)
		}
		for i := range p.Vectors {
			off := 8 * per * i
			p.Vectors[i].Re = append(p.Vectors[i].Re, math.Float64frombits(binary.LittleEndian.Uint64(buf[off:])))
			if p.Complex {
				p.Vectors[i].Im = append(p.Vectors[i].Im, math.Float64frombits(binary.LittleEndian.Uint64(buf[off+8:])))
			}
		}
	}
	// Plots written one after another may have a line break between.
	if b, err := r.Peek(1); err == nil && b[0] == '\n' {
		_, _ = r.ReadByte()
	}
	return nil
}

// readValues reads a plot's values written as text: each point's
// index and first value on a line, its other values on lines of their
// own; a complex value is "re,im".
func readValues(r *bufio.Reader, p *Plot) error {
	for pt := 0; pt < p.Points; pt++ {
		for i := 0; i < len(p.Vectors); {
			line, err := r.ReadString('\n')
			if err != nil && line == "" {
				return fmt.Errorf("point %d: %w", pt+1, err)
			}
			f := strings.Fields(line)
			if len(f) == 0 {
				continue // a blank line between points
			}
			v := f[len(f)-1]
			re, im, _ := strings.Cut(v, ",")
			x, _ := strconv.ParseFloat(re, 64)
			p.Vectors[i].Re = append(p.Vectors[i].Re, x)
			if p.Complex {
				y, _ := strconv.ParseFloat(im, 64)
				p.Vectors[i].Im = append(p.Vectors[i].Im, y)
			}
			i++
		}
	}
	return nil
}
