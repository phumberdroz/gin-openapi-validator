# gin-openapi-validator

## Usage

```go
import "github.com/phumberdroz/gin-openapi-validator"

// Basic (logs violations only)
r.Use(ginopenapivalidator.Validator(openapiYAMLBytes))

// Strict mode: return 500 when response is invalid
r.Use(ginopenapivalidator.Validator(openapiYAMLBytes, ginopenapivalidator.ValidatorOptions{
    StrictResponse: true,
}))
```
