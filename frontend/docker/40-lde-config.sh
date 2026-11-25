#!/bin/sh
# Runs from /docker-entrypoint.d at container start (nginx image convention).
# Generates, from API_BASE_URL / COLLAB_URL:
#   - /usr/share/nginx/html/config.js        (window.__LDE_CONFIG__ for the SPA)
#   - /etc/nginx/conf.d/security-headers.inc (CSP + nosniff + Referrer-Policy)
# Both values are optional; unset means same-origin (/api/v1, /collab).
set -eu

API_BASE_URL="${API_BASE_URL:-}"
COLLAB_URL="${COLLAB_URL:-}"

# Strict allow-lists: the values end up in a JS file and an HTTP header, so
# anything outside a conservative URL charset is refused (fail closed).
PATH_RE='/[A-Za-z0-9._~/-]*'
HOST_RE='[A-Za-z0-9.-]+(:[0-9]+)?'
if [ -n "$API_BASE_URL" ] && ! printf '%s' "$API_BASE_URL" | grep -Eq "^($PATH_RE|https?://$HOST_RE($PATH_RE)?)\$"; then
  echo "lde-config: invalid API_BASE_URL (want /path or http(s)://host[:port][/path])" >&2
  exit 1
fi
if [ -n "$COLLAB_URL" ] && ! printf '%s' "$COLLAB_URL" | grep -Eq "^($PATH_RE|wss?://$HOST_RE($PATH_RE)?)\$"; then
  echo "lde-config: invalid COLLAB_URL (want /path or ws(s)://host[:port][/path])" >&2
  exit 1
fi

# scheme://host[:port] of an absolute URL; empty for a same-origin path.
origin_of() {
  case "$1" in
    *://*) printf '%s' "$1" | sed -E 's#^([a-z]+://[^/]+).*#\1#' ;;
  esac
}
API_ORIGIN="$(origin_of "$API_BASE_URL")"
COLLAB_ORIGIN="$(origin_of "$COLLAB_URL")"

cat > /usr/share/nginx/html/config.js <<JS
window.__LDE_CONFIG__ = { apiBaseUrl: "$API_BASE_URL", collabUrl: "$COLLAB_URL" }
JS

# 'self' covers same-origin http(s) and (modern browsers) ws(s). Cross-origin
# api/collab are added explicitly; attachments are fetched from the api so its
# origin is also allowed for img-src.
CONNECT="'self' blob: $API_ORIGIN $COLLAB_ORIGIN"
IMG="'self' blob: data: $API_ORIGIN"

# Relaxations (each needed by the built app, verified under this CSP):
#   style-src 'unsafe-inline'  Vuetify/Vue :style bindings and the print iframe
#   connect-src blob:          pdf.js fetches the attachment's blob: URL
#   worker-src blob:           kept for libraries that spawn blob workers (pdf.js
#                              itself uses a hashed same-origin worker file)
#   img-src blob: data:        attachment blobs, inline SVG/data icons
#   frame-src about: blob:     print/export iframe (srcdoc) and blob previews
CSP="default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src $IMG; font-src 'self' data:; connect-src $CONNECT; worker-src 'self' blob:; frame-src about: blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

cat > /etc/nginx/conf.d/security-headers.inc <<INC
add_header Content-Security-Policy "$CSP" always;
add_header X-Content-Type-Options "nosniff" always;
add_header Referrer-Policy "no-referrer" always;
add_header X-Frame-Options "DENY" always;
INC
echo "lde-config: wrote config.js and security-headers.inc"
