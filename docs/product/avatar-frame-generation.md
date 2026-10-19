# Avatar frame artwork generation

> Doc type: product and asset standard
>
> Status: Active
>
> Owner: Platform maintainers
>
> Last verified: 2026-10-19

This specification applies to optional avatar-frame artwork configured by site managers. The
decoration catalog starts empty; generated art does not become a product option until an
administrator creates and enables a matching frame definition.

The user-provided [profile reference image](assets/avatar-frame-reference.png) shows the intended
overlay relationship: a transparent circular frame sits over the avatar while the portrait remains
visible through its center. It is a visual reference, not a default frame or catalog entry.

## Canvas and composition

- Deliver a square RGBA PNG with a transparent background. Transparent WebP is also accepted by the
  renderer. Use a 1024 × 1024 canvas unless the source generator requires another square size.
- Draw a centered circular overlay. Keep the outer ring inside about 92% of the canvas and leave an
  avatar opening about 78–82% of the canvas diameter.
- The entire inner opening and the area outside the frame must remain alpha-transparent. Do not fill
  either area with white, black, a checkerboard, shadow, glow, or simulated transparency.
- Keep the ring and its ornaments inside the narrow annulus. Avoid text, watermarks, portraits,
  detached particles, and details that extend beyond the canvas. Leave the lower-right corner clear
  enough for the platform badge.
- Use clear shapes and restrained detail that remain legible at 32–48 px. Check that the frame does
  not obscure faces or important avatar content.

## Generation and review

Ask the image generator for one front-facing circular overlay on a square transparent canvas, with
no avatar or background. Explicitly require a fully transparent center and exterior, a clean annular
ring, and no text or watermark. Generate visual variants separately instead of asking one image to
contain several alternatives.

Before installation, verify the alpha channel and inspect the frame over light and dark backgrounds
and over several portrait crops. Sample pixels in the center and outside the ring; both regions must
be fully transparent. Reject images whose transparency is only a checkerboard pattern or whose ring
covers the portrait.

## Installation and configuration

In `/admin/decorations`, pick the solid-color frame style to ship a gradient border from colors alone, or
pick the image style and upload a square PNG/WebP with a transparent center. The upload returns a
unique `/static/decorations/<uuid>.<ext>` URL and fills the draft path; save the `avatarFrame`
definition to keep that asset. Unsaved uploads expire after 24 hours. To replace artwork, upload a
new file and save the definition with its new URL; never reuse a filename because `/static` has a
long browser cache. SiteManager can upload and use artwork without an approval step. Existing
embedded assets under `apps/gooseforum/resource/static/decorations/` remain usable by entering their
paths manually. Set light and dark fallback colors and adjust the artwork scale in the live preview:
the setting is the artwork diameter as a percentage of the avatar diameter (50%–200%, default 125%). It scales
around the avatar center and is saved for both Web and Flutter. The server accepts only same-origin PNG/WebP paths in this directory; external URLs,
query strings, and traversal paths are rejected.

Keep the fallback colors even when the artwork loads successfully. Web and Flutter use them when an
image is unavailable or fails to decode. Preview the configured style in the admin page, then grant
it to a test account and verify the profile, feed, chat, and badge overlay before enabling it for
users.
