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

The plugin supports `inline` and `file` configuration. Inline mode is backwards-compatible and convenient for small rulesets. File mode is recommended for large rulesets and global middlewares because the parsed and compiled redirects are shared by file path for the lifetime of the Traefik process.

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

File mode loads redirects from a JSON file mounted in the Traefik container. The first `New()` call for a new `filePath` reads, decodes, validates and compiles the file. Later instances with the same path reuse the immutable compiled rules without filesystem I/O, parsing, validation or compilation.

The path must be absolute. File mode accepts only regular files with a maximum size of 16 MiB (16,777,216 bytes). Directories, pipes, devices, sockets, and other special files are rejected.

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

Only the `redirects` field is accepted in this file. Configuration fields such as `mode` and `filePath` belong in the Middleware resource.

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

### Updating file-based rules

File mode treats a rules file as immutable for the lifetime of the Traefik process. The process keeps the first successfully compiled snapshot for each `filePath`. Replacing the file at the same path while Traefik is running does not invalidate that snapshot.

When the rules change, deploy the file as a new version and restart or roll out Traefik. For Kubernetes deployments, content-versioned ConfigMaps are recommended. For example, Kustomize's default ConfigMap generator behavior adds a content hash to generated ConfigMap names. A change to `redirects.json` then produces a new ConfigMap reference and a Traefik rollout.

The plugin does not watch files or call Kubernetes APIs. New file contents become active only in a new Traefik process with an empty plugin cache.

# Static configuration

```yaml
experimental:
  plugins:
    bulkRedirects:
      moduleName: github.com/doodlescheduling/traefik-bulk-redirects
      version: v0.0.1
```
