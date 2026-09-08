package web

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"math"
	"net/http"
	"strings"

	webassets "universeatwar/web"
)

// Illustrations are addressed by a stable slot name, never by a file name:
//
//	GET /art/{category}/{slug}
//
// The handler serves a real picture from web/static/art/{category}/{slug}.* as
// soon as one is dropped there, and otherwise draws a deterministic placeholder
// so that a screen is never missing an image. Adding artwork is therefore a
// matter of adding files, never of touching a template.

// artCategories bounds what a request may ask for, which also keeps the slug
// from ever reaching the filesystem as an arbitrary path.
var artCategories = map[string]func(seed uint32, slug string) string{
	"building": drawInstallation,
	"defense":  drawTurret,
	"research": drawResearch,
	"ship":     drawShip,
	"resource": drawResource,
	"body":     drawBody,
	"banner":   drawBanner,
}

// artExtensions are tried in order of preference when looking for real artwork.
var artExtensions = []string{".webp", ".avif", ".png", ".jpg", ".svg"}

func (h *Handler) art(response http.ResponseWriter, request *http.Request) {
	category := request.PathValue("category")
	draw, known := artCategories[category]
	if !known {
		http.NotFound(response, request)
		return
	}
	slug := artSlug(request.PathValue("slug"))
	if slug == "" {
		http.NotFound(response, request)
		return
	}
	if name, contentType, found := findArtwork(category, slug); found {
		payload, err := webassets.Files.ReadFile(name)
		if err == nil {
			response.Header().Set("Content-Type", contentType)
			response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			_, _ = response.Write(payload)
			return
		}
	}
	response.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	// A placeholder may be replaced by real artwork on the next build, so it is
	// cached for a day rather than for a year.
	response.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = response.Write([]byte(draw(artSeed(category, slug), slug)))
}

// artSlug keeps identifiers to the shape the domain uses, which rules out any
// separator a path traversal would need.
func artSlug(raw string) string {
	if len(raw) == 0 || len(raw) > 64 {
		return ""
	}
	var cleaned strings.Builder
	for _, letter := range strings.ToLower(raw) {
		switch {
		case letter >= 'a' && letter <= 'z', letter >= '0' && letter <= '9', letter == '-', letter == '_':
			cleaned.WriteRune(letter)
		default:
			return ""
		}
	}
	return cleaned.String()
}

func findArtwork(category, slug string) (string, string, bool) {
	for _, extension := range artExtensions {
		name := "static/art/" + category + "/" + slug + extension
		if _, err := fs.Stat(webassets.Files, name); err == nil {
			return name, artContentType(extension), true
		}
	}
	return "", "", false
}

func artContentType(extension string) string {
	switch extension {
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".png":
		return "image/png"
	case ".jpg":
		return "image/jpeg"
	default:
		return "image/svg+xml; charset=utf-8"
	}
}

// artSeed turns a slot name into the single number every placeholder derives its
// shape and its colour from, so the same slot always draws the same picture.
func artSeed(category, slug string) uint32 {
	digest := fnv.New32a()
	_, _ = digest.Write([]byte(category + "/" + slug))
	return digest.Sum32()
}

// pick spreads one seed over several independent choices.
func pick(seed uint32, index, span int) int {
	if span <= 0 {
		return 0
	}
	mixed := seed*2654435761 + uint32(index)*2246822519
	mixed ^= mixed >> 13
	return int(mixed % uint32(span))
}

func svg(width, height int, body string) string {
	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img">%s</svg>`,
		width, height, width, height, body)
}

// drawInstallation sketches an industrial skyline: a ground line, a few towers
// of seeded height and a dome, which reads as a planetary installation.
func drawInstallation(seed uint32, _ string) string {
	hue := 190 + pick(seed, 1, 40)
	var shapes strings.Builder
	fmt.Fprintf(&shapes,
		`<defs><linearGradient id="s" x1="0" y1="0" x2="0" y2="1">`+
			`<stop offset="0" stop-color="hsl(%d 45%% 22%%)"/><stop offset="1" stop-color="hsl(%d 55%% 8%%)"/>`+
			`</linearGradient></defs><rect width="160" height="120" fill="url(#s)"/>`, hue, hue)
	// A low sun behind the silhouette gives the block some depth.
	fmt.Fprintf(&shapes, `<circle cx="%d" cy="86" r="38" fill="hsl(%d 70%% 45%%)" opacity=".28"/>`, 40+pick(seed, 2, 80), hue)
	towers := 4 + pick(seed, 3, 3)
	for tower := 0; tower < towers; tower++ {
		width := 12 + pick(seed, 10+tower, 16)
		height := 26 + pick(seed, 20+tower, 52)
		left := 8 + tower*(144/towers) + pick(seed, 30+tower, 8)
		fmt.Fprintf(&shapes, `<rect x="%d" y="%d" width="%d" height="%d" fill="hsl(%d 30%% 12%%)"/>`,
			left, 96-height, width, height, hue)
		fmt.Fprintf(&shapes, `<rect x="%d" y="%d" width="%d" height="2" fill="hsl(%d 80%% 62%%)" opacity=".7"/>`,
			left, 96-height, width, hue)
		// A couple of lit windows keep the silhouette from looking abandoned.
		for window := 0; window < 3; window++ {
			fmt.Fprintf(&shapes, `<rect x="%d" y="%d" width="3" height="3" fill="hsl(%d 90%% 70%%)" opacity=".55"/>`,
				left+3+window*5, 96-height+8+pick(seed, 40+tower*3+window, height/2), hue+18)
		}
	}
	fmt.Fprintf(&shapes, `<path d="M0 96 H160 V120 H0 Z" fill="hsl(%d 40%% 6%%)"/>`, hue)
	fmt.Fprintf(&shapes, `<ellipse cx="%d" cy="96" rx="22" ry="12" fill="hsl(%d 35%% 16%%)"/>`, 24+pick(seed, 4, 110), hue)
	return svg(160, 120, shapes.String())
}

// drawResearch draws concentric orbits around a bright core.
func drawResearch(seed uint32, _ string) string {
	hue := 150 + pick(seed, 1, 130)
	var shapes strings.Builder
	fmt.Fprintf(&shapes,
		`<defs><radialGradient id="r"><stop offset="0" stop-color="hsl(%d 60%% 24%%)"/>`+
			`<stop offset="1" stop-color="hsl(%d 65%% 6%%)"/></radialGradient></defs>`+
			`<rect width="160" height="120" fill="url(#r)"/>`, hue, hue)
	rings := 3 + pick(seed, 2, 3)
	for ring := 0; ring < rings; ring++ {
		radius := 14 + ring*9
		tilt := 15 + pick(seed, 10+ring, 60)
		fmt.Fprintf(&shapes,
			`<ellipse cx="80" cy="60" rx="%d" ry="%d" fill="none" stroke="hsl(%d 85%% 66%%)" stroke-width="1.2" opacity="%.2f" transform="rotate(%d 80 60)"/>`,
			radius+ring*4, radius/2, hue, 0.75-float64(ring)*0.09, tilt)
	}
	fmt.Fprintf(&shapes, `<circle cx="80" cy="60" r="11" fill="hsl(%d 90%% 72%%)" opacity=".9"/>`, hue)
	fmt.Fprintf(&shapes, `<circle cx="80" cy="60" r="20" fill="hsl(%d 90%% 72%%)" opacity=".16"/>`, hue)
	for node := 0; node < 3; node++ {
		angle := float64(pick(seed, 20+node, 360)) * math.Pi / 180
		fmt.Fprintf(&shapes, `<circle cx="%.1f" cy="%.1f" r="2.4" fill="hsl(%d 95%% 80%%)"/>`,
			80+math.Cos(angle)*float64(24+node*8), 60+math.Sin(angle)*float64(12+node*4), hue)
	}
	return svg(160, 120, shapes.String())
}

// drawShip sketches a dart-shaped hull pointing right, with swept wings and a
// lit engine block behind it.
func drawShip(seed uint32, _ string) string {
	hue := 195 + pick(seed, 1, 40)
	nose := 128 + pick(seed, 2, 20)
	tail := 26 + pick(seed, 3, 14)
	span := 9 + pick(seed, 4, 9)
	sweep := 16 + pick(seed, 5, 16)
	var shapes strings.Builder
	fmt.Fprintf(&shapes,
		`<defs><linearGradient id="h" x1="0" y1="0" x2="0" y2="1">`+
			`<stop offset="0" stop-color="hsl(%d 40%% 18%%)"/><stop offset="1" stop-color="hsl(%d 50%% 6%%)"/>`+
			`</linearGradient><linearGradient id="m" x1="0" y1="0" x2="0" y2="1">`+
			`<stop offset="0" stop-color="hsl(%d 18%% 78%%)"/><stop offset="52%%" stop-color="hsl(%d 20%% 58%%)"/>`+
			`<stop offset="53%%" stop-color="hsl(%d 24%% 34%%)"/><stop offset="1" stop-color="hsl(%d 26%% 22%%)"/>`+
			`</linearGradient></defs><rect width="160" height="120" fill="url(#h)"/>`,
		hue, hue, hue, hue, hue, hue)
	// Wings first, so the fuselage sits on top of them.
	fmt.Fprintf(&shapes, `<path d="M%d %d L%d %d L%d %d Z" fill="hsl(%d 28%% 30%%)"/>`,
		nose-42, 60-span, tail+14, 60-span-sweep, tail+4, 60-span, hue)
	fmt.Fprintf(&shapes, `<path d="M%d %d L%d %d L%d %d Z" fill="hsl(%d 28%% 30%%)"/>`,
		nose-42, 60+span, tail+14, 60+span+sweep, tail+4, 60+span, hue)
	// Fuselage.
	fmt.Fprintf(&shapes,
		`<path d="M%d 60 L%d %d L%d %d L%d %d L%d %d L%d %d L%d %d Z" fill="url(#m)"/>`,
		nose, nose-34, 60-span, tail+10, 60-span, tail, 60-span/2,
		tail, 60+span/2, tail+10, 60+span, nose-34, 60+span)
	// A spine catches the light along the hull.
	fmt.Fprintf(&shapes, `<path d="M%d 60 L%d 60" stroke="hsl(%d 30%% 88%%)" stroke-width="1.4" opacity=".55"/>`, nose-6, tail+8, hue)
	// Canopy.
	fmt.Fprintf(&shapes, `<ellipse cx="%d" cy="60" rx="9" ry="%d" fill="hsl(%d 90%% 68%%)" opacity=".85"/>`, nose-30, span/2+1, hue)
	// Engines.
	pods := 1 + pick(seed, 6, 3)
	for pod := 0; pod < pods; pod++ {
		offset := (pod - pods/2) * 7
		fmt.Fprintf(&shapes, `<rect x="%d" y="%d" width="7" height="5" rx="2" fill="hsl(%d 24%% 44%%)"/>`, tail-6, 58+offset, hue)
		fmt.Fprintf(&shapes, `<ellipse cx="%d" cy="%d" rx="9" ry="3" fill="hsl(%d 95%% 64%%)" opacity=".7"/>`, tail-11, 60+offset, hue+18)
	}
	return svg(160, 120, shapes.String())
}

// drawTurret sketches a ground emplacement: a bunker base and a raised barrel.
func drawTurret(seed uint32, _ string) string {
	hue := 200 + pick(seed, 1, 30)
	barrels := 1 + pick(seed, 2, 3)
	elevation := 26 + pick(seed, 3, 18)
	var shapes strings.Builder
	fmt.Fprintf(&shapes,
		`<defs><linearGradient id="h" x1="0" y1="0" x2="0" y2="1">`+
			`<stop offset="0" stop-color="hsl(%d 42%% 18%%)"/><stop offset="1" stop-color="hsl(%d 52%% 6%%)"/>`+
			`</linearGradient><linearGradient id="m" x1="0" y1="0" x2="0" y2="1">`+
			`<stop offset="0" stop-color="hsl(%d 16%% 72%%)"/><stop offset="1" stop-color="hsl(%d 24%% 26%%)"/>`+
			`</linearGradient></defs><rect width="160" height="120" fill="url(#h)"/>`,
		hue, hue, hue, hue)
	// Ground.
	fmt.Fprintf(&shapes, `<path d="M0 96 H160 V120 H0 Z" fill="hsl(%d 40%% 8%%)"/>`, hue)
	fmt.Fprintf(&shapes, `<ellipse cx="80" cy="96" rx="46" ry="10" fill="hsl(%d 34%% 14%%)"/>`, hue)
	// Barrels, angled up and to the right.
	for barrel := 0; barrel < barrels; barrel++ {
		offset := (barrel - barrels/2) * 8
		fmt.Fprintf(&shapes,
			`<path d="M%d %d L%d %d L%d %d L%d %d Z" fill="hsl(%d 20%% 52%%)"/>`,
			74+offset, 72-elevation/2, 126+offset, 72-elevation, 129+offset, 79-elevation, 77+offset, 79-elevation/2, hue)
		fmt.Fprintf(&shapes, `<circle cx="%d" cy="%d" r="3" fill="hsl(%d 95%% 66%%)" opacity=".8"/>`,
			127+offset, 75-elevation, hue+16)
	}
	// Housing and base.
	fmt.Fprintf(&shapes, `<path d="M56 96 L62 %d H98 L104 96 Z" fill="url(#m)"/>`, 96-elevation)
	fmt.Fprintf(&shapes, `<rect x="48" y="86" width="64" height="12" rx="3" fill="hsl(%d 22%% 34%%)"/>`, hue)
	fmt.Fprintf(&shapes, `<rect x="66" y="%d" width="28" height="4" fill="hsl(%d 90%% 66%%)" opacity=".6"/>`, 92-elevation, hue)
	return svg(160, 120, shapes.String())
}

// bodyPalette gives a moon its grey and a planet a seeded, plausible hue.
func drawBody(seed uint32, slug string) string {
	hue, saturation := 20+pick(seed, 1, 320), 45
	if strings.Contains(slug, "moon") || strings.Contains(slug, "lune") {
		hue, saturation = 215, 8
	}
	var shapes strings.Builder
	fmt.Fprintf(&shapes,
		`<defs><radialGradient id="p" cx="34%%" cy="30%%" r="78%%">`+
			`<stop offset="0" stop-color="hsl(%d %d%% 66%%)"/>`+
			`<stop offset="55%%" stop-color="hsl(%d %d%% 40%%)"/>`+
			`<stop offset="100%%" stop-color="hsl(%d %d%% 12%%)"/></radialGradient>`+
			`<radialGradient id="t" cx="30%%" cy="28%%" r="80%%">`+
			`<stop offset="60%%" stop-color="#000" stop-opacity="0"/>`+
			`<stop offset="100%%" stop-color="#000" stop-opacity=".72"/></radialGradient>`+
			`<clipPath id="c"><circle cx="64" cy="64" r="58"/></clipPath></defs>`,
		hue, saturation, hue, saturation, hue, saturation)
	fmt.Fprintf(&shapes, `<circle cx="64" cy="64" r="58" fill="url(#p)"/><g clip-path="url(#c)">`)
	for blotch := 0; blotch < 6; blotch++ {
		fmt.Fprintf(&shapes,
			`<ellipse cx="%d" cy="%d" rx="%d" ry="%d" fill="hsl(%d %d%% %d%%)" opacity=".38"/>`,
			8+pick(seed, 10+blotch, 112), 8+pick(seed, 20+blotch, 112),
			8+pick(seed, 30+blotch, 24), 5+pick(seed, 40+blotch, 14),
			hue+pick(seed, 50+blotch, 24)-12, saturation, 24+pick(seed, 60+blotch, 34))
	}
	shapes.WriteString(`</g><circle cx="64" cy="64" r="58" fill="url(#t)"/>`)
	fmt.Fprintf(&shapes, `<circle cx="64" cy="64" r="58" fill="none" stroke="hsl(%d %d%% 74%%)" stroke-width="1" opacity=".35"/>`, hue, saturation)
	return svg(128, 128, shapes.String())
}

// drawBanner fills the wide header of a screen with a nebula and a planet limb.
func drawBanner(seed uint32, _ string) string {
	hue := 190 + pick(seed, 1, 90)
	var shapes strings.Builder
	fmt.Fprintf(&shapes,
		`<defs><linearGradient id="b" x1="0" y1="0" x2="0" y2="1">`+
			`<stop offset="0" stop-color="hsl(%d 55%% 10%%)"/><stop offset="1" stop-color="hsl(%d 60%% 4%%)"/>`+
			`</linearGradient><radialGradient id="n" cx="%d%%" cy="30%%" r="60%%">`+
			`<stop offset="0" stop-color="hsl(%d 80%% 52%%)" stop-opacity=".45"/>`+
			`<stop offset="1" stop-color="hsl(%d 80%% 52%%)" stop-opacity="0"/></radialGradient>`+
			`<linearGradient id="l" x1="0" y1="0" x2="0" y2="1">`+
			`<stop offset="0" stop-color="hsl(%d 55%% 26%%)"/><stop offset="1" stop-color="hsl(%d 60%% 7%%)"/>`+
			`</linearGradient></defs>`,
		hue, hue, 20+pick(seed, 2, 60), hue+30, hue+30, hue, hue)
	shapes.WriteString(`<rect width="640" height="200" fill="url(#b)"/><rect width="640" height="200" fill="url(#n)"/>`)
	for star := 0; star < 46; star++ {
		fmt.Fprintf(&shapes, `<circle cx="%d" cy="%d" r="%.1f" fill="#fff" opacity="%.2f"/>`,
			pick(seed, 100+star, 640), pick(seed, 200+star, 190),
			0.5+float64(pick(seed, 300+star, 3))*0.35, 0.2+float64(pick(seed, 400+star, 6))*0.11)
	}
	// Only the limb of a nearby world shows, low in the frame: the banner is a
	// sky, not a landscape.
	limb := 120 + pick(seed, 3, 400)
	fmt.Fprintf(&shapes, `<ellipse cx="%d" cy="352" rx="520" ry="170" fill="url(#l)"/>`, limb)
	fmt.Fprintf(&shapes, `<ellipse cx="%d" cy="352" rx="520" ry="170" fill="none" stroke="hsl(%d 92%% 78%%)" stroke-width="2.5" opacity=".65"/>`, limb, hue)
	return svg(640, 200, shapes.String())
}

// resourceGlyphs are drawn by hand rather than seeded: four icons the player has
// to recognise instantly cannot be left to chance.
var resourceGlyphs = map[string]string{
	"metal": `<rect width="32" height="32" fill="none"/>` +
		`<path d="M4 20 L10 12 H24 L28 20 Z" fill="#cdd8e2"/>` +
		`<path d="M4 20 H28 V26 H4 Z" fill="#93a2b0"/>` +
		`<path d="M10 12 H24 L21 8 H13 Z" fill="#eef3f8"/>`,
	"crystal": `<path d="M16 3 L27 13 L16 29 L5 13 Z" fill="#6fc9f5"/>` +
		`<path d="M16 3 L27 13 L16 29 Z" fill="#a9e3ff"/>` +
		`<path d="M5 13 H27" stroke="#0b3145" stroke-width="1" opacity=".5"/>`,
	"deuterium": `<path d="M16 3 C22 12 25 17 25 21 A9 9 0 0 1 7 21 C7 17 10 12 16 3 Z" fill="#4fd6b8"/>` +
		`<path d="M16 3 C22 12 25 17 25 21 A9 9 0 0 1 16 30 Z" fill="#2ba98d"/>` +
		`<ellipse cx="13" cy="20" rx="2.6" ry="3.6" fill="#d8fff5" opacity=".7"/>`,
	"energy": `<path d="M18 2 L7 18 H14 L13 30 L25 13 H18 Z" fill="#f3c969"/>` +
		`<path d="M18 2 L7 18 H14 Z" fill="#fde9b0"/>`,
	"darkmatter": `<circle cx="16" cy="16" r="11" fill="#8d3bd6"/>` +
		`<circle cx="16" cy="16" r="11" fill="none" stroke="#e08cff" stroke-width="2" opacity=".8"/>` +
		`<circle cx="12" cy="12" r="3" fill="#f2c9ff" opacity=".7"/>`,
}

func drawResource(seed uint32, slug string) string {
	if glyph, known := resourceGlyphs[slug]; known {
		return svg(32, 32, glyph)
	}
	hue := pick(seed, 1, 360)
	return svg(32, 32, fmt.Sprintf(
		`<circle cx="16" cy="16" r="12" fill="hsl(%d 60%% 45%%)"/>`+
			`<circle cx="16" cy="16" r="12" fill="none" stroke="hsl(%d 80%% 72%%)" stroke-width="1.5"/>`, hue, hue))
}
