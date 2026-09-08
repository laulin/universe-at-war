# Illustration slots

Every picture of the interface is requested as `/art/{category}/{slug}`.

The handler serves `web/static/art/{category}/{slug}.{webp,avif,png,jpg,svg}`
when the file exists, and otherwise draws a deterministic placeholder. Adding
real artwork therefore means dropping a file here and rebuilding: no template
and no stylesheet changes.

| Category   | Slug                          | Suggested size |
|------------|-------------------------------|----------------|
| `building` | the building identifier       | 640 x 480      |
| `research` | the research identifier       | 640 x 480      |
| `ship`     | the unit identifier           | 640 x 480      |
| `defense`  | the unit identifier           | 640 x 480      |
| `body`     | `planet` or `moon`            | 512 x 512      |
| `banner`   | the section name              | 1920 x 600     |
| `resource` | `metal`, `crystal`, `deuterium`, `energy` | 64 x 64 |

Slugs are lowercase and limited to letters, digits, `-` and `_`.
