package universe

// Spread is where the nth of a batch of `count` empires aims to settle. It is
// an aim and not a claim: the position it names may already be taken, and the
// caller is expected to walk on from there to the first free one.
//
// Founding a whole population in one go otherwise piles it up wherever the map
// happens to start, which is the corner the first players already occupy. The
// galaxies are cycled first, so two empires founded one after the other are
// never neighbours, and the systems of each galaxy are walked at an even
// stride so the batch spans the map instead of crowding its beginning.
//
// A batch of nobody, or a topology that bounds nothing, aims at nothing: the
// zero coordinate, which sends the caller back to the start of the map.
func Spread(limits Limits, index, count int) Coordinate {
	if limits.Galaxies <= 0 || limits.Systems <= 0 || limits.Positions <= 0 || count <= 0 {
		return Coordinate{}
	}
	if index < 0 {
		index = 0
	}
	perGalaxy := (count + limits.Galaxies - 1) / limits.Galaxies
	stride := limits.Systems / max(1, perGalaxy)
	if stride < 1 {
		stride = 1
	}
	// Half a stride of offset keeps the first aim clear of the first system,
	// which is where a universe puts whoever registered before anybody thought
	// about spreading a population out.
	system := ((index/limits.Galaxies)*stride + stride/2) % limits.Systems
	return Coordinate{
		Galaxy:   index%limits.Galaxies + 1,
		System:   system + 1,
		Position: index%limits.Positions + 1,
	}
}
