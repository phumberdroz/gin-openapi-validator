package ginopenapivalidator_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ginopenapivalidator "github.com/phumberdroz/gin-openapi-validator/pkg/gin-openapi-validator"
)

//go:embed "petstore.yaml"
var spec []byte

func newRouter(opts ...ginopenapivalidator.ValidatorOptions) *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(ginopenapivalidator.Validator(spec, opts...))
	r.GET("/pets", func(c *gin.Context) {
		c.JSON(http.StatusOK, []gin.H{{"name": "string", "tag": "string", "id": 0}})
	})
	r.GET("/users", func(c *gin.Context) {
		c.JSON(http.StatusOK, []gin.H{{"uuid": "bc1a80b7-6e76-4985-be3d-cbf8f8e79a2f"}})
	})
	r.GET("/pets/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"no": "NO"})
	})
	r.POST("/pets", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"name": "string", "tag": "string", "id": 0})
	})

	return r
}

func performRequest(t *testing.T, router *gin.Engine, method, url, body string, setContentType bool) *httptest.ResponseRecorder {
	t.Helper()

	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	require.NoError(t, err)

	if setContentType {
		req.Header.Set("Content-Type", "application/json")
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	return recorder
}

func TestStatusOk(t *testing.T) {
	router := newRouter()

	resp := performRequest(t, router, http.MethodPost, "/pets", `{"name":"string","tag":"string"}`, true)
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestStatusOkUsersUUID(t *testing.T) {
	router := newRouter()

	resp := performRequest(t, router, http.MethodGet, "/users?userId=bc1a80b7-6e76-4985-be3d-cbf8f8e79a2f", "", false)
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestBadRequests(t *testing.T) {
	router := newRouter()

	tests := []struct {
		name                  string
		method                string
		url                   string
		body                  string
		setContentType        bool
		expectedStatusCode    int
		responseShouldContain string
	}{
		{
			name:                  "NotFound unknown route",
			method:                http.MethodGet,
			url:                   "/a/route/that/will/never/exist",
			expectedStatusCode:    http.StatusNotFound,
			responseShouldContain: "no matching operation was found",
		},
		{
			name:                  "NotFound invalid path parameter",
			method:                http.MethodGet,
			url:                   "/pets/notAnInt",
			expectedStatusCode:    http.StatusNotFound,
			responseShouldContain: `Resource not found with 'id' value: notAnInt`,
		},
		{
			name:                  "ValidationError query parameter",
			method:                http.MethodGet,
			url:                   "/pets?limit=TEST",
			expectedStatusCode:    http.StatusBadRequest,
			responseShouldContain: "Parameter 'limit' in query is invalid: TEST is an invalid integer",
		},
		{
			name:                  "ParseError invalid JSON body",
			method:                http.MethodPost,
			url:                   "/pets",
			body:                  "not json",
			setContentType:        true,
			expectedStatusCode:    http.StatusBadRequest,
			responseShouldContain: "Could not parse request body",
		},
		{
			name:                  "ValidationError invalid body type",
			method:                http.MethodPost,
			url:                   "/pets",
			body:                  `{"name":"string","tag":"string","age":"I am a string"}`,
			setContentType:        true,
			expectedStatusCode:    http.StatusUnprocessableEntity,
			responseShouldContain: "Field must be set to integer or not be present See /age",
		},
		{
			name:                  "ValidationError missing required field",
			method:                http.MethodPost,
			url:                   "/pets",
			body:                  `{"test":"string","tag":"string"}`,
			setContentType:        true,
			expectedStatusCode:    http.StatusUnprocessableEntity,
			responseShouldContain: `{"error":"property \"name\" is missing See /name"}`,
		},
		{
			name:                  "ValidationError missing body",
			method:                http.MethodPost,
			url:                   "/pets",
			setContentType:        true,
			expectedStatusCode:    http.StatusBadRequest,
			responseShouldContain: `request body has an error: value is required but missing`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := performRequest(t, router, tc.method, tc.url, tc.body, tc.setContentType)
			assert.Equal(t, tc.expectedStatusCode, resp.Code)
			assert.Contains(t, resp.Body.String(), tc.responseShouldContain)

			var js json.RawMessage
			assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), &js))
		})
	}
}

func TestResponseValidationLogsWithSlogAndPreservesResponse(t *testing.T) {
	var logOutput bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logOutput, nil))
	router := newRouter(ginopenapivalidator.ValidatorOptions{Logger: logger})

	resp := performRequest(t, router, http.MethodGet, "/pets/1", "", true)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.JSONEq(t, `{"no":"NO"}`, resp.Body.String())
	assert.Contains(t, logOutput.String(), "response payload violates OpenAPI contract")
	assert.Contains(t, logOutput.String(), "status=200")
}

func TestStrictResponseReturnsInternalServerError(t *testing.T) {
	router := newRouter(ginopenapivalidator.ValidatorOptions{StrictResponse: true})

	resp := performRequest(t, router, http.MethodGet, "/pets/1", "", true)

	assert.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.JSONEq(t, `{"detail":"Response body does not conform to the OpenAPI specification","error":"Internal Server Error"}`, resp.Body.String())
}

func TestCustomRequestErrorHandlerHandlesRouteErrors(t *testing.T) {
	var contractErr *ginopenapivalidator.ContractError

	router := newRouter(ginopenapivalidator.ValidatorOptions{
		RequestErrorHandler: func(c *gin.Context, err error) {
			require.ErrorAs(t, err, &contractErr)
			c.AbortWithStatusJSON(http.StatusTeapot, gin.H{"error": err.Error()})
		},
	})

	resp := performRequest(t, router, http.MethodGet, "/does-not-exist", "", false)

	assert.Equal(t, http.StatusTeapot, resp.Code)
	assert.Contains(t, resp.Body.String(), "no matching operation was found")
	require.NotNil(t, contractErr)
	assert.Equal(t, ginopenapivalidator.ValidationPhaseRequest, contractErr.Phase)
	assert.Equal(t, ginopenapivalidator.ValidationKindRoute, contractErr.Kind)
	assert.Equal(t, http.StatusNotFound, contractErr.Status)
}

func TestCustomRequestErrorHandlerHandlesValidationErrors(t *testing.T) {
	var handledErr error

	var contractErr *ginopenapivalidator.ContractError

	router := newRouter(ginopenapivalidator.ValidatorOptions{
		RequestErrorHandler: func(c *gin.Context, err error) {
			handledErr = err
			require.ErrorAs(t, err, &contractErr)

			c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": "custom request handler"})
		},
	})

	resp := performRequest(t, router, http.MethodPost, "/pets", "not json", true)

	require.Error(t, handledErr)
	require.NotNil(t, contractErr)
	assert.Equal(t, ginopenapivalidator.ValidationPhaseRequest, contractErr.Phase)
	assert.Equal(t, ginopenapivalidator.ValidationKindParse, contractErr.Kind)
	assert.Equal(t, "Could not parse request body", contractErr.Title)
	assert.Equal(t, http.StatusBadGateway, resp.Code)
	assert.JSONEq(t, `{"error":"custom request handler"}`, resp.Body.String())
}

func TestCustomResponseErrorHandlerIsInvoked(t *testing.T) {
	var handledErr error

	var contractErr *ginopenapivalidator.ContractError

	router := newRouter(ginopenapivalidator.ValidatorOptions{
		ResponseErrorHandler: func(c *gin.Context, err error) {
			handledErr = err
			require.ErrorAs(t, err, &contractErr)
		},
	})

	resp := performRequest(t, router, http.MethodGet, "/pets/1", "", true)

	require.Error(t, handledErr)
	require.NotNil(t, contractErr)
	assert.Equal(t, ginopenapivalidator.ValidationPhaseResponse, contractErr.Phase)
	assert.Equal(t, ginopenapivalidator.ValidationKindSchema, contractErr.Kind)
	require.NotNil(t, contractErr.ResponseError)
	assert.Equal(t, http.StatusInternalServerError, contractErr.Status)
	assert.NotEmpty(t, contractErr.Detail)
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.JSONEq(t, `{"no":"NO"}`, resp.Body.String())
}

func TestCustomResponseErrorHandlerCanReplaceResponse(t *testing.T) {
	router := newRouter(ginopenapivalidator.ValidatorOptions{
		ResponseErrorHandler: func(c *gin.Context, err error) {
			c.AbortWithStatusJSON(http.StatusTeapot, gin.H{
				"error":  "custom response handler",
				"detail": err.Error(),
			})
		},
	})

	resp := performRequest(t, router, http.MethodGet, "/pets/1", "", true)

	assert.Equal(t, http.StatusTeapot, resp.Code)
	assert.Contains(t, resp.Body.String(), "custom response handler")
	assert.NotContains(t, resp.Body.String(), `"no":"NO"`)
}

func TestStrictResponseFallsBackToDefaultWhenCustomResponseHandlerDoesNothing(t *testing.T) {
	router := newRouter(ginopenapivalidator.ValidatorOptions{
		StrictResponse: true,
		ResponseErrorHandler: func(c *gin.Context, err error) {
			_ = err
		},
	})

	resp := performRequest(t, router, http.MethodGet, "/pets/1", "", true)

	assert.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.JSONEq(t, `{"detail":"Response body does not conform to the OpenAPI specification","error":"Internal Server Error"}`, resp.Body.String())
}

func TestNilLoggerDoesNotLogOrPanic(t *testing.T) {
	router := newRouter(ginopenapivalidator.ValidatorOptions{})

	resp := performRequest(t, router, http.MethodGet, "/pets/1", "", true)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.True(t, strings.Contains(resp.Body.String(), `"no":"NO"`))
}
