# Illustration slots

Every picture of the interface is requested as `/art/{category}/{slug}`.

The handler serves `web/static/art/{category}/{slug}.{webp,avif,png,jpg,svg}`
when the file exists, and otherwise draws a deterministic placeholder. Adding
real artwork therefore means dropping a file here and rebuilding: no template
and no stylesheet changes.

| Category   | Slug                          | Suggested size | Filled |
|------------|-------------------------------|----------------|--------|
| `building` | the building identifier       | 640 x 480      | all 13 planetary buildings |
| `research` | the research identifier       | 640 x 480      | no     |
| `ship`     | the unit identifier           | 640 x 480      | all 14 |
| `defense`  | the unit identifier           | 640 x 480      | no     |
| `body`     | `planet` or `moon`            | 512 x 512      | no     |
| `banner`   | the section name              | 1920 x 600     | shipyard |
| `resource` | `metal`, `crystal`, `deuterium`, `energy` | 64 x 64 | no |

Slugs are lowercase and limited to letters, digits, `-` and `_`.

## Where these files come from

A picture in here is a derivative. Its master lives in
`images/{category}/{slug}.png`, outside the embed root, and `make art`
regenerates this whole folder from those: centre-cropped to the size of the
slot, then encoded as WebP.

The derivatives are committed. Building the game has to work from a plain
checkout on a machine that has never heard of ImageMagick, and the continuous
integration compiles the tree exactly as it stands. See
`docs/adr/0003-masters-et-derives-d-illustration.md`.
