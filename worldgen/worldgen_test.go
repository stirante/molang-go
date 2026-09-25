package worldgen_test

import (
	"math"
	"testing"

	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/worldgen"
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
	if got := fns["has_any_biome_tags"]([]float64{desertID, forestID}, nil); got != 1 {
		t.Errorf("has_any_biome_tags(desert,forest) = %v, want 1", got)
	}
	if got := fns["has_all_biome_tags"]([]float64{desertID, forestID}, nil); got != 0 {
		t.Errorf("has_all_biome_tags(desert,forest) = %v, want 0", got)
	}
	if got := fns["has_all_biome_tags"]([]float64{forestID, forestID}, nil); got != 1 {
		t.Errorf("has_all_biome_tags(forest,forest) = %v, want 1", got)
	}
	// No tags: 0 for both, not the vacuous "all of nothing" 1.
	if got := fns["has_all_biome_tags"](nil, nil); got != 0 {
		t.Errorf("has_all_biome_tags() = %v, want 0", got)
	}
	if got := fns["has_any_biome_tags"](nil, nil); got != 0 {
		t.Errorf("has_any_biome_tags() = %v, want 0", got)
	}
	// has_biome_tag: 1, 3 or 4 arguments; 2 is not a form it accepts.
	for n, want := range map[int]float64{1: 1, 2: 0, 3: 1, 4: 1, 5: 0} {
		args := make([]float64, n)
		args[0] = forestID
		if got := fns["has_biome_tag"](args, nil); got != want {
			t.Errorf("has_biome_tag with %d arguments = %v, want %v", n, got, want)
		}
	}
	// The descriptor tag queries belong to another query set.
	for _, name := range []string{"any_tag", "all_tags"} {
		if fns[name] != nil || worldgen.IsQuery(name) {
			t.Errorf("%s is registered as a world_gen query", name)
		}
	}
}

func TestRegisterCoversQueryNames(t *testing.T) {
	dst := map[string]eval.QueryFunc{}
	worldgen.Register(dst, func(float64) bool { return false }, nil)
	if len(dst) != len(worldgen.QueryNames) {
		t.Fatalf("Register filled %d queries, QueryNames lists %d", len(dst), len(worldgen.QueryNames))
	}
	for _, name := range worldgen.QueryNames {
		if dst[name] == nil || !worldgen.IsQuery(name) {
			t.Errorf("query.%s is listed but not registered", name)
		}
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
