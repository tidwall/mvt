// Copyright (c) 2018, Joshua J Baker. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mbdraw

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Tile represents a Mapbox Vector Tile
type Tile struct {
	layers []Layer
}

// Layer represents a layer
type Layer struct {
	name     string
	features []Feature
}

// AddLayer adds a layer
func (t *Tile) AddLayer(name string) *Layer {
	t.layers = append(t.layers, Layer{name: name})
	return &t.layers[len(t.layers)-1]
}

// GeometryType represents geometry type
type GeometryType byte

const (
	// Unknown is an unknown geometry type
	Unknown GeometryType = 0
	// Point is a point
	Point GeometryType = 1
	// LineString is a line string
	LineString GeometryType = 2
	// Polygon is a polygon
	Polygon GeometryType = 3
)

type tag struct {
	key   string
	value interface{}
}

const (
	moveTo    = 1
	lineTo    = 2
	closePath = 7
)

type command struct {
	which int
	x, y  float64
}

// Feature represents a feature
type Feature struct {
	geomType GeometryType
	id       uint64
	hasID    bool
	tags     []tag
	geometry []command
}

// AddFeature add a geometry feature
func (l *Layer) AddFeature(geomType GeometryType) *Feature {
	l.features = append(l.features, Feature{geomType: geomType})
	return &l.features[len(l.features)-1]
}

// SetID set the id
func (f *Feature) SetID(id uint64) {
	f.id = id
	f.hasID = true
}

// AddTag adds a tag
func (f *Feature) AddTag(key string, value interface{}) {
	f.tags = append(f.tags, tag{key, value})
}

// MoveTo move to a point. The tile is 256x256.
func (f *Feature) MoveTo(x, y float64) {
	f.geometry = append(f.geometry, command{moveTo, x, y})
}

// LineTo draws a line to a point. The tile is 256x256.
func (f *Feature) LineTo(x, y float64) {
	f.geometry = append(f.geometry, command{lineTo, x, y})
}

// ClosePath closes a path
func (f *Feature) ClosePath() {
	f.geometry = append(f.geometry, command{closePath, 0, 0})
}

// Render renders the tile to a protobuf file for displaying on a map.
func (t *Tile) Render() []byte {
	var pb []byte
	for _, layer := range t.layers {
		pb = layer.append(pb)
	}
	return pb

}
func (l *Layer) append(vpb []byte) []byte {
	var pb []byte
	pb = append(pb, 10)

	// collect and encode tags
	var ekeys []string
	var evals []string
	var keysidxs []int
	var valsidxs []int
	keym := make(map[string]int)
	valm := make(map[string]int)
	var keyidx int
	var validx int
	for _, feature := range l.features {
		for _, tag := range feature.tags {
			ekey := encodeKey(tag.key)
			eval := encodeValue(tag.value)
			idx, ok := keym[ekey]
			if !ok {
				ekeys = append(ekeys, ekey)
				idx = keyidx
				keyidx++
				keym[ekey] = idx
			}
			keysidxs = append(keysidxs, idx)
			idx, ok = valm[eval]
			if !ok {
				evals = append(evals, eval)
				idx = validx
				validx++
				valm[eval] = idx
			}
			valsidxs = append(valsidxs, idx)
		}
	}

	pb = appendUvarint(pb, uint64(len(l.name)))
	pb = append(pb, l.name...)
	for _, feature := range l.features {
		pb = feature.append(pb, keysidxs, valsidxs)
	}
	for _, ekey := range ekeys {
		pb = append(pb, ekey...)
	}
	for _, eval := range evals {
		pb = append(pb, eval...)
	}
	pb = append(pb, 120)
	pb = append(pb, 2)

	// add the size to the beginning
	vpb = append(vpb, 26)
	vpb = appendUvarint(vpb, uint64(len(pb)))
	vpb = append(vpb, pb...)
	return vpb
}

func (f *Feature) append(
	vpb []byte, keysidxs, valsidxs []int,
) []byte {
	var pb []byte
	if f.hasID {
		pb = append(pb, 8)
		pb = appendUvarint(pb, f.id)
	}

	if len(f.tags) > 0 {
		pb = append(pb, 18)
		pb = appendUvarint(pb, uint64(len(f.tags)*2))
		for i := range f.tags {
			pb = appendUvarint(pb, uint64(keysidxs[i]))
			pb = appendUvarint(pb, uint64(valsidxs[i]))
		}
	}

	switch f.geomType {
	default:
		pb = append(pb, 24)
		pb = appendUvarint(pb, uint64(f.geomType))
	case Unknown:
		// optional
	}

	if len(f.geometry) > 0 {
		var gpb []byte
		var lastx, lasty int64
		var total int
		if f.geometry[0].which != moveTo {
			gpb = appendUvarint(gpb, uint64(commandInteger(moveTo, 1)))
			gpb = appendVarint(gpb, 0)
			gpb = appendVarint(gpb, 0)
			total += 3
		}
		for i := 0; i < len(f.geometry); {
			count := 1
			which := f.geometry[i].which
			for j := i + 1; j < len(f.geometry); j++ {
				if f.geometry[j].which != which {
					break
				}
				count++
			}
			gpb = appendUvarint(gpb, uint64(commandInteger(which, count)))
			total++
			switch which {
			default:
				i++
			case moveTo, lineTo:
				for j := 0; j < count; j++ {
					x := int64(f.geometry[i+j].x / 256.0 * 4096.0)
					y := int64(f.geometry[i+j].y / 256.0 * 4096.0)
					relx, rely := x-lastx, y-lasty
					lastx, lasty = x, y
					gpb = appendVarint(gpb, relx)
					gpb = appendVarint(gpb, rely)
					total += 2
				}
				i += count
			}
		}
		pb = append(pb, 34)
		pb = appendUvarint(pb, uint64(len(gpb)))
		pb = append(pb, gpb...)

	}

	// add the size to the beginning
	vpb = append(vpb, 18)
	vpb = appendUvarint(vpb, uint64(len(pb)))
	vpb = append(vpb, pb...)
	return vpb
}

func commandInteger(id, count int) uint32 {
	return uint32((id & 0x7) | (count << 3))
}

func encodeKey(key string) string {
	var pb []byte
	pb = append(pb, 26)
	pb = appendString(pb, key)
	return string(pb)
}
func encodeValue(v interface{}) string {
	var vpb []byte
	switch v := v.(type) {
	case string:
		vpb = append(append(vpb, 10), appendString(nil, v)...)
	case uint64:
		vpb = append(append(vpb, 40), appendUvarint(nil, v)...)
	case float32:
		vpb = append(vpb, 21, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(vpb[1:], math.Float32bits(v))
	case float64:
		vpb = append(vpb, 25, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(vpb[1:], math.Float64bits(v))
	case int64:
		vpb = appendVarint(append(vpb, 48), v)
	case bool:
		if v {
			vpb = append(vpb, 56, 1)
		} else {
			vpb = append(vpb, 56, 0)
		}
	case uint8:
		return encodeValue(uint64(v))
	case uint16:
		return encodeValue(uint64(v))
	case uint32:
		return encodeValue(uint64(v))
	case int8:
		return encodeValue(int64(v))
	case int16:
		return encodeValue(int64(v))
	case int32:
		return encodeValue(int64(v))
	case []byte:
		return encodeValue(string(v))
	default:
		return encodeValue(fmt.Sprintf("%v", v))
	}
	var pb []byte
	pb = append(pb, 34)
	pb = appendUvarint(pb, uint64(len(vpb)))
	pb = append(pb, vpb...)
	return string(pb)
}

func appendString(pb []byte, s string) []byte {
	pb = appendUvarint(pb, uint64(len(s)))
	return append(pb, s...)
}
func appendUvarint(pb []byte, n uint64) []byte {
	var buf [32]byte
	sz := binary.PutUvarint(buf[:], uint64(n))
	return append(pb, buf[:sz]...)
}
func appendVarint(pb []byte, n int64) []byte {
	var buf [32]byte
	sz := binary.PutVarint(buf[:], int64(n))
	return append(pb, buf[:sz]...)
}
func quadratic(x0, y0, x1, y1, x2, y2, t float64) (x, y float64) {
	u := 1 - t
	a := u * u
	b := 2 * u * t
	c := t * t
	x = a*x0 + b*x1 + c*x2
	y = a*y0 + b*y1 + c*y2
	return
}

// QuadraticTo draw a quadratic curve
func (f *Feature) QuadraticTo(x1, y1, x2, y2 float64) {
	var x0, y0 float64
	if len(f.geometry) > 0 {
		x0 = f.geometry[len(f.geometry)-1].x
		y0 = f.geometry[len(f.geometry)-1].y
	}
	l := (math.Hypot(x1-x0, y1-y0) +
		math.Hypot(x2-x1, y2-y1))
	n := int(l + 0.5)
	if n < 4 {
		n = 4
	}
	d := float64(n) - 1
	for i := 0; i < n; i++ {
		t := float64(i) / d
		f.LineTo(quadratic(x0, y0, x1, y1, x2, y2, t))
	}
}

func cubic(x0, y0, x1, y1, x2, y2, x3, y3, t float64) (x, y float64) {
	u := 1 - t
	a := u * u * u
	b := 3 * u * u * t
	c := 3 * u * t * t
	d := t * t * t
	x = a*x0 + b*x1 + c*x2 + d*x3
	y = a*y0 + b*y1 + c*y2 + d*y3
	return
}

// CubicTo draw a cubic curve
func (f *Feature) CubicTo(x1, y1, x2, y2, x3, y3 float64) {
	var x0, y0 float64
	if len(f.geometry) > 0 {
		x0 = f.geometry[len(f.geometry)-1].x
		y0 = f.geometry[len(f.geometry)-1].y
	}
	l := (math.Hypot(x1-x0, y1-y0) +
		math.Hypot(x2-x1, y2-y1) +
		math.Hypot(x3-x2, y3-y2))
	n := int(l + 0.5)
	if n < 4 {
		n = 4
	}
	d := float64(n) - 1
	for i := 0; i < n; i++ {
		t := float64(i) / d
		f.LineTo(cubic(x0, y0, x1, y1, x2, y2, x3, y3, t))
	}
}
