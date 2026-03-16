package ginopenapivalidator

import (
	"bytes"
	"log/slog"
	"net/http"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/gin-gonic/gin"
)

// responseBodyWriter captures the response body.
type responseBodyWriter struct {
	gin.ResponseWriter
	body           *bytes.Buffer
	validationBody *bytes.Buffer
	statusCode     int
	headers        http.Header
	wroteHeader    bool
	flushed        bool
	rewriteBody    bool
}

func (w *responseBodyWriter) prepareRewrite() {
	if !w.rewriteBody || w.flushed {
		return
	}

	w.body.Reset()
	w.validationBody.Reset()
	w.headers.Del("Content-Length")
	w.rewriteBody = false
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	w.prepareRewrite()

	if !w.wroteHeader {
		w.WriteHeaderNow()
	}

	n, err := w.body.Write(b)
	if err != nil {
		return n, err
	}

	_, _ = w.validationBody.Write(b)

	return n, nil
}

func (w *responseBodyWriter) WriteString(s string) (int, error) {
	w.prepareRewrite()

	if !w.wroteHeader {
		w.WriteHeaderNow()
	}

	n, err := w.body.WriteString(s)
	if err != nil {
		return n, err
	}

	_, _ = w.validationBody.WriteString(s)

	return n, nil
}

func (w *responseBodyWriter) WriteHeader(code int) {
	w.prepareRewrite()

	w.statusCode = code
	w.wroteHeader = true
}

func (w *responseBodyWriter) Header() http.Header {
	return w.headers
}

func (w *responseBodyWriter) WriteHeaderNow() {
	if w.wroteHeader {
		return
	}

	w.wroteHeader = true
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
}

func (w *responseBodyWriter) Status() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}

	return w.statusCode
}

func (w *responseBodyWriter) Size() int {
	return w.body.Len()
}

func (w *responseBodyWriter) Written() bool {
	return w.wroteHeader || w.body.Len() > 0
}

func (w *responseBodyWriter) committed() bool {
	return w.flushed
}

func (w *responseBodyWriter) Flush() {
	w.flush()
	w.body.Reset()

	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseBodyWriter) flush() {
	if !w.flushed {
		for k, vv := range w.headers {
			w.ResponseWriter.Header()[k] = append([]string(nil), vv...)
		}

		if w.statusCode == 0 {
			w.statusCode = http.StatusOK
		}

		w.ResponseWriter.WriteHeader(w.statusCode)
		w.flushed = true
	}

	_, _ = w.ResponseWriter.Write(w.body.Bytes())
}

func newResponseBodyWriter(writer gin.ResponseWriter) *responseBodyWriter {
	return &responseBodyWriter{
		ResponseWriter: writer,
		body:           &bytes.Buffer{},
		validationBody: &bytes.Buffer{},
		headers:        cloneHeader(writer.Header()),
		statusCode:     http.StatusOK,
	}
}

func cloneResponseBodyWriter(writer gin.ResponseWriter, source *responseBodyWriter) *responseBodyWriter {
	cloned := newResponseBodyWriter(writer)

	cloned.statusCode = source.statusCode
	cloned.wroteHeader = source.wroteHeader
	cloned.flushed = false
	cloned.rewriteBody = true

	if source.body.Len() > 0 {
		_, _ = cloned.body.Write(source.body.Bytes())
	}

	if source.validationBody.Len() > 0 {
		_, _ = cloned.validationBody.Write(source.validationBody.Bytes())
	}

	cloned.headers = cloneHeader(source.headers)

	return cloned
}

type responseWriterSnapshot struct {
	statusCode  int
	wroteHeader bool
	headers     http.Header
	body        string
}

func snapshotResponseBodyWriter(writer *responseBodyWriter) responseWriterSnapshot {
	return responseWriterSnapshot{
		statusCode:  writer.statusCode,
		wroteHeader: writer.wroteHeader,
		headers:     cloneHeader(writer.headers),
		body:        writer.body.String(),
	}
}

func responseBodyWriterChanged(before responseWriterSnapshot, after *responseBodyWriter) bool {
	if before.statusCode != after.statusCode || before.wroteHeader != after.wroteHeader || before.body != after.body.String() {
		return true
	}

	return !headersEqual(before.headers, after.headers)
}

func headersEqual(left, right http.Header) bool {
	if len(left) != len(right) {
		return false
	}

	for key, leftValues := range left {
		rightValues, ok := right[key]
		if !ok || len(leftValues) != len(rightValues) {
			return false
		}

		for i, value := range leftValues {
			if rightValues[i] != value {
				return false
			}
		}
	}

	return true
}

func cloneHeader(header http.Header) http.Header {
	cloned := make(http.Header, len(header))
	for k, vv := range header {
		cloned[k] = append([]string(nil), vv...)
	}

	return cloned
}

type ValidatorOptions struct {
	// If true, the middleware returns HTTP 500 when the response body
	// violates the OpenAPI specifications before any bytes have been
	// flushed to the client.
	StrictResponse bool
	// Logger receives non-strict response validation failures when provided.
	Logger *slog.Logger
	// RequestErrorHandler handles request validation failures when provided.
	RequestErrorHandler func(*gin.Context, error)
	// ResponseErrorHandler handles response validation failures when provided.
	// It can replace the response only before any bytes have been flushed.
	ResponseErrorHandler func(*gin.Context, error)
}

// Validator returns an OpenAPI validation middleware for Gin.
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

	requestErrorHandler := defaultRequestErrorHandler
	if options.RequestErrorHandler != nil {
		requestErrorHandler = options.RequestErrorHandler
	}

	return validatorHandler(router, options, requestErrorHandler)
}

func validatorHandler(router routers.Router, options ValidatorOptions, requestErrorHandler func(*gin.Context, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestValidationInput, err := validateIncomingRequest(c, router)
		if err != nil {
			requestErrorHandler(c, newContractError(ValidationPhaseRequest, err))

			if !c.IsAborted() {
				c.Abort()
			}

			return
		}

		originalWriter := c.Writer
		w := newResponseBodyWriter(originalWriter)

		c.Writer = w
		c.Next()

		if err = validateOutgoingResponse(c, requestValidationInput, w); err != nil {
			handleResponseValidationError(c, originalWriter, w, options, newContractError(ValidationPhaseResponse, err))
			return
		}

		c.Writer = originalWriter

		w.flush()
	}
}

func validateIncomingRequest(c *gin.Context, router routers.Router) (*openapi3filter.RequestValidationInput, error) {
	route, pathParams, err := router.FindRoute(c.Request)
	if err != nil {
		return nil, err
	}

	requestValidationInput := &openapi3filter.RequestValidationInput{
		Request:    c.Request,
		PathParams: pathParams,
		Route:      route,
	}

	if err = openapi3filter.ValidateRequest(c.Request.Context(), requestValidationInput); err != nil {
		return nil, err
	}

	return requestValidationInput, nil
}

func validateOutgoingResponse(c *gin.Context, requestValidationInput *openapi3filter.RequestValidationInput, writer *responseBodyWriter) error {
	responseValidationInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: requestValidationInput,
		Status:                 writer.Status(),
		Header:                 writer.headers,
	}

	if writer.validationBody.Len() > 0 {
		responseValidationInput.SetBodyBytes(writer.validationBody.Bytes())
	}

	return openapi3filter.ValidateResponse(c.Request.Context(), responseValidationInput)
}

func handleResponseValidationError(c *gin.Context, originalWriter gin.ResponseWriter, capturedWriter *responseBodyWriter, options ValidatorOptions, err error) {
	if capturedWriter.committed() {
		logResponseValidationError(c, options.Logger, err)

		c.Writer = originalWriter
		capturedWriter.flush()

		return
	}

	if options.ResponseErrorHandler != nil {
		handlerWriter := cloneResponseBodyWriter(originalWriter, capturedWriter)
		before := snapshotResponseBodyWriter(handlerWriter)

		c.Writer = handlerWriter
		options.ResponseErrorHandler(c, err)

		if responseBodyWriterChanged(before, handlerWriter) {
			handlerWriter.flush()
			return
		}
	}

	if options.StrictResponse {
		c.Writer = newResponseBodyWriter(originalWriter)
		defaultResponseErrorHandler(options)(c, err)
		c.Writer.(*responseBodyWriter).flush()

		return
	}

	defaultResponseErrorHandler(options)(c, err)

	c.Writer = originalWriter

	capturedWriter.flush()
}

func defaultRequestErrorHandler(c *gin.Context, err error) {
	decodedValidationError, decodeErr := Decode(err)
	if decodeErr != nil || decodedValidationError == nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})

		return
	}

	c.AbortWithStatusJSON(decodedValidationError.Status, gin.H{
		"error": decodedValidationError.Title,
	})
}

func defaultResponseErrorHandler(options ValidatorOptions) func(*gin.Context, error) {
	return func(c *gin.Context, err error) {
		if !options.StrictResponse {
			logResponseValidationError(c, options.Logger, err)

			return
		}

		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error":  "Internal Server Error",
			"detail": "Response body does not conform to the OpenAPI specification",
		})
	}
}

func logResponseValidationError(c *gin.Context, logger *slog.Logger, err error) {
	if logger == nil {
		return
	}

	attrs := []any{"error", err}
	if responseWriter, ok := c.Writer.(*responseBodyWriter); ok {
		attrs = append(attrs,
			"status", responseWriter.Status(),
			"headers", headerPairs(responseWriter.headers),
		)
	}

	logger.ErrorContext(c.Request.Context(), "response payload violates OpenAPI contract", attrs...)
}

func headerPairs(header http.Header) []string {
	pairs := make([]string, 0, len(header))
	for key, values := range header {
		for _, value := range values {
			pairs = append(pairs, key+": "+value)
		}
	}

	sort.Strings(pairs)

	return pairs
}
