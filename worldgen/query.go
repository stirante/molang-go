package worldgen

import "github.com/stirante/molang-go/eval"

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

// BiomeTagFuncs returns the world_gen biome tag queries: query.has_biome_tag,
// query.has_any_biome_tags and query.has_all_biome_tags. Molang has no string
// type — a call like query.has_biome_tag('forest') arrives with 'forest'
// already reduced to its interned numeric id (see eval.InternString) — so
// hasTag receives that raw numeric argument value and decides membership;
// this package stays decoupled from any particular tag-set representation.
//
//   - has_biome_tag takes one tag, and exactly 1, 3 or 4 arguments: (tag),
//     (tag, x, z) and (tag, x, y, z). Any other count answers 0, two included.
//     The positional forms ask about another block in the game; hasTag has no
//     position, so here they ask about the same biome as the one-argument form.
//   - has_any_biome_tags / has_all_biome_tags take any number of tags and
//     always ask about the origin. With no tags at all both answer 0 — the
//     game checks for an empty list before it looks at the biome.
//
// query.any_tag and query.all_tags are not here: they are the block and item
// descriptor tag queries, a different query set that worldgen Molang does not
// resolve.
func BiomeTagFuncs(hasTag func(argValue float64) bool) map[string]eval.QueryFunc {
	has := func(args []float64, _ *eval.Context) float64 {
		switch len(args) {
		case 1, 3, 4:
			if hasTag(args[0]) {
				return 1
			}
		}
		return 0
	}
	any := func(args []float64, _ *eval.Context) float64 {
		for _, a := range args {
			if hasTag(a) {
				return 1
			}
		}
		return 0
	}
	all := func(args []float64, _ *eval.Context) float64 {
		if len(args) == 0 {
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
		"has_biome_tag":      has,
		"has_any_biome_tags": any,
		"has_all_biome_tags": all,
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

// QueryNames is the game's world_gen query set: every query worldgen Molang
// (features, feature rules, biome surface adjustments) resolves, and nothing
// else. The game resolves query names while it parses, against the set the
// field allows, so a name outside this list is not a query that answers 0 —
// the expression fails to parse. Register implements all six.
var QueryNames = []string{
	"above_top_solid",
	"has_all_biome_tags",
	"has_any_biome_tags",
	"has_biome_tag",
	"heightmap",
	"noise",
}

// IsQuery reports whether query.<name> is in the world_gen set. name is the
// member, lower-case, without the namespace.
func IsQuery(name string) bool {
	for _, n := range QueryNames {
		if n == name {
			return true
		}
	}
	return false
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
