# gin-openapi-validator

OpenAPI request/response validation middleware for the Gin web framework.

This package plugs into Gin and validates incoming requests and outgoing responses against an OpenAPI 3 specification using `kin-openapi`. It is designed to be lightweight and easy to integrate in existing services.

## Features
- Validate requests (path params, query params, headers, and body).
- Validate responses and opt into structured logging for validation errors.
- Optionally fail invalid responses in strict mode.
- Customize request and response validation failure handling.
- Supports custom string formats (for example UUID RFC 4122).
- Simple middleware API for Gin.

## Install
```bash
go get github.com/phumberdroz/gin-openapi-validator
```

## Usage
```go
package main

import (
	_ "embed"
	"log/slog"

	"github.com/gin-gonic/gin"
	ginopenapivalidator "github.com/phumberdroz/gin-openapi-validator/pkg/gin-openapi-validator"
)

//go:embed api/openapi.yaml
var spec []byte

func main() {
	r := gin.Default()

	// Pick one configuration per router or route group.

	// Basic mode.
	r.Use(ginopenapivalidator.Validator(spec))

	// Or strict mode:
	// r.Use(ginopenapivalidator.Validator(spec, ginopenapivalidator.ValidatorOptions{
	// 	StrictResponse: true,
	// }))

	// Or custom mode:
	// r.Use(ginopenapivalidator.Validator(spec, ginopenapivalidator.ValidatorOptions{
	// 	Logger: slog.Default(),
	// 	RequestErrorHandler: func(c *gin.Context, err error) {
	// 		c.AbortWithStatusJSON(400, gin.H{"error": err.Error()})
	// 	},
	// 	ResponseErrorHandler: func(c *gin.Context, err error) {
	// 		c.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
	// 	},
	// }))

	r.GET("/pets", func(c *gin.Context) {
		c.JSON(200, []gin.H{{"name": "string", "tag": "string", "id": 1}})
	})

	r.Run(":8080")
}
```

## Response Rewrite Limit
`ResponseErrorHandler` can replace an invalid response only before any bytes have been flushed to the client.

Once a handler calls `c.Writer.Flush()`, the response is considered committed. At that point the validator can still detect and optionally log the validation failure, but it will pass the committed response through instead of attempting to rewrite it.

## Project Structure
- `pkg/gin-openapi-validator/`: library source and tests
- `pkg/gin-openapi-validator/petstore.yaml`: test OpenAPI fixture

## Testing
```bash
go test ./...
```

## Versioning
This project does not currently publish releases. Consumers should vendor or pin the module version in `go.mod`.

## Contributing
See `AGENTS.md` for contributor guidelines and local development tips.

## License
See `LICENSE` for details.
