# Illustration masters

A picture in here is a master: the full-resolution source of one illustration.
It is never served. It sits outside the `//go:embed` root of `web/assets.go` on
purpose, so the binary carries the derivative and not the two megabytes it was
cut from.

## Naming

The directory is the category and the basename is the slug, which is the
identifier the domain already uses. `images/ship/cruiser.png` fills
`/art/ship/cruiser`, and no file anywhere says so: that is the whole point.
Adding a ship to the catalogue and dropping `images/ship/<its id>.png` here is
the entire procedure.

The slug must be lowercase and limited to letters, digits, `-` and `_`, which
is the shape `artSlug` accepts.

## Regenerating

```sh
make art
```

It rewrites the whole of `web/static/art/` from this directory: centre-cropped
to the size of the slot, then encoded as WebP. Commit the master and the
derivative together.

The run is reproducible for a given toolchain, not across toolchains: another
libwebp will re-encode to different bytes with no visible change. These files
were last generated with ImageMagick 6.9.12 and libwebp 1.3.2.

## Framing

The derivative is a centre crop at the aspect ratio of the slot — 4:3 for a
ship, so a square master loses one eighth of its height off the top and one
eighth off the bottom. A subject that runs to the edge will lose it. The fix is
to re-frame the master; the build has no per-file knobs, and giving it any
would bring back the correspondence table this layout exists to avoid.

`banner/shipyard.png` is cut from the battleship rather than drawn wide, so it
is enlarged to fill a 1920x600 slot. It holds up at the height the banner is
actually displayed, and it stands in until a natively wide picture exists.
