package spice

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"
)

// Series for charts: a plot's vectors as the app draws them. A complex
// (AC) vector becomes its magnitude in dB and its phase in degrees; a
// long trace is thinned to what a screen can show, keeping its shape.

// Series is one line of a chart: its name, unit and points.
type Series struct {
	Name string    `json:"name"`
	Unit string    `json:"unit"`
	X    []float64 `json:"x"`
	Y    []float64 `json:"y"`
}

// unitOf is the unit of a vector's kind.
func unitOf(kind string) string {
	switch strings.ToLower(kind) {
	case "voltage":
		return "V"
	case "current":
		return "A"
	case "time":
		return "s"
	case "frequency":
		return "Hz"
	}
	return ""
}

// Scale is a plot's first vector, its x axis: time, frequency or the
// swept value.
func (p Plot) Scale() Vector {
	if len(p.Vectors) == 0 {
		return Vector{}
	}
	return p.Vectors[0]
}

// Vector finds a plot's vector by name (ignoring case).
func (p Plot) Vector(name string) (Vector, bool) {
	for _, v := range p.Vectors {
		if strings.EqualFold(v.Name, name) {
			return v, true
		}
	}
	return Vector{}, false
}

// Series is a vector of the plot against its scale, at most max points:
// a complex vector as its magnitude (dB) or phase (°) — part "mag"
// (default) or "phase".
func (p Plot) Series(name, part string, max int) (Series, error) {
	v, ok := p.Vector(name)
	if !ok {
		var names []string
		for _, x := range p.Vectors[1:] {
			names = append(names, x.Name)
		}
		return Series{}, fmt.Errorf("%s has no %s: it has %s", p.Name, name, strings.Join(names, ", "))
	}
	x := p.Scale().Re
	y := v.Re
	unit := unitOf(v.Kind)
	if p.Complex {
		y = make([]float64, len(v.Re))
		for i := range v.Re {
			c := complex(v.Re[i], v.Im[i])
			if part == "phase" {
				y[i] = cmplx.Phase(c) * 180 / math.Pi
			} else {
				y[i] = 20 * math.Log10(math.Max(cmplx.Abs(c), 1e-300))
			}
		}
		unit = "dB"
		if part == "phase" {
			unit = "°"
		}
	}
	xs, ys := Downsample(x, y, max)
	return Series{Name: name, Unit: unit, X: xs, Y: ys}, nil
}

// Downsample thins a trace to n points by largest-triangle-three-buckets,
// which keeps its peaks and edges; a trace that fits is returned as is.
func Downsample(x, y []float64, n int) ([]float64, []float64) {
	if n < 3 || len(x) <= n || len(x) != len(y) {
		return x, y
	}
	ox, oy := []float64{x[0]}, []float64{y[0]}
	every := float64(len(x)-2) / float64(n-2)
	a := 0
	for i := 0; i < n-2; i++ {
		// The average of the next bucket.
		start, end := int(float64(i+1)*every)+1, int(float64(i+2)*every)+1
		if end > len(x) {
			end = len(x)
		}
		avgX, avgY := 0.0, 0.0
		for j := start; j < end; j++ {
			avgX += x[j]
			avgY += y[j]
		}
		if cnt := float64(end - start); cnt > 0 {
			avgX, avgY = avgX/cnt, avgY/cnt
		}
		// The point of this bucket with the largest triangle.
		from, to := int(float64(i)*every)+1, int(float64(i+1)*every)+1
		best, bestArea := from, -1.0
		for j := from; j < to && j < len(x); j++ {
			area := math.Abs((x[a]-avgX)*(y[j]-y[a]) - (x[a]-x[j])*(avgY-y[a]))
			if area > bestArea {
				best, bestArea = j, area
			}
		}
		ox, oy = append(ox, x[best]), append(oy, y[best])
		a = best
	}
	return append(ox, x[len(x)-1]), append(oy, y[len(y)-1])
}
