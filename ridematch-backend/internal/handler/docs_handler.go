package handler

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// openapiSpec is embedded into the binary at build time, so serving docs
// never depends on a file being present relative to the working directory
// at runtime (important once this is deployed behind Caddy/pm2/Docker).
//
//go:embed openapi_embed.json
var openapiSpec []byte

const swaggerUIPage = `<!DOCTYPE html>
<html>
<head>
  <title>RideMatch API Docs</title>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css" />
  <style>body { margin: 0; }</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = () => {
      window.ui = SwaggerUIBundle({
        url: '/docs/openapi.json',
        dom_id: '#swagger-ui',
        presets: [SwaggerUIBundle.presets.apis],
        layout: 'BaseLayout'
      });
    };
  </script>
</body>
</html>`

// DocsHandler serves the interactive API documentation.
type DocsHandler struct{}

// NewDocsHandler constructs a DocsHandler.
func NewDocsHandler() *DocsHandler {
	return &DocsHandler{}
}

// UI serves the Swagger UI HTML page at GET /docs.
func (h *DocsHandler) UI(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerUIPage))
}

// Spec serves the raw OpenAPI 3.0 JSON spec at GET /docs/openapi.json.
func (h *DocsHandler) Spec(c *gin.Context) {
	c.Data(http.StatusOK, "application/json", openapiSpec)
}
