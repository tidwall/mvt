// Copyright (c) 2018, Joshua J Baker. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mbdraw

import (
	"fmt"
	"testing"
)

func TestDraw(t *testing.T) {
	var tile Tile
	layer := tile.AddLayer("make love to magical numbers")
	feature := layer.AddFeature(LineString)
	feature.AddTag("anti", "freeze")
	feature.AddTag("should be consumed", false)
	feature.SetID(100)
	feature.LineTo(256, 256)

	pb := tile.Render()

	if fmt.Sprintf("%v", pb) != "[26 94 10 28 109 97 107 101 32 108 111 118 101 32 116 111 32 109 97 103 105 99 97 108 32 110 117 109 98 101 114 115 18 20 8 100 18 4 0 0 1 1 24 2 34 8 9 0 0 10 128 64 128 64 26 4 97 110 116 105 26 18 115 104 111 117 108 100 32 98 101 32 99 111 110 115 117 109 101 100 34 8 10 6 102 114 101 101 122 101 34 2 56 0 120 2]" {
		t.Fatal("fatal bad no no")
	}
}
