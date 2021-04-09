package ginopenapivalidator_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ginopenapivalidator "github.com/phumberdroz/gin-openapi-validator/pkg/gin-openapi-validator"
)

var hook *test.Hook
var r *gin.Engine

//go:embed "petstore.yaml"
var s []byte

func TestMain(m *testing.M) {
	setupRouter()
	hook = test.NewGlobal()
	code := m.Run()
	os.Exit(code)
}

func setupRouter() {
	r = gin.Default()
	r.Use(ginopenapivalidator.Validator(s))
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
}

func request(request *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request)
	return w
}

func TestGetStatusOk(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/pets", bytes.NewBuffer([]byte(`{"name": "string","tag": "string"}`)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp := request(req)
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestPostStatusOk(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/pets", bytes.NewBuffer([]byte(`{"name": "string","tag": "string"}`)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp := request(req)
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestStatusOkButWrongResponse(t *testing.T) {
	defer hook.Reset()
	req, err := http.NewRequest(http.MethodGet, "/pets/1", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp := request(req)
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Len(t, hook.Entries, 1)
	assert.Equal(t, logrus.ErrorLevel, hook.LastEntry().Level)
	assert.Equal(t, "could not validate response payload", hook.LastEntry().Message)
}

func TestStatusOkUsersUuid(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/users?userId=bc1a80b7-6e76-4985-be3d-cbf8f8e79a2f", nil)
	assert.NoError(t, err)
	resp := request(req)
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestBadRequests(t *testing.T) {
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
			name:                  "NotFound: unknow route",
			method:                http.MethodGet,
			url:                   "/a/route/that/will/never/exist",
			body:                  "",
			setContentType:        false,
			expectedStatusCode:    http.StatusNotFound,
			responseShouldContain: "no matching operation was found",
		}, {
			name:                  "NotFound: unknow route",
			method:                http.MethodGet,
			url:                   "/pets/notAnInt",
			body:                  "",
			setContentType:        false,
			expectedStatusCode:    http.StatusNotFound,
			responseShouldContain: `Resource not found with 'id' value: notAnInt`,
		}, {
			name:                  "ValidationError",
			method:                http.MethodGet,
			url:                   "/pets?limit=TEST",
			body:                  "",
			setContentType:        false,
			expectedStatusCode:    http.StatusBadRequest,
			responseShouldContain: "Parameter 'limit' in query is invalid: TEST is an invalid integer",
		}, {
			name:                  "ParseError: Not JSON with ContentType",
			method:                http.MethodPost,
			url:                   "/pets",
			body:                  "not json",
			setContentType:        true,
			expectedStatusCode:    http.StatusBadRequest,
			responseShouldContain: "Could not parse request body",
		}, {
			name:                  "ValidationError: Wrong Body age should be int instead of string",
			method:                http.MethodPost,
			url:                   "/pets",
			body:                  `{"name": "string","tag": "string", "age": "I am a string"}`,
			setContentType:        true,
			expectedStatusCode:    http.StatusUnprocessableEntity,
			responseShouldContain: "Field must be set to integer or not be present See /age",
		}, {
			name:                  "ValidationError: Wrong Body missing required field",
			method:                http.MethodPost,
			url:                   "/pets",
			body:                  `{"test": "string", "tag": "string"}`,
			setContentType:        true,
			expectedStatusCode:    http.StatusUnprocessableEntity,
			responseShouldContain: `{"error":"property \"name\" is missing See /name"}`,
		}, {
			name:                  "ValidationError: missing body",
			method:                http.MethodPost,
			url:                   "/pets",
			body:                  "",
			setContentType:        true,
			expectedStatusCode:    http.StatusBadRequest,
			responseShouldContain: `request body has an error: value is required but missing`,
		},
	}
	for _, tc := range tests {
		testCase := tc
		t.Run(testCase.name, func(t *testing.T) {
			hook.Reset()
			req, err := http.NewRequest(testCase.method, testCase.url, bytes.NewBuffer([]byte(testCase.body)))
			assert.NoError(t, err)
			if testCase.setContentType {
				req.Header.Set("Content-Type", "application/json")
			}
			resp := request(req)
			assert.Equal(t, testCase.expectedStatusCode, resp.Code)
			assert.Contains(t, resp.Body.String(), testCase.responseShouldContain)
			var js json.RawMessage
			assert.NoError(t, json.Unmarshal(resp.Body.Bytes(), &js))
			assert.Len(t, hook.Entries, 0)
		})
	}
}
