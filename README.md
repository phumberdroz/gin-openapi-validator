# gin-openapi-validator

OpenAPI request/response validation middleware for the Gin web framework.

This package plugs into Gin and validates incoming requests and outgoing responses against an OpenAPI 3 specification using `kin-openapi`. It is designed to be lightweight and easy to integrate in existing services.

## Features
- Validate requests (path params, query params, headers, and body).
- Validate responses and log validation errors.
- Optionally fail invalid responses in strict mode.
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

	"github.com/gin-gonic/gin"
	ginopenapivalidator "github.com/phumberdroz/gin-openapi-validator/pkg/gin-openapi-validator"
)

//go:embed api/openapi.yaml
var spec []byte

func main() {
	r := gin.Default()

	// Basic mode: logs response validation failures only.
	r.Use(ginopenapivalidator.Validator(spec))

	// Strict mode: invalid responses return 500.
	r.Use(ginopenapivalidator.Validator(spec, ginopenapivalidator.ValidatorOptions{
		StrictResponse: true,
	}))

	r.GET("/pets", func(c *gin.Context) {
		c.JSON(200, []gin.H{{"name": "string", "tag": "string", "id": 1}})
	})

	r.Run(":8080")
}
```

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
