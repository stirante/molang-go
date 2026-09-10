package worldgen

import "molang-go/eval"

// HeightSource answers query.heightmap/query.above_top_solid. A real
// worldgen tool would back this with actual column data; this package only
// defines the shape.
type HeightSource interface {
	HeightmapAt(x, z float64) float64
	AboveTopSolidAt(x, z float64) float64
}

// NoiseFunc returns the query.noise QueryFunc: exactly 2 args required
// (matching the engine's own Molang noise query, which gates on
// "vector size == 32 bytes"); anything else returns 0, the engine's own
// default.
func NoiseFunc() eval.QueryFunc {
	return func(args []float64, _ *eval.Context) float64 {
		if len(args) != 2 {
			return 0
		}
		return Noise(args[0], args[1])
	}
}

// BiomeTagFuncs returns the query.has_biome_tag/any_tag/all_tags QueryFuncs.
// Molang has no string type — a call like query.has_biome_tag('forest')
// arrives with 'forest' already reduced to its interned numeric id (see
// eval.InternString) — so hasTag receives that raw numeric argument value
// and decides membership; this package stays decoupled from any particular
// tag-set representation.
func BiomeTagFuncs(hasTag func(argValue float64) bool) map[string]eval.QueryFunc {
	has := func(args []float64, _ *eval.Context) float64 {
		if len(args) < 1 {
			return 0
		}
		if hasTag(args[0]) {
			return 1
		}
		return 0
	}
	any := func(args []float64, _ *eval.Context) float64 {
		if len(args) < 1 {
			return 0
		}
		for _, a := range args {
			if hasTag(a) {
				return 1
			}
		}
		return 0
	}
	all := func(args []float64, _ *eval.Context) float64 {
		if len(args) < 1 {
			return 0
		}
		for _, a := range args {
			if !hasTag(a) {
				return 0
			}
		}
		return 1
	}
	return map[string]eval.QueryFunc{
		"has_biome_tag": has,
		"any_tag":       any,
		"all_tags":      all,
	}
}

// HeightFuncs returns the query.heightmap/query.above_top_solid QueryFuncs.
// Both require exactly 2 args (x, z), matching the "vector size == 32
// bytes" gate confirmed for both the engine's heightmap query and its
// above-top-solid query; anything else (or a nil src) returns 0.
func HeightFuncs(src HeightSource) map[string]eval.QueryFunc {
	heightmap := func(args []float64, _ *eval.Context) float64 {
		if len(args) != 2 || src == nil {
			return 0
		}
		return src.HeightmapAt(args[0], args[1])
	}
	aboveTopSolid := func(args []float64, _ *eval.Context) float64 {
		if len(args) != 2 || src == nil {
			return 0
		}
		return src.AboveTopSolidAt(args[0], args[1])
	}
	return map[string]eval.QueryFunc{
		"heightmap":       heightmap,
		"above_top_solid": aboveTopSolid,
	}
}

// Register fills dst with every world_gen query this package implements.
// dst is typically eval.Context.QueryFuncs.
func Register(dst map[string]eval.QueryFunc, hasTag func(argValue float64) bool, heights HeightSource) {
	dst["noise"] = NoiseFunc()
	for k, v := range BiomeTagFuncs(hasTag) {
		dst[k] = v
	}
	for k, v := range HeightFuncs(heights) {
		dst[k] = v
	}
}
