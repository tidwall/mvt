// Copyright (c) 2018, Joshua J Baker. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mvt

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Tile represents a Mapbox Vector Tile
type Tile struct {
	layers []*Layer
}

// Layer represents a layer
type Layer struct {
	name      string
	features  []*Feature
	extent    uint32
	hasExtent bool
}

// SetExtent sets the layers extent. Default is 4096.
func (l *Layer) SetExtent(extent uint32) {
	l.extent = extent
	l.hasExtent = true
}

// AddLayer adds a layer
func (t *Tile) AddLayer(name string) *Layer {
	t.layers = append(t.layers, &Layer{name: name})
	return t.layers[len(t.layers)-1]
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
	key string
	val interface{}
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
	l.features = append(l.features, &Feature{geomType: geomType})
	return l.features[len(l.features)-1]
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

// MoveTo moves to a point. The tile is 256x256.
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

func (l *Layer) collectTags() (
	keysa, valsa []string,
	tagidxs []int,
) {
	var keyidx, validx int
	keys := make(map[string]int)
	vals := make(map[string]int)
	for _, feature := range l.features {
		for _, tag := range feature.tags {
			key := encodeKey(tag.key)
			if idx, ok := keys[key]; !ok {
				tagidxs = append(tagidxs, keyidx)
				keys[key] = keyidx
				keyidx++
				keysa = append(keysa, key)
			} else {
				tagidxs = append(tagidxs, idx)
			}
			val := encodeValue(tag.val)
			if idx, ok := vals[val]; !ok {
				tagidxs = append(tagidxs, validx)
				vals[val] = validx
				validx++
				valsa = append(valsa, val)
			} else {
				tagidxs = append(tagidxs, idx)
			}
		}
	}
	return
}

func commandXY(cmd *command, extent float64) (int64, int64) {
	x := cmd.x / 256.0 * extent
	y := cmd.y / 256.0 * extent
	min := 0 - extent*0.10
	max := extent + extent*0.10
	if x < min {
		x = min
	} else if x > max {
		x = max
	}
	if y < min {
		y = min
	} else if y > max {
		y = max
	}
	return int64(x), int64(y)
}

func (l *Layer) append(vpb []byte) []byte {
	var pb []byte
	keysa, valsa, tagidxs := l.collectTags()

	if len(l.name) > 0 {
		pb = append(pb, 10)
		pb = appendUvarint(pb, uint64(len(l.name)))
		pb = append(pb, l.name...)
	}
	var extent float64 = 4096
	if l.hasExtent {
		extent = float64(l.extent)
	}
	for _, feature := range l.features {
		pb, tagidxs = feature.append(pb, tagidxs, extent)
	}
	for _, v := range keysa {
		pb = append(pb, v...)
	}
	for _, v := range valsa {
		pb = append(pb, v...)
	}

	// add extent
	pb = append(pb, 40)
	pb = appendUvarint(pb, uint64(extent))

	// add version
	pb = append(pb, 120, 2)

	// add the size to the beginning
	vpb = append(vpb, 26)
	vpb = appendUvarint(vpb, uint64(len(pb)))
	vpb = append(vpb, pb...)
	return vpb
}

func (f *Feature) append(
	vpb []byte, tagidxs []int, extent float64,
) ([]byte, []int) {
	var pb []byte
	if f.hasID {
		pb = append(pb, 8)
		pb = appendUvarint(pb, f.id)
	}

	if len(f.tags) > 0 {
		var tpb = make([]byte, 0, len(f.tags)*2)
		for range f.tags {
			tpb = appendUvarint(tpb, uint64(tagidxs[0]))
			tpb = appendUvarint(tpb, uint64(tagidxs[1]))
			tagidxs = tagidxs[2:]
		}
		pb = append(pb, 18)
		pb = appendUvarint(pb, uint64(len(tpb)))
		pb = append(pb, tpb...)
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
		var hasMoveTo bool
		var lastx, lasty int64
		for i := 0; i < len(f.geometry); i++ {
			switch f.geometry[i].which {
			case closePath:
				gpb = appendUvarint(gpb, uint64(commandInteger(closePath, 1)))
				hasMoveTo = false
			case moveTo:
				gpb = appendUvarint(gpb, uint64(commandInteger(moveTo, 1)))
				x, y := commandXY(&f.geometry[i], extent)
				gpb = appendVarint(gpb, x)
				gpb = appendVarint(gpb, y)
				lastx, lasty = x, y
				hasMoveTo = true
			case lineTo:
				if !hasMoveTo {
					// Move to 0x0 to make this geometry valid
					gpb = appendUvarint(gpb, uint64(commandInteger(moveTo, 1)))
					gpb = appendVarint(gpb, 0)
					gpb = appendVarint(gpb, 0)
					lastx, lasty = 0, 0
					hasMoveTo = true
				}
				var tbuf []byte
				var count int
				for ; i < len(f.geometry); i++ {
					if f.geometry[i].which != lineTo {
						break
					}
					x, y := commandXY(&f.geometry[i], extent)
					relx, rely := x-lastx, y-lasty
					if relx == 0 && rely == 0 {
						continue
					}
					lastx, lasty = x, y

					tbuf = appendVarint(tbuf, relx)
					tbuf = appendVarint(tbuf, rely)
					count++
				}
				gpb = appendUvarint(gpb, uint64(commandInteger(lineTo, count)))
				gpb = append(gpb, tbuf...)
				i--
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
	return vpb, tagidxs
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
	vpb := append(pb, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	sz := binary.PutUvarint(vpb[len(pb):], n)
	return vpb[:len(pb)+sz]
}
func appendVarint(pb []byte, n int64) []byte {
	vpb := append(pb, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	sz := binary.PutVarint(vpb[len(pb):], n)
	return vpb[:len(pb)+sz]
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

const (
	gMinLat   = -85.05112878
	gMaxLat   = 85.05112878
	gMinLon   = -180.0
	gMaxLon   = 180.0
	gTileSize = 256
)

// LatLonXY converts a lat/lon to an point x/y for the specified map tile.
func LatLonXY(lat, lon float64, tileX, tileY, tileZ int) (x, y float64) {
	lat = clamp(lat, gMinLat, gMaxLat)
	lon = clamp(lon, gMinLon, gMaxLon)
	lx := (lon + 180) / 360
	sinLat := math.Sin(lat * math.Pi / 180)
	ly := 0.5 - math.Log((1+sinLat)/(1-sinLat))/(4*math.Pi)
	mapSize := float64(uint64(256) << uint(tileZ))
	pixelX := clamp(lx*mapSize+0, 0, mapSize)
	pixelY := clamp(ly*mapSize+0, 0, mapSize)
	return pixelX - float64(tileX<<8), pixelY - float64(tileY<<8)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func tileXYToPixelXY(tileX, tileY int) (pixelX, pixelY int) {
	return tileX << 8, tileY << 8
}

func gMapSize(levelOfDetail int) uint64 {
	return gTileSize << levelOfDetail
}

func pixelXYToLatLon(pixelX, pixelY, levelOfDetail int) (lat, lon float64) {
	mapSize := float64(gMapSize(levelOfDetail))
	x := (clamp(float64(pixelX), 0, mapSize-1) / mapSize) - 0.5
	y := 0.5 - (clamp(float64(pixelY), 0, mapSize-1) / mapSize)
	lat = 90 - 360*math.Atan(math.Exp(-y*2*math.Pi))/math.Pi
	lon = 360 * x
	return
}

// TileBounds returns the lat/lon bounds around a tile.
func TileBounds(tileX, tileY, tileZ int,
) (minLat, minLon, maxLat, maxLon float64) {
	levelOfDetail := tileZ
	size := int(1 << levelOfDetail)
	pixelX, pixelY := tileXYToPixelXY(tileX, tileY)
	maxLat, minLon = pixelXYToLatLon(pixelX, pixelY, levelOfDetail)
	pixelX, pixelY = tileXYToPixelXY(tileX+1, tileY+1)
	minLat, maxLon = pixelXYToLatLon(pixelX, pixelY, levelOfDetail)
	if size == 0 || tileX%size == 0 {
		minLon = gMinLon
	}
	if size == 0 || tileX%size == size-1 {
		maxLon = gMaxLon
	}
	if tileY <= 0 {
		maxLat = gMaxLat
	}
	if tileY >= size-1 {
		minLat = gMinLat
	}
	return
}
