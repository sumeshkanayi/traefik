# Compress

Compressing the Response before Sending it to the Client
{: .subtitle }

![Compress](../assets/img/middleware/compress.png)

The Compress middleware enables the gzip compression. 

## Configuration Examples

```yaml tab="Docker"
# Enable gzip compression
labels:
  - "traefik.http.middlewares.test-compress.compress=true"
```

```yaml tab="Kubernetes"
# Enable gzip compression
apiVersion: traefik.containo.us/v1alpha1
kind: Middleware
metadata:
  name: test-compress
spec:
  compress: {}
```

```yaml tab="Consul Catalog"
# Enable gzip compression
- "traefik.http.middlewares.test-compress.compress=true"
```

```json tab="Marathon"
"labels": {
  "traefik.http.middlewares.test-compress.compress": "true"
}
```

```yaml tab="Rancher"
# Enable gzip compression
labels:
  - "traefik.http.middlewares.test-compress.compress=true"
```

```toml tab="File (TOML)"
# Enable gzip compression
[http.middlewares]
  [http.middlewares.test-compress.compress]
```

```yaml tab="File (YAML)"
# Enable gzip compression
http:
  middlewares:
    test-compress:
      compress: {}
```

!!! info
    
    Responses are compressed when:
    
    * The response body is larger than `1400` bytes.
    * The `Accept-Encoding` request header contains `gzip`.
    * The response is not already compressed, i.e. the `Content-Encoding` response header is not already set.

## Configuration Options

### `excludedContentTypes`

`excludedContentTypes` specifies a list of content types to compare the `Content-Type` header of the incoming requests to before compressing.

The requests with content types defined in `excludedContentTypes` are not compressed.

Content types are compared in a case-insensitive, whitespace-ignored manner.

```yaml tab="Docker"
labels:
  - "traefik.http.middlewares.test-compress.compress.excludedcontenttypes=text/event-stream"
```

```yaml tab="Kubernetes"
apiVersion: traefik.containo.us/v1alpha1
kind: Middleware
metadata:
  name: test-compress
spec:
  compress:
    excludedContentTypes:
      - text/event-stream
```

```yaml tab="Consul Catalog"
- "traefik.http.middlewares.test-compress.compress.excludedcontenttypes=text/event-stream"
```

```json tab="Marathon"
"labels": {
  "traefik.http.middlewares.test-compress.compress.excludedcontenttypes": "text/event-stream"
}
```

```yaml tab="Rancher"
labels:
  - "traefik.http.middlewares.test-compress.compress.excludedcontenttypes=text/event-stream"
```

```toml tab="File (TOML)"
[http.middlewares]
  [http.middlewares.test-compress.compress]
    excludedContentTypes = ["text/event-stream"]
```

```yaml tab="File (YAML)"
http:
  middlewares:
    test-compress:
      compress:
        excludedContentTypes:
          - text/event-stream
```
