#!/bin/sh
# Regenerate the illustrations the binary embeds, from the masters in images/.
#
# A master is named after the slot it fills: images/{category}/{slug}.png
# becomes web/static/art/{category}/{slug}.webp, so nothing here carries a
# correspondence table that could drift from the catalogue.
#
# The derivatives are committed. This is a developer tool and never a build
# step: the continuous integration compiles the tree as it is checked out, and
# no machine needs an image toolchain to build or to run the game.

set -eu

quality=${ART_QUALITY:-80}
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

# ImageMagick 7 dropped the convert alias and ImageMagick 6 never had magick,
# so neither name alone tells whether the tool is there.
if command -v magick >/dev/null 2>&1; then
	resize=magick
elif command -v convert >/dev/null 2>&1; then
	resize=convert
else
	echo "build-art: ImageMagick is missing (magick or convert)" >&2
	exit 1
fi
if ! command -v cwebp >/dev/null 2>&1; then
	echo "build-art: cwebp is missing; install libwebp" >&2
	exit 1
fi

# One thread, so a resize cannot depend on how many cores happened to run it.
MAGICK_THREAD_LIMIT=1
export MAGICK_THREAD_LIMIT

# The geometry of a slot, as web/static/art/README.md documents it. An unknown
# category stops the run rather than inventing a size for it.
geometry() {
	case $1 in
	building | research | ship | defense) echo 640x480 ;;
	body) echo 512x512 ;;
	banner) echo 1920x600 ;;
	resource) echo 64x64 ;;
	*)
		echo "build-art: unknown category $1" >&2
		return 1
		;;
	esac
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

count=0
for master in "$root"/images/*/*.png; do
	[ -e "$master" ] || continue
	category=$(basename "$(dirname "$master")")
	slug=$(basename "$master" .png)
	# The shape the handler accepts, so a master can never name a slot the
	# server would refuse to serve. Whether the slug is an identifier the
	# domain knows is settled by the tests, which can read the catalogue.
	case $slug in
	*[!a-z0-9_-]*)
		echo "build-art: $master is not named after a slug" >&2
		exit 1
		;;
	esac
	size=$(geometry "$category")
	target="$root/web/static/art/$category/$slug.webp"
	mkdir -p "$(dirname "$target")"
	# Cover the frame from its centre: the interface crops the picture to the
	# shape of its slot anyway, so the file may as well already be that shape.
	"$resize" "$master" -resize "$size^" -gravity center -extent "$size" -strip "$work/frame.png"
	cwebp -quiet -q "$quality" -m 6 "$work/frame.png" -o "$target"
	printf '%-8s %-18s %7s bytes\n' "$category" "$slug" "$(wc -c <"$target")"
	count=$((count + 1))
done

# A derivative whose master disappeared would travel in every binary and be
# served to nobody. It is reported rather than deleted: only the files this
# script produces are looked at, and even those may have been renamed on
# purpose between two runs.
for derivative in "$root"/web/static/art/*/*.webp; do
	[ -e "$derivative" ] || continue
	category=$(basename "$(dirname "$derivative")")
	slug=$(basename "$derivative" .webp)
	if [ ! -f "$root/images/$category/$slug.png" ]; then
		echo "build-art: web/static/art/$category/$slug.webp has no master" >&2
	fi
done

echo "build-art: $count illustrations regenerated at quality $quality"
