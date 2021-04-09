package ginopenapivalidator

import (
	"bytes"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

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
}

// Validator returns a OpenAPI Validator middleware. It takes as argument doc where
// this is meant to be yaml byte array
func Validator(doc []byte, _ ...ValidatorOptions) gin.HandlerFunc {
	openapi3.DefineStringFormat("uuid", openapi3.FormatOfStringForUUIDOfRFC4122)

	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromData(doc)
	if err != nil {
		panic("failed to setup swagger middleware")
	}

	router, err := legacy.NewRouter(swagger)
	if err != nil {
		panic(err)
	}
	return func(c *gin.Context) {
		// Find route
		route, pathParams, err := router.FindRoute(c.Request)
		if err != nil {
			decodedValidationError, errDecode := Decode(err)
			if errDecode != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			} else {
				c.AbortWithStatusJSON(decodedValidationError.Status, gin.H{"error": decodedValidationError.Title})
			}
			return
		}
		requestValidationInput := &openapi3filter.RequestValidationInput{
			Request:    c.Request,
			PathParams: pathParams,
			Route:      route,
		}
		err = openapi3filter.ValidateRequest(c.Request.Context(), requestValidationInput)
		w := &responseBodyWriter{body: &bytes.Buffer{}, ResponseWriter: c.Writer}
		c.Writer = w
		if err != nil {
			decodedValidationError, errDecode := Decode(err)
			if errDecode != nil && decodedValidationError != nil && decodedValidationError.Status != 0 && decodedValidationError.Title != "" {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			} else {
				c.AbortWithStatusJSON(decodedValidationError.Status, gin.H{"error": decodedValidationError.Title})
			}
			return
		}
		c.Next()
		responseValidationInput := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: requestValidationInput,
			Status:                 c.Writer.Status(),
			Header: http.Header{
				"Content-Type": []string{
					c.Writer.Header().Get("Content-Type"),
				},
			},
		}
		if w.body.String() != "" {
			responseValidationInput.SetBodyBytes(w.body.Bytes())
		}
		// Validate response.
		if err := openapi3filter.ValidateResponse(c.Request.Context(), responseValidationInput); err != nil {
			log.WithError(err).Error("could not validate response payload")
		}
	}
}
