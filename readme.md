# Traefik Plugin Bulk Redirects

[![release](https://img.shields.io/github/release/DoodleScheduling/traefik-bulk-redirects/all.svg)](https://github.com/DoodleScheduling/traefik-bulk-redirects/releases)
[![report](https://goreportcard.com/badge/github.com/DoodleScheduling/traefik-bulk-redirects)](https://goreportcard.com/report/github.com/DoodleScheduling/traefik-bulk-redirects)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/DoodleScheduling/traefik-bulk-redirects/badge)](https://api.securityscorecards.dev/projects/github.com/DoodleScheduling/traefik-bulk-redirects)
[![Coverage Status](https://coveralls.io/repos/github/DoodleScheduling/traefik-bulk-redirects/badge.svg?branch=master)](https://coveralls.io/github/DoodleScheduling/traefik-bulk-redirects?branch=master)
[![license](https://img.shields.io/github/license/DoodleScheduling/traefik-bulk-redirects.svg)](https://github.com/DoodleScheduling/traefik-bulk-redirects/blob/master/LICENSE)

A Traefik middleware plugin for Cloudflare-style bulk redirects. 
It allows defining multiple redirects in a single Traefik Middleware configuration.
This plugin supports exact redirects, subpath redirects, query string preservation, and configurable redirect status codes.

# Redirect fields

| Key | Description |
| --- | --- |
| `sourceURL` | absolute source URL to match |
| `targetURL` | absolute redirect destination URL |
| `statusCode` | redirect status code: `301`, `302`, `303`, `307`, `308` |
| `preserveQueryString` | `enabled` appends the original query string to the target URL |
| `subpathMatching` | `enabled` matches the source path and all child paths below it |

# Configuration modes

The plugin supports `inline` and `file` configuration. Inline mode is backwards-compatible and convenient for small rulesets. File mode is recommended for large rulesets and global middlewares because the parsed and compiled redirects are shared using the file path and checksum as an explicit version.

## Inline mode

Inline mode embeds redirects in the Middleware resource. It is selected when `mode` is omitted or set to `inline`.

Existing configurations do not need to change:


```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: bulk-redirects
spec:
  plugin:
    bulkRedirects:
      # mode: inline # Optional; inline is the default.
      redirects:
      - sourceURL: https://example.com/premium/coupon
        targetURL: https://example.com/en/premium/
        statusCode: 302
        preserveQueryString: enabled
        subpathMatching: disabled
      - sourceURL: https://example.com/docs
        targetURL: https://example.com/en/resources
        statusCode: 301
        preserveQueryString: enabled
        subpathMatching: enabled
```

Inline mode is simple, but Traefik must decode the complete redirect list for every middleware instance. Prefer file mode for large rulesets.

## File mode

File mode loads redirects from a JSON file mounted in the Traefik container. The first `New()` call for a new `filePath` and `fileChecksum` reads, verifies, decodes, validates and compiles the file. Later instances with the same key reuse the immutable compiled rules without reading or parsing the file again.

The path must be absolute. File mode accepts only regular files with a maximum size of 16 MiB (16,777,216 bytes). Directories, pipes, devices, sockets, and other special files are rejected. The checksum must use the canonical lowercase format `sha256:<64 lowercase hexadecimal characters>`.

### Redirect file

```json
{
  "redirects": [
    {
      "sourceURL": "https://example.com/old",
      "targetURL": "https://example.com/new",
      "statusCode": 301,
      "preserveQueryString": true,
      "subpathMatching": false
    }
  ]
}
```

Only the `redirects` field is accepted in this file. Configuration fields such as `mode`, `filePath` and `fileChecksum` belong in the Middleware resource.

Generate the checksum on Linux:

```shell
sha256sum redirects.json
```

Or on macOS:

```shell
shasum -a 256 redirects.json
```

Prefix the resulting lowercase hash with `sha256:`. For example:

```text
sha256:8a343c...<64 hexadecimal characters in total>
```

The checksum is the ruleset version contract. **`fileChecksum` must be updated whenever the file contents change.** If the file changes without a corresponding checksum change, existing cached instances may continue using the previously compiled ruleset. This is intentional.

### Middleware

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: bulk-redirects
spec:
  plugin:
    bulkRedirects:
      mode: file
      filePath: /etc/traefik/bulk-redirects/redirects.json
      fileChecksum: sha256:<64-lowercase-hex-characters>
```

### Kubernetes ConfigMap

The plugin does not call Kubernetes APIs. The file must be mounted into every Traefik pod by the deployment configuration.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: bulk-redirect-rules
data:
  redirects.json: |
    {
      "redirects": [
        {
          "sourceURL": "https://example.com/old",
          "targetURL": "https://example.com/new",
          "statusCode": 301,
          "preserveQueryString": true,
          "subpathMatching": false
        }
      ]
    }
```

Example Deployment fragments:

```yaml
spec:
  template:
    spec:
      containers:
        - name: traefik
          volumeMounts:
            - name: bulk-redirect-rules
              mountPath: /etc/traefik/bulk-redirects
              readOnly: true
      volumes:
        - name: bulk-redirect-rules
          configMap:
            name: bulk-redirect-rules
```

For production, prefer versioned or immutable ConfigMaps when possible, update `fileChecksum` with every ruleset change, and consider rolling out Traefik when the mounted ConfigMap changes. A rollout is not required by the plugin: when Traefik invokes `New()` with a new path/checksum version, the plugin loads that version.

# Static configuration

```yaml
experimental:
  plugins:
    bulkRedirects:
      moduleName: github.com/doodlescheduling/traefik-bulk-redirects
      version: v0.0.1
```
