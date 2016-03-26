package geohash

import "math"

type Direction int

const (
	North Direction = iota
	NorthEast
	East
	SouthEast
	South
	SouthWest
	West
	NorthWest
)

type Range struct {
	Min uint64
	Max uint64
}

// full precision hash
type GeoHash interface {
	Hash() uint64
	Coordinates() (latitude, longitude float64)

	// 1..64, the higher the better
	Precision() uint
	GetInPrecision(precision uint) GeoHash

	// will return adjacent hash in the precision the hash is
	GetAdjacent(dir Direction) GeoHash

	GetNeighbors() [8]GeoHash

	GetHashRangesInside(radius float64) []Range
}

type fastGeoHash struct {
	hash                  uint64
	latBits               uint32
	lonBits               uint32
	lat                   float64
	lon                   float64
	precision             uint
	coordinatesCalculated bool
	hashCalculated        bool
	bitsCalculated        bool
}

// Return full precision hash which contains the given coordinates
func FromCoordinates(latitude, longitude float64) GeoHash {
	f := &fastGeoHash{
		lat: latitude,
		lon: longitude,
		coordinatesCalculated: true,
		hashCalculated:        false,
		bitsCalculated:        false,
	}
	return f
}

func FromHash(hash uint64, precision uint) GeoHash {
	mask := ^uint64((1 << (64 - precision)) - 1)
	f := &fastGeoHash{
		hash:                  hash & mask,
		precision:             precision,
		coordinatesCalculated: false,
		hashCalculated:        true,
		bitsCalculated:        false,
	}
	return f
}

func (f *fastGeoHash) Hash() uint64 {
	if !f.hashCalculated {
		if !f.bitsCalculated {
			f.calcBitsFromCoords()
		}
		f.hash = interleave(f.latBits, f.lonBits)
		f.hashCalculated = true
	}
	return f.hash
}

func (f *fastGeoHash) Coordinates() (float64, float64) {
	if !f.coordinatesCalculated {
		if !f.bitsCalculated {
			f.latBits, f.lonBits = deinterleave(f.hash)
			f.bitsCalculated = true
		}
		f.calcCoordsFromBits()
	}
	return f.lat, f.lon
}

func (f *fastGeoHash) Precision() uint {
	return f.precision
}

func (f *fastGeoHash) GetInPrecision(precision uint) GeoHash {
	h := f.Hash()
	return FromHash(h, precision)
}

func (f *fastGeoHash) GetAdjacent(dir Direction) GeoHash {

	// to get adjacent we need lat+lon in bit format
	if !f.bitsCalculated {
		if f.hashCalculated {
			f.latBits, f.lonBits = deinterleave(f.hash)
			f.bitsCalculated = true
		} else {
			f.calcBitsFromCoords()
		}
	}

	lonPrecision := f.precision / 2
	latPrecision := lonPrecision
	if f.precision&1 != 0 {
		// odd precision will cause latitude to be less accurate
		latPrecision -= 1
	}

	latBits := f.latBits
	lonBits := f.lonBits

	// check that we're not going over poles
	// calculate latitude
	switch dir {
	case North:
		fallthrough
	case NorthEast:
		fallthrough
	case NorthWest:
		if latBits-uint32((1<<(32-latPrecision))-1) == 0 {
			return nil
			//return nil, fmt.Errorf("Could not get next block to north, as the current one is already touching north pole")
		}
		// go one block north
		latBits += uint32(1 << (32 - latPrecision))
		break
	case South:
		fallthrough
	case SouthEast:
		fallthrough
	case SouthWest:
		if latBits == 0 {
			// same as above, but touching south pole
			return nil
			//return nil, fmt.Errorf("Could not get next block to south, as the current one is already touching south pole")
		}
		// go one block south
		latBits -= uint32(1 << (32 - latPrecision))
		break
	}

	// calculate longitude
	switch dir {
	case East:
		fallthrough
	case NorthEast:
		fallthrough
	case SouthEast:
		// go one block east
		lonBits += uint32(1 << (32 - lonPrecision))
		break
	case West:
		fallthrough
	case NorthWest:
		fallthrough
	case SouthWest:
		// go one block west
		lonBits -= uint32(1 << (32 - lonPrecision))
		break
	}

	return &fastGeoHash{
		latBits:               latBits,
		lonBits:               lonBits,
		precision:             f.precision,
		coordinatesCalculated: false,
		hashCalculated:        false,
		bitsCalculated:        true,
	}
}

func (f *fastGeoHash) GetNeighbors() [8]GeoHash {
	return [8]GeoHash{
		f.GetAdjacent(North),
		f.GetAdjacent(NorthEast),
		f.GetAdjacent(East),
		f.GetAdjacent(SouthEast),
		f.GetAdjacent(South),
		f.GetAdjacent(SouthWest),
		f.GetAdjacent(West),
		f.GetAdjacent(NorthWest),
	}
}

func (f *fastGeoHash) GetHashRangesInside(radius float64) []Range {
	lat, _ := f.Coordinates()
	bestPrecision := getProximitySearchPrecision(lat, radius)

	h := f.GetInPrecision(bestPrecision)
	neighbors := h.GetNeighbors()

	ranges := make([]Range, 1)
	min := ^uint64(math.MaxUint64 >> bestPrecision)
	max := uint64(math.MaxUint64 >> bestPrecision)
	hash := h.Hash()
	ranges[0] = Range{
		Min: hash & min,
		Max: hash | max,
	}
	for _, n := range neighbors {
		hash = n.Hash()
		r := Range{
			Min: hash & min,
			Max: hash | max,
		}
		found := false
		for i, _ := range ranges {
			if r.Max+1 == ranges[i].Min {
				// new range before i
				ranges[i].Min = r.Min
				found = true
				for j := i + 1; j < len(ranges); j++ {
					if ranges[j].Max+1 == r.Min {
						ranges[i].Min = ranges[j].Min
						ranges[j] = ranges[len(ranges)-1]
						ranges = ranges[:len(ranges)-1]
						break
					}
				}
				break
			} else if ranges[i].Max+1 == r.Min {
				// new range after i
				ranges[i].Max = r.Max
				found = true
				for j := i + 1; j < len(ranges); j++ {
					if r.Max+1 == ranges[j].Min {
						ranges[i].Max = ranges[j].Max
						ranges[j] = ranges[len(ranges)-1]
						ranges = ranges[:len(ranges)-1]
						break
					}
				}
				break
			}
		}
		if !found {
			ranges = append(ranges, r)
		}
	}

	return ranges
}

func (f *fastGeoHash) calcBitsFromCoords() {
	// 0..1
	lat_offset := (f.lat + 90.0) / 180.0
	lon_offset := (f.lon + 180.0) / 360.0

	lat_offset *= 1 << 32
	lon_offset *= 1 << 32

	f.latBits = uint32(lat_offset)
	f.lonBits = uint32(lon_offset)
	f.bitsCalculated = true
}
func (f *fastGeoHash) calcCoordsFromBits() {
	lat_offset := float64(f.latBits) + 0.5
	lon_offset := float64(f.lonBits) + 0.5

	lat_offset /= 1 << 32
	lon_offset /= 1 << 32

	f.lat = -90.0 + lat_offset*180.0
	f.lon = -180.0 + lon_offset*360.0
	f.coordinatesCalculated = true
}

// will return a precision in which a geohash will be at minimum radius x radius
// this will be useful when querying 3x3 hashes
// |--------|--------|--------|
// |        |        |        |
// |        |     / -|\       |
// |--------|--------|--------|
// |        |        |        |
// |       (|       x|       )|
// |--------|--------|--------|
// |        |        |        |
// |        |     \ _|/       |
// |--------|--------|--------|
func getProximitySearchPrecision(latitude float64, radius float64) uint {
	// in meters
	const EarthRadius = 6378137
	const LatitudeDegreeLength = EarthRadius * (math.Pi / 180.0)
	var LongitudeDegreeLength = LatitudeDegreeLength * math.Cos(latitude*math.Pi/180.0)

	requiredLatDegrees := radius / LatitudeDegreeLength
	requiredLonDegrees := radius / LongitudeDegreeLength

	// TODO: optimize
	var latPrecision uint = 0
	latDegrees := 180.0
	for i := 0; i < 32; i++ {
		if requiredLatDegrees >= latDegrees/2 {
			break
		}
		latDegrees /= 2
		latPrecision += 1
	}
	var lonPrecision uint = 0
	lonDegrees := 360.0
	for i := 0; i < 32; i++ {
		if requiredLonDegrees >= lonDegrees/2 {
			break
		}
		lonDegrees /= 2
		lonPrecision += 1
	}

	// now we need to find the best precision which is small enough to fit both requirements

	// if longitude precision is smaller, we are quite far north/south from equator
	if latPrecision < lonPrecision {
		return lonPrecision*2 - 1
	}

	return lonPrecision * 2
}

const b0 = 0x5555555555555555 // 0101 0101  0101 0101  0101 0101  0101 0101 ...
const b1 = 0x3333333333333333 // 0011 0011  0011 0011  0011 0011  0011 0011 ...
const b2 = 0x0F0F0F0F0F0F0F0F // 0000 1111  0000 1111  0000 1111  0000 1111 ...
const b3 = 0x00FF00FF00FF00FF // 0000 0000  1111 1111  0000 0000  1111 1111 ...
const b4 = 0x0000FFFF0000FFFF // 0000 0000  0000 0000  1111 1111  1111 1111 ...
const b5 = 0x00000000FFFFFFFF // 0000 0000  0000 0000  0000 0000  0000 0000 ...

func interleave(x, y uint32) (z uint64) {

	i := uint64(x)
	j := uint64(y)

	i = (i | (i << 16)) & b4
	j = (j | (j << 16)) & b4

	i = (i | (i << 8)) & b3
	j = (j | (j << 8)) & b3

	i = (i | (i << 4)) & b2
	j = (j | (j << 4)) & b2

	i = (i | (i << 2)) & b1
	j = (j | (j << 2)) & b1

	i = (i | (i << 1)) & b0
	j = (j | (j << 1)) & b0

	z = i | (j << 1)
	return
}
func deinterleave(z uint64) (x uint32, y uint32) {
	i := z
	j := z >> 1

	i = i & b0
	j = j & b0

	i = (i | (i >> 1)) & b1
	j = (j | (j >> 1)) & b1

	i = (i | (i >> 2)) & b2
	j = (j | (j >> 2)) & b2

	i = (i | (i >> 4)) & b3
	j = (j | (j >> 4)) & b3

	i = (i | (i >> 8)) & b4
	j = (j | (j >> 8)) & b4

	i = (i | (i >> 16)) & b5
	j = (j | (j >> 16)) & b5

	x = uint32(i)
	y = uint32(j)
	return
}
