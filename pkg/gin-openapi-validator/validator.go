package ginopenapivalidator

import (
	"bytes"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// responseBodyWriter captures the response body
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// ValidatorOptions currently not used but we may use it in the future to add options.
type ValidatorOptions struct {
	// If true, the middleware returns HTTP 500 when the response body
	// violates the OpenAPI specifications.
	StrictResponse bool
}

// Validator returns a OpenAPI Validator middleware. It takes as argument doc where
// this is meant to be yaml byte array
func Validator(doc []byte, opts ...ValidatorOptions) gin.HandlerFunc {
	var options ValidatorOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	openapi3.DefineStringFormatValidator("uuid", openapi3.NewRegexpFormatValidator(openapi3.FormatOfStringForUUIDOfRFC4122))

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	swagger, err := loader.LoadFromData(doc)
	if err != nil {
		panic("failed to load OpenAPI document: " + err.Error())
	}

	router, err := gorillamux.NewRouter(swagger)
	if err != nil {
		panic("failed to create router: " + err.Error())
	}

	return func(c *gin.Context) {
		// Find route
		route, pathParams, err := router.FindRoute(c.Request)
		if err != nil {
			// Handle route not found / method not allowed
			ve, decodeErr := Decode(err)
			if decodeErr != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "internal server error",
				})
			} else {
				c.AbortWithStatusJSON(ve.Status, gin.H{
					"error": ve.Title,
				})
			}
			return
		}

		// Validate request
		requestValidationInput := &openapi3filter.RequestValidationInput{
			Request:    c.Request,
			PathParams: pathParams,
			Route:      route,
		}
		if err = openapi3filter.ValidateRequest(c.Request.Context(), requestValidationInput); err != nil {
			ve, decodeErr := Decode(err)
			if decodeErr != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "internal server error",
				})
			} else {
				c.AbortWithStatusJSON(ve.Status, gin.H{
					"error": ve.Title,
				})
			}
			return
		}

		w := &responseBodyWriter{
			body:           &bytes.Buffer{},
			ResponseWriter: c.Writer,
		}
		c.Writer = w

		// Execute next handlers
		c.Next()

		responseValidationInput := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: requestValidationInput,
			Status:                 c.Writer.Status(),
			Header:                 c.Writer.Header(),
		}

		if w.body.Len() > 0 {
			responseValidationInput.SetBodyBytes(w.body.Bytes())
		}

		if err := openapi3filter.ValidateResponse(c.Request.Context(), responseValidationInput); err != nil {
			log.WithError(err).Error("could not validate response payload")

			if options.StrictResponse {
				// Strict mode
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":  "Internal Server Error",
					"detail": "Response body does not conform to the OpenAPI specification",
				})
				return
			}
		}
	}
}
