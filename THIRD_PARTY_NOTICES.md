# Third-Party Notices

Beacon vendors browser JavaScript and CSS assets into `static/` so the
dashboard can run locally without loading CDN resources. These files are copied
from npm packages by `npm run vendor`. `static/vendor-manifest.json` records
the exact package versions, licenses, upstream URLs, source paths, and SHA-256
hashes used for the current vendored files.

| Vendored file | Source package | Version | License | Upstream |
| --- | --- | --- | --- | --- |
| `static/js/vendor/htmx.min.js` | `htmx.org` | 2.0.10 | 0BSD | <https://github.com/bigskysoftware/htmx> |
| `static/js/vendor/htmx-ext-sse.js` | `htmx-ext-sse` | 2.2.4 | 0BSD | <https://github.com/bigskysoftware/htmx-extensions> |
| `static/js/vendor/chart.umd.min.js` | `chart.js` | 4.5.1 | MIT | <https://github.com/chartjs/Chart.js> |
| `static/js/vendor/chartjs-adapter-date-fns.bundle.min.js` | `chartjs-adapter-date-fns` | 3.0.0 | MIT | <https://github.com/chartjs/chartjs-adapter-date-fns> |
| `static/js/vendor/chartjs-adapter-date-fns.bundle.min.js` | `date-fns` | 4.1.0 | MIT | <https://github.com/date-fns/date-fns> |
| `static/js/vendor/highlight.min.js` | `@highlightjs/cdn-assets` | 11.11.1 | BSD-3-Clause | <https://github.com/highlightjs/highlight.js> |
| `static/css/vendor/github-dark.min.css` | `@highlightjs/cdn-assets` | 11.11.1 | BSD-3-Clause | <https://github.com/highlightjs/highlight.js> |

Release archives include this notice file and Beacon's repository license.
When vendored assets are refreshed, run `npm run vendor`, review
`static/vendor-manifest.json`, and update this table from `package-lock.json`
and the package license metadata in `node_modules`. Run `npm run vendor:check`
before opening the PR; CI uses the same check to catch stale assets, manifest
metadata, notice entries, and unconfigured files left in the vendor
directories.

## htmx.org

Vendored file: `static/js/vendor/htmx.min.js`

Source package: `htmx.org` 2.0.10

License: 0BSD

Copyright: htmx contributors

```text
Zero-Clause BSD

Permission to use, copy, modify, and/or distribute this software for
any purpose with or without fee is hereby granted.

THE SOFTWARE IS PROVIDED “AS IS” AND THE AUTHOR DISCLAIMS ALL
WARRANTIES WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES
OF MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE
FOR ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY
DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER
IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT
OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
```

## htmx-ext-sse

Vendored file: `static/js/vendor/htmx-ext-sse.js`

Source package: `htmx-ext-sse` 2.2.4

License: 0BSD

Copyright: Copyright (c) 2023, Alexander Petros

```text
BSD Zero Clause License

Copyright (c) 2023, Alexander Petros

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
```

## Chart.js

Vendored file: `static/js/vendor/chart.umd.min.js`

Source package: `chart.js` 4.5.1

License: MIT

Copyright: Copyright (c) 2014-2024 Chart.js Contributors

```text
The MIT License (MIT)

Copyright (c) 2014-2024 Chart.js Contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## chartjs-adapter-date-fns

Vendored file: `static/js/vendor/chartjs-adapter-date-fns.bundle.min.js`

Source package: `chartjs-adapter-date-fns` 3.0.0

License: MIT

Copyright: Copyright (c) 2019 Chart.js Contributors

```text
The MIT License (MIT)

Copyright (c) 2019 Chart.js Contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## date-fns

Vendored file: `static/js/vendor/chartjs-adapter-date-fns.bundle.min.js`

Source package: `date-fns` 4.1.0

License: MIT

Copyright: Copyright (c) 2021 Sasha Koss and Lesha Koss

```text
MIT License

Copyright (c) 2021 Sasha Koss and Lesha Koss
https://kossnocorp.mit-license.org

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Highlight.js CDN assets

Vendored files:

- `static/js/vendor/highlight.min.js`
- `static/css/vendor/github-dark.min.css`

Source package: `@highlightjs/cdn-assets` 11.11.1

License: BSD-3-Clause

Copyright: Copyright (c) 2006, Ivan Sagalaev.

```text
BSD 3-Clause License

Copyright (c) 2006, Ivan Sagalaev.
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice, this
  list of conditions and the following disclaimer.

* Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.

* Neither the name of the copyright holder nor the names of its contributors
  may be used to endorse or promote products derived from this software without
  specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
