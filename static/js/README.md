# static/js

The only JavaScript this site serves.

## htmx.min.js

**htmx 2.0.4**, vendored rather than loaded from a CDN.

| | |
|---|---|
| Source | `https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js` |
| Size | 51 KB |
| SRI | `sha384-HGfztofotfshcF7+8n44JQL2oJmowVChPTg48S+jvZoztPfvwD79OC/LTtG6dMp+` |
| License | BSD 2-Clause (Big Sky Software) |

That hash is htmx's own published one for 2.0.4, and it matches the bytes in
this directory. To check, or to verify a replacement after an upgrade:

```sh
printf 'sha384-'; openssl dgst -sha384 -binary static/js/htmx.min.js | openssl base64 -A
```

### Why it is vendored and not on a CDN

Three reasons, in the order they matter here:

1. **The site has to work on the night.** Thirty people open the slate within
   about ten minutes of each other, from a link in a group chat, and a CDN
   that is slow or blocked for one of them is a picker that silently does
   nothing on their phone. A file we serve ourselves fails exactly when the
   rest of the site does and not otherwise.
2. **No third party learns who is voting.** A CDN script tag tells whoever
   hosts it every IP that loaded a page, which is the whole group.
3. **It makes a future Content-Security-Policy easy.** A same-origin script is
   covered by `script-src 'self'`; a CDN one needs a host allowance and an
   integrity attribute to be worth anything.

### What uses it

The TMDB search box on `/submit`, and nothing else (nap-eie). That page works
with this file absent or blocked: the search form is a real `<form
method="get">` and every result is a real `<a href>`, so search falls back to
a page load and the manual four-box form is untouched. See
`internal/web/templates/search.templ`.

### Upgrading

Drop the new file in, re-run the hash command above against htmx's published
SRI for that version, and update this table. Nothing else references the
version number.
