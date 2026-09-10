package worldgen_test

import (
	"math"
	"testing"

	"molang-go/eval"
	"molang-go/worldgen"
)

func TestNoiseIsDeterministic(t *testing.T) {
	a := worldgen.Noise(12.5, -7.25)
	b := worldgen.Noise(12.5, -7.25)
	if a != b {
		t.Fatalf("Noise not deterministic: %v vs %v", a, b)
	}
	if math.IsNaN(a) || math.IsInf(a, 0) {
		t.Fatalf("Noise produced non-finite value: %v", a)
	}
	c := worldgen.Noise(0, 0)
	if c == a {
		t.Fatalf("Noise(0,0) unexpectedly equals Noise(12.5,-7.25)")
	}
}

func TestBiomeTagFuncs(t *testing.T) {
	forestID := eval.InternString("forest")
	desertID := eval.InternString("desert")
	hasTag := func(v float64) bool { return v == forestID }

	fns := worldgen.BiomeTagFuncs(hasTag)
	if got := fns["has_biome_tag"]([]float64{forestID}, nil); got != 1 {
		t.Errorf("has_biome_tag(forest) = %v, want 1", got)
	}
	if got := fns["has_biome_tag"]([]float64{desertID}, nil); got != 0 {
		t.Errorf("has_biome_tag(desert) = %v, want 0", got)
	}
	if got := fns["any_tag"]([]float64{desertID, forestID}, nil); got != 1 {
		t.Errorf("any_tag(desert,forest) = %v, want 1", got)
	}
	if got := fns["all_tags"]([]float64{desertID, forestID}, nil); got != 0 {
		t.Errorf("all_tags(desert,forest) = %v, want 0", got)
	}
}

type constHeights float64

func (h constHeights) HeightmapAt(x, z float64) float64     { return float64(h) }
func (h constHeights) AboveTopSolidAt(x, z float64) float64 { return float64(h) }

func TestHeightFuncs(t *testing.T) {
	fns := worldgen.HeightFuncs(constHeights(64))
	if got := fns["heightmap"]([]float64{1, 2}, nil); got != 64 {
		t.Errorf("heightmap = %v, want 64", got)
	}
	if got := fns["above_top_solid"]([]float64{1, 2}, nil); got != 64 {
		t.Errorf("above_top_solid = %v, want 64", got)
	}
	if got := fns["heightmap"]([]float64{1}, nil); got != 0 {
		t.Errorf("heightmap with wrong arity = %v, want 0", got)
	}
}
