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
	body       *bytes.Buffer
	statusCode int
	headers    http.Header
	strict     bool
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if err == nil {
		w.body.Write(b[:n])
	}
	return n, err
}

func (w *responseBodyWriter) WriteHeader(code int) {
	w.statusCode = code
	if !w.strict {
		w.ResponseWriter.WriteHeader(code)
	}
}

// Header returns the captured headers
func (w *responseBodyWriter) Header() http.Header {
	return w.headers
}

// flush writes the buffered response to the real writer
func (w *responseBodyWriter) flush() {
	for k, vv := range w.headers {
		for _, v := range vv {
			w.ResponseWriter.Header().Add(k, v)
		}
	}
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(w.statusCode)
	// Flush body
	w.ResponseWriter.Write(w.body.Bytes())
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
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
			headers:        make(http.Header),
			strict:         options.StrictResponse,
			statusCode:     http.StatusOK,
		}
		// Copy original headers to our buffer
		for k, vv := range c.Writer.Header() {
			for _, v := range vv {
				w.headers.Add(k, v)
			}
		}

		c.Writer = w

		c.Next()

		responseValidationInput := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: requestValidationInput,
			Status:                 w.statusCode,
			Header:                 w.headers,
		}

		if w.body.Len() > 0 {
			responseValidationInput.SetBodyBytes(w.body.Bytes())
		}

		err = openapi3filter.ValidateResponse(c.Request.Context(), responseValidationInput)
		if err != nil {
			log.WithError(err).Error("response payload violates OpenAPI contract")

			if w.strict {
				// Strict mode
				c.Writer.Header().Set("Content-Type", "application/json")
				c.Writer.WriteHeader(http.StatusInternalServerError)
				c.Writer.Write([]byte(`{"error":"Internal Server Error","detail":"Response body does not conform to the OpenAPI specification"}`))
				return
			}
			// Non-strict
		}

		w.flush()
	}
}
