# Third-Party Notices

Beacon vendors browser JavaScript and CSS assets into `static/` so the
dashboard can run locally without loading CDN resources. These files are copied
from npm packages by `npm run vendor`.

| Vendored file | Source package | Version | License | Upstream |
| --- | --- | --- | --- | --- |
| `static/js/vendor/htmx.min.js` | `htmx.org` | 2.0.10 | 0BSD | <https://github.com/bigskysoftware/htmx> |
| `static/js/vendor/htmx-ext-sse.js` | `htmx-ext-sse` | 2.2.4 | 0BSD | <https://github.com/bigskysoftware/htmx-extensions> |
| `static/js/vendor/chart.umd.min.js` | `chart.js` | 4.5.1 | MIT | <https://github.com/chartjs/Chart.js> |
| `static/js/vendor/chartjs-adapter-date-fns.bundle.min.js` | `chartjs-adapter-date-fns` | 3.0.0 | MIT | <https://github.com/chartjs/chartjs-adapter-date-fns> |
| `static/js/vendor/highlight.min.js` | `@highlightjs/cdn-assets` | 11.11.1 | BSD-3-Clause | <https://github.com/highlightjs/highlight.js> |
| `static/css/vendor/github-dark.min.css` | `@highlightjs/cdn-assets` | 11.11.1 | BSD-3-Clause | <https://github.com/highlightjs/highlight.js> |

Release archives include this notice file and Beacon's repository license.
When vendored assets are refreshed, update this table from `package-lock.json`
and the package license metadata in `node_modules`.
